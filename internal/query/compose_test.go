package query

import (
	"strings"
	"testing"
)

func termTexts(src string) []string {
	spans := Terms(src)
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Text)
	}
	return out
}

func joined(src string) string { return strings.Join(termTexts(src), "|") }

// Splitting a query into terms is where three TypeScript implementations each
// went wrong, in the same way: they split on whitespace, so a quoted value
// containing a space stopped being one term.
func TestTermsRespectQuotesAndGroups(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"from:alice", "from:alice"},
		{"from:alice is:unread", "from:alice|is:unread"},
		{"-from:alice", "-from:alice"},
		// The case that produced duplicates on a second click.
		{`subject:"quarterly report"`, `subject:"quarterly report"`},
		{`subject:"quarterly report" is:unread`, `subject:"quarterly report"|is:unread`},
		{`from:"a b" to:"c d"`, `from:"a b"|to:"c d"`},
		// A group is one term, spaces and all.
		{"from:(alice OR bob)", "from:(alice OR bob)"},
		{"-(from:alice after:2026-01)", "-(from:alice after:2026-01)"},
		{"from:(a OR b) is:unread", "from:(a OR b)|is:unread"},
		// Stages are not terms.
		{"is:unread | count by week", "is:unread"},
		{"| count by week", ""},
		// A pipe inside quotes is not a pipeline.
		{`subject:"a | b"`, `subject:"a | b"`},
	}
	for _, c := range cases {
		if got := joined(c.in); got != c.want {
			t.Errorf("Terms(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The collision key is the field, with the dash removed. `-from:x` constrains
// `from`, and a key of "-from" meant a negated term never replaced its twin.
func TestTermFieldIgnoresNegation(t *testing.T) {
	cases := map[string]string{
		"from:alice":     "from",
		"-from:alice":    "from",
		"NOT from:alice": "from",
		"sender:alice":   "from", // aliases resolve
		"invoice":        "",     // a bare word constrains no field
		`"a phrase"`:     "",
		"(a OR b)":       "",
		"is:unread":      "is",
	}
	for term, want := range cases {
		if got := TermField(term); got != want {
			t.Errorf("TermField(%q) = %q, want %q", term, got, want)
		}
	}
}

// One term per field is what a facet click means.
func TestWithTermReplacesTheSameFacet(t *testing.T) {
	cases := []struct{ query, add, want string }{
		{"", "from:alice", "from:alice"},
		{"is:unread", "from:alice", "is:unread from:alice"},
		// Picking a second sender replaces the first: asking for mail from both
		// at once would match nothing.
		{"from:alice", "from:bob", "from:bob"},
		{"is:unread from:alice", "from:bob", "is:unread from:bob"},
		// A negated term is the same facet.
		{"-from:alice", "from:bob", "from:bob"},
		{"from:alice", "-from:bob", "-from:bob"},
		// Bare words accumulate; two words narrow rather than contradict.
		{"invoice", "receipt", "invoice receipt"},
		// Adding what is already there changes nothing.
		{"from:alice is:unread", "from:alice", "is:unread from:alice"},
	}
	for _, c := range cases {
		if got := WithTerm(c.query, c.add); got != c.want {
			t.Errorf("WithTerm(%q, %q) = %q, want %q", c.query, c.add, got, c.want)
		}
	}
}

// A quoted value must not duplicate itself when clicked twice — the bug that
// produced `subject:"quarterly report" subject:"quarterly report"`.
func TestWithTermDoesNotDuplicateQuotedValues(t *testing.T) {
	q := ""
	for i := 0; i < 3; i++ {
		q = WithTerm(q, `subject:"quarterly report"`)
	}
	if got := joined(q); got != `subject:"quarterly report"` {
		t.Errorf("three clicks produced %q", q)
	}
}

// Stages have to stay at the end, or the query stops parsing.
func TestComposingKeepsThePipelineLast(t *testing.T) {
	cases := []struct{ query, add, want string }{
		{"| count by week", "from:alice", "from:alice | count by week"},
		{"is:unread | count by week", "from:alice", "is:unread from:alice | count by week"},
		{"from:alice | top domain 5", "from:bob", "from:bob | top domain 5"},
	}
	for _, c := range cases {
		got := WithTerm(c.query, c.add)
		if got != c.want {
			t.Errorf("WithTerm(%q, %q) = %q, want %q", c.query, c.add, got, c.want)
		}
		if _, err := Parse(got); err != nil {
			t.Errorf("WithTerm(%q, %q) produced something that does not parse: %v", c.query, c.add, err)
		}
	}
}

func TestWithoutTerm(t *testing.T) {
	cases := []struct{ query, remove, want string }{
		{"from:alice is:unread", "from:alice", "is:unread"},
		{`subject:"a b" is:unread`, `subject:"a b"`, "is:unread"},
		{"from:alice", "from:alice", ""},
		{"from:alice | count", "from:alice", "| count"},
		{"from:alice is:unread", "from:nobody", "from:alice is:unread"},
	}
	for _, c := range cases {
		if got := WithoutTerm(c.query, c.remove); got != c.want {
			t.Errorf("WithoutTerm(%q, %q) = %q, want %q", c.query, c.remove, got, c.want)
		}
	}
}

// Selecting a folder while a filter is applied changes the folder and keeps the
// filter. Replacing the whole query would silently discard the cross-filter.
func TestReplaceFieldPreservesEverythingElse(t *testing.T) {
	cases := []struct{ query, field, term, want string }{
		{"folder:inbox from:alice", "folder", "folder:sent", "from:alice folder:sent"},
		{"from:alice", "folder", "folder:sent", "from:alice folder:sent"},
		{"folder:inbox", "folder", "", ""},
		{"folder:inbox from:alice | count", "folder", "folder:sent", "from:alice folder:sent | count"},
	}
	for _, c := range cases {
		if got := ReplaceField(c.query, c.field, c.term); got != c.want {
			t.Errorf("ReplaceField(%q, %q, %q) = %q, want %q",
				c.query, c.field, c.term, got, c.want)
		}
	}
}

// The folder collision that produced a silently empty mailbox: two folder terms
// AND together and match nothing. Replacing by field is what prevents it.
func TestFolderTermsCannotCollide(t *testing.T) {
	q := "folder:inbox from:alice"
	q = ReplaceField(q, "folder", "folder:sent")

	folders := 0
	for _, t := range Terms(q) {
		if t.Field == "folder" {
			folders++
		}
	}
	if folders != 1 {
		t.Errorf("query %q carries %d folder terms, want 1", q, folders)
	}
}

// Composition runs against text someone is still typing, so it must not fail on
// anything a parser would reject.
func TestComposingNeverPanicsOnPartialInput(t *testing.T) {
	for _, src := range []string{
		"", "-", "(", "from:", `subject:"unclosed`, "|", "| count by", "((", ")", "a OR",
	} {
		Terms(src)
		WithTerm(src, "from:alice")
		WithoutTerm(src, "from:alice")
		ReplaceField(src, "folder", "folder:inbox")
	}
}

// Spans have to identify the exact text, so removing a term is a splice rather
// than a string match.
func TestSpansAddressTheOriginalText(t *testing.T) {
	src := `is:unread subject:"quarterly report" from:alice`
	for _, span := range Terms(src) {
		if got := src[span.Start:span.End]; got != span.Text {
			t.Errorf("span %d-%d is %q but Text is %q", span.Start, span.End, got, span.Text)
		}
	}
}

// Quoting is syntax, so a pill editor hands over parts and the server assembles
// them. The rule is narrow: quote only when leaving it bare would reparse.
func TestFormatTerm(t *testing.T) {
	cases := []struct {
		field, value string
		negated      bool
		want         string
	}{
		{"from", "alice@acme.com", false, "from:alice@acme.com"},
		{"from", "alice@acme.com", true, "-from:alice@acme.com"},
		// Whitespace would split it into two terms.
		{"subject", "quarterly report", false, `subject:"quarterly report"`},
		{"subject", `say "hi"`, false, `subject:"say ""hi"""`},
		// Operators are structural and must survive unquoted.
		{"from", "=alice@acme.com", false, "from:=alice@acme.com"},
		{"from", "*@acme.com", false, "from:*@acme.com"},
		{"from", "me()", false, "from:me()"},
		// A bare word has no field.
		{"", "invoice", false, "invoice"},
		{"", "invoice", true, "-invoice"},
		// Aliases resolve to the canonical name.
		{"sender", "alice", false, "from:alice"},
	}
	for _, c := range cases {
		got := FormatTerm(c.field, c.value, c.negated)
		if got != c.want {
			t.Errorf("FormatTerm(%q, %q, %v) = %q, want %q", c.field, c.value, c.negated, got, c.want)
		}
		// Whatever it builds has to parse, and has to survive a round trip.
		if _, err := Parse(got); err != nil {
			t.Errorf("FormatTerm(%q, %q) built %q, which does not parse: %v", c.field, c.value, got, err)
		}
		if spans := Terms(got); len(spans) != 1 || spans[0].Text != got {
			t.Errorf("FormatTerm built %q, which is not one term: %v", got, spans)
		}
	}
}

// A pill edits the term at its own offset. Matching by text would pick the
// wrong one when a query carries two terms that read alike.
func TestReplaceAt(t *testing.T) {
	src := `folder:inbox from:alice is:unread`
	spans := Terms(src)
	if len(spans) != 3 {
		t.Fatalf("expected 3 terms, got %d", len(spans))
	}

	got := ReplaceAt(src, spans[1].Start, "from:bob")
	if got != "folder:inbox from:bob is:unread" {
		t.Errorf("replacing the middle term gave %q", got)
	}

	// An empty replacement removes it, which is what a pill's × does.
	if got := ReplaceAt(src, spans[1].Start, ""); got != "folder:inbox is:unread" {
		t.Errorf("removing the middle term gave %q", got)
	}

	// Stages survive.
	withStage := "from:alice | count by week"
	at := Terms(withStage)[0].Start
	if got := ReplaceAt(withStage, at, "from:bob"); got != "from:bob | count by week" {
		t.Errorf("replacing alongside a pipeline gave %q", got)
	}

	// A stale offset leaves the query alone rather than appending a duplicate.
	if got := ReplaceAt(src, 999, "from:bob"); got != src {
		t.Errorf("a stale offset changed the query to %q", got)
	}
}

func stageVerbs(src string) string {
	spans := Stages(src)
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Text)
	}
	return strings.Join(out, "|")
}

func TestStagesSplitsThePipeline(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"from:alice", ""},
		{"from:alice | timeline", "timeline"},
		{"| count by week", "count by week"},
		{"is:unread | sort date desc | limit 10", "sort date desc|limit 10"},
		// A pipe inside quotes is not a stage boundary.
		{`subject:"a | b" | timeline`, "timeline"},
		// Trailing bar while someone is still typing.
		{"from:alice |", ""},
		{"from:alice | ", ""},
	}
	for _, c := range cases {
		if got := stageVerbs(c.in); got != c.want {
			t.Errorf("Stages(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStageSpansAddressTheOriginalText(t *testing.T) {
	src := "is:unread | sort date desc | limit 10"
	for _, span := range Stages(src) {
		if got := src[span.Start:span.End]; got != span.Text {
			t.Errorf("span %d-%d is %q but Text is %q", span.Start, span.End, got, span.Text)
		}
	}
}

// A query can end in only one aggregate, so adding a terminal stage has to
// replace whichever is already there. Appending would build a query the
// planner rejects — a toggle that produces an error is a broken toggle.
func TestWithStageReplacesTheTerminal(t *testing.T) {
	cases := []struct{ query, stage, want string }{
		{"from:alice", "timeline", "from:alice | timeline"},
		{"", "timeline", "| timeline"},
		// Same verb twice is one stage.
		{"from:alice | timeline", "timeline", "from:alice | timeline"},
		// A different terminal supersedes.
		{"from:alice | count by week", "timeline", "from:alice | timeline"},
		{"from:alice | timeline", "count by week", "from:alice | count by week"},
		{"from:alice | top domain 5", "timeline", "from:alice | timeline"},
		// Non-terminal stages survive alongside a terminal one.
		{"from:alice | limit 10", "timeline", "from:alice | limit 10 | timeline"},
		{"is:unread | sort date desc | count", "timeline", "is:unread | sort date desc | timeline"},
		// A leading bar on the argument is accepted, since that is how a user
		// would write it.
		{"from:alice", "| timeline", "from:alice | timeline"},
	}
	for _, c := range cases {
		got := WithStage(c.query, c.stage)
		if got != c.want {
			t.Errorf("WithStage(%q, %q) = %q, want %q", c.query, c.stage, got, c.want)
		}
		if _, err := Parse(got); err != nil {
			t.Errorf("WithStage(%q, %q) built %q, which does not parse: %v",
				c.query, c.stage, got, err)
		}
	}
}

func TestWithoutStage(t *testing.T) {
	cases := []struct{ query, verb, want string }{
		{"from:alice | timeline", "timeline", "from:alice"},
		{"from:alice | limit 10 | timeline", "timeline", "from:alice | limit 10"},
		{"| timeline", "timeline", ""},
		{"from:alice", "timeline", "from:alice"},
		{"from:alice | count", "timeline", "from:alice | count"},
	}
	for _, c := range cases {
		if got := WithoutStage(c.query, c.verb); got != c.want {
			t.Errorf("WithoutStage(%q, %q) = %q, want %q", c.query, c.verb, got, c.want)
		}
	}
}

// Toggling on and then off has to give back exactly what was there, or the
// query bar drifts every time someone flips a switch.
func TestStageToggleRoundTrips(t *testing.T) {
	for _, src := range []string{
		"from:alice",
		"is:unread folder:inbox",
		"from:alice | limit 10",
		`subject:"quarterly report"`,
		"",
	} {
		on := WithStage(src, "timeline")
		if !HasStage(on, "timeline") {
			t.Errorf("WithStage(%q) did not turn it on: %q", src, on)
		}
		if off := WithoutStage(on, "timeline"); off != src {
			t.Errorf("round trip of %q gave %q", src, off)
		}
	}
}

// Composition runs against text someone is still typing.
func TestStageComposingNeverPanicsOnPartialInput(t *testing.T) {
	for _, src := range []string{
		"", "|", "| ", "||", `subject:"unclosed | timeline`, "from:alice | ", "| count by",
	} {
		Stages(src)
		WithStage(src, "timeline")
		WithoutStage(src, "timeline")
		HasStage(src, "timeline")
	}
}
