package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Options supplies the pieces of the compiler that depend on the database
// rather than on the language.
//
// They are injected rather than imported so this package stays free of a
// dependency on store — store imports query, and the reverse would be a cycle.
// The practical benefit is that folder membership has exactly one definition
// (store.folderClause) instead of a copy here that could drift from it.
type Options struct {
	// FolderSQL returns the predicate for a named folder view over alias `m`.
	FolderSQL func(folder string) (string, bool)
	// ThreadIDs resolves a message id to every message in its thread.
	//
	// Resolved in Go rather than compiled to a recursive CTE because the CTE
	// would have to sit at statement level, which stops `thread:` from
	// composing inside OR and NOT like every other term.
	ThreadIDs func(messageID string) ([]string, error)
	// SavedQuery resolves a saved query's name to its text.
	SavedQuery func(name string) (string, bool, error)
	// SelfAddresses returns the addresses belonging to configured accounts,
	// which is what me() means.
	SelfAddresses func() ([]string, error)
	// TimeField gives the JSON path holding an extraction's own timestamp.
	//
	// A weekly digest sent on Monday reports the previous week: charting it by
	// the message's date shifts every point. Annotators declare which
	// extracted field is the real time axis, and this reads that declaration.
	TimeField func(annotator string) string
	// KnownAnnotator reports whether an annotator exists, and is used to
	// reject a name that does not.
	//
	// Without it a typo is indistinguishable from a real negative: `label:invoces`
	// returns nothing, which reads as "no invoices" rather than "no such
	// annotator". Every other unknown name in this language is an error, and
	// this one has more reason to be than most, because annotator names are
	// user-defined and therefore easy to get wrong.
	KnownAnnotator func(name string) bool

	// FullText enables the FTS5 index for prose matching. When false the same
	// terms compile to substring matching, which is slower and token-blind but
	// returns a correct answer — see store.FullTextAvailable.
	FullText bool

	DefaultLimit int
	MaxLimit     int
	// Offset pages the message result. Aggregates ignore it: paging a
	// GROUP BY by row offset gives a different answer each time the
	// underlying data moves, which is worse than no paging.
	Offset int
}

func (o Options) defaultLimit() int {
	if o.DefaultLimit <= 0 {
		return 50
	}
	return o.DefaultLimit
}

func (o Options) maxLimit() int {
	if o.MaxLimit <= 0 {
		return 10000
	}
	return o.MaxLimit
}

// Compiled is a statement ready for database/sql.
type Compiled struct {
	SQL  string
	Args []any
	// Columns names the result shape: either message columns or the
	// label/value pair an aggregate produces.
	Aggregate bool
}

// CompileFilter turns a filter expression into a WHERE clause over alias `m`.
//
// The returned SQL is always parenthesised and never empty — "everything"
// compiles to "1=1" so callers can concatenate without special cases.
func CompileFilter(n Node, opt Options) (string, []any, error) {
	return CompileFilterFor(n, opt, EntityMessage)
}

// CompileFilterFor compiles a filter against a named row source.
func CompileFilterFor(n Node, opt Options, entity string) (string, []any, error) {
	c := &compiler{opt: opt, entity: entity}
	sql, err := c.node(n, false)
	if err != nil {
		return "", nil, err
	}
	return sql, c.args, nil
}

type compiler struct {
	opt  Options
	args []any
	// entity is the table this query is about. When it is a ticket query,
	// message predicates are wrapped in a test over the ticket's evidence.
	entity string
	// resolving names the saved query currently being inlined, and is what
	// stops one from referencing another.
	resolving string
}

// idColumn names the primary key of the table this query reads.
//
// Aliased, because the same word means a different column depending on what
// the query is about — which is the whole reason `id` is registered once per
// entity rather than once globally.
func (c *compiler) idColumn() string {
	switch c.entity {
	case EntityTicket:
		return "t.id"
	case EntityDraft:
		return "d.id"
	}
	return "m.id"
}

func (c *compiler) arg(v any) string {
	c.args = append(c.args, v)
	return "?"
}

// node compiles one node, pushing negation down to the terms.
//
// Negation is passed to compileTerm rather than wrapped around its output
// because one field defines its own complement: see the label case.
func (c *compiler) node(n Node, negated bool) (string, error) {
	switch t := n.(type) {
	case *All:
		if negated {
			return "1=0", nil
		}
		return "1=1", nil

	case *Not:
		return c.node(t.Node, !negated)

	case *And:
		// De Morgan: NOT (a AND b) is (NOT a) OR (NOT b).
		return c.join(t.Nodes, negated, " AND ", " OR ")

	case *Or:
		return c.join(t.Nodes, negated, " OR ", " AND ")

	case *Term:
		return c.term(t, negated)

	default:
		return "", fmt.Errorf("cannot compile %T", n)
	}
}

func (c *compiler) join(nodes []Node, negated bool, sep, negSep string) (string, error) {
	if len(nodes) == 0 {
		return "1=1", nil
	}
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		s, err := c.node(n, negated)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	if negated {
		sep = negSep
	}
	return "(" + strings.Join(parts, sep) + ")", nil
}

// wrap applies plain negation. Fields whose complement is not the simple
// inverse do not use it.
func wrap(sql string, negated bool) string {
	if negated {
		return "(NOT (" + sql + "))"
	}
	return "(" + sql + ")"
}

func (c *compiler) term(t *Term, negated bool) (string, error) {
	// A bare word has no field to look up; everything else is checked against
	// its declaration before any SQL is generated, so an unusable term is an
	// error with a suggestion rather than a query that runs and returns the
	// wrong rows.
	// `in:` selects the row source and contributes no predicate of its own —
	// Query.Entity has already read it.
	if t.Field == "in" {
		if _, ok := Entities[strings.ToLower(t.Value)]; !ok {
			return "", at(t, fmt.Errorf("in: %q is not a kind of thing (%s)",
				t.Value, strings.Join(EntityNames, ", ")))
		}
		if negated {
			return "", at(t, fmt.Errorf("in: cannot be negated; it says what the query is about"))
		}
		return "1=1", nil
	}

	if t.Field != "" {
		f, ok := LookupFieldIn(c.entity, t.Field)
		if !ok {
			return "", at(t, unknownFieldError(t.Field))
		}
		if err := f.validate(t); err != nil {
			return "", at(t, err)
		}
		// A field can be declared and still be unanswerable here: a draft has
		// no flags, no attachments and no thread, so `is:unread` in a drafts
		// query is a question about mail the draft is not.
		if c.entity == EntityDraft && f.Entity == EntityMessage && !DraftServes(t.Field) {
			return "", at(t, fmt.Errorf(
				"%s: drafts have no %s — it is a property of received mail", t.Field, t.Field))
		}
	}

	// In a ticket query, a predicate about mail is a question about the
	// ticket's evidence: "tickets whose mail came from Stripe". Wrapping here
	// rather than in every message case means each field is written once and
	// works in both worlds.
	//
	// The predicate is always compiled unnegated and the negation applied
	// outside it, because "no source message is from Stripe" is the question —
	// negating inside would ask whether *some* source is from someone else,
	// which is the same anti-join trap that `-to:` has over recipients.
	// Compiling once also matters mechanically: each dispatch appends its
	// arguments, so building both forms would bind twice as many values as the
	// statement has placeholders.
	if c.entity == EntityTicket && isMessageField(t.Field) {
		inner, err := c.dispatch(t, false)
		if err != nil {
			return "", at(t, err)
		}
		exists := "EXISTS (SELECT 1 FROM ticket_sources ts JOIN messages m ON m.id = ts.message_id " +
			"WHERE ts.ticket_id = t.id AND " + inner + ")"
		return wrap(exists, negated), nil
	}

	if c.entity == EntityDraft {
		sql, err := c.draftTerm(t, negated)
		return sql, at(t, err)
	}

	sql, err := c.dispatch(t, negated)
	if err != nil {
		return "", at(t, err)
	}
	return sql, nil
}

// draftTerm compiles against the drafts table.
//
// Drafts keep their recipients as JSON columns rather than participant rows —
// they have never been through the importer, and an unsent address is not a
// correspondent yet — so the address fields are substring tests over that JSON
// rather than the anti-join a message query uses. `-to:x` is still "no
// recipient is x", because the column holds the whole list.
func (c *compiler) draftTerm(t *Term, negated bool) (string, error) {
	switch t.Field {
	case "folder":
		// Already read by Query.Entity; it selects the source, it does not
		// filter within it.
		if strings.EqualFold(t.Value, "drafts") {
			return "1=1", nil
		}
		return "", fmt.Errorf("folder: a drafts query is not about mail folders")

	case "id":
		return wrap("d.id = "+c.arg(t.Value), negated), nil

	case "status", "origin":
		if t.Op == OpGlob {
			return wrap(c.stringPredicate("d."+t.Field, t), negated), nil
		}
		return wrap("d."+t.Field+" = "+c.arg(strings.ToLower(t.Value)), negated), nil

	case "to", "cc", "bcc":
		return wrap(c.stringPredicate("d."+t.Field+"_addrs", t), negated), nil

	case "subject":
		return wrap(c.stringPredicate("d.subject", t), negated), nil

	case "body":
		return wrap(c.stringPredicate("d.body", t), negated), nil

	case "", "text":
		return wrap("("+c.stringPredicate("d.subject", t)+" OR "+
			c.stringPredicate("d.body", t)+" OR "+
			c.stringPredicate("d.to_addrs", t)+")", negated), nil

	case "account":
		return wrap("d.account_id = "+c.arg(t.Value), negated), nil

	case "after", "before", "on":
		start, end, err := ParseDateValue(t.Value)
		if err != nil {
			return "", fmt.Errorf("%s: %w", t.Field, err)
		}
		switch t.Field {
		case "after":
			return wrap("d.created_at >= "+c.arg(start), negated), nil
		case "before":
			return wrap("d.created_at < "+c.arg(start), negated), nil
		default:
			return wrap("(d.created_at >= "+c.arg(start)+" AND d.created_at < "+c.arg(end)+")", negated), nil
		}
	}
	return "", fmt.Errorf("%s: not available for drafts", t.Field)
}

// isMessageField reports whether a field describes mail rather than a ticket.
func isMessageField(field string) bool {
	if field == "" {
		return true
	}
	f, ok := LookupField(field)
	return ok && f.Entity == EntityMessage
}

// at attaches a term's position to an error that has none.
func at(t *Term, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return &Error{Pos: t.Pos, Msg: err.Error()}
}

func (c *compiler) dispatch(t *Term, negated bool) (string, error) {
	switch t.Field {
	case "":
		return c.freeText(t, negated)
	case "text":
		return c.freeText(t, negated)
	case "subject":
		return c.textColumn(t, negated, "subject", "m.subject")
	case "body":
		return c.textColumn(t, negated, "body", "m.body")

	case "from", "to", "cc", "bcc", "anyone":
		return c.participant(t, negated)

	case "account":
		return wrap("m.account_id = "+c.arg(t.Value), negated), nil

	case "mailbox":
		return wrap(c.stringPredicate("m.mailbox", t), negated), nil

	case "folder":
		if c.opt.FolderSQL == nil {
			return "", fmt.Errorf("folder: is not available here")
		}
		sql, ok := c.opt.FolderSQL(strings.ToLower(t.Value))
		if !ok {
			return "", fmt.Errorf("folder: %q is not a folder", t.Value)
		}
		if sql == "" {
			// "all" constrains nothing, and its negation is nothing either.
			return "1=1", nil
		}
		return wrap(sql, negated), nil

	case "is":
		return c.isTerm(t, negated)

	case "has":
		return c.hasTerm(t, negated)

	case "after", "before", "on":
		return c.dateTerm(t, negated)

	case "larger", "smaller":
		return c.sizeTerm(t, negated)

	case "label":
		return c.labelTerm(t, negated)

	case "unlabeled":
		return c.unlabeledTerm(t, negated)

	case "conf":
		return c.confTerm(t, negated)

	case "extract":
		return c.extractTerm(t, negated)

	case "thread":
		return c.threadTerm(t, negated)

	case "id":
		// Exact, always. An identity has no partial match, and treating it as
		// a substring would make id:abc quietly select everything whose id
		// contains abc.
		return wrap(c.idColumn()+" = "+c.arg(t.Value), negated), nil

	case "saved":
		return c.savedTerm(t, negated)

	case "status", "raised":
		col := "t." + ticketColumn(t.Field)
		if t.Op == OpGlob {
			return wrap(c.stringPredicate(col, t), negated), nil
		}
		return wrap(col+" = "+c.arg(strings.ToLower(t.Value)), negated), nil

	case "priority":
		return wrap(c.stringPredicate("t.priority", t), negated), nil

	case "ticket":
		return wrap(c.stringPredicate("t.title", t), negated), nil

	case "due":
		// Forward-looking: "due:7d" is the next seven days, not the last.
		start, _, err := ParseFutureDate(t.Value)
		if err != nil {
			return "", fmt.Errorf("due: %w", err)
		}
		// Inclusive of the named day, and never matches a ticket with no due
		// date — "due this week" is a question about tickets that have one.
		return wrap("(t.due_at IS NOT NULL AND t.due_at < "+c.arg(start+dayMillis)+")", negated), nil

	default:
		return "", unknownFieldError(t.Field)
	}
}

// savedTerm inlines a saved query.
//
// Resolved at compile time rather than stored as a reference, so a saved query
// is a building block — `saved:invoices after:7d` composes with everything
// else, and a board column is just a query naming one.
//
// One level only. Allowing a saved query to reference another needs cycle
// detection and turns "what does this query mean" into a resolution step
// rather than something you can read.
func (c *compiler) savedTerm(t *Term, negated bool) (string, error) {
	if c.opt.SavedQuery == nil {
		return "", fmt.Errorf("saved: is not available here")
	}
	if c.resolving != "" {
		return "", fmt.Errorf("saved:%s is referenced from saved query %q, and saved queries cannot nest",
			t.Value, c.resolving)
	}

	text, ok, err := c.opt.SavedQuery(t.Value)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("saved: no saved query named %q (list them with `iql query saved`)", t.Value)
	}

	q, err := Parse(text)
	if err != nil {
		return "", fmt.Errorf("saved query %q does not parse: %w", t.Value, err)
	}
	if len(q.Stages) > 0 {
		return "", fmt.Errorf("saved query %q ends in a pipeline stage, so it selects groups rather than messages and cannot be used as a filter", t.Value)
	}

	c.resolving = t.Value
	sql, err := c.node(q.Filter, negated)
	c.resolving = ""
	return sql, err
}

// stringPredicate builds a comparison against a text column.
//
// Substring matching uses instr() rather than LIKE '%'||?||'%' so that a value
// containing % or _ means itself. Escaping LIKE wildcards correctly requires an
// ESCAPE clause and a rewrite of the value, and getting that wrong makes
// `from:100%` silently match everything.
func (c *compiler) stringPredicate(col string, t *Term) string {
	switch t.Op {
	case OpExact:
		return "LOWER(" + col + ") = LOWER(" + c.arg(t.Value) + ")"
	case OpGlob:
		// GLOB is case-sensitive and uses * and ? natively, which is exactly
		// the syntax people type. Both sides are lowered to keep it usable.
		return "LOWER(" + col + ") GLOB LOWER(" + c.arg(t.Value) + ")"
	default:
		return "instr(LOWER(" + col + "), LOWER(" + c.arg(t.Value) + ")) > 0"
	}
}

// participant compiles an address predicate as an existence test over the
// edge table.
//
// This is the shape that makes negation correct. `-to:alice` becomes "no row
// exists where a recipient is alice"; the same query written as a join with
// `address != 'alice'` would match any message that also had a second
// recipient, which is nearly all of them.
func (c *compiler) participant(t *Term, negated bool) (string, error) {
	var role string
	switch t.Field {
	case "from":
		role = "from"
	case "to":
		role = "to"
	case "cc":
		role = "cc"
	case "bcc":
		role = "bcc"
	case "anyone":
		role = ""
	}

	conds := []string{"p.message_id = m.id"}
	if role != "" {
		conds = append(conds, "p.role = "+c.arg(role))
	}

	if isSelfFunc(t.Value) {
		// me() is the addresses of the configured accounts. Deriving it means
		// "mail I sent" stops requiring you to know and type your own address,
		// which is the most common thing a person wants and the most annoying
		// to spell.
		if c.opt.SelfAddresses == nil {
			return "", fmt.Errorf("%s: me() is not available here", t.Field)
		}
		addrs, err := c.opt.SelfAddresses()
		if err != nil {
			return "", err
		}
		if len(addrs) == 0 {
			return "", fmt.Errorf("%s: me() has nothing to resolve to — no account has an address configured (`iql account add --email ...`)", t.Field)
		}
		placeholders := make([]string, len(addrs))
		for i, a := range addrs {
			placeholders[i] = c.arg(strings.ToLower(a))
		}
		conds = append(conds, "p.address IN ("+strings.Join(placeholders, ", ")+")")
	} else {
		conds = append(conds, c.stringPredicate("p.address", t))
	}

	sql := "EXISTS (SELECT 1 FROM message_participants p WHERE " + strings.Join(conds, " AND ") + ")"
	return wrap(sql, negated), nil
}

// isSelfFunc reports whether a value is the me() function.
func isSelfFunc(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "me()")
}

// freeTextColumns are what a bare word searches.
//
// normalized_body is included because it is where HTML-only mail keeps its
// readable text: body holds the plain part, which such a message does not
// have, so without it an imported newsletter is findable only by subject.
var freeTextColumns = []string{"m.subject", "m.body", "m.normalized_body", "m.from_addr"}

// freeText searches every indexed column.
func (c *compiler) freeText(t *Term, negated bool) (string, error) {
	if t.Op == OpGlob || t.Op == OpExact || !c.opt.FullText {
		parts := make([]string, 0, len(freeTextColumns))
		for _, col := range freeTextColumns {
			parts = append(parts, c.stringPredicate(col, t))
		}
		return wrap(strings.Join(parts, " OR "), negated), nil
	}
	return wrap(c.fts(ftsPhrase(t.Value)), negated), nil
}

// textColumn searches one FTS column, or falls back to the stored column for
// operators the index cannot answer and for builds that have no index.
func (c *compiler) textColumn(t *Term, negated bool, ftsCol, sqlCol string) (string, error) {
	if t.Op == OpGlob || t.Op == OpExact || !c.opt.FullText {
		return wrap(c.stringPredicate(sqlCol, t), negated), nil
	}
	return wrap(c.fts(ftsCol+":"+ftsPhrase(t.Value)), negated), nil
}

// fts builds a full-text membership test.
//
// A subquery on rowid rather than a join so it composes under OR and NOT: a
// joined FTS table cannot be negated without turning the whole statement
// inside out.
func (c *compiler) fts(match string) string {
	return "m.rowid IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH " + c.arg(match) + ")"
}

// ftsPhrase quotes a value as a single FTS5 phrase.
//
// Quoting is not optional: FTS5 treats bare punctuation as query syntax, so an
// unquoted subject line with a hyphen or a colon in it is a syntax error rather
// than a search. A trailing * survives as a prefix match, which is the one
// operator worth exposing.
func ftsPhrase(v string) string {
	prefix := false
	if strings.HasSuffix(v, "*") {
		prefix = true
		v = strings.TrimSuffix(v, "*")
	}
	q := `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	if prefix {
		q += "*"
	}
	return q
}

// Flag predicates. IMAP flags are a JSON array in one column, so these match
// the escaped flag inside its text — `"\\Seen"` as JSON encodes `\Seen`.
const (
	flagSeen    = `m.flags LIKE '%\\Seen%'`
	flagFlagged = `m.flags LIKE '%\\Flagged%'`
	flagDeleted = `m.flags LIKE '%\\Deleted%'`
	flagDraft   = `m.flags LIKE '%\\Draft%'`
	flagAnswer  = `m.flags LIKE '%\\Answered%'`
	flagJunk    = `(m.flags LIKE '%\\Junk%' OR m.flags LIKE '%$Junk%')`
)

func (c *compiler) isTerm(t *Term, negated bool) (string, error) {
	var sql string
	switch strings.ToLower(t.Value) {
	case "unread", "new":
		sql = "NOT (" + flagSeen + ")"
	case "read", "seen":
		sql = flagSeen
	case "starred", "flagged":
		sql = flagFlagged
	case "deleted", "trashed":
		sql = flagDeleted
	case "draft":
		sql = flagDraft
	case "answered", "replied":
		sql = flagAnswer
	case "junk", "spam":
		sql = flagJunk
	case "reply":
		// A message that is itself a reply, by header rather than by flag.
		sql = "m.in_reply_to IS NOT NULL"
	default:
		return "", fmt.Errorf("is: %q is not a state (unread, read, starred, deleted, draft, answered, junk, reply)", t.Value)
	}
	return wrap(sql, negated), nil
}

func (c *compiler) hasTerm(t *Term, negated bool) (string, error) {
	var sql string
	switch strings.ToLower(t.Value) {
	case "attachment", "attachments":
		// Any recorded attachment, including one InboxQL chose not to store.
		// The row is evidence the message carried it.
		sql = "EXISTS (SELECT 1 FROM attachments att WHERE att.message_id = m.id)"
	case "file", "files", "storedattachment":
		// Only attachments actually on disk and openable.
		sql = "EXISTS (SELECT 1 FROM attachments att WHERE att.message_id = m.id AND att.storage_path IS NOT NULL AND att.storage_path != '')"
	case "label", "labels":
		sql = "EXISTS (SELECT 1 FROM annotations a WHERE a.message_id = m.id AND a.status = 'ok')"
	case "reply", "parent":
		sql = "m.in_reply_to IS NOT NULL"
	case "to", "cc", "bcc":
		// Emptiness over a multi-valued field. `-has:cc` is "nobody was
		// copied", which needs the same anti-join shape as -cc:someone.
		sql = "EXISTS (SELECT 1 FROM message_participants p WHERE p.message_id = m.id AND p.role = " +
			c.arg(strings.ToLower(t.Value)) + ")"
	default:
		return "", fmt.Errorf("has: %q is not a property (attachment, file, label, reply, to, cc, bcc)", t.Value)
	}
	return wrap(sql, negated), nil
}

func (c *compiler) dateTerm(t *Term, negated bool) (string, error) {
	start, end, err := ParseDateValue(t.Value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Field, err)
	}

	var sql string
	switch t.Field {
	case "after":
		// Inclusive of the named day, which is what "since" means to everyone.
		sql = "m.date >= " + c.arg(start)
	case "before":
		// Exclusive of the named day: "before the 5th" does not include the
		// 5th. Documented, because the opposite convention also exists.
		sql = "m.date < " + c.arg(start)
	case "on":
		sql = "(m.date >= " + c.arg(start) + " AND m.date < " + c.arg(end) + ")"
	}
	return wrap(sql, negated), nil
}

func (c *compiler) sizeTerm(t *Term, negated bool) (string, error) {
	n, err := ParseSize(t.Value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Field, err)
	}
	op := ">"
	if t.Field == "smaller" {
		op = "<"
	}
	return wrap("m.size "+op+" "+c.arg(n), negated), nil
}

// effectiveAnnotation is the row that counts for a message.
//
// Two rules, and both matter:
//
//   - Results from an earlier version are kept for provenance but do not
//     answer queries. Treating them as live would let one query silently mix
//     two different definitions of the same label.
//   - A human ruling counts at any version and outranks the machine. It has to
//     survive a version bump, or correcting a label would mean correcting it
//     again after every prompt edit; and it has to suppress the machine row for
//     the same message, or a human "yes" over a machine "no" would satisfy
//     `label:x` and `-label:x` at once and break the three-way partition.
const effectiveAnnotation = `(
	a.source = 'human'
	OR (
		a.annotator_version = an.version
		AND NOT EXISTS (
			SELECT 1 FROM annotations h
			WHERE h.message_id = a.message_id
			  AND h.annotator_id = a.annotator_id
			  AND h.source = 'human'
		)
	)
)`

// annotationExists builds an existence test over an annotator's effective
// results for a message.
//
// The name placeholder is bound last, after any caller-supplied conditions,
// because those conditions carry their own arguments and were appended to the
// argument list when they were built. Binding the name first would put the
// placeholders and the arguments in different orders — which does not fail
// loudly, it just answers the wrong question.
func (c *compiler) checkAnnotator(field, name string) error {
	if c.opt.KnownAnnotator == nil || c.opt.KnownAnnotator(name) {
		return nil
	}
	return fmt.Errorf("%s: no annotator named %q (list them with `iql annotate list`)", field, name)
}

func (c *compiler) annotationExists(name string, extra ...string) string {
	conds := []string{
		"a.message_id = m.id",
		"an.id = a.annotator_id",
		effectiveAnnotation,
	}
	conds = append(conds, extra...)
	conds = append(conds, "an.name = "+c.arg(name))

	return "EXISTS (SELECT 1 FROM annotations a JOIN annotators an ON an.id = a.annotator_id WHERE " +
		strings.Join(conds, " AND ") + ")"
}

// labelTerm is the one field whose negation is not its complement.
//
// A label is three-valued: the annotator said yes, the annotator said no, or
// the annotator has never seen this message. `-label:x` compiles to
// "evaluated, and no", NOT to "not (evaluated and yes)".
//
// The difference is the whole point. On a mailbox where an annotator has
// covered 10k of 200k messages, the plain complement returns 190k messages
// nobody has looked at and presents them as a negative result. Requiring the
// annotator to have actually run means a negative answer is always an answer.
// `unlabeled:x` asks the other question, and `-label:x OR unlabeled:x` spells
// out "not known to be x" for anyone who wants the loose reading.
func (c *compiler) labelTerm(t *Term, negated bool) (string, error) {
	if err := c.checkAnnotator("label", t.Value); err != nil {
		return "", err
	}
	status := "a.status = 'ok'"
	if negated {
		status = "a.status = 'empty'"
	}
	extra := []string{status}
	if t.Qualifier != "" && !negated {
		conf, err := strconv.ParseFloat(t.Qualifier, 64)
		if err != nil {
			return "", fmt.Errorf("label: confidence %q is not a number", t.Qualifier)
		}
		extra = append(extra, "a.confidence >= "+c.arg(conf))
	}
	// Deliberately not wrapped in NOT: the negation is already expressed by
	// asking for the opposite status.
	return "(" + c.annotationExists(t.Value, extra...) + ")", nil
}

// unlabeledTerm matches messages an annotator has never evaluated.
func (c *compiler) unlabeledTerm(t *Term, negated bool) (string, error) {
	if err := c.checkAnnotator("unlabeled", t.Value); err != nil {
		return "", err
	}
	exists := c.annotationExists(t.Value)
	if negated {
		// "not unlabeled" is "evaluated at all", either way.
		return "(" + exists + ")", nil
	}
	return "(NOT " + exists + ")", nil
}

func (c *compiler) confTerm(t *Term, negated bool) (string, error) {
	v, err := strconv.ParseFloat(t.Value, 64)
	if err != nil {
		return "", fmt.Errorf("conf: %q is not a number", t.Value)
	}
	op, err := comparisonOperator(t.Op, ">=")
	if err != nil {
		return "", fmt.Errorf("conf: %w", err)
	}
	sql := "EXISTS (SELECT 1 FROM annotations a WHERE a.message_id = m.id AND a.confidence " + op + " " + c.arg(v) + ")"
	return wrap(sql, negated), nil
}

// extractTerm matches on extracted structured data.
//
//	extract:saas-metrics                 the extractor produced something
//	extract:saas-metrics.signups>1000    a field of what it produced
func (c *compiler) extractTerm(t *Term, negated bool) (string, error) {
	spec := t.Value
	op := t.Op
	value := ""

	// The comparison rides inside the value because the field name is part of
	// the path: `extract:a.b>1` splits after the path, not after the field.
	for _, cand := range []struct {
		sym string
		op  Op
	}{{">=", OpGreaterOrEqual}, {"<=", OpLessOrEqual}, {"!=", OpExact}, {">", OpGreater}, {"<", OpLess}, {"=", OpExact}} {
		if i := strings.Index(spec, cand.sym); i > 0 {
			op, value, spec = cand.op, spec[i+len(cand.sym):], spec[:i]
			break
		}
	}

	name, field, hasField := strings.Cut(spec, ".")
	if name == "" {
		return "", fmt.Errorf("extract: needs an annotator name")
	}
	if err := c.checkAnnotator("extract", name); err != nil {
		return "", err
	}

	extra := []string{"a.status = 'ok'"}
	if hasField && field != "" {
		path := "$." + field
		expr := "json_extract(a.data_json, " + c.arg(path) + ")"
		if value == "" {
			extra = append(extra, expr+" IS NOT NULL")
		} else if n, err := strconv.ParseFloat(value, 64); err == nil {
			sqlOp, err := comparisonOperator(op, "=")
			if err != nil {
				return "", fmt.Errorf("extract: %w", err)
			}
			extra = append(extra, "CAST("+expr+" AS REAL) "+sqlOp+" "+c.arg(n))
		} else {
			sqlOp, err := comparisonOperator(op, "=")
			if err != nil {
				return "", fmt.Errorf("extract: %w", err)
			}
			if sqlOp == "=" {
				extra = append(extra, "LOWER(CAST("+expr+" AS TEXT)) = LOWER("+c.arg(value)+")")
			} else {
				extra = append(extra, "CAST("+expr+" AS TEXT) "+sqlOp+" "+c.arg(value))
			}
		}
	}

	return wrap(c.annotationExists(name, extra...), negated), nil
}

func (c *compiler) threadTerm(t *Term, negated bool) (string, error) {
	if c.opt.ThreadIDs == nil {
		return "", fmt.Errorf("thread: is not available here")
	}
	ids, err := c.opt.ThreadIDs(t.Value)
	if err != nil {
		return "", fmt.Errorf("thread: %w", err)
	}
	if len(ids) == 0 {
		// An unknown message has an empty thread, and its negation is
		// everything — which is the honest answer, not an error.
		if negated {
			return "1=1", nil
		}
		return "1=0", nil
	}
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = c.arg(id)
	}
	return wrap("m.id IN ("+strings.Join(placeholders, ", ")+")", negated), nil
}

// dayMillis is one day, for making a date bound inclusive of its own day.
const dayMillis = int64(24 * 60 * 60 * 1000)

// ticketColumn maps a ticket field name onto its column.
func ticketColumn(field string) string {
	if field == "raised" {
		return "origin"
	}
	return field
}

func comparisonOperator(op Op, dflt string) (string, error) {
	switch op {
	case OpGreater:
		return ">", nil
	case OpGreaterOrEqual:
		return ">=", nil
	case OpLess:
		return "<", nil
	case OpLessOrEqual:
		return "<=", nil
	case OpExact, OpMatch:
		return dflt, nil
	default:
		return "", fmt.Errorf("operator %s is not a comparison", op)
	}
}
