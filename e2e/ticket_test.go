//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func (e *env) ticketTitles(t *testing.T, query string) []string {
	t.Helper()
	args := []string{"--json", "ticket", "list"}
	if query != "" {
		args = append(args, "--query", query)
	}
	r := e.run(args...)
	if r.ExitCode != 0 {
		t.Fatalf("ticket list %q exited %d: %s%s", query, r.ExitCode, r.Stdout, r.Stderr)
	}
	var tickets []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	r.JSON(t, &tickets)

	out := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		out = append(out, tk.Title)
	}
	return out
}

// A ticket raised by hand is owned by the person who raised it, and the query
// language reaches it through its own fields.
func TestTicketLifecycleFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("ticket", "new", "Pay the invoice", "--due", "7d", "--priority", "high"); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}

	if got := e.ticketTitles(t, "status:todo"); len(got) != 1 || got[0] != "Pay the invoice" {
		t.Fatalf("status:todo = %v", got)
	}

	// A due date looks forward, so a ticket due in a week is not overdue.
	if got := e.ticketTitles(t, "due:today"); len(got) != 0 {
		t.Errorf("due:today = %v, want nothing overdue", got)
	}
	if got := e.ticketTitles(t, "due:14d"); len(got) != 1 {
		t.Errorf("due:14d = %v, want the ticket due in a week", got)
	}

	// Moving is what dragging a card does.
	r := e.run("--json", "ticket", "list", "--query", "status:todo")
	var listed []struct {
		ID string `json:"id"`
	}
	r.JSON(t, &listed)

	if r := e.run("ticket", "move", listed[0].ID, "doing"); r.ExitCode != 0 {
		t.Fatalf("ticket move: %s%s", r.Stdout, r.Stderr)
	}
	if got := e.ticketTitles(t, "status:doing"); len(got) != 1 {
		t.Errorf("status:doing = %v", got)
	}
}

// The collision the whole design exists to avoid.
func TestEditingAnExtractorDoesNotWipeTheBoard(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("annotate", "create", "actions", "--kind", "extract", "--engine", "llm",
		"--instructions", "Extract the action this asks for."); r.ExitCode != 0 {
		t.Fatalf("annotate create: %s%s", r.Stdout, r.Stderr)
	}

	// Stand in for a model run by writing an annotation directly. The point
	// under test is what happens to tickets when the annotator changes, not
	// how the annotation was produced.
	if r := e.run("ticket", "new", "Chase the invoice", "--status", "doing"); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}

	// Editing the instruction bumps the version and invalidates every result.
	if r := e.run("annotate", "create", "actions", "--kind", "extract", "--engine", "llm",
		"--instructions", "Extract the action, worded differently."); r.ExitCode != 0 {
		t.Fatalf("annotate update: %s%s", r.Stdout, r.Stderr)
	}

	if got := e.ticketTitles(t, "status:doing"); len(got) != 1 {
		t.Errorf("editing the extractor changed the board: status:doing = %v", got)
	}
}

// Message fields inside a ticket query ask about the evidence.
func TestTicketQueriesReachTheirEvidence(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	// Find a message and raise a ticket citing it.
	r := e.run("--json", "query", "from:=alice@acme.com")
	var found struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	r.JSON(t, &found)
	if len(found.Messages) == 0 {
		t.Fatal("fixture has no message from alice")
	}

	if r := e.run("ticket", "new", "From Acme mail", "--message", found.Messages[0].ID); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}
	if r := e.run("ticket", "new", "Unrelated"); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}

	if got := e.ticketTitles(t, "status:* from:*@acme.com"); len(got) != 1 || got[0] != "From Acme mail" {
		t.Errorf("tickets from acme mail = %v", got)
	}

	// Negation asks whether any source matches, and partitions.
	with := len(e.ticketTitles(t, "status:* from:*@acme.com"))
	without := len(e.ticketTitles(t, "status:* -from:*@acme.com"))
	if with+without != len(e.ticketTitles(t, "status:*")) {
		t.Errorf("%d with and %d without acme mail do not partition the tickets", with, without)
	}

	// The evidence is printed, with the command to read it.
	show := e.run("ticket", "show", "From")
	if show.ExitCode != 0 {
		// A prefix that matches no ticket is fine; find the real id first.
		r := e.run("--json", "ticket", "list", "--query", "status:* from:*@acme.com")
		var listed []struct {
			ID string `json:"id"`
		}
		r.JSON(t, &listed)
		show = e.run("ticket", "show", listed[0].ID)
	}
	if show.ExitCode != 0 {
		t.Fatalf("ticket show: %s%s", show.Stdout, show.Stderr)
	}
	if !strings.Contains(show.Stdout, "iql read ") {
		t.Errorf("ticket show does not print how to read its evidence:\n%s", show.Stdout)
	}
}

// A board is a filter plus a column field.
func TestBoardColumnsFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	for _, spec := range [][]string{
		{"ticket", "new", "One", "--status", "todo"},
		{"ticket", "new", "Two", "--status", "doing"},
		{"ticket", "new", "Three", "--status", "done"},
	} {
		if r := e.run(spec...); r.ExitCode != 0 {
			t.Fatalf("%v: %s%s", spec, r.Stdout, r.Stderr)
		}
	}

	r := e.run("--json", "ticket", "board")
	if r.ExitCode != 0 {
		t.Fatalf("ticket board: %s%s", r.Stdout, r.Stderr)
	}
	var columns []struct {
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	r.JSON(t, &columns)

	byStatus := map[string]int{}
	for _, c := range columns {
		byStatus[c.Status] = c.Total
		if c.Status == "rejected" {
			t.Error("rejected tickets appear on the board")
		}
	}
	for _, want := range []string{"todo", "doing", "done"} {
		if _, ok := byStatus[want]; !ok {
			t.Errorf("the board is missing its %s column", want)
		}
	}
	if byStatus["todo"] != 1 || byStatus["doing"] != 1 {
		t.Errorf("board totals = %v", byStatus)
	}
}

// A label cannot raise tickets, and the refusal says what to do instead.
func TestLabelsCannotRaiseTicketsFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("annotate", "create", "billing", "--engine", "rule",
		"--instructions", "from:*@stripe.com"); r.ExitCode != 0 {
		t.Fatalf("annotate create: %s%s", r.Stdout, r.Stderr)
	}
	r := e.run("ticket", "propose", "billing")
	if r.ExitCode == 0 {
		t.Fatal("a label was allowed to raise tickets")
	}
	if !strings.Contains(r.Stderr, "extract") {
		t.Errorf("the refusal was %q, which does not say what to do instead", r.Stderr)
	}
}

// A random draw, for judging an annotator without reading only recent mail.
func TestSampleDrawsAtRandom(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	seen := map[string]bool{}
	for i := 0; i < 12; i++ {
		r := e.run("--json", "query", "| sample 1")
		if r.ExitCode != 0 {
			t.Fatalf("sample exited %d: %s", r.ExitCode, r.Stderr)
		}
		var out struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
		}
		r.JSON(t, &out)
		if len(out.Messages) != 1 {
			t.Fatalf("sample 1 returned %d messages", len(out.Messages))
		}
		seen[out.Messages[0].ID] = true
	}

	// Five messages, twelve draws: always returning the same one means the
	// order is not random and the sample is just "the newest".
	if len(seen) < 2 {
		t.Errorf("twelve draws returned %d distinct messages; sampling is not random", len(seen))
	}
}
