// Package ui renders the human-facing half of the CLI.
//
// It exists because the alternative had drifted badly: every command
// hand-rolled its own column widths with printf verbs — eight different
// widths across the tree — and invented its own status vocabulary, so the
// same idea appeared as `[  ok  ]`, `[ ready ]`, `[ FAIL ]`, `[blocked]`,
// `[ absent]` and a bare `*` depending on which file you were reading. Fixed
// widths also meant any value longer than the guess either broke the
// alignment or was silently cut.
//
// Everything here writes plain text when the destination is not a terminal,
// so piping into grep, awk or a log file gets exactly what it always did.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI escapes, used only when Enabled reports true for the destination.
const (
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
	red       = "\033[31m"
	green     = "\033[32m"
	yellow    = "\033[33m"
	blue      = "\033[34m"
	cyanColor = "\033[36m"
)

// Enabled reports whether w should receive colour.
//
// Three things must all hold: the writer is the real stdout, that stdout is a
// terminal, and the environment has not asked us to stop. NO_COLOR is honoured
// as an unset-or-not check per the convention at https://no-color.org, and
// TERM=dumb covers the terminals that cannot render escapes at all.
func Enabled(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Printer renders styled output to one destination.
//
// Colour is resolved once, when the printer is built, rather than at each call
// site — so a command cannot accidentally colour half its output.
type Printer struct {
	w      io.Writer
	colour bool
}

// New builds a Printer for w, deciding colour from the destination.
func New(w io.Writer) *Printer { return &Printer{w: w, colour: Enabled(w)} }

// NewWithColour builds a Printer with colour forced on or off, for tests and
// for an explicit --colour flag.
func NewWithColour(w io.Writer, colour bool) *Printer {
	return &Printer{w: w, colour: colour}
}

// Writer exposes the underlying destination.
func (p *Printer) Writer() io.Writer { return p.w }

// Colour reports whether this printer emits escapes.
func (p *Printer) Colour() bool { return p.colour }

func (p *Printer) paint(code, s string) string {
	if !p.colour || s == "" {
		return s
	}
	return code + s + reset
}

// Bold, Dim and the colour helpers return s wrapped in the relevant escape,
// or s unchanged when colour is off.
func (p *Printer) Bold(s string) string   { return p.paint(bold, s) }
func (p *Printer) Dim(s string) string    { return p.paint(dim, s) }
func (p *Printer) Red(s string) string    { return p.paint(red, s) }
func (p *Printer) Green(s string) string  { return p.paint(green, s) }
func (p *Printer) Yellow(s string) string { return p.paint(yellow, s) }
func (p *Printer) Blue(s string) string   { return p.paint(blue, s) }
func (p *Printer) Cyan(s string) string   { return p.paint(cyanColor, s) }

// Printf writes formatted text with no styling of its own.
func (p *Printer) Printf(format string, args ...any) {
	fmt.Fprintf(p.w, format, args...)
}

// Status is the outcome of one checked thing.
//
// One vocabulary for every command that reports a list of outcomes — doctor's
// checks, `account verify`, `import sources` — so the same state always looks
// the same wherever it appears.
type Status int

const (
	// OK is a thing that is working.
	OK Status = iota
	// Warn is a thing that works but wants attention.
	Warn
	// Bad is a thing that is broken or unavailable.
	Bad
	// Absent is a thing that is simply not here, which is not a failure.
	Absent
	// Note is neutral information in a list of outcomes.
	Note
)

// label is the fixed-width word for a status. Padded to a common width so
// labels line up without the caller counting spaces.
func (s Status) label() string {
	switch s {
	case OK:
		return "ok    "
	case Warn:
		return "warn  "
	case Bad:
		return "fail  "
	case Absent:
		return "absent"
	default:
		return "      "
	}
}

func (p *Printer) colourFor(s Status, text string) string {
	switch s {
	case OK:
		return p.Green(text)
	case Warn:
		return p.Yellow(text)
	case Bad:
		return p.Red(text)
	case Absent:
		return p.Dim(text)
	default:
		return text
	}
}

// Status writes one outcome line: a coloured label, a subject, and detail.
//
//	ok      database          schema v14
//	fail    imap              host not found
func (p *Printer) Status(s Status, subject, detail string) {
	fmt.Fprintf(p.w, "%s  %-22s %s\n", p.colourFor(s, s.label()), subject, detail)
}

// styleMark fences a styled run inside a cell so the table can measure the
// visible text and colour it separately. It never reaches the terminal.
const styleMark = "\x00"

// tabwriter was the obvious choice here and it is the wrong one: it measures
// cells in bytes, so an ANSI sequence is counted as visible width. Its Escape
// mechanism is meant to solve exactly that, but fencing either the escapes or
// the whole cell still perturbed the column width in practice — a coloured
// table and a plain one did not line up. Widths are therefore computed here
// from the visible runes and the padding is emitted directly, which is both
// predictable and the only version that passes
// TestColourDoesNotChangeLayout.

// Table accumulates rows and writes them aligned on Flush.
//
// Columns size themselves to their contents, which is the entire point:
// nothing here needs a width guessed in advance, so a long account id or a
// UUID widens its column instead of breaking the layout.
type Table struct {
	p       *Printer
	headers []string
	rows    [][]string
}

// NewTable starts a table with the given column headings.
//
// Headings are conventionally short and upper-case; they are dimmed rather
// than bolded so the data stays the brightest thing on screen.
func (p *Printer) NewTable(headers ...string) *Table {
	return &Table{p: p, headers: headers}
}

// Row appends one row. Extra cells are dropped and missing ones are blank, so
// a ragged call site cannot panic mid-listing.
func (t *Table) Row(cells ...string) {
	out := make([]string, len(t.headers))
	for i := range out {
		if i < len(cells) {
			out[i] = cells[i]
		}
	}
	t.rows = append(t.rows, out)
}

// mark wraps text so Flush can colour it without counting the escape as width.
func (t *Table) mark(code, text string) string {
	if !t.p.colour || text == "" {
		return text
	}
	return styleMark + code + styleMark + text + styleMark
}

// Cell styles one data cell by status. Callers that colour a cell must route
// it through here rather than through Red/Dim/Bold, so the width stays right.
func (t *Table) Cell(s Status, text string) string {
	switch s {
	case OK:
		return t.mark(green, text)
	case Warn:
		return t.mark(yellow, text)
	case Bad:
		return t.mark(red, text)
	case Absent:
		return t.mark(dim, text)
	default:
		return text
	}
}

// Emphasise styles a data cell bold, for the one value in a row the reader is
// scanning for.
func (t *Table) Emphasise(text string) string { return t.mark(bold, text) }

// visible returns the cell text with any style markers removed, which is what
// the column width is measured against.
func visible(cell string) string {
	if !strings.Contains(cell, styleMark) {
		return cell
	}
	parts := strings.Split(cell, styleMark)
	// parts alternates: before, code, text, after...
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 0 {
			b.WriteString(part)
		}
	}
	return b.String()
}

// render returns the cell with style markers turned into real escapes.
func render(cell string) string {
	if !strings.Contains(cell, styleMark) {
		return cell
	}
	parts := strings.Split(cell, styleMark)
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			b.WriteString(part) // the escape code
		} else {
			b.WriteString(part)
		}
	}
	return b.String() + reset
}

// Rows reports how many data rows have been added.
func (t *Table) Rows() int { return len(t.rows) }

// Flush writes the aligned table.
//
// Padding is computed from the visible rune count, never the byte length, so
// colour and non-ASCII text both leave the layout untouched. The final column
// is not padded, so no line carries trailing whitespace.
func (t *Table) Flush() error {
	if len(t.rows) == 0 {
		return nil
	}

	all := make([][]string, 0, len(t.rows)+1)
	if len(t.headers) > 0 {
		head := make([]string, len(t.headers))
		for i, h := range t.headers {
			head[i] = t.mark(dim, h)
		}
		all = append(all, head)
	}
	all = append(all, t.rows...)

	widths := make([]int, len(t.headers))
	for _, row := range all {
		for i, cell := range row {
			if n := len([]rune(visible(cell))); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	for _, row := range all {
		for i, cell := range row {
			b.WriteString(render(cell))
			if i < len(row)-1 {
				pad := widths[i] - len([]rune(visible(cell))) + 2
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(t.p.w, b.String())
	return err
}

// Truncate shortens s to at most n runes, marking the cut with an ellipsis.
//
// Tables size themselves, so this is for values with no useful upper bound —
// a subject line or an error message — rather than for holding a layout
// together.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
