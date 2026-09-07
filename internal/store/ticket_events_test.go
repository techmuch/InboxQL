package store

import (
	"testing"
	"time"
)

func eventKinds(t *testing.T, ticketID string) []string {
	t.Helper()
	events, err := TicketEvents(ticketID)
	if err != nil {
		t.Fatalf("TicketEvents: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A ticket records its present; the log records how it got there. Without this
// a timeline can say a ticket is done but never when it started.
func TestTicketHistoryRecordsMoves(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{Title: "Pay the invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	if err := SetTicketStatus(tk.ID, TicketDoing); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}
	if err := SetTicketStatus(tk.ID, TicketDone); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}

	want := []string{EventCreated, EventStatus, EventStatus}
	if got := eventKinds(t, tk.ID); !equalStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}

	events, _ := TicketEvents(tk.ID)
	if events[0].To != TicketTodo {
		t.Errorf("created event landed in %q, want %q", events[0].To, TicketTodo)
	}
	if events[1].From != TicketTodo || events[1].To != TicketDoing {
		t.Errorf("first move was %q -> %q, want todo -> doing", events[1].From, events[1].To)
	}
	if events[2].From != TicketDoing || events[2].To != TicketDone {
		t.Errorf("second move was %q -> %q, want doing -> done", events[2].From, events[2].To)
	}
}

// Saving a ticket without changing anything must not append to its history.
// A log that grows on every write is a log nobody can read.
func TestSavingWithoutChangesLogsNothing(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{Title: "Pay the invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := SaveTicket(tk); err != nil {
			t.Fatalf("SaveTicket: %v", err)
		}
	}

	if got := eventKinds(t, tk.ID); !equalStrings(got, []string{EventCreated}) {
		t.Errorf("three no-op saves produced %v", got)
	}
}

// Priority and due are the other two fields that change what is being asked
// for, so they are logged. Title and body are not: they describe the same work.
func TestPriorityAndDueAreLoggedButProseIsNot(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{Title: "Pay the invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	due := time.Date(2026, 3, 8, 0, 0, 0, 0, time.Local)
	tk.Priority = "high"
	tk.DueAt = &due
	tk.Title = "Pay the quarterly invoice"
	tk.Body = "Rewritten."
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	want := []string{EventCreated, EventPriority, EventDue}
	if got := eventKinds(t, tk.ID); !equalStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}

	events, _ := TicketEvents(tk.ID)
	if events[2].From != "" || events[2].To != "2026-03-08" {
		t.Errorf("due event was %q -> %q, want \"\" -> 2026-03-08", events[2].From, events[2].To)
	}
}

// A re-run attaches evidence for every message it already knows about. Logging
// those would bury the real history under a re-run's worth of noise.
func TestReattachingEvidenceIsNotAnEvent(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{Title: "Pay the invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := AttachSource(tk.ID, "m1", ""); err != nil {
			t.Fatalf("AttachSource: %v", err)
		}
	}
	if err := AttachSource(tk.ID, "m5", ""); err != nil {
		t.Fatalf("AttachSource: %v", err)
	}

	want := []string{EventCreated, EventEvidence, EventEvidence}
	if got := eventKinds(t, tk.ID); !equalStrings(got, want) {
		t.Errorf("history = %v, want %v (one per distinct message)", got, want)
	}
}

// Extraction proposing a ticket is attributed to the annotator, not to a
// person. A board that claims you raised what a model raised is lying about
// the one thing provenance exists to record.
func TestProposedTicketsAreAttributedToTheAnnotator(t *testing.T) {
	openQueryFixture(t)
	seedExtractor(t)

	if _, err := ProposeTickets("actions", 0.9, false); err != nil {
		t.Fatalf("ProposeTickets: %v", err)
	}

	res, err := RunQuery("in:tickets", 100, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Tickets) == 0 {
		t.Fatal("extraction raised no tickets")
	}

	for _, tk := range res.Tickets {
		events, err := TicketEvents(tk.ID)
		if err != nil {
			t.Fatalf("TicketEvents: %v", err)
		}
		if len(events) == 0 {
			t.Fatalf("ticket %q has no history", tk.Title)
		}
		if events[0].Kind != EventCreated {
			t.Errorf("ticket %q opens with %q, want created", tk.Title, events[0].Kind)
		}
		if events[0].Actor != OriginAnnotator {
			t.Errorf("ticket %q was attributed to %q, want %q",
				tk.Title, events[0].Actor, OriginAnnotator)
		}
	}
}

// Tickets that existed before the log did get the one event that is a fact
// rather than a guess. Inventing the moves in between would be worse than the
// gap it fills.
func TestExistingTicketsAreBackfilledWithTheirCreation(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	// A ticket written as v19 would have left it: no history at all.
	created := time.Date(2026, 3, 2, 9, 0, 0, 0, time.Local)
	if _, err := db.Exec(`
		INSERT INTO tickets (id, title, status, origin, created_at, updated_at)
		VALUES ('old', 'Raised before the log existed', 'doing', 'annotator', ?, ?)`,
		created.UnixMilli(), created.UnixMilli()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec("DELETE FROM ticket_events"); err != nil {
		t.Fatalf("clear events: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 19;"); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	CloseDB()

	// Reopening runs the migration, which is the path a real upgrade takes.
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(CloseDB)

	events, err := TicketEvents("old")
	if err != nil {
		t.Fatalf("TicketEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("backfill produced %d events, want exactly 1", len(events))
	}
	if events[0].Kind != EventCreated {
		t.Errorf("backfilled event is %q, want created", events[0].Kind)
	}
	// The status it is in now, not a fabricated todo -> doing move.
	if events[0].To != "doing" {
		t.Errorf("backfilled status is %q, want doing", events[0].To)
	}
	if !events[0].At.Equal(created) {
		t.Errorf("backfilled at %v, want the ticket's own created_at %v", events[0].At, created)
	}
	if events[0].Actor != OriginAnnotator {
		t.Errorf("backfilled actor is %q, want annotator", events[0].Actor)
	}
}

// A ticket raised with a due date had it set at that moment. Folding it into
// the "raised" line reads correctly until the date changes later, at which
// point that line shows today's value at the original time.
func TestCreatingWithADueDateLogsIt(t *testing.T) {
	openQueryFixture(t)

	due := time.Date(2026, 3, 8, 0, 0, 0, 0, time.Local)
	tk := &Ticket{Title: "Pay the invoice", Status: TicketTodo, Priority: "high", DueAt: &due}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	want := []string{EventCreated, EventPriority, EventDue}
	if got := eventKinds(t, tk.ID); !equalStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}

	events, _ := TicketEvents(tk.ID)
	if events[2].From != "" || events[2].To != "2026-03-08" {
		t.Errorf("due event = %q -> %q", events[2].From, events[2].To)
	}
	// And a later change is one more line, not a rewrite of the first.
	moved := due.AddDate(0, 0, 7)
	tk.DueAt = &moved
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	events, _ = TicketEvents(tk.ID)
	if len(events) != 4 {
		t.Fatalf("after moving the date there are %d events, want 4", len(events))
	}
	if events[3].From != "2026-03-08" || events[3].To != "2026-03-15" {
		t.Errorf("the move reads %q -> %q", events[3].From, events[3].To)
	}
}
