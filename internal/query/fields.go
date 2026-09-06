package query

import (
	"fmt"
	"sort"
	"strings"
)

// ValueType is what kind of value a field takes.
//
// The type decides which operators are legal, how a value is parsed, and where
// autocomplete looks for candidates. Before this existed, all three were
// hand-wired separately in compileTerm's switch, in the parser, and in the
// static list the fields endpoint returned — three copies of the same
// knowledge, already drifting.
type ValueType string

const (
	TypeText      ValueType = "text"      // prose: token matching through FTS
	TypeAddress   ValueType = "address"   // an email address, substring by default
	TypeDate      ValueType = "date"      // 2026-08-15, 2026-08, today, 7d
	TypeSize      ValueType = "size"      // 5mb, 500kb, a byte count
	TypeNumber    ValueType = "number"    // a bare number, compared
	TypeEnum      ValueType = "enum"      // one of a fixed set
	TypeIdent     ValueType = "ident"     // an exact identifier
	TypeAnnotator ValueType = "annotator" // the name of a label or extractor
	TypeQuery     ValueType = "query"     // the name of a saved query
	TypePath      ValueType = "path"      // annotator.field, optionally compared
)

// Entities a field can belong to.
//
// Only messages exist today. The column is here because tickets are a planned
// entity with their own fields (status, due, priority), and retrofitting an
// entity dimension onto a registry that assumed one row source would mean
// touching every caller. Adding it now costs a struct field.
const (
	EntityMessage = "message"
	EntityTicket  = "ticket"
	EntityDraft   = "draft"
)

// Entities are the row sources a query can be about, and the values `in:`
// accepts. Plural because that is how they read: `in:drafts`.
var Entities = map[string]string{
	"mail":     EntityMessage,
	"mails":    EntityMessage,
	"messages": EntityMessage,
	"message":  EntityMessage,
	"tickets":  EntityTicket,
	"ticket":   EntityTicket,
	"drafts":   EntityDraft,
	"draft":    EntityDraft,
}

// EntityNames lists what `in:` accepts, for help and completion.
var EntityNames = []string{"mail", "drafts", "tickets"}

// Field declares one queryable field.
type Field struct {
	Name    string
	Entity  string
	Type    ValueType
	Aliases []string
	// Ops are the operators this field accepts. OpMatch is always the default
	// and is always legal.
	Ops []Op
	// Enum lists the legal values for TypeEnum, in the order help should show
	// them.
	Enum []string
	// Values names where autocomplete finds candidates from the data. Empty
	// means the field has no data-driven values.
	Values string
	// Summary is one line, shown in autocomplete and generated help.
	Summary string
	Example string
	// Primary marks the declaration that wins when a field name belongs to
	// several entities.
	//
	// `status:` means something to both tickets and drafts, with different
	// values each. Without a tiebreak, `status:todo` would be ambiguous and
	// every existing query naming it would start erroring. Tickets hold the
	// name; drafts reach theirs through `in:drafts`.
	Primary bool
}

// Value sources autocomplete can draw on. The store resolves these; the query
// package only names them, since it has no database.
const (
	ValuesAddresses  = "addresses"
	ValuesAccounts   = "accounts"
	ValuesMailboxes  = "mailboxes"
	ValuesAnnotators = "annotators"
	ValuesSaved      = "saved"
	ValuesFolders    = "folders"
)

var matchOps = []Op{OpMatch, OpExact, OpGlob}
var compareOps = []Op{OpMatch, OpGreater, OpGreaterOrEqual, OpLess, OpLessOrEqual}

// Registry is every field the language understands.
//
// Order is the order help and autocomplete present them: the ones reached for
// most often first, rather than alphabetically.
var Registry = []Field{
	{
		Name: "from", Entity: EntityMessage, Type: TypeAddress, Aliases: []string{"sender"},
		Ops: matchOps, Values: ValuesAddresses,
		Summary: "who sent it", Example: "from:stripe",
	},
	{
		Name: "to", Entity: EntityMessage, Type: TypeAddress, Aliases: []string{"recipient"},
		Ops: matchOps, Values: ValuesAddresses,
		Summary: "a recipient", Example: "to:me@example.com",
	},
	{
		Name: "cc", Entity: EntityMessage, Type: TypeAddress,
		Ops: matchOps, Values: ValuesAddresses,
		Summary: "copied", Example: "cc:bob@acme.com",
	},
	{
		Name: "bcc", Entity: EntityMessage, Type: TypeAddress,
		Ops: matchOps, Values: ValuesAddresses,
		Summary: "blind copied", Example: "bcc:legal@acme.com",
	},
	{
		Name: "anyone", Entity: EntityMessage, Type: TypeAddress,
		Ops: matchOps, Values: ValuesAddresses,
		Summary: "in any role — sender or recipient", Example: "anyone:alice@acme.com",
	},
	{
		Name: "subject", Entity: EntityMessage, Type: TypeText,
		Ops:     matchOps,
		Summary: "words in the subject line", Example: "subject:invoice",
	},
	{
		Name: "body", Entity: EntityMessage, Type: TypeText,
		Ops:     matchOps,
		Summary: "words in the body", Example: "body:refund",
	},
	{
		Name: "text", Entity: EntityMessage, Type: TypeText,
		Ops:     matchOps,
		Summary: "words anywhere — the same as a bare word", Example: "text:invoice",
	},
	{
		Name: "is", Entity: EntityMessage, Type: TypeEnum,
		Enum:    []string{"unread", "read", "starred", "deleted", "draft", "answered", "junk", "reply"},
		Summary: "message state", Example: "is:unread",
	},
	{
		Name: "has", Entity: EntityMessage, Type: TypeEnum,
		Enum:    []string{"attachment", "file", "label", "reply", "to", "cc", "bcc"},
		Summary: "what the message carries", Example: "has:attachment",
	},
	{
		Name: "after", Entity: EntityMessage, Type: TypeDate, Aliases: []string{"since"},
		Summary: "on or after this date", Example: "after:2026-01-01",
	},
	{
		Name: "before", Entity: EntityMessage, Type: TypeDate, Aliases: []string{"until"},
		Summary: "strictly before this date", Example: "before:2026-03",
	},
	{
		Name: "on", Entity: EntityMessage, Type: TypeDate,
		Summary: "within this day, month or year", Example: "on:2026-02-14",
	},
	{
		Name: "larger", Entity: EntityMessage, Type: TypeSize, Aliases: []string{"bigger"},
		Summary: "bigger than this", Example: "larger:5mb",
	},
	{
		Name: "smaller", Entity: EntityMessage, Type: TypeSize,
		Summary: "smaller than this", Example: "smaller:10kb",
	},
	{
		Name: "account", Entity: EntityMessage, Type: TypeIdent, Aliases: []string{"acct"},
		Values:  ValuesAccounts,
		Summary: "which account it arrived on", Example: "account:work",
	},
	{
		Name: "folder", Entity: EntityMessage, Type: TypeEnum,
		Enum:    []string{"inbox", "starred", "sent", "drafts", "spam", "trash", "all"},
		Values:  ValuesFolders,
		Summary: "the mailbox view", Example: "folder:sent",
	},
	{
		Name: "mailbox", Entity: EntityMessage, Type: TypeText,
		Ops: matchOps, Values: ValuesMailboxes,
		Summary: "the IMAP folder it was read from", Example: "mailbox:INBOX",
	},
	{
		Name: "label", Entity: EntityMessage, Type: TypeAnnotator,
		Values:  ValuesAnnotators,
		Summary: "an annotator said yes; add @0.9 for a confidence floor", Example: "label:invoice",
	},
	{
		Name: "unlabeled", Entity: EntityMessage, Type: TypeAnnotator,
		Values:  ValuesAnnotators,
		Summary: "an annotator has never evaluated this message", Example: "unlabeled:invoice",
	},
	{
		Name: "conf", Entity: EntityMessage, Type: TypeNumber,
		Ops:     compareOps,
		Summary: "confidence of any annotation", Example: "conf>0.9",
	},
	{
		Name: "extract", Entity: EntityMessage, Type: TypePath,
		Values:  ValuesAnnotators,
		Summary: "extracted data, optionally compared", Example: "extract:metrics.signups>100",
	},
	{
		Name: "thread", Entity: EntityMessage, Type: TypeIdent,
		Summary: "every message in this one's conversation", Example: "thread:<message-id>",
	},
	// --- tickets -----------------------------------------------------------
	//
	// A query that mentions any of these is answered from the tickets table;
	// message fields inside it become a test over the ticket's evidence. That
	// is what "traceable to the mail that created it" means when it is a query
	// rather than a claim.
	{
		Name: "status", Entity: EntityTicket, Type: TypeEnum, Primary: true,
		Ops:     []Op{OpMatch, OpGlob},
		Enum:    []string{"proposed", "todo", "doing", "done", "rejected"},
		Summary: "where a ticket sits", Example: "status:todo",
	},
	{
		Name: "priority", Entity: EntityTicket, Type: TypeText,
		Ops:     matchOps,
		Summary: "a ticket's priority", Example: "priority:high",
	},
	{
		Name: "due", Entity: EntityTicket, Type: TypeDate,
		Summary: "due on or before this date", Example: "due:7d",
	},
	{
		Name: "ticket", Entity: EntityTicket, Type: TypeText,
		Ops:     matchOps,
		Summary: "words in a ticket's title", Example: "ticket:invoice",
	},
	{
		Name: "raised", Entity: EntityTicket, Type: TypeEnum,
		Ops:     []Op{OpMatch, OpGlob},
		Enum:    []string{"human", "annotator"},
		Summary: "who raised the ticket", Example: "raised:annotator",
	},

	// --- drafts ------------------------------------------------------------
	//
	// A draft is outgoing and unsent, deliberately not a row in `messages` so
	// it cannot be deduplicated against real mail. It is its own entity, and
	// `in:drafts` is how a query says so.
	{
		Name: "status", Entity: EntityDraft, Type: TypeEnum,
		Ops:     []Op{OpMatch, OpGlob},
		Enum:    []string{"draft", "queued", "sent", "failed"},
		Summary: "where a draft sits; needs in:drafts", Example: "in:drafts status:queued",
	},
	{
		Name: "origin", Entity: EntityDraft, Type: TypeEnum,
		Ops:     []Op{OpMatch, OpGlob},
		Enum:    []string{"human", "agent"},
		Summary: "who composed the draft", Example: "origin:agent",
	},

	{
		Name: "in", Entity: EntityMessage, Type: TypeEnum,
		Enum:    EntityNames,
		Summary: "which kind of thing the query is about", Example: "in:drafts",
	},
	{
		Name: "saved", Entity: EntityMessage, Type: TypeQuery,
		Values:  ValuesSaved,
		Summary: "everything a saved query matches", Example: "saved:invoices",
	},
}

// draftServes names the message fields a drafts query can answer from the
// drafts table's own columns. Everything else is about mail a draft does not
// have.
var draftServes = map[string]bool{
	"to": true, "cc": true, "bcc": true,
	"subject": true, "body": true, "text": true,
	"account": true, "after": true, "before": true, "on": true,
	// folder:drafts is how the mailbox names this entity; the compiler reads
	// it as the selector it is rather than a predicate.
	"folder": true,
}

// DraftServes reports whether a message field is answerable for a draft.
func DraftServes(field string) bool { return draftServes[canonicalField(field)] }

var (
	byName   = map[string]*Field{}
	byEntity = map[string]map[string]*Field{}
	aliases  = map[string]string{}
)

func init() {
	for i := range Registry {
		f := &Registry[i]
		if byEntity[f.Entity] == nil {
			byEntity[f.Entity] = map[string]*Field{}
		}
		byEntity[f.Entity][f.Name] = f

		// The unscoped map keeps the first declaration unless a later one
		// claims the name as primary.
		if existing, taken := byName[f.Name]; !taken || (f.Primary && !existing.Primary) {
			byName[f.Name] = f
		}
		for _, a := range f.Aliases {
			aliases[a] = f.Name
		}
	}
}

func canonicalName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := aliases[n]; ok {
		return canonical
	}
	return n
}

// LookupField resolves a name or alias without an entity in hand.
//
// Where a name belongs to several entities this returns the primary one, which
// is what keeps `status:todo` meaning tickets.
func LookupField(name string) (*Field, bool) {
	f, ok := byName[canonicalName(name)]
	return f, ok
}

// LookupFieldIn resolves a name against one entity.
//
// Falls back to the message declaration when the entity does not claim the
// name itself, so `subject:` works in a drafts query without being declared
// twice — DraftServes decides whether that fallback can actually be compiled.
func LookupFieldIn(entity, name string) (*Field, bool) {
	n := canonicalName(name)
	if f, ok := byEntity[entity][n]; ok {
		return f, true
	}
	if f, ok := byEntity[EntityMessage][n]; ok {
		return f, true
	}
	return nil, false
}

// EntityOf reports which entity a field name uniquely identifies.
//
// A name claimed by several entities identifies none of them: `status:` cannot
// say whether a query is about tickets or drafts, so it does not try. `in:`
// exists for that.
func EntityOf(name string) (string, bool) {
	n := canonicalName(name)

	var claimants []string
	var primary string
	for entity, fields := range byEntity {
		f, ok := fields[n]
		if !ok {
			continue
		}
		claimants = append(claimants, entity)
		if f.Primary {
			primary = entity
		}
	}

	switch {
	case len(claimants) == 1 && claimants[0] != EntityMessage:
		return claimants[0], true
	case len(claimants) > 1 && primary != "" && primary != EntityMessage:
		// Several entities claim the name and one holds it. `status:` means
		// tickets unless a query says `in:drafts`, which keeps every query
		// written before drafts existed meaning what it did.
		return primary, true
	}
	return "", false
}

// ClaimedBy lists the entities that declare a field name, for error messages.
func ClaimedBy(name string) []string {
	n := canonicalName(name)
	var out []string
	for _, entity := range []string{EntityMessage, EntityDraft, EntityTicket} {
		if _, ok := byEntity[entity][n]; ok {
			out = append(out, entity)
		}
	}
	return out
}

// canonicalField maps what someone typed onto the declared name.
func canonicalField(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	if c, ok := aliases[f]; ok {
		return c
	}
	return f
}

// KnownFields lists every field name, in registry order.
func KnownFields() []string {
	out := make([]string, 0, len(Registry))
	for i := range Registry {
		out = append(out, Registry[i].Name)
	}
	return out
}

// allows reports whether a field accepts an operator.
//
// A field with no declared Ops accepts only OpMatch, which is the honest
// default: `after:>2026-01` is not meaningful, because the comparison is
// already in the field's name.
func (f *Field) allows(op Op) bool {
	if op == OpMatch {
		return true
	}
	for _, o := range f.Ops {
		if o == op {
			return true
		}
	}
	return false
}

// validate checks a term against its field's declaration.
//
// Rejecting here rather than in the SQL compiler means an unusable term is a
// parse-time error with a suggestion, not a query that runs and quietly
// returns the wrong rows.
func (f *Field) validate(t *Term) error {
	if !f.allows(t.Op) {
		return fmt.Errorf("%s: %s does not take %s; try %s",
			f.Name, f.Name, describeOp(t.Op), f.Example)
	}
	// A glob over an enum is how "any value of this field" is said, which is
	// also how a ticket query says "every ticket". Checking membership would
	// reject `status:*` for not being one of the listed values.
	if f.Type == TypeEnum && t.Op == OpGlob {
		return nil
	}
	if f.Type == TypeEnum && len(f.Enum) > 0 {
		for _, v := range f.Enum {
			if strings.EqualFold(v, t.Value) {
				return nil
			}
		}
		return fmt.Errorf("%s: %q is not one of %s", f.Name, t.Value, strings.Join(f.Enum, ", "))
	}
	return nil
}

func describeOp(op Op) string {
	switch op {
	case OpExact:
		return "an exact match (=)"
	case OpGlob:
		return "a glob (*)"
	case OpGreater, OpGreaterOrEqual, OpLess, OpLessOrEqual:
		return "a comparison (" + op.String() + ")"
	default:
		return "that operator"
	}
}

// suggestField finds the closest declared field to something misspelled.
//
// A typo is the common case for an unknown field, and "no such field
// \"form\"" is a worse answer than "did you mean from?".
func suggestField(name string) string {
	name = strings.ToLower(name)
	best, bestScore := "", 1<<30
	for _, candidate := range KnownFields() {
		if d := editDistance(name, candidate); d < bestScore {
			best, bestScore = candidate, d
		}
	}
	// Beyond a couple of edits it is not a typo of anything, and a confident
	// wrong suggestion is worse than none.
	if bestScore > 2 {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// unknownFieldError builds the message for a field nobody declared.
func unknownFieldError(name string) error {
	if owners := ClaimedBy(name); len(owners) > 0 {
		plural := make([]string, 0, len(owners))
		for _, o := range owners {
			plural = append(plural, o+"s")
		}
		return fmt.Errorf("%s: belongs to %s — say %s",
			name, strings.Join(plural, " and "), "in:"+plural[0])
	}
	if s := suggestField(name); s != "" {
		return fmt.Errorf("unknown field %q — did you mean %s?", name, s)
	}
	names := KnownFields()
	sort.Strings(names)
	return fmt.Errorf("unknown field %q (fields: %s)", name, strings.Join(names, ", "))
}

// GroupKeys are the fields an aggregate can group by.
var GroupKeys = []string{
	"from", "domain", "to", "cc", "bcc", "anyone",
	"account", "mailbox", "label", "thread", "subject", "topic",
	"hour", "day", "week", "month", "year",
}

// Buckets are the time granularities series and aggregates accept.
var Buckets = []string{"hour", "day", "week", "month", "year"}

// StageNames are the pipeline verbs, in the order help presents them.
var StageNames = []string{
	"count", "top", "sort", "limit", "sample", "thread", "participants",
	"extract", "series", "sum", "avg", "min", "max",
}

// SortKeys are the columns a message result can be ordered by.
var SortKeys = []string{"date", "size", "subject", "from", "account"}
