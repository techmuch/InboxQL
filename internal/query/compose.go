package query

import "strings"

// TermSpan is one top-level term of a query, with where it sits in the source.
//
// Spans rather than just text, so a client removing a term splices it out
// instead of trying to match it back by string equality — which fails the
// moment a value contains a space.
type TermSpan struct {
	Text string `json:"text"`
	// Field is the term's field name with any leading dash removed, or empty
	// for a bare word. It is what "one term per field" is decided on.
	Field   string `json:"field,omitempty"`
	Negated bool   `json:"negated"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

// Terms splits a query into its top-level terms.
//
// # Why this is here rather than in the frontend
//
// Three separate TypeScript implementations of this have existed in this
// project, and all three were wrong in the same way: they split on whitespace,
// so a quoted value containing a space stopped being one term. That produced
// duplicate terms on a second click, a collision key of "-from" for a negated
// term, and a folder term that silently ANDed itself into an empty result.
//
// The lexer already knows where a term begins and ends. Answering from it means
// one implementation and no drift.
//
// Tolerant by design: this runs against text someone is still typing, so an
// unterminated quote yields the terms up to it rather than an error.
func Terms(src string) []TermSpan {
	out := []TermSpan{}

	i := 0
	for i < len(src) {
		if isSpace(src[i]) {
			i++
			continue
		}

		// A pipeline is not made of terms. Everything from the first top-level
		// bar onward is stages, and rewriting it is not this function's job.
		if src[i] == '|' {
			break
		}

		start := i
		negated := false
		if src[i] == '-' && i+1 < len(src) && !isSpace(src[i+1]) {
			negated = true
			i++
		}

		depth := 0
		inQuote := false
		for i < len(src) {
			c := src[i]
			switch {
			case c == '"':
				inQuote = !inQuote
			case inQuote:
				// Everything inside quotes belongs to the term, spaces included.
			case c == '(':
				depth++
			case c == ')':
				if depth > 0 {
					depth--
				}
			case depth == 0 && (isSpace(c) || c == '|'):
				goto done
			}
			i++
		}
	done:
		text := src[start:i]
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, TermSpan{
			Text:    text,
			Field:   TermField(text),
			Negated: negated,
			Start:   start,
			End:     i,
		})
	}

	return out
}

// TermField reports the field a term constrains.
//
// The leading dash comes off first: `-from:x` constrains `from`, not `-from`.
// Getting that wrong meant a negated term never replaced its positive twin.
func TermField(term string) string {
	t := strings.TrimSpace(term)
	t = strings.TrimPrefix(t, "-")
	if strings.HasPrefix(strings.ToLower(t), "not ") {
		t = strings.TrimSpace(t[4:])
	}
	if t == "" || strings.HasPrefix(t, "(") {
		return ""
	}
	// A quoted term is a value, not a field:value pair.
	if strings.HasPrefix(t, `"`) {
		return ""
	}
	colon := strings.Index(t, ":")
	if colon < 0 {
		return ""
	}
	return canonicalField(t[:colon])
}

// pipelineAt returns the offset of the first top-level pipe, or -1.
func pipelineAt(src string) int {
	inQuote := false
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '"':
			inQuote = !inQuote
		case '|':
			if !inQuote {
				return i
			}
		}
	}
	return -1
}

// WithTerm adds a term to a query, replacing any other term for the same field.
//
// One term per field is what a facet click means: picking a second sender in
// Top Senders replaces the first rather than asking for mail from both, which
// would match nothing. A term with no field — a bare word — accumulates,
// because two words are a narrower search rather than a contradiction.
//
// The term lands in the filter, before any pipeline: `| count by week` has to
// stay at the end or the query stops parsing.
func WithTerm(src, term string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		return src
	}

	field := TermField(term)
	keep := []string{}
	for _, t := range Terms(src) {
		if t.Text == term {
			continue // already there; the replacement below re-adds it
		}
		if field != "" && t.Field == field {
			continue // same facet, superseded
		}
		keep = append(keep, t.Text)
	}
	keep = append(keep, term)

	return joinFilterAndPipeline(strings.Join(keep, " "), src)
}

// WithoutTerm removes a term.
func WithoutTerm(src, term string) string {
	term = strings.TrimSpace(term)
	keep := []string{}
	for _, t := range Terms(src) {
		if t.Text != term {
			keep = append(keep, t.Text)
		}
	}
	return joinFilterAndPipeline(strings.Join(keep, " "), src)
}

// ReplaceField swaps whichever term holds a field for a new one, leaving the
// rest of the query alone.
//
// This is what a rail click does for a built-in folder: selecting Inbox while
// `from:alice` is applied should change the folder, not discard the filter.
// Passing an empty term removes the field entirely.
func ReplaceField(src, field, term string) string {
	field = canonicalField(field)
	keep := []string{}
	for _, t := range Terms(src) {
		if t.Field != field {
			keep = append(keep, t.Text)
		}
	}
	if strings.TrimSpace(term) != "" {
		keep = append(keep, strings.TrimSpace(term))
	}
	return joinFilterAndPipeline(strings.Join(keep, " "), src)
}

// HasTerm reports whether a query already carries a term.
func HasTerm(src, term string) bool {
	term = strings.TrimSpace(term)
	for _, t := range Terms(src) {
		if t.Text == term {
			return true
		}
	}
	return false
}

// joinFilterAndPipeline reattaches the original query's stages to a rewritten
// filter.
func joinFilterAndPipeline(filter, original string) string {
	filter = strings.TrimSpace(filter)
	if at := pipelineAt(original); at >= 0 {
		stages := strings.TrimSpace(original[at:])
		if filter == "" {
			return stages
		}
		return filter + " " + stages
	}
	return filter
}

// FormatTerm assembles a term from its parts, quoting the value when it needs it.
//
// Quoting is syntax, so it belongs here rather than in whatever is editing a
// pill. The rule is narrow on purpose: a value is quoted only when leaving it
// bare would change how it parses — whitespace would split it into two terms,
// and a quote character would unbalance the string.
//
// Operators are left alone. `=alice@x.com`, `*@acme.com` and `me()` are values
// that mean something structural, and wrapping them in quotes would turn each
// into a literal search for its own punctuation.
func FormatTerm(field, value string, negated bool) string {
	value = strings.TrimSpace(value)

	if strings.ContainsAny(value, " \t\n\"") {
		value = `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}

	term := value
	if field != "" {
		term = canonicalField(field) + ":" + value
	}
	if negated {
		term = "-" + term
	}
	return term
}

// ReplaceAt swaps the term starting at an offset for another.
//
// Addressed by offset rather than by text because a pill knows where its term
// sits — Terms gave it the span — and matching by string would pick the wrong
// one when a query carries two terms that read alike.
//
// An empty replacement removes the term, which is what a pill's × does.
func ReplaceAt(src string, start int, term string) string {
	keep := []string{}
	replaced := false

	for _, t := range Terms(src) {
		if t.Start == start {
			replaced = true
			if strings.TrimSpace(term) != "" {
				keep = append(keep, strings.TrimSpace(term))
			}
			continue
		}
		keep = append(keep, t.Text)
	}

	// An offset that names no term leaves the query alone rather than
	// appending: the caller is working from a stale span, and guessing would
	// silently duplicate a term.
	if !replaced {
		return src
	}
	return joinFilterAndPipeline(strings.Join(keep, " "), src)
}
