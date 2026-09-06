package store

import (
	"strings"
	"testing"
	"time"
)

func ticketIDs(t *testing.T, q string) []string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	if res.Kind != "tickets" {
		t.Fatalf("RunQuery(%q): kind = %q, want tickets", q, res.Kind)
	}
	out := make([]string, 0, len(res.Tickets))
	for _, tk := range res.Tickets {
		out = append(out, tk.Title)
	}
	return out
}

// seedExtractor stands in for an LLM run: annotations exist, tickets do not yet.
func seedExtractor(t *testing.T) *Annotator {
	t.Helper()

	a := mustAnnotator(t, &Annotator{
		Name: "actions", Kind: KindExtract, Engine: EngineLLM,
		Instructions: "Extract the action this message asks for.",
	})

	high, low := 0.95, 0.3
	if err := SaveAnnotations(a.ID, a.Version, "m1", []*Annotation{
		{Status: StatusOK, Source: SourceLLM, Confidence: &high,
			DataJSON: `{"title":"Pay the quarterly invoice","due":"7d","priority":"high"}`},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}
	if err := SaveAnnotations(a.ID, a.Version, "m3", []*Annotation{
		{Status: StatusOK, Source: SourceLLM, Confidence: &low,
			DataJSON: `{"title":"File the Stripe receipt"}`},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}
	return a
}

// Extraction proposes; it does not create. A task list you cannot trust is
// worse than none, so a low-confidence result waits to be accepted.
func TestExtractionProposesRatherThanCreating(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	out, err := ProposeTickets("actions", 0.9, false)
	if err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	if out.Proposed != 2 || out.Accepted != 1 {
		t.Fatalf("proposed %d accepted %d, want 2 and 1", out.Proposed, out.Accepted)
	}

	// The confident one is working; the unsure one is queued.
	if got := ticketIDs(t, "status:todo"); len(got) != 1 || got[0] != "Pay the quarterly invoice" {
		t.Errorf("status:todo = %v", got)
	}
	if got := ticketIDs(t, "status:proposed"); len(got) != 1 || got[0] != "File the Stripe receipt" {
		t.Errorf("status:proposed = %v", got)
	}
}

// The collision the design exists to avoid: annotations are versioned and
// invalidated when the instruction changes. If a ticket were an annotation,
// editing an extraction prompt would wipe the board.
func TestEditingThePromptDoesNotWipeTheBoard(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	before := ticketIDs(t, "status:*")
	if len(before) != 2 {
		t.Fatalf("expected 2 tickets, got %v", before)
	}

	// Someone moves one along, then edits the extractor's instruction.
	res, _ := RunQuery("status:todo", 10, 0)
	if err := SetTicketStatus(res.Tickets[0].ID, TicketDoing); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}
	updated := mustAnnotator(t, &Annotator{
		Name: "actions", Kind: KindExtract, Engine: EngineLLM,
		Instructions: "Extract the action, but differently this time.",
	})
	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}

	// Every annotation is now stale, and the board is untouched.
	if after := ticketIDs(t, "status:*"); len(after) != 2 {
		t.Errorf("a prompt edit changed the board: %v", after)
	}
	if got := ticketIDs(t, "status:doing"); len(got) != 1 {
		t.Errorf("the status someone set was lost: %v", got)
	}
}

// A re-run attaches evidence. It never rewrites state a person set.
func TestRerunsAttachEvidenceWithoutRewritingState(t *testing.T) {
	openQueryFixture(t)
	a := seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	res, _ := RunQuery("status:todo", 10, 0)
	ticket := res.Tickets[0]
	if err := SetTicketStatus(ticket.ID, TicketDoing); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}

	// A later message in the same conversation produces another annotation.
	// m5 is a reply to m1, so it shares a thread key.
	conf := 0.99
	if err := SaveAnnotations(a.ID, a.Version, "m5", []*Annotation{
		{Status: StatusOK, Source: SourceLLM, Confidence: &conf,
			DataJSON: `{"title":"Chase the invoice again"}`},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}

	out, err := ProposeTickets("actions", 0.9, false)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if out.Existing == 0 {
		t.Error("the re-run raised a duplicate rather than recognising the conversation")
	}

	after, err := GetTicket(ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if after.Status != TicketDoing {
		t.Errorf("the re-run reset the status to %q", after.Status)
	}
	if after.Title != ticket.Title {
		t.Errorf("the re-run rewrote the title to %q", after.Title)
	}
	if len(after.Sources) < 2 {
		t.Errorf("the new message was not attached as evidence: %d sources", len(after.Sources))
	}
}

// One ticket per conversation per annotator, so running twice is idempotent.
func TestProposingTwiceRaisesNothingNew(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	first, err := ProposeTickets("actions", 0.9, false)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ProposeTickets("actions", 0.9, false)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Proposed != 0 {
		t.Errorf("the second run raised %d more tickets, want 0", second.Proposed)
	}
	if second.Existing != first.Proposed {
		t.Errorf("recognised %d existing, want %d", second.Existing, first.Proposed)
	}
}

// A dry run reports and writes nothing.
func TestProposeDryRunWritesNothing(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	out, err := ProposeTickets("actions", 0.9, true)
	if err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	if out.Proposed != 2 {
		t.Errorf("dry run reported %d, want 2", out.Proposed)
	}
	if got := ticketIDs(t, "status:*"); len(got) != 0 {
		t.Errorf("a dry run wrote %v", got)
	}
}

// Provenance is the differentiator: a ticket knows the mail behind it.
func TestTicketsCarryTheirEvidence(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	res, _ := RunQuery("status:*", 10, 0)
	for _, tk := range res.Tickets {
		if len(tk.Sources) == 0 {
			t.Fatalf("ticket %q has no evidence", tk.Title)
		}
		if tk.Sources[0].Subject == "" {
			t.Errorf("evidence for %q carries no subject", tk.Title)
		}
	}
}

// A message field inside a ticket query is a question about the evidence:
// "tickets whose mail came from Acme", not "tickets that are from Acme".
func TestMessageFieldsQueryTheEvidence(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}

	// m1 is from alice@acme.com, m3 from billing@stripe.com.
	if got := ticketIDs(t, "status:* from:*@acme.com"); len(got) != 1 {
		t.Errorf("tickets from acme mail = %v, want 1", got)
	}
	if got := ticketIDs(t, "status:* from:*@stripe.com"); len(got) != 1 {
		t.Errorf("tickets from stripe mail = %v, want 1", got)
	}

	// Negation asks whether *any* source matches, not whether some source
	// fails to — the same anti-join distinction as recipients.
	pos := len(ticketIDs(t, "status:* from:*@acme.com"))
	neg := len(ticketIDs(t, "status:* -from:*@acme.com"))
	if pos+neg != len(ticketIDs(t, "status:*")) {
		t.Errorf("%d with and %d without acme mail do not partition the tickets", pos, neg)
	}
}

// Merging folds evidence together, which is how a thread-per-ticket default
// survives contact with a project that spans several conversations.
func TestMergeCombinesEvidence(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}
	res, _ := RunQuery("status:*", 10, 0)
	if len(res.Tickets) != 2 {
		t.Fatalf("expected 2 tickets")
	}
	into, from := res.Tickets[0], res.Tickets[1]

	if err := MergeTickets(into.ID, from.ID); err != nil {
		t.Fatalf("MergeTickets: %v", err)
	}
	merged, err := GetTicket(into.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if len(merged.Sources) != 2 {
		t.Errorf("merged ticket has %d sources, want 2", len(merged.Sources))
	}
	if gone, _ := GetTicket(from.ID); gone != nil {
		t.Error("the merged-from ticket still exists")
	}
}

// A due date looks forward. `--due 7d` meaning a week ago was a real bug: the
// ticket was born overdue and nothing said so.
func TestDueDatesLookForward(t *testing.T) {
	openQueryFixture(t)

	due, err := ParseTicketDue("7d")
	if err != nil {
		t.Fatalf("ParseTicketDue: %v", err)
	}
	if due.Before(time.Now()) {
		t.Errorf("--due 7d resolved to %s, which is in the past", due.Format("2006-01-02"))
	}

	tk := &Ticket{Title: "Renew", Status: TicketTodo, DueAt: due}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if got := ticketIDs(t, "due:14d"); len(got) != 1 {
		t.Errorf("due:14d = %v, want the ticket due in a week", got)
	}
	if got := ticketIDs(t, "due:today"); len(got) != 0 {
		t.Errorf("due:today = %v, want nothing overdue", got)
	}
}

// Closing stamps a time; reopening clears it, so a reopened ticket does not
// still look finished.
func TestClosingAndReopening(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{Title: "Something", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	if err := SetTicketStatus(tk.ID, TicketDone); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}
	done, _ := GetTicket(tk.ID)
	if done.ClosedAt == nil {
		t.Error("a done ticket carries no closed time")
	}

	if err := SetTicketStatus(tk.ID, TicketDoing); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}
	reopened, _ := GetTicket(tk.ID)
	if reopened.ClosedAt != nil {
		t.Error("a reopened ticket still carries a closed time")
	}
}

// A board is a filter plus a column field, and the two stay separate.
func TestBoardColumns(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}

	columns, err := BoardColumns("", 10)
	if err != nil {
		t.Fatalf("BoardColumns: %v", err)
	}

	byStatus := map[string]*BoardColumn{}
	for _, c := range columns {
		byStatus[c.Status] = c
		if c.Status == TicketRejected {
			t.Error("rejected tickets appear on the board; they are evidence, not work")
		}
	}
	if byStatus[TicketTodo] == nil || byStatus[TicketTodo].Total != 1 {
		t.Errorf("todo column = %+v", byStatus[TicketTodo])
	}
	// The defaults are present even before a ticket reaches them.
	for _, s := range DefaultStatuses {
		if byStatus[s] == nil {
			t.Errorf("the board is missing its %s column", s)
		}
	}

	// The board's filter scopes which tickets appear, independently of the
	// columns. Nothing came from Acme in the proposed column.
	scoped, err := BoardColumns("from:*@stripe.com", 10)
	if err != nil {
		t.Fatalf("BoardColumns: %v", err)
	}
	total := int64(0)
	for _, c := range scoped {
		total += c.Total
	}
	if total != 1 {
		t.Errorf("a board scoped to stripe mail holds %d tickets, want 1", total)
	}
}

// A label cannot extract records, and saying so at definition time beats
// failing halfway through a run.
func TestLabelsCannotRaiseTickets(t *testing.T) {
	openQueryFixture(t)

	mustAnnotator(t, &Annotator{
		Name: "urgent", Kind: KindLabel, Engine: EngineRule, Instructions: "from:x",
	})
	_, err := ProposeTickets("urgent", 0.9, false)
	if err == nil {
		t.Fatal("a label was allowed to raise tickets")
	}
	if !strings.Contains(err.Error(), "extract") {
		t.Errorf("the error was %q, which does not say what to do instead", err)
	}
}
