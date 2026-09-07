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
	value = quoteValue(strings.TrimSpace(value))

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

// StageSpan is one stage of a query's pipeline, with where it sits.
type StageSpan struct {
	// Verb is the stage's name, lowercased: count, top, timeline.
	Verb  string `json:"verb"`
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Stages splits a query's pipeline into its stages.
//
// The mirror of Terms, and here for the same reason: a client that wants to
// know whether `| timeline` is on should ask rather than pattern-match the
// string. Tolerant in the same way too — it runs against text someone is still
// typing.
func Stages(src string) []StageSpan {
	out := []StageSpan{}

	at := pipelineAt(src)
	if at < 0 {
		return out
	}

	inQuote := false
	start := at + 1
	flush := func(end int) {
		text := strings.TrimSpace(src[start:end])
		if text == "" {
			return
		}
		verb := ""
		if fields := strings.Fields(text); len(fields) > 0 {
			verb = strings.ToLower(fields[0])
		}
		// Spans address the trimmed text, so a caller splicing one out does not
		// take the surrounding whitespace with it.
		lead := strings.Index(src[start:end], text)
		out = append(out, StageSpan{
			Verb: verb, Text: text,
			Start: start + lead, End: start + lead + len(text),
		})
	}

	for i := at + 1; i < len(src); i++ {
		switch src[i] {
		case '"':
			inQuote = !inQuote
		case '|':
			if !inQuote {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(src))

	return out
}

// terminalStages are the verbs that decide a result's shape.
//
// A query can end in only one of them — the planner rejects a second — so
// adding one has to replace whichever is already there. Encoding that here is
// what lets a UI toggle `| timeline` on without first working out whether the
// query it is toggling ends in `| count by week`.
var terminalStages = map[string]bool{
	"count": true, "top": true, "participants": true, "timeline": true,
	"series": true, "sum": true, "avg": true, "min": true, "max": true,
}

// StageVerb reports the verb a stage expression starts with.
func StageVerb(stage string) string {
	if fields := strings.Fields(strings.TrimSpace(stage)); len(fields) > 0 {
		return strings.ToLower(strings.TrimPrefix(fields[0], "|"))
	}
	return ""
}

// WithStage adds a stage to a query's pipeline.
//
// A stage replaces any other with the same verb, and a terminal one also
// replaces any other terminal — asking for a count and a timeline at once is
// not a narrower question, it is two questions.
func WithStage(src, stage string) string {
	stage = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stage), "|"))
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return src
	}
	verb := StageVerb(stage)

	keep := []string{}
	for _, s := range Stages(src) {
		if s.Verb == verb {
			continue
		}
		if terminalStages[verb] && terminalStages[s.Verb] {
			continue
		}
		keep = append(keep, s.Text)
	}
	keep = append(keep, stage)

	return joinStages(filterOf(src), keep)
}

// WithoutStage removes every stage with the given verb.
func WithoutStage(src, verb string) string {
	verb = StageVerb(verb)
	keep := []string{}
	for _, s := range Stages(src) {
		if s.Verb != verb {
			keep = append(keep, s.Text)
		}
	}
	return joinStages(filterOf(src), keep)
}

// HasStage reports whether a query's pipeline contains a verb.
func HasStage(src, verb string) bool {
	verb = StageVerb(verb)
	for _, s := range Stages(src) {
		if s.Verb == verb {
			return true
		}
	}
	return false
}

// FilterOf returns the filter half of a query, without its pipeline.
//
// What a query *selects*, separated from what it then *does* with it. A caller
// that has its own aggregate to apply — the dashboard's widgets each are one —
// must take this rather than the whole string, because appending a second
// terminal stage to a query that already has one produces something the
// planner rejects outright.
func FilterOf(src string) string { return filterOf(src) }

// filterOf returns the filter half of a query, without its pipeline.
func filterOf(src string) string {
	if at := pipelineAt(src); at >= 0 {
		return strings.TrimSpace(src[:at])
	}
	return strings.TrimSpace(src)
}

func joinStages(filter string, stages []string) string {
	out := filter
	for _, s := range stages {
		if out == "" {
			out = "| " + s
			continue
		}
		out += " | " + s
	}
	return strings.TrimSpace(out)
}

// FormatGroupTerm assembles one term matching any of several values.
//
// A hand-picked selection is the case this exists for: six chosen messages are
// not a filter anyone can phrase, they are six identities, and the query for
// them is `id:(a OR b OR c OR …)`.
//
// Here rather than in a client for the same reason FormatTerm is: the quoting,
// the grouping parens and the fact that one value needs neither are all
// grammar. A client that built this string would get the single-value case
// wrong first and the quoted-value case wrong second.
func FormatGroupTerm(field string, values []string, negated bool) string {
	clean := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		clean = append(clean, quoteValue(v))
	}

	switch len(clean) {
	case 0:
		return ""
	case 1:
		// No parens for one value: `id:(a)` parses, but it reads like a
		// truncated list and round-trips into something a user did not write.
		return FormatTerm(field, values[0], negated)
	}

	term := canonicalField(field) + ":(" + strings.Join(clean, " OR ") + ")"
	if negated {
		term = "-" + term
	}
	return term
}

// quoteValue applies FormatTerm's quoting rule to a bare value.
func quoteValue(v string) string {
	if strings.ContainsAny(v, " \t\n\"") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}
