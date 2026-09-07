package store

import (
	"strings"
	"testing"
	"time"
)

func runThreads(t *testing.T, q string) []*Thread {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	if res.Kind != "threads" {
		t.Fatalf("RunQuery(%q): kind = %q, want threads", q, res.Kind)
	}
	return res.Threads
}

func entryKinds(t *Thread) []string {
	out := make([]string, 0, len(t.Entries))
	for _, e := range t.Entries {
		out = append(out, e.Kind)
	}
	return out
}

func findThread(threads []*Thread, key string) *Thread {
	for _, t := range threads {
		if t.Key == key {
			return t
		}
	}
	return nil
}

// The point of the whole feature: a conversation and the ticket it caused,
// in one ordering.
func TestTimelineInterleavesAConversationWithWhatItCaused(t *testing.T) {
	openQueryFixture(t)

	// m1 and m5 are one conversation: m5's References points at m1.
	tk := &Ticket{ThreadKey: "root@acme.com", Title: "Pay the quarterly invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := AttachSource(tk.ID, "m1", ""); err != nil {
		t.Fatalf("AttachSource: %v", err)
	}
	if err := SetTicketStatus(tk.ID, TicketDone); err != nil {
		t.Fatalf("SetTicketStatus: %v", err)
	}

	d := &Draft{ID: "d1", AccountID: "acct", InReplyTo: "<root@acme.com>",
		Subject: "Re: Quarterly invoice attached", To: []string{"alice@acme.com"}}
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	threads := runThreads(t, "from:=alice@acme.com | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1: %v", len(threads), threads)
	}

	th := threads[0]
	if th.Key != "root@acme.com" {
		t.Errorf("thread key = %q, want root@acme.com", th.Key)
	}
	// Only alice's message matched the filter, but a timeline is about the
	// conversation, so its reply is there too.
	if th.MessageCount != 2 {
		t.Errorf("thread holds %d messages, want 2 (the filter matched one)", th.MessageCount)
	}
	if th.TicketCount != 1 || th.DraftCount != 1 {
		t.Errorf("thread holds %d tickets and %d drafts, want 1 and 1",
			th.TicketCount, th.DraftCount)
	}
	if th.Subject != "Quarterly invoice attached" {
		t.Errorf("thread subject = %q", th.Subject)
	}

	// Every kind is represented, and the ticket appears as its history rather
	// than as one undated card.
	kinds := strings.Join(entryKinds(th), ",")
	for _, want := range []string{EntryMessage, EntryEvent, EntryDraft} {
		if !strings.Contains(kinds, want) {
			t.Errorf("timeline has no %s entry: %s", want, kinds)
		}
	}

	// Ordered by time, which is the only ordering that answers "what happened".
	for i := 1; i < len(th.Entries); i++ {
		if th.Entries[i].At.Before(th.Entries[i-1].At) {
			t.Fatalf("entry %d (%s at %v) precedes entry %d (%s at %v)",
				i, th.Entries[i].Kind, th.Entries[i].At,
				i-1, th.Entries[i-1].Kind, th.Entries[i-1].At)
		}
	}

	// Both participants of the conversation, deduplicated and normalised.
	if len(th.Participants) < 3 {
		t.Errorf("participants = %v, want alice, bob and me", th.Participants)
	}
}

// The query that makes the stage worth having: open tickets, each with the
// conversation that caused it. It reads the join in the other direction.
func TestTimelineWorksFromTicketsBackToMail(t *testing.T) {
	openQueryFixture(t)

	tk := &Ticket{ThreadKey: "root@acme.com", Title: "Pay the quarterly invoice", Status: TicketTodo}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	threads := runThreads(t, "status:todo | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}
	if threads[0].MessageCount != 2 {
		t.Errorf("a ticket's thread pulled in %d messages, want the 2 that caused it",
			threads[0].MessageCount)
	}
	if threads[0].Subject != "Quarterly invoice attached" {
		t.Errorf("subject = %q, want the conversation's, not the ticket's", threads[0].Subject)
	}
}

// A ticket nobody raised from mail still has to appear. Silently omitting rows
// is the failure this project keeps re-learning: the view looks like it works.
func TestTimelineKeepsTicketsThatHaveNoConversation(t *testing.T) {
	openQueryFixture(t)

	standalone := &Ticket{Title: "Renew the domain", Status: TicketTodo}
	if err := SaveTicket(standalone); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	attached := &Ticket{ThreadKey: "root@acme.com", Title: "Pay the invoice", Status: TicketTodo}
	if err := SaveTicket(attached); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	threads := runThreads(t, "status:todo | timeline")
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2 (one of them conversationless)", len(threads))
	}

	lone := findThread(threads, standalone.ID)
	if lone == nil {
		t.Fatalf("the ticket with no conversation vanished; got keys %v",
			[]string{threads[0].Key, threads[1].Key})
	}
	if lone.MessageCount != 0 {
		t.Errorf("a conversationless ticket pulled in %d messages", lone.MessageCount)
	}
	if lone.Subject != "Renew the domain" {
		t.Errorf("its thread is named %q, want the ticket's own title", lone.Subject)
	}
}

// A key falls back to the row's own id, and the loader has to look up that
// branch only for rows that really have no key — otherwise one conversation
// could pull in an unrelated row that happens to share its id.
func TestTimelineFallbackDoesNotCrossWires(t *testing.T) {
	openQueryFixture(t)

	// m2 has no References, so its own message-id is its thread key. Give a
	// ticket the *message row id* as its own id, which is the collision the
	// two-branch match exists to prevent.
	if _, err := db.Exec(`
		INSERT INTO tickets (id, title, status, origin, created_at, updated_at)
		VALUES ('m2', 'Unrelated ticket that shares an id', 'todo', 'human', ?, ?)`,
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	threads := runThreads(t, "subject:Lunch | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}
	if threads[0].TicketCount != 0 {
		t.Errorf("the message's conversation absorbed %d unrelated tickets",
			threads[0].TicketCount)
	}
}

// Labels are the other thing a conversation accumulates, and seeing them in
// place is the point of running an annotator at all.
func TestTimelineShowsLabelsInPlace(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "billing", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:=alice@acme.com",
	})
	if err := SaveAnnotations(a.ID, a.Version, "m1", []*Annotation{
		{Status: StatusOK, Source: SourceRule, DataJSON: `{"label":"billing"}`},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}

	threads := runThreads(t, "from:=alice@acme.com | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}

	var found *ThreadEntry
	for i := range threads[0].Entries {
		if threads[0].Entries[i].Kind == EntryAnnotation {
			found = &threads[0].Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("no label on the timeline: %v", entryKinds(threads[0]))
	}
	if found.Actor != "billing" {
		t.Errorf("label is attributed to %q, want the annotator's name", found.Actor)
	}
}

// The stage groups; it does not list. Two conversations must not collapse into
// one row or fan out into one row per message.
func TestTimelineReturnsOneRowPerConversation(t *testing.T) {
	openQueryFixture(t)

	threads := runThreads(t, "in:messages | timeline")

	seen := map[string]bool{}
	total := 0
	for _, th := range threads {
		if seen[th.Key] {
			t.Errorf("conversation %q appeared twice", th.Key)
		}
		seen[th.Key] = true
		total += th.MessageCount
	}
	// Five messages in the fixture, two of which are one conversation.
	if total != 5 {
		t.Errorf("threads account for %d messages, want all 5", total)
	}
	if len(threads) != 4 {
		t.Errorf("got %d conversations, want 4 (m1+m5 are one)", len(threads))
	}
}
