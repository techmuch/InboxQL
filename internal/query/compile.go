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
	// IgnoredTopicWords are the subject words that are not topics.
	//
	// Supplied here rather than filtered by whoever renders a chart: the
	// dashboard stripped them and the query language did not, so `| top topic`
	// in Desk led with `fwd:` and `re:` while Topic Trends hid them — one
	// question with two answers depending on where it was asked.
	IgnoredTopicWords func() []string

	// SimilarIDs resolves a message id to the messages nearest it in meaning,
	// at or above a threshold. A negative threshold means "use the configured
	// default", which is the only place the default is read — a similarity
	// threshold is not portable between embedding models, so it belongs with
	// the data rather than in this package.
	SimilarIDs func(messageID string, threshold float64) ([]string, error)

	// SimilarAttachments does the same for files, over the text extraction and
	// OCR produced. Separate from SimilarIDs because the two are indexed over
	// different things and a file's neighbours are files.
	SimilarAttachments func(key string, threshold float64) ([]string, error)

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
	case EntityContact:
		// A contact's identity is its address; there is no separate key,
		// because two rows for one address would be two contacts.
		return "c.address"
	case EntityAttachment:
		// A file's identity is its bytes. Falling back to the row's own id
		// keeps a part whose bytes were never stored — too large, or arriving
		// before extraction existed — as a file in its own right rather than
		// collapsing every such part in the mailbox into one.
		return attachmentKey
	}
	return "m.id"
}

// attachmentKey is what makes two attachment rows the same file.
//
// Content addressing, so the same document sent twice is one file with two
// occurrences rather than two files that happen to look alike. This appears in
// the compiler and again in the planner's GROUP BY; they must agree, so it is
// written once.
const attachmentKey = "COALESCE(NULLIF(a.content_hash, ''), a.id)"

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

	if c.entity == EntityContact {
		sql, err := c.contactTerm(t, negated)
		return sql, at(t, err)
	}

	if c.entity == EntityAttachment {
		sql, err := c.attachmentTerm(t, negated)
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
// contactTerm compiles a term about a contact.
//
// The counted fields are correlated subqueries over message_participants
// rather than stored columns. That is slower per row and it is the whole
// point: a cached count is a second answer to "how much mail is there from
// this person", and it goes wrong the first time a message is deleted.
func (c *compiler) contactTerm(t *Term, negated bool) (string, error) {
	switch t.Field {
	case "in":
		// Read by Query.Entity; it selects the source, it does not filter.
		return "1=1", nil

	case "id", "email":
		if t.Field == "id" || t.Op == OpExact {
			return wrap("c.address = "+c.arg(strings.ToLower(t.Value)), negated), nil
		}
		return wrap(c.stringPredicate("c.address", t), negated), nil

	case "name":
		// Every place a name can come from, strongest first — asking for a
		// name should find the contact whatever taught the system to call
		// them that.
		return wrap("("+c.stringPredicate("COALESCE(c.display_name, '')", t)+
			" OR "+c.stringPredicate("COALESCE(c.header_name, '')", t)+
			" OR "+c.stringPredicate("COALESCE(c.first_name, '') || ' ' || COALESCE(c.last_name, '')", t)+
			")", negated), nil

	case "kind":
		if t.Op == OpGlob {
			return wrap(c.stringPredicate("c.kind", t), negated), nil
		}
		return wrap("c.kind = "+c.arg(strings.ToLower(t.Value)), negated), nil

	case "org":
		return wrap(c.stringPredicate("COALESCE(c.org, '')", t), negated), nil

	case "phone":
		return wrap(c.stringPredicate("COALESCE(c.phone, '')", t), negated), nil

	case "messages", "sent":
		col := "(SELECT COUNT(DISTINCT p.message_id) FROM message_participants p WHERE p.address = c.address)"
		if t.Field == "sent" {
			col = "(SELECT COUNT(*) FROM message_participants p WHERE p.address = c.address AND p.role = 'from')"
		}
		op, err := comparisonOperator(t.Op, "=")
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(t.Value), 64)
		if err != nil {
			return "", fmt.Errorf("%s: %q is not a number", t.Field, t.Value)
		}
		return wrap(col+" "+op+" "+c.arg(n), negated), nil

	case "tag":
		tagVal := strings.ToLower(strings.TrimSpace(t.Value))
		if t.Op == OpGlob {
			pattern := strings.ReplaceAll(tagVal, "*", "%")
			return wrap("EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.address = c.address AND ct.tag LIKE "+c.arg(pattern)+")", negated), nil
		}
		return wrap("EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.address = c.address AND ct.tag = "+c.arg(tagVal)+")", negated), nil

	case "notes", "note":
		return wrap(c.stringPredicate("COALESCE(c.notes, '')", t), negated), nil

	case "awaiting":
		val := strings.ToLower(strings.TrimSpace(t.Value))
		switch val {
		case "me":
			return wrap(`EXISTS (
				SELECT 1 FROM messages m_latest
				JOIN message_participants p_latest ON p_latest.message_id = m_latest.id AND p_latest.role = 'from'
				WHERE p_latest.address = c.address
				  AND m_latest.date = (
				      SELECT MAX(m_sub.date) FROM messages m_sub
				      WHERE COALESCE(m_sub.thread_key, m_sub.id) = COALESCE(m_latest.thread_key, m_latest.id)
				  )
			)`, negated), nil
		case "them":
			if c.opt.SelfAddresses == nil {
				return "", fmt.Errorf("awaiting:them needs configured accounts")
			}
			selfs, err := c.opt.SelfAddresses()
			if err != nil {
				return "", err
			}
			if len(selfs) == 0 {
				return wrap("1=0", negated), nil
			}
			ph := make([]string, len(selfs))
			for i, s := range selfs {
				ph[i] = c.arg(strings.ToLower(s))
			}
			return wrap(`EXISTS (
				SELECT 1 FROM messages m_latest
				JOIN message_participants p_latest ON p_latest.message_id = m_latest.id AND p_latest.role = 'from'
				JOIN messages m_contact ON COALESCE(m_contact.thread_key, m_contact.id) = COALESCE(m_latest.thread_key, m_latest.id)
				JOIN message_participants p_contact ON p_contact.message_id = m_contact.id AND p_contact.address = c.address
				WHERE p_latest.address IN (`+strings.Join(ph, ", ")+`)
				  AND m_latest.date = (
				      SELECT MAX(m_sub.date) FROM messages m_sub
				      WHERE COALESCE(m_sub.thread_key, m_sub.id) = COALESCE(m_latest.thread_key, m_latest.id)
				  )
			)`, negated), nil
		default:
			return "", fmt.Errorf("awaiting:%s is not valid (try awaiting:me or awaiting:them)", t.Value)
		}

	case "has":
		switch strings.ToLower(t.Value) {
		case "phone":
			return wrap("COALESCE(c.phone, '') != ''", negated), nil
		case "name":
			return wrap("(COALESCE(c.display_name, '') != '' OR COALESCE(c.header_name, '') != '')", negated), nil
		case "org":
			return wrap("COALESCE(c.org, '') != ''", negated), nil
		case "notes", "note":
			return wrap("COALESCE(c.notes, '') != ''", negated), nil
		case "tag", "tags":
			return wrap("EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.address = c.address)", negated), nil
		case "awaiting":
			return wrap(`EXISTS (
				SELECT 1 FROM messages m_latest
				JOIN message_participants p_latest ON p_latest.message_id = m_latest.id AND p_latest.role = 'from'
				WHERE p_latest.address = c.address
				  AND m_latest.date = (
				      SELECT MAX(m_sub.date) FROM messages m_sub
				      WHERE COALESCE(m_sub.thread_key, m_sub.id) = COALESCE(m_latest.thread_key, m_latest.id)
				  )
			)`, negated), nil
		}
		return "", fmt.Errorf("has:%s is not something a contact can have (try phone, name, org, notes, tag or awaiting)", t.Value)
	}

	// Anything else is a question about their mail, answered through the edges.
	inner, args, err := CompileFilterFor(&Term{Field: t.Field, Value: t.Value, Op: t.Op}, c.opt, EntityMessage)
	if err != nil {
		return "", fmt.Errorf("%s is not a contact field: %w", t.Field, err)
	}
	c.args = append(c.args, args...)
	return wrap("EXISTS (SELECT 1 FROM message_participants p JOIN messages m ON m.id = p.message_id "+
		"WHERE p.address = c.address AND ("+inner+"))", negated), nil
}

// attachmentSameFile tests whether two attachment rows are the same file.
//
// Written from the same key both sides, so a row always matches itself: a part
// with no stored hash falls back to its own id, and comparing that to another
// row's id is false — which is right, since nothing is known to be identical
// to bytes that were never captured.
func attachmentSameFile(occ, outer string) string {
	key := func(alias string) string {
		return "COALESCE(NULLIF(" + alias + ".content_hash, ''), " + alias + ".id)"
	}
	return key(occ) + " = " + key(outer)
}

// attachmentTerm compiles a term about a file.
//
// # Why the counted fields reach across occurrences
//
// The row this compiles against is one occurrence — one (message, file) edge —
// but the entity is the file, and the planner groups occurrences back down to
// one row per file. So a predicate over a per-occurrence column applies to any
// occurrence, and a predicate over the file as a whole is a subquery across all
// of them. `filename:` is the first kind: a file sent once as "invoice.pdf" and
// once as "invoice-copy.pdf" answers to both names, because it really did
// arrive under both.
func (c *compiler) attachmentTerm(t *Term, negated bool) (string, error) {
	// Every occurrence of this same file, for the questions that are about the
	// file rather than about one arrival of it.
	occurrences := func(inner string) string {
		return "(SELECT " + inner + " FROM attachments occ WHERE " +
			attachmentSameFile("occ", "a") + ")"
	}

	// anyOccurrence lifts a predicate about one arrival to a predicate about
	// the file. Every column that can differ between arrivals of identical
	// bytes — the name it was sent under, whether it was embedded or attached —
	// goes through this. It is not only about matching more: the planner groups
	// occurrences into one row and counts them, so a predicate left at the
	// occurrence level would drop some arrivals from the group and report a
	// file that went to five people as having reached one.
	anyOccurrence := func(pred string) string {
		return "EXISTS (SELECT 1 FROM attachments occ WHERE " +
			attachmentSameFile("occ", "a") + " AND " + pred + ")"
	}

	switch t.Field {
	case "in":
		// Read by Query.Entity; it selects the source, it does not filter.
		return "1=1", nil

	case "id":
		// The content hash names the file. A prefix is accepted because that
		// is how a hash is ever typed or pasted from a listing.
		v := strings.ToLower(strings.TrimSpace(t.Value))
		return wrap("LOWER("+attachmentKey+") = "+c.arg(v)+
			" OR instr(LOWER(COALESCE(a.content_hash, '')), "+c.arg(v)+") = 1", negated), nil

	case "filename", "file", "name":
		// A file sent once as "invoice.pdf" and once as "invoice-copy.pdf"
		// answers to both names, because it really did arrive under both.
		return wrap(anyOccurrence(c.stringPredicate("COALESCE(occ.filename, '')", t)), negated), nil

	case "type", "mime", "filetype":
		pred, err := c.attachmentType(t)
		if err != nil {
			return "", err
		}
		return wrap(anyOccurrence(pred), negated), nil

	case "size":
		n, err := ParseSize(t.Value)
		if err != nil {
			return "", fmt.Errorf("size: %w", err)
		}
		op, err := comparisonOperator(t.Op, "=")
		if err != nil {
			return "", err
		}
		return wrap("a.size "+op+" "+c.arg(n), negated), nil

	case "messages":
		op, err := comparisonOperator(t.Op, "=")
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(t.Value), 64)
		if err != nil {
			return "", fmt.Errorf("messages: %q is not a number", t.Value)
		}
		return wrap(occurrences("COUNT(DISTINCT occ.message_id)")+" "+op+" "+c.arg(n), negated), nil

	case "content", "inside", "fulltext":
		return c.attachmentContent(t, negated)

	case "similar":
		return c.similarAttachmentTerm(t, negated)

	case "is":
		switch strings.ToLower(strings.TrimSpace(t.Value)) {
		case "read", "searchable":
			// Read, and something was found. `searchable` is the same set said
			// the way somebody thinking about search would say it.
			return wrap(extractionStatus("ok"), negated), nil
		case "unread":
			// Nobody has looked at this file yet.
			return wrap("NOT EXISTS (SELECT 1 FROM attachment_extractions e "+
				"WHERE e.content_hash = a.content_hash)", negated), nil
		case "scanned":
			// Read, and found to hold no text: an image of a page. The set OCR
			// exists for, and the reason `empty` is a recorded status rather
			// than an absent row.
			return wrap(extractionStatus("empty"), negated), nil
		case "stored":
			return wrap("COALESCE(a.storage_path, '') != ''", negated), nil
		case "missing":
			// Recorded but not on disk: too large to keep, or captured before
			// there was anywhere to put it. Worth being able to ask for, since
			// it is the set that a preview cannot open.
			return wrap("COALESCE(a.storage_path, '') = ''", negated), nil
		case "inline":
			return wrap(anyOccurrence("occ.inline = 1"), negated), nil
		case "attached":
			return wrap(anyOccurrence("occ.inline = 0"), negated), nil
		case "shared":
			// The same bytes on more than one message — a document that went
			// round, rather than a one-off.
			return wrap(occurrences("COUNT(DISTINCT occ.message_id)")+" > 1", negated), nil
		}
		return "", fmt.Errorf(
			"is: %q is not a file state (stored, missing, inline, attached, shared, "+
				"read, unread, scanned, searchable)", t.Value)

	case "has":
		switch strings.ToLower(strings.TrimSpace(t.Value)) {
		case "text", "content":
			return wrap(extractionStatus("ok"), negated), nil
		}
		return "", fmt.Errorf("has: %q is not something a file has (try text)", t.Value)
	}

	// Anything else is a question about the mail this file arrived on — "PDFs
	// that came from Stripe", "files from last March". The same reach-through
	// as tickets, and for the same reason: each message field is written once
	// and works from both sides.
	//
	// Over every occurrence, not one, because the entity is the file. A
	// document that arrived from two people is from both of them, and
	// `from:alice` should find it.
	// A bare word in a file query is nearly always the file's name — or, now
	// that files are read, a word inside one. It is also sometimes a word in
	// the mail that carried it, and nothing distinguishes which was meant, so
	// it is all three: typing `invoice` finds invoice.pdf, a PDF whose text
	// says invoice, and a file attached to mail about one.
	//
	// Built before the mail half, and that ordering is load-bearing: arguments
	// are bound in the order they are appended, so generating these
	// placeholders after the mail compiler had already appended its own would
	// bind them the wrong way round — a query that runs, returns the wrong
	// rows, and looks like a bad search rather than a bug.
	byName, byContent := "", ""
	if t.Field == "" {
		byName = anyOccurrence(c.stringPredicate("COALESCE(occ.filename, '')", &Term{Value: t.Value}))
		if inside, err := c.attachmentContent(&Term{Value: t.Value, Op: t.Op}, false); err == nil {
			byContent = inside
		}
	}

	inner, args, err := CompileFilterFor(&Term{Field: t.Field, Value: t.Value, Op: t.Op}, c.opt, EntityMessage)
	if err != nil {
		return "", fmt.Errorf("%s is not a file field: %w", t.Field, err)
	}
	c.args = append(c.args, args...)
	onMail := "EXISTS (SELECT 1 FROM attachments occ JOIN messages m ON m.id = occ.message_id " +
		"WHERE " + attachmentSameFile("occ", "a") + " AND (" + inner + "))"

	if byName != "" {
		parts := byName
		if byContent != "" {
			parts += " OR " + byContent
		}
		return wrap("("+parts+" OR "+onMail+")", negated), nil
	}
	return wrap(onMail, negated), nil
}

// extractionStatus tests what a file's extraction recorded.
func extractionStatus(status string) string {
	return "EXISTS (SELECT 1 FROM attachment_extractions e " +
		"WHERE e.content_hash = a.content_hash AND e.status = '" + status + "')"
}

// attachmentContent searches the words inside a file.
//
// # Why this is not the same as a bare word
//
// A bare word in a file query asks about the filename and the mail that
// carried the file, because that is what somebody typing one word usually
// means. `content:` asks only about what is inside — "the invoice that says
// 4815", not "the mail that mentions 4815". Those are different questions, and
// before extraction existed only one of them could be asked at all.
//
// Falls back to substring matching without the full-text index, exactly as the
// message side does: correct, slower, and token-blind, which is what search was
// before the index existed.
func (c *compiler) attachmentContent(t *Term, negated bool) (string, error) {
	value := strings.TrimSpace(t.Value)
	if value == "" {
		return "", fmt.Errorf("content: needs something to look for")
	}

	// Over every page of the file, because a file is one document to a person
	// and several rows here.
	inner := ""
	if c.opt.FullText && t.Op == OpMatch {
		inner = "at.rowid IN (SELECT rowid FROM attachment_text_fts WHERE attachment_text_fts MATCH " +
			c.arg(ftsPhrase(value)) + ")"
	} else {
		inner = c.stringPredicate("at.text", t)
	}

	return wrap("EXISTS (SELECT 1 FROM attachment_text at "+
		"WHERE at.content_hash = a.content_hash AND ("+inner+"))", negated), nil
}

// attachmentType matches a kind of file.
//
// # Why one field and not two
//
// People ask for "pdfs" and "images", and MIME types are neither of those
// spellings — application/vnd.openxmlformats-officedocument.wordprocessingml.document
// is a Word file, and nobody is going to type that. But an exact MIME type is
// the precise thing when precision is wanted. Rather than making the user
// choose between `type:` and `mime:`, one field answers both: a known category
// name expands to its types, and anything else is matched against the MIME
// string directly. `type:pdf` and `type:application/pdf` both work, and so does
// `type:openxmlformats` for someone who knows what they are looking at.
//
// Returns a bare predicate over alias `occ`; the caller lifts it to the file.
func (c *compiler) attachmentType(t *Term) (string, error) {
	value := strings.ToLower(strings.TrimSpace(t.Value))
	if value == "" {
		return "", fmt.Errorf("type: needs a kind of file (%s)", strings.Join(FileTypeNames, ", "))
	}

	if types, ok := FileTypeCategories[value]; ok && t.Op != OpGlob {
		parts := make([]string, 0, len(types))
		for _, mime := range types {
			if strings.HasSuffix(mime, "/") {
				// A whole top-level type: image/, audio/, video/.
				parts = append(parts, "instr(LOWER(COALESCE(occ.mime_type, '')), "+c.arg(mime)+") = 1")
				continue
			}
			parts = append(parts, "LOWER(COALESCE(occ.mime_type, '')) = "+c.arg(mime))
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	}

	// Not a category: match the MIME type, and the filename's extension too,
	// so `type:xlsx` finds a spreadsheet a server labelled octet-stream — which
	// this mailbox really does contain.
	byMIME := c.stringPredicate("COALESCE(occ.mime_type, '')", t)
	byExt := "LOWER(COALESCE(occ.filename, '')) LIKE " + c.arg("%."+value)
	return "(" + byMIME + " OR " + byExt + ")", nil
}

// FileTypeCategories are the words people actually use for kinds of file.
//
// A trailing slash means a whole top-level MIME type. Exported so autocomplete
// can offer these names rather than making someone guess which words work.
var FileTypeCategories = map[string][]string{
	"pdf":   {"application/pdf"},
	"image": {"image/"},
	"photo": {"image/"},
	"audio": {"audio/"},
	"video": {"video/"},
	"doc": {
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.oasis.opendocument.text",
		"application/rtf",
	},
	"sheet": {
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.oasis.opendocument.spreadsheet",
		"text/csv",
	},
	"slides": {
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.oasis.opendocument.presentation",
	},
	"archive": {
		"application/zip", "application/x-tar", "application/gzip",
		"application/x-7z-compressed", "application/x-rar-compressed",
	},
	"calendar": {"text/calendar", "application/ics"},
	"contact":  {"text/vcard", "text/x-vcard"},
	"text":     {"text/plain", "text/markdown"},
}

// FileTypeNames lists the categories in the order help should show them.
var FileTypeNames = []string{
	"pdf", "image", "doc", "sheet", "slides",
	"archive", "calendar", "contact", "text", "audio", "video",
}

// FileTypeLabel names a MIME type the way a person would.
//
// The same table read backwards, so what a listing shows and what `type:`
// accepts are the same words: a row labelled "doc" is found by `type:doc`. A
// type in no category falls back to its subtype, because
// "vnd.openxmlformats-officedocument.wordprocessingml.document" is not a label,
// and neither is "application".
func FileTypeLabel(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if m == "" {
		return "unknown"
	}
	// Names in declared order, so a type in two categories gets the one help
	// lists first rather than whichever the map iterated to.
	for _, name := range FileTypeNames {
		for _, candidate := range FileTypeCategories[name] {
			if strings.HasSuffix(candidate, "/") {
				if strings.HasPrefix(m, candidate) {
					return name
				}
				continue
			}
			if m == candidate {
				return name
			}
		}
	}
	if _, sub, ok := strings.Cut(m, "/"); ok && sub != "" {
		return sub
	}
	return m
}

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

	case "similar":
		return c.similarTerm(t, negated)

	case "topic":
		// An anti-join, like every other multi-valued field: -topic:x has to
		// mean "no topic is x", not "some topic is not x".
		return wrap("EXISTS (SELECT 1 FROM message_topics mt WHERE mt.message_id = m.id AND "+
			c.stringPredicate("mt.topic", t)+")", negated), nil

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

// similarTerm resolves a message id to its neighbours.
//
// # Why the threshold is part of the term
//
// A similarity threshold is not one number. Browsing wants it loose, assigning
// a message to a topic wants it middling, and deciding two topics are the same
// wants it tight — and none of those values transfer to a different embedding
// model, because cosine values sit in a band whose width is a property of the
// model. So it is written where the question is asked, with a configured
// default behind it, exactly like every other tunable in this language.
func (c *compiler) similarTerm(t *Term, negated bool) (string, error) {
	if c.opt.SimilarIDs == nil {
		return "", fmt.Errorf("similar: needs embeddings; run `iql annotate embed --profile <name>`")
	}

	spec, threshold, err := parseSimilarValue(t.Value, "a message id")
	if err != nil {
		return "", err
	}

	ids, err := c.opt.SimilarIDs(spec, threshold)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		// Nothing is near enough. An impossible predicate rather than an
		// error: "no neighbours at this threshold" is an answer.
		return wrap("1=0", negated), nil
	}

	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, c.arg(id))
	}
	return wrap("m.id IN ("+strings.Join(placeholders, ", ")+")", negated), nil
}

// parseSimilarValue splits an id from an optional threshold.
//
// The comparison rides inside the value: `similar:abc>0.85` splits after the
// id, because the id is the value and the threshold qualifies it. Shared
// between the message and file forms so the two cannot come to disagree about
// what `>0.85` means.
func parseSimilarValue(value, what string) (spec string, threshold float64, err error) {
	spec, threshold = value, -1.0

	for _, sym := range []string{">=", ">"} {
		if i := strings.Index(spec, sym); i > 0 {
			raw := strings.TrimSpace(spec[i+len(sym):])
			n, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr != nil {
				return "", 0, fmt.Errorf("similar: %q is not a similarity between 0 and 1", raw)
			}
			if n < 0 || n > 1 {
				return "", 0, fmt.Errorf("similar: %v is outside 0..1; cosine similarity cannot exceed 1", n)
			}
			threshold, spec = n, strings.TrimSpace(spec[:i])
			break
		}
	}
	if spec == "" {
		return "", 0, fmt.Errorf("similar: needs %s", what)
	}
	return spec, threshold, nil
}

// similarAttachmentTerm resolves a file to the files nearest it in meaning.
//
// # What "similar" means for a file
//
// The words inside it, as extraction or OCR read them, with the filename
// leading. So it finds the other copy of a contract filed under a different
// name, the rest of a supplier's invoices, the blank version of a form that
// was filled in — the things a filename search cannot reach and a word search
// only reaches if you already know the word.
//
// A file nothing has read cannot take part, and that is worth an error rather
// than an empty result: "no similar files" and "this file has never been read"
// are indistinguishable from the outside, and only one of them is fixable.
func (c *compiler) similarAttachmentTerm(t *Term, negated bool) (string, error) {
	if c.opt.SimilarAttachments == nil {
		return "", fmt.Errorf(
			"similar: needs embeddings over file text; run `iql annotate embed --attachments`")
	}

	spec, threshold, err := parseSimilarValue(t.Value, "a file's content hash")
	if err != nil {
		return "", err
	}

	keys, err := c.opt.SimilarAttachments(spec, threshold)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return wrap("1=0", negated), nil
	}

	placeholders := make([]string, 0, len(keys))
	for _, key := range keys {
		placeholders = append(placeholders, c.arg(key))
	}
	return wrap(attachmentKey+" IN ("+strings.Join(placeholders, ", ")+")", negated), nil
}
