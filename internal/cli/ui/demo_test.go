package ui

import (
	"os"
	"testing"
)

// Not an assertion — a way to eyeball the rendering. Run with:
//
//	go test ./internal/cli/ui -run Demo -v
func TestDemoRendering(t *testing.T) {
	p := NewWithColour(os.Stdout, true)
	tb := p.NewTable("ID", "STATUS", "TO", "SUBJECT")
	tb.Row("5f27877d-bf7d-4e31-b808-f07326f1ae5a", tb.Cell(Warn, "draft"), "c@d.com", "Another draft")
	tb.Row("i1", tb.Cell(OK, "sent"), "a@b.com", Truncate("A much longer subject line than any column guess allowed for", 40))
	tb.Flush()
	p.Printf("\n")
	p.Status(OK, "database", "schema v14")
	p.Status(Warn, "llm provider", "not configured")
	p.Status(Bad, "imap", "host not found")
	p.Status(Absent, "apple-mail", "not installed")
}
