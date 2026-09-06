package query

import (
	"strings"
	"testing"
)

// values returns just the candidate strings, for readable assertions.
func values(c *Completion) []string {
	out := make([]string, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		out = append(out, cand.Value)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// The cursor position, not the whole string, decides what can be typed.
func TestCompletionContexts(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		pos     int
		context Context
		field   string
	}{
		{"empty query", "", 0, ContextField, ""},
		{"partial field", "fro", 3, ContextField, ""},
		{"after a colon", "from:", 5, ContextValue, "from"},
		{"partial value", "from:al", 7, ContextValue, "from"},
		{"second term", "is:unread fr", 12, ContextField, ""},
		{"after a pipe", "| ", 2, ContextStage, ""},
		{"partial stage", "| cou", 5, ContextStage, ""},
		{"group key", "| count by ", 11, ContextGroupKey, ""},
		{"group key, no by", "| count ", 8, ContextGroupKey, ""},
		{"sort column", "| sort ", 7, ContextSortKey, ""},
		{"sort direction", "| sort size ", 12, ContextSortKey, ""},
		{"bucket", "| series signups by ", 20, ContextBucket, ""},
		{"inside a group", "(from:", 6, ContextValue, "from"},
		// An alias resolves to the field it names, so its values still complete.
		{"aliased field", "sender:al", 9, ContextValue, "from"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Complete(c.src, c.pos)
			if got.Context != c.context {
				t.Errorf("context = %q, want %q", got.Context, c.context)
			}
			if got.Field != c.field {
				t.Errorf("field = %q, want %q", got.Field, c.field)
			}
		})
	}
}

// Completion runs on text that is mid-edit, which is exactly the text a parser
// is built to reject. It must never fail on any of it.
func TestCompletionNeverFailsOnPartialInput(t *testing.T) {
	for _, src := range []string{
		"", "-", "(", "from:", "from:(", `subject:"unclosed`, "| ", "|", "a OR",
		"from:x |", "--", ")", "| count by", "conf>", "extract:a.",
	} {
		for pos := 0; pos <= len(src); pos++ {
			c := Complete(src, pos)
			if c == nil {
				t.Fatalf("Complete(%q, %d) returned nil", src, pos)
			}
		}
	}

	// Out-of-range positions are clamped rather than panicking.
	if c := Complete("from:x", 99); c == nil {
		t.Error("a position past the end returned nil")
	}
	if c := Complete("from:x", -5); c == nil {
		t.Error("a negative position returned nil")
	}
}

func TestFieldCompletionFiltersByPrefix(t *testing.T) {
	c := Complete("fro", 3)
	got := values(c)
	if !contains(got, "from:") {
		t.Errorf("completing %q did not offer from: — got %v", "fro", got)
	}
	for _, v := range got {
		if !strings.HasPrefix(v, "fro") {
			t.Errorf("offered %q, which does not match the prefix", v)
		}
	}

	// A field with no prefix offers everything declared.
	if n := len(values(Complete("", 0))); n != len(Registry) {
		t.Errorf("an empty query offered %d fields, want %d", n, len(Registry))
	}
}

// An enum's values are known without a database, so they come back directly.
func TestEnumValuesCompleteWithoutTheStore(t *testing.T) {
	c := Complete("is:", 3)
	got := values(c)
	for _, want := range []string{"unread", "starred", "reply"} {
		if !contains(got, want) {
			t.Errorf("is: did not offer %q — got %v", want, got)
		}
	}

	c = Complete("is:un", 5)
	if got := values(c); len(got) != 1 || got[0] != "unread" {
		t.Errorf("is:un offered %v, want just unread", got)
	}
}

// Anything drawn from the user's mail is named rather than answered here.
func TestDataDrivenFieldsNameTheirSource(t *testing.T) {
	cases := map[string]string{
		"from:":      ValuesAddresses,
		"account:":   ValuesAccounts,
		"label:":     ValuesAnnotators,
		"saved:":     ValuesSaved,
		"mailbox:":   ValuesMailboxes,
		"unlabeled:": ValuesAnnotators,
	}
	for src, want := range cases {
		c := Complete(src, len(src))
		if c.ValueSource != want {
			t.Errorf("%s named source %q, want %q", src, c.ValueSource, want)
		}
	}

	// A field whose values are fixed asks for nothing.
	if c := Complete("is:", 3); c.ValueSource != "" {
		t.Errorf("is: named a value source %q; its values are declared", c.ValueSource)
	}
}

// me() is the answer to the most common question and the one nobody guesses.
func TestSelfFunctionIsOffered(t *testing.T) {
	if got := values(Complete("from:", 5)); !contains(got, "me()") {
		t.Errorf("from: did not offer me() — got %v", got)
	}
	if got := values(Complete("subject:", 8)); contains(got, "me()") {
		t.Error("subject: offered me(), which is meaningless for prose")
	}
}

// The replaced span has to cover the whole token, or completing mid-word
// duplicates what was already typed.
func TestReplacementSpan(t *testing.T) {
	// Cursor after "al" in "from:alice", with more text following.
	src := "from:alice after:7d"
	c := Complete(src, 7) // just past "al"

	if c.Prefix != "al" {
		t.Errorf("prefix = %q, want al", c.Prefix)
	}
	if got := src[c.Start:c.End]; got != "alice" {
		t.Errorf("replacing %q, want the whole value alice", got)
	}
}

// A negated term still completes, and says that it is negated.
func TestNegationIsReported(t *testing.T) {
	c := Complete("-from:al", 8)
	if !c.Negated {
		t.Error("a term beginning with - was not reported as negated")
	}
	if c.Field != "from" || c.Prefix != "al" {
		t.Errorf("field = %q prefix = %q, want from and al", c.Field, c.Prefix)
	}

	c = Complete("-fr", 3)
	if !c.Negated || c.Context != ContextField {
		t.Errorf("negated field completion: negated = %v context = %q", c.Negated, c.Context)
	}
}

// An unknown field has no values worth guessing at.
func TestUnknownFieldOffersNothing(t *testing.T) {
	c := Complete("nosuchfield:x", 13)
	if c.Context != ContextValue {
		t.Errorf("context = %q, want value", c.Context)
	}
	if len(c.Candidates) != 0 || c.ValueSource != "" {
		t.Errorf("offered %v from %q for an unknown field", values(c), c.ValueSource)
	}
}
