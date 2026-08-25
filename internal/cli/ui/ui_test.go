package ui

import (
	"bytes"
	"strings"
	"testing"
)

// A buffer is not a terminal, so nothing may be coloured. This is what keeps
// escape sequences out of pipes, logs and CI output.
func TestNoColourWhenNotATerminal(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf)
	if p.Colour() {
		t.Fatal("colour enabled for a non-terminal writer")
	}
	p.Printf("%s\n", p.Red("danger"))
	if strings.Contains(buf.String(), "\033") {
		t.Errorf("escape sequence leaked into a buffer: %q", buf.String())
	}
}

func TestNoColorEnvDisablesColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	if NewWithColour(&buf, true).Colour() != true {
		t.Fatal("explicit colour should still be honoured")
	}
	if Enabled(&buf) {
		t.Error("NO_COLOR did not disable colour")
	}
}

// Columns must size to the widest cell. The old hand-rolled printf widths are
// exactly what this replaces, so a value wider than any guess must still line
// up rather than push its neighbour out.
func TestTableSizesColumnsToContent(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewWithColour(&buf, false).NewTable("ID", "NAME")
	tbl.Row("a", "short")
	tbl.Row("a-very-long-identifier-indeed", "x")
	if err := tbl.Flush(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 rows: %q", len(lines), buf.String())
	}
	// The second column must start at the same offset on every line.
	at := func(line, col string) int { return strings.Index(line, col) }
	if a, b := at(lines[1], "short"), at(lines[2], "x"); a != b {
		t.Errorf("column two starts at %d and %d; the table is not aligned:\n%s", a, b, buf.String())
	}
	if !strings.Contains(lines[0], "NAME") {
		t.Errorf("header row missing: %q", lines[0])
	}
}

// tabwriter measures bytes, so a styled cell would be padded as though the
// escape sequence were visible text. Only the header line is styled, and only
// after joining, so alignment survives colour.
func TestColourDoesNotChangeLayout(t *testing.T) {
	var plain, coloured bytes.Buffer
	for _, tc := range []struct {
		buf    *bytes.Buffer
		colour bool
	}{{&plain, false}, {&coloured, true}} {
		tbl := NewWithColour(tc.buf, tc.colour).NewTable("ID", "NAME")
		tbl.Row("a", "short")
		tbl.Row("bbbbbbbb", "x")
		if err := tbl.Flush(); err != nil {
			t.Fatal(err)
		}
	}

	strip := func(s string) string {
		for _, esc := range []string{"\033[0m", "\033[1m", "\033[2m"} {
			s = strings.ReplaceAll(s, esc, "")
		}
		return s
	}
	if got, want := strip(coloured.String()), plain.String(); got != want {
		t.Errorf("colour changed the layout:\n coloured (stripped): %q\n plain:               %q", got, want)
	}
}

func TestTableTolueratesRaggedRows(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewWithColour(&buf, false).NewTable("A", "B", "C")
	tbl.Row("only-one")
	tbl.Row("a", "b", "c", "ignored")
	if err := tbl.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ignored") {
		t.Error("an extra cell was printed past the declared columns")
	}
	if tbl.Rows() != 2 {
		t.Errorf("Rows() = %d, want 2", tbl.Rows())
	}
}

func TestStatusLabelsShareAWidth(t *testing.T) {
	seen := map[int]bool{}
	for _, s := range []Status{OK, Warn, Bad, Absent, Note} {
		seen[len(s.label())] = true
	}
	if len(seen) != 1 {
		t.Errorf("status labels have differing widths %v; columns after them will not line up", seen)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("short string was altered: %q", got)
	}
	if got := Truncate("hello world", 8); got != "hello w…" {
		t.Errorf("Truncate = %q, want %q", got, "hello w…")
	}
	// Runes, not bytes: cutting mid-character would produce mojibake.
	if got := Truncate("héllo wörld", 6); len([]rune(got)) != 6 {
		t.Errorf("Truncate returned %d runes, want 6: %q", len([]rune(got)), got)
	}
}
