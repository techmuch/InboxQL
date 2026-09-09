// Package query implements InboxQL's query language: a filter expression with
// an optional pipeline of aggregation stages, compiled to parameterised SQL.
//
// # Why a language
//
// Before this package there were three independent filter implementations —
// SearchQuery, AnalyticsFilter and ListMessagesFiltered — which disagreed with
// each other about what `from` meant (substring in one, equality in the other
// two). Every new filter dimension cost a struct field and three edits. One
// parser and one compiler replace all of it, and adding a dimension is a case
// in compileTerm.
//
// # Shape
//
//	from:stripe after:2026-01-01 has:attachment
//	is:unread -from:*@acme.com | count by week
//	label:invoice conf>0.9 | sum amount by month
//
// A query is a filter followed by zero or more `|` stages. The filter selects
// messages; the stages reshape the result.
//
// # Negation
//
// Negation is a unary operator over any node, not a property of a term, so
// `-(from:alice after:2026-01)` parses and compiles like anything else.
//
// Multi-valued fields negate as NOT EXISTS over their edge rows rather than
// as `!=` against a joined row. That distinction is the whole reason
// message_participants exists: `-to:alice` must mean "no recipient is alice",
// where `!=` inside a join would mean "some recipient is not alice" and match
// nearly every message with more than one recipient.
//
// Labels are the deliberate exception to plain complement, and the reason is
// in [Term]. See CompileFilter.
package query

import "strings"

// Op is how a term compares its value.
type Op int

const (
	// OpMatch is the field's natural default: a token match for prose
	// (subject, body, free text) and a substring match for addresses. The two
	// differ because they are asked different questions — nobody searches
	// prose for a fragment inside a word, and everybody searches addresses for
	// a domain fragment.
	OpMatch          Op = iota
	OpExact             // =value
	OpGlob              // value containing *
	OpGreater           // >value
	OpGreaterOrEqual    // >=value
	OpLess              // <value
	OpLessOrEqual       // <=value
)

func (o Op) String() string {
	switch o {
	case OpExact:
		return "="
	case OpGlob:
		return "glob"
	case OpGreater:
		return ">"
	case OpGreaterOrEqual:
		return ">="
	case OpLess:
		return "<"
	case OpLessOrEqual:
		return "<="
	default:
		return "match"
	}
}

// Node is one element of a filter expression.
type Node interface{ node() }

// And matches when every child matches. Adjacent terms are implicitly ANDed.
type And struct{ Nodes []Node }

// Or matches when any child matches.
type Or struct{ Nodes []Node }

// Not inverts its child.
//
// For most nodes this is a plain SQL NOT. For a label term it is not: see
// compileTerm, where "-label:x" means evaluated-and-false rather than
// not-evaluated-true.
type Not struct{ Node Node }

// All matches every message. The empty query.
type All struct{}

// Term is a single field predicate.
type Term struct {
	// Field is the canonical field name, already lowercased and de-aliased.
	// Empty means free text across the indexed columns.
	Field string
	Op    Op
	Value string
	// Qualifier carries a secondary constraint the field defines for itself.
	// Only labels use it today, for the confidence floor in `label:invoice@0.9`.
	Qualifier string
	// Pos is where this term started in the source.
	//
	// Kept so a semantic complaint — an unknown field, a value outside an
	// enum, an unparseable date — can point at the offending token the way a
	// syntax error does. Without it those errors arrive as prose and the
	// reader has to find the problem by eye.
	Pos int
}

func (*And) node()  {}
func (*Or) node()   {}
func (*Not) node()  {}
func (*All) node()  {}
func (*Term) node() {}

// StageKind identifies a pipeline verb.
type StageKind string

const (
	StageCount        StageKind = "count"
	StageTop          StageKind = "top"
	StageSort         StageKind = "sort"
	StageLimit        StageKind = "limit"
	StageSample       StageKind = "sample"
	StageSeries       StageKind = "series"
	StageAggregate    StageKind = "sum" // sum/avg/min/max, distinguished by Func
	StageExtract      StageKind = "extract"
	StageThread       StageKind = "thread"
	StageParticipants StageKind = "participants"
	// StageTimeline groups the result into conversations and returns, for
	// each, everything anchored to it: the messages, the tickets they raised,
	// the drafts that answer them.
	StageTimeline StageKind = "timeline"
	// StageNetwork returns who appears alongside whom.
	StageNetwork StageKind = "network"
	// StageTopics returns what a contact is associated with, ranked by how
	// disproportionately they discuss it.
	StageTopics StageKind = "topics"
)

// Stage is one step of the pipeline.
type Stage struct {
	Kind StageKind
	// Func is the aggregate for StageAggregate: sum, avg, min, max.
	Func string
	// Field is what the stage operates on: the grouping key for count, the
	// sort column for sort, the extracted field for series and aggregates.
	Field string
	// Bucket is the time granularity for series: day, week, month, year.
	Bucket string
	// N is the row cap for top and limit.
	N int
	// Desc reverses sort order.
	Desc bool
	// Annotator names the extractor for `extract` and for series/aggregates
	// that read extracted values.
	Annotator string
}

// Query is a parsed filter plus its pipeline.
type Query struct {
	Filter Node
	Stages []Stage
	// Source is the raw text, kept for error messages and for round-tripping
	// a query back into the UI's search bar.
	Source string
}

// Entity reports which table this query is about.
//
// Resolution order, and the order matters:
//
//  1. An explicit `in:` term. It always wins, and it is the only way to say
//     something a field name cannot — `status:` belongs to both tickets and
//     drafts, so it identifies neither.
//  2. Otherwise, a field that belongs to exactly one non-message entity.
//     `due:` means tickets; `origin:` means drafts.
//  3. Otherwise mail, which is what most queries are about.
//
// Inferring rather than requiring a declaration is deliberate: someone writing
// `status:todo from:acme` means one thing, and should not have to say which
// table it lives in.
func (q *Query) Entity() string {
	if entity, ok := explicitEntity(q.Filter); ok {
		return entity
	}
	if entity, ok := inferredEntity(q.Filter); ok {
		return entity
	}
	return EntityMessage
}

// explicitEntity finds a term that names the row source outright.
//
// `folder:drafts` counts, and has to: it is the spelling the mailbox and
// AGENTS.md already use, and drafts stopped being a folder over `messages` the
// moment they became their own entity. Keeping it as sugar means no caller
// written against the old contract breaks.
func explicitEntity(n Node) (string, bool) {
	var found string
	var ok bool
	walkTerms(n, func(t *Term) {
		switch t.Field {
		case "in":
			if e, valid := Entities[strings.ToLower(t.Value)]; valid {
				found, ok = e, true
			}
		case "folder":
			if strings.EqualFold(t.Value, "drafts") {
				found, ok = EntityDraft, true
			}
		}
	})
	return found, ok
}

// inferredEntity finds an entity named by a field that only one entity claims.
func inferredEntity(n Node) (string, bool) {
	var found string
	var ok bool
	walkTerms(n, func(t *Term) {
		if ok {
			return
		}
		if e, unique := EntityOf(t.Field); unique {
			found, ok = e, true
		}
	})
	return found, ok
}

// walkTerms visits every term in a filter.
func walkTerms(n Node, visit func(*Term)) {
	switch t := n.(type) {
	case *And:
		for _, c := range t.Nodes {
			walkTerms(c, visit)
		}
	case *Or:
		for _, c := range t.Nodes {
			walkTerms(c, visit)
		}
	case *Not:
		walkTerms(t.Node, visit)
	case *Term:
		visit(t)
	}
}

// IsAggregate reports whether the pipeline reduces messages to groups rather
// than returning messages.
func (q *Query) IsAggregate() bool {
	for _, s := range q.Stages {
		switch s.Kind {
		case StageCount, StageTop, StageSeries, StageAggregate, StageParticipants:
			return true
		}
	}
	return false
}
