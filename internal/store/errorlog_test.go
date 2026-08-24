package store

import (
	"strings"
	"testing"
	"unicode"
)

// A MIME parser handed a corrupt message quotes the offending bytes back, and
// those bytes came from an email. Storing them raw means escape sequences in
// terminal output and unreadable rows in the UI.
func TestSanitiseStripsControlBytes(t *testing.T) {
	raw := "malformed header: \x00\x01\x02\x1b[31mred\x1b[0m\x07"
	got := sanitiseForLog(raw)

	for _, r := range got {
		if r != '\n' && r != '\t' && !unicode.IsPrint(r) {
			t.Errorf("non-printable rune %q survived sanitisation of %q", r, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Error("an ANSI escape survived; terminal output would be corrupted")
	}
	// The legible part must still be there — sanitising should not destroy the
	// diagnostic.
	if !strings.Contains(got, "malformed header") {
		t.Errorf("useful text was lost: %q", got)
	}
}

func TestSanitiseKeepsNewlinesAndTabs(t *testing.T) {
	got := sanitiseForLog("line one\nline two\tindented")
	if !strings.Contains(got, "\n") || !strings.Contains(got, "\t") {
		t.Errorf("structure was stripped: %q", got)
	}
}

// One pathological error must not be able to bloat the database.
func TestSanitiseTruncates(t *testing.T) {
	got := sanitiseForLog(strings.Repeat("a", maxLoggedMessage*3))
	if len(got) > maxLoggedMessage+8 {
		t.Errorf("stored %d bytes, want about %d", len(got), maxLoggedMessage)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("truncation is not marked, so the text looks complete")
	}
}

func TestSanitiseKeepsUnicode(t *testing.T) {
	got := sanitiseForLog("café — naïve 日本語")
	if got != "café — naïve 日本語" {
		t.Errorf("legitimate unicode was mangled: %q", got)
	}
}

func TestSanitiseTrimsSurroundingSpace(t *testing.T) {
	if got := sanitiseForLog("  padded  "); got != "padded" {
		t.Errorf("got %q, want %q", got, "padded")
	}
}
