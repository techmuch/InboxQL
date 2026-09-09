package query

import (
	"strings"
)

// Context is what the cursor is sitting in.
type Context string

const (
	// ContextField: the start of a term, so field names are wanted.
	ContextField Context = "field"
	// ContextValue: after `field:`, so values for that field are wanted.
	ContextValue Context = "value"
	// ContextStage: after `|`, so pipeline verbs are wanted.
	ContextStage Context = "stage"
	// ContextGroupKey: after `count by` or `top`, so grouping keys are wanted.
	ContextGroupKey Context = "groupkey"
	// ContextBucket: after `series x by`, so time buckets are wanted.
	ContextBucket Context = "bucket"
	// ContextSortKey: after `sort`.
	ContextSortKey Context = "sortkey"
)

// Candidate is one completion offer.
type Candidate struct {
	// Value is the text to insert.
	Value string `json:"value"`
	// Detail is shown beside it: a field's summary, a value's message count.
	Detail string `json:"detail,omitempty"`
	// Kind lets a UI group or icon the list.
	Kind string `json:"kind,omitempty"`
}

// Completion describes what may be typed at a cursor position.
//
// Start and End delimit the text the candidate replaces, so a client inserts
// by splicing rather than by guessing how much of the token it typed.
type Completion struct {
	Context Context `json:"context"`
	// Field is set for ContextValue: which field's values are wanted.
	Field string `json:"field,omitempty"`
	// ValueType is the field's declared type, so a client can render a date
	// picker rather than a list where that suits.
	ValueType string `json:"valueType,omitempty"`
	// ValueSource names where the store should look for data-driven
	// candidates. Empty when the candidates below are already complete.
	ValueSource string `json:"valueSource,omitempty"`
	// Prefix is what has been typed so far in the token being completed.
	Prefix string `json:"prefix"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	// Candidates are the ones this package can supply without a database.
	Candidates []Candidate `json:"candidates"`
	// Negated records that the token began with a dash, so a client can show
	// that the completion will exclude rather than include.
	Negated bool `json:"negated,omitempty"`
}

// Complete works out what can be typed at pos.
//
// # Why this is here and not in the frontend
//
// The alternative is a second implementation of the grammar in TypeScript,
// which is guaranteed to drift from this one — and the failure mode is an
// editor confidently offering something the server then rejects. One grammar,
// one answer.
//
// This deliberately does not use the parser. Text under a cursor is usually
// incomplete — `from:al`, `is:unread |`, an unclosed quote — and a parser's
// job is to reject exactly those. So this scans, tolerantly, and never fails:
// the worst answer it gives is "field names", which is the right answer for an
// empty query anyway.
func Complete(src string, pos int) *Completion {
	if pos < 0 {
		pos = 0
	}
	if pos > len(src) {
		pos = len(src)
	}

	before := src[:pos]
	start := tokenStart(before)
	token := before[start:]

	// The replaced span runs to the end of the word the cursor sits inside, so
	// completing in the middle of a token replaces the whole thing.
	end := pos
	for end < len(src) && !isBoundary(src[end]) {
		end++
	}

	negated := false
	if strings.HasPrefix(token, "-") {
		negated = true
		token = token[1:]
		start++
	}

	c := &Completion{Prefix: token, Start: start, End: end, Negated: negated, Candidates: []Candidate{}}

	// A pipeline changes what the words mean, so work out which stage the
	// cursor is in before anything else.
	if stage, words, ok := currentStage(before); ok {
		completeStage(c, stage, words, token)
		return c
	}

	// `field:value` — the colon is what commits to a value.
	if i := strings.Index(token, ":"); i >= 0 {
		name := canonicalField(token[:i])
		value := token[i+1:]
		c.Context = ContextValue
		c.Field = name
		c.Prefix = value
		c.Start = start + i + 1

		f, ok := LookupField(name)
		if !ok {
			// An unknown field has no values worth offering, and saying so is
			// better than offering the wrong ones.
			c.Candidates = nil
			return c
		}
		c.ValueType = string(f.Type)
		completeValue(c, f, value)
		return c
	}

	c.Context = ContextField
	completeFields(c, token)
	return c
}

// completeFields offers field names, filtered by what has been typed.
func completeFields(c *Completion, prefix string) {
	prefix = strings.ToLower(prefix)
	for i := range Registry {
		f := &Registry[i]
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		c.Candidates = append(c.Candidates, Candidate{
			Value: f.Name + ":", Detail: f.Summary, Kind: "field",
		})
	}
	// Aliases only surface once the prefix rules out the canonical spelling,
	// so the list is not doubled for someone typing from scratch.
	if prefix != "" {
		for alias, canonical := range aliases {
			if strings.HasPrefix(alias, prefix) && !strings.HasPrefix(canonical, prefix) {
				c.Candidates = append(c.Candidates, Candidate{
					Value: alias + ":", Detail: "same as " + canonical, Kind: "alias",
				})
			}
		}
	}
}

// completeValue offers what can follow `field:`.
func completeValue(c *Completion, f *Field, prefix string) {
	lower := strings.ToLower(prefix)

	// A fixed set is answerable here and needs no database.
	if len(f.Enum) > 0 {
		for _, v := range f.Enum {
			if strings.HasPrefix(v, lower) {
				c.Candidates = append(c.Candidates, Candidate{Value: v, Kind: "value"})
			}
		}
	}

	switch f.Type {
	case TypeDate:
		for _, v := range []struct{ value, detail string }{
			{"today", "since midnight"},
			{"yesterday", "the day before today"},
			{"7d", "the last seven days"},
			{"30d", "the last thirty days"},
			{"1y", "the last year"},
		} {
			if strings.HasPrefix(v.value, lower) {
				c.Candidates = append(c.Candidates, Candidate{Value: v.value, Detail: v.detail, Kind: "value"})
			}
		}
	case TypeSize:
		for _, v := range []string{"1mb", "5mb", "10mb", "100kb"} {
			if strings.HasPrefix(v, lower) {
				c.Candidates = append(c.Candidates, Candidate{Value: v, Kind: "value"})
			}
		}
	case TypeAddress:
		// me() is worth surfacing: it is the answer to the most common
		// question and the one nobody guesses is available.
		if strings.HasPrefix("me()", lower) {
			c.Candidates = append(c.Candidates, Candidate{
				Value: "me()", Detail: "your own addresses", Kind: "function",
			})
		}
	}

	// Anything drawn from the data is named rather than answered, because this
	// package has no database.
	c.ValueSource = f.Values
}

// currentStage reports which pipeline stage the cursor is in, and the words
// already typed in it.
//
// Returns false when the cursor is still in the filter half of the query.
func currentStage(before string) (verb string, words []string, ok bool) {
	i := strings.LastIndex(before, "|")
	if i < 0 {
		return "", nil, false
	}
	fields := strings.Fields(before[i+1:])
	if len(fields) == 0 {
		return "", nil, true
	}
	return strings.ToLower(fields[0]), fields[1:], true
}

// completeStage offers pipeline verbs, and then whatever the verb takes.
func completeStage(c *Completion, verb string, words []string, token string) {
	lower := strings.ToLower(token)

	// Still typing the verb itself: no complete word has been committed yet.
	if len(words) == 0 && (verb == "" || verb == lower) {
		c.Context = ContextStage
		for _, s := range StageNames {
			if strings.HasPrefix(s, lower) {
				c.Candidates = append(c.Candidates, Candidate{
					Value: s, Detail: stageSummary(s), Kind: "stage",
				})
			}
		}
		return
	}

	// `by` is optional sugar and does not change what comes next.
	trimmed := words
	if len(trimmed) > 0 && strings.EqualFold(trimmed[len(trimmed)-1], "by") {
		trimmed = trimmed[:len(trimmed)-1]
	}

	switch verb {
	case "count", "top":
		if len(trimmed) == 0 {
			c.Context = ContextGroupKey
			offerAll(c, GroupKeys, lower, "group")
		}
	case "sort":
		if len(trimmed) == 0 {
			c.Context = ContextSortKey
			offerAll(c, SortKeys, lower, "column")
		} else if len(trimmed) == 1 {
			c.Context = ContextSortKey
			offerAll(c, []string{"asc", "desc"}, lower, "direction")
		}
	case "series", "sum", "avg", "min", "max":
		// The field is an extracted one, which only the store knows.
		if len(trimmed) == 0 {
			c.Context = ContextValue
			c.ValueSource = ValuesAnnotators
		} else {
			c.Context = ContextBucket
			offerAll(c, Buckets, lower, "bucket")
		}
	case "extract":
		if len(trimmed) == 0 {
			c.Context = ContextValue
			c.ValueSource = ValuesAnnotators
		}
	default:
		c.Context = ContextStage
	}
}

func offerAll(c *Completion, values []string, prefix, kind string) {
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			c.Candidates = append(c.Candidates, Candidate{Value: v, Kind: kind})
		}
	}
}

func stageSummary(name string) string {
	switch name {
	case "count":
		return "how many, or how many per group"
	case "top":
		return "the largest groups"
	case "sort":
		return "reorder the messages"
	case "limit":
		return "cap the rows"
	case "sample":
		return "a random draw, for judging an annotator"
	case "thread":
		return "expand to whole conversations"
	case "timeline":
		return "conversations, with the tickets and drafts they produced"
	case "network":
		return "who appears alongside whom, and how often"
	case "topics":
		return "what these contacts are associated with, most distinctive first"
	case "participants":
		return "who appears, and how often"
	case "extract":
		return "read an extractor's structured output"
	case "series":
		return "an extracted value over time"
	case "sum", "avg", "min", "max":
		return name + " an extracted field"
	}
	return ""
}

// tokenStart finds where the word under the cursor began.
func tokenStart(before string) int {
	i := len(before)
	for i > 0 && !isBoundary(before[i-1]) {
		i--
	}
	return i
}

// isBoundary reports whether a byte ends a token.
//
// Quotes are not boundaries: `subject:"quarterly re` is one token still being
// typed, and treating the quote as a break would complete against "re" as
// though it were a field.
func isBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '(', ')', '|':
		return true
	}
	return false
}
