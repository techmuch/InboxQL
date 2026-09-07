//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

type timelineEntry struct {
	Kind    string `json:"kind"`
	At      string `json:"at"`
	Actor   string `json:"actor"`
	Summary string `json:"summary"`
}

type timelineThread struct {
	Key          string          `json:"key"`
	Subject      string          `json:"subject"`
	Participants []string        `json:"participants"`
	Entries      []timelineEntry `json:"entries"`
	MessageCount int             `json:"messageCount"`
	TicketCount  int             `json:"ticketCount"`
	DraftCount   int             `json:"draftCount"`
}

func (e *env) timeline(t *testing.T, expr string) []timelineThread {
	t.Helper()
	r := e.run("--json", "query", expr)
	if r.ExitCode != 0 {
		t.Fatalf("query %q exited %d: %s%s", expr, r.ExitCode, r.Stdout, r.Stderr)
	}
	var out struct {
		Kind    string           `json:"kind"`
		Threads []timelineThread `json:"threads"`
	}
	r.JSON(t, &out)
	if out.Kind != "threads" {
		t.Fatalf("query %q returned kind %q, want threads", expr, out.Kind)
	}
	return out.Threads
}

// messageID resolves a query to the single message it matches.
//
// Imported mail gets generated row ids, so a test cannot name one literally —
// it has to find it the way a user would, by asking for it.
func (e *env) messageID(t *testing.T, expr string) string {
	t.Helper()
	r := e.run("--json", "query", expr)
	if r.ExitCode != 0 {
		t.Fatalf("query %q exited %d: %s%s", expr, r.ExitCode, r.Stdout, r.Stderr)
	}
	var found struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	r.JSON(t, &found)
	if len(found.Messages) != 1 {
		t.Fatalf("query %q matched %d messages, want exactly 1", expr, len(found.Messages))
	}
	return found.Messages[0].ID
}

// The whole feature, through the real binary: a conversation and the ticket
// raised from it, in one time-ordered view.
func TestTimelineShowsAConversationAndWhatItCaused(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	// m1 and m4 are one conversation; m5 shares the subject but has no
	// References, so it must stay a conversation of its own.
	threads := e.timeline(t, "subject:\"Quarterly invoice\" | timeline")
	if len(threads) != 2 {
		t.Fatalf("got %d conversations, want 2 (the real thread and the lookalike)", len(threads))
	}

	var main *timelineThread
	for i := range threads {
		if threads[i].MessageCount == 2 {
			main = &threads[i]
		}
	}
	if main == nil {
		t.Fatalf("no conversation holds both the invoice and its reply: %+v", threads)
	}
	if len(main.Participants) != 3 {
		t.Errorf("participants = %v, want alice, bob and me", main.Participants)
	}

	// Raise a ticket against that conversation and move it.
	if r := e.run("ticket", "new", "Pay the quarterly invoice", "--thread", main.Key); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}
	ids := e.ticketIDs(t, "status:todo")
	if len(ids) != 1 {
		t.Fatalf("expected one ticket, got %v", ids)
	}
	if r := e.run("ticket", "move", ids[0], "doing"); r.ExitCode != 0 {
		t.Fatalf("ticket move: %s%s", r.Stdout, r.Stderr)
	}

	alice := e.messageID(t, "from:=alice@acme.com")
	threads = e.timeline(t, "thread:"+alice+" | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d conversations, want 1", len(threads))
	}
	th := threads[0]
	if th.TicketCount != 1 {
		t.Fatalf("conversation holds %d tickets, want 1", th.TicketCount)
	}

	// The ticket appears as its history: raised, then moved. That narrative is
	// the reason ticket_events exists — a card alone cannot say when work
	// started.
	var moves []string
	for _, entry := range th.Entries {
		if entry.Kind == "event" {
			moves = append(moves, entry.Summary)
		}
	}
	if len(moves) != 2 {
		t.Fatalf("ticket history on the timeline = %v, want raised then moved", moves)
	}
	if !strings.Contains(moves[0], "raised") {
		t.Errorf("first moment is %q, want the ticket being raised", moves[0])
	}
	if !strings.Contains(moves[1], "doing") {
		t.Errorf("second moment is %q, want the move to doing", moves[1])
	}

	// Time-ordered, which is the only ordering that answers "what happened".
	for i := 1; i < len(th.Entries); i++ {
		if th.Entries[i].At < th.Entries[i-1].At {
			t.Fatalf("entry %d (%s) precedes entry %d (%s)",
				i, th.Entries[i].At, i-1, th.Entries[i-1].At)
		}
	}
}

// The query the stage exists for: open work, each with the conversation that
// caused it. It reads the same join in the other direction.
func TestTimelineFromTicketsBackToTheMail(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	alice := e.messageID(t, "from:=alice@acme.com")
	threads := e.timeline(t, "thread:"+alice+" | timeline")
	key := threads[0].Key

	if r := e.run("ticket", "new", "Pay the quarterly invoice", "--thread", key); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}
	// A second ticket with no conversation at all. It must still appear:
	// dropping rows a view cannot place is how a broken view looks healthy.
	if r := e.run("ticket", "new", "Renew the domain"); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}

	threads = e.timeline(t, "status:todo | timeline")
	if len(threads) != 2 {
		t.Fatalf("got %d conversations, want 2", len(threads))
	}

	withMail, without := 0, 0
	for _, th := range threads {
		if th.MessageCount > 0 {
			withMail++
			if th.Subject != "Quarterly invoice" {
				t.Errorf("thread subject = %q, want the conversation's", th.Subject)
			}
		} else {
			without++
			if th.Subject != "Renew the domain" {
				t.Errorf("conversationless ticket is named %q, want its own title", th.Subject)
			}
		}
	}
	if withMail != 1 || without != 1 {
		t.Errorf("got %d threads with mail and %d without, want 1 and 1", withMail, without)
	}
}

// The terminal rendering is nested, because flattening a timeline into a table
// loses the one thing it exists to show.
func TestTimelinePrintsAsIndentedConversations(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	alice := e.messageID(t, "from:=alice@acme.com")
	r := e.run("query", "thread:"+alice+" | timeline")
	if r.ExitCode != 0 {
		t.Fatalf("query exited %d: %s%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	out := r.Stdout

	if !strings.Contains(out, "Quarterly invoice") {
		t.Errorf("no conversation heading in:\n%s", out)
	}
	if !strings.Contains(out, "  mail  ") {
		t.Errorf("no indented mail entries in:\n%s", out)
	}
	if !strings.Contains(out, "alice@acme.com") || !strings.Contains(out, "bob@acme.com") {
		t.Errorf("both correspondents should appear in:\n%s", out)
	}
	if !strings.Contains(out, "1 conversation") {
		t.Errorf("no summary line in:\n%s", out)
	}
}

// ticketIDs returns the ids of tickets a query matches.
func (e *env) ticketIDs(t *testing.T, query string) []string {
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
		ID string `json:"id"`
	}
	r.JSON(t, &tickets)

	out := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		out = append(out, tk.ID)
	}
	return out
}

// Raising a ticket from a message must file it in that message's conversation.
// Attaching the evidence without setting the key left the ticket invisible on
// its own thread's timeline.
func TestTicketRaisedFromAMessageJoinsItsConversation(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	alice := e.messageID(t, "from:=alice@acme.com")
	if r := e.run("ticket", "new", "Pay the quarterly invoice", "--message", alice); r.ExitCode != 0 {
		t.Fatalf("ticket new: %s%s", r.Stdout, r.Stderr)
	}

	threads := e.timeline(t, "thread:"+alice+" | timeline")
	if len(threads) != 1 {
		t.Fatalf("got %d conversations, want 1", len(threads))
	}
	if threads[0].TicketCount != 1 {
		t.Fatalf("the ticket did not land on its own conversation: %+v", threads[0])
	}
	if threads[0].MessageCount != 2 {
		t.Errorf("conversation holds %d messages, want the invoice and its reply",
			threads[0].MessageCount)
	}
}

// The Threads toggle is a stage in the query, not a display mode beside it.
// A view flag kept alongside the query is the shape that has produced a bar
// showing one thing while the server ran another, three times now.
func TestThreadsToggleIsQueryComposition(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	srv := e.startServer()

	getJSON := func(path string, out any) {
		t.Helper()
		resp := srv.browserRequest(t, "GET", path, "",
			map[string]string{"Sec-Fetch-Site": "same-origin"})
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s returned %d", path, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
	}

	compose := func(params string) string {
		var out struct {
			Query string `json:"query"`
		}
		getJSON("/api/query/compose?"+params, &out)
		return out.Query
	}

	if got := compose("q=folder%3Ainbox&stage=timeline"); got != "folder:inbox | timeline" {
		t.Errorf("turning it on gave %q", got)
	}
	if got := compose("q=folder%3Ainbox+%7C+timeline&dropStage=timeline"); got != "folder:inbox" {
		t.Errorf("turning it off gave %q", got)
	}
	// A query can end in only one aggregate, so the toggle replaces rather
	// than appends — otherwise it would build something the planner rejects.
	if got := compose("q=folder%3Ainbox+%7C+count+by+week&stage=timeline"); got != "folder:inbox | timeline" {
		t.Errorf("toggling over an aggregate gave %q", got)
	}

	// And the composed query is one the server will actually run.
	var res struct {
		Kind    string           `json:"kind"`
		Threads []timelineThread `json:"threads"`
	}
	getJSON("/api/query?q=folder%3Ainbox+%7C+timeline", &res)
	if res.Kind != "threads" {
		t.Fatalf("kind = %q, want threads", res.Kind)
	}
	if len(res.Threads) == 0 {
		t.Fatal("no conversations came back")
	}
}
