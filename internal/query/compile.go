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
	c := &compiler{opt: opt}
	sql, err := c.node(n, false)
	if err != nil {
		return "", nil, err
	}
	return sql, c.args, nil
}

type compiler struct {
	opt  Options
	args []any
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

	default:
		return "", fmt.Errorf("unknown field %q (try: %s)", t.Field, strings.Join(KnownFields, ", "))
	}
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
	conds = append(conds, c.stringPredicate("p.address", t))

	sql := "EXISTS (SELECT 1 FROM message_participants p WHERE " + strings.Join(conds, " AND ") + ")"
	return wrap(sql, negated), nil
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
	default:
		return "", fmt.Errorf("has: %q is not a property (attachment, file, label, reply)", t.Value)
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
