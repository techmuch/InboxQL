package calsync

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func open(t *testing.T) *Engine {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	return New()
}

func summaries(t *testing.T, from, to time.Time) []string {
	occ, _ := store.ListOccurrences(from, to, nil)
	var out []string
	for _, o := range occ {
		s := o.Summary
		if o.Proposed {
			s += " (proposed)"
		}
		out = append(out, s)
	}
	return out
}

func TestHumanAppliesAgentProposes(t *testing.T) {
	e := open(t)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	r, err := e.Create(store.LocalCalendarID, calendar.Event{Summary: "Lunch", Start: start, End: start.Add(time.Hour)}, OriginHuman)
	if err != nil || r.State != "applied" {
		t.Fatalf("%+v %v", r, err)
	}
	a, _ := e.Create(store.LocalCalendarID, calendar.Event{Summary: "Agent's idea", Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}, OriginAgent)
	if a.State != "proposed" {
		t.Fatalf("agent state %q", a.State)
	}
	got := strings.Join(summaries(t, start, start.Add(4*time.Hour)), "|")
	if got != "Lunch|Agent's idea (proposed)" {
		t.Errorf("got %s", got)
	}
	// An agent's edit changes nothing until approved.
	upd := calendar.Event{Summary: "Long lunch", Start: start, End: start.Add(2 * time.Hour)}
	p, _ := e.Update(r.EventID, ScopeAll, time.Time{}, upd, OriginAgent)
	if ev, _ := store.GetEvent(r.EventID); ev.Summary != "Lunch" {
		t.Errorf("proposed edit was applied: %q", ev.Summary)
	}
	if _, err := e.Approve(p.ChangeID); err != nil {
		t.Fatal(err)
	}
	if ev, _ := store.GetEvent(r.EventID); ev.Summary != "Long lunch" {
		t.Errorf("approved edit not applied: %q", ev.Summary)
	}
	if _, err := e.Approve(p.ChangeID); err == nil {
		t.Error("approved twice")
	}
	// Rejecting a proposed event removes it.
	if err := e.Reject(a.ChangeID); err != nil {
		t.Fatal(err)
	}
	if got := summaries(t, start, start.Add(4*time.Hour)); len(got) != 1 {
		t.Errorf("after reject %v", got)
	}
	// Invitations cannot be edited from here.
	if _, err := e.Create(store.MailCalendarID, calendar.Event{Summary: "x", Start: start, End: start}, OriginHuman); err != ErrReadOnly {
		t.Errorf("mail calendar writable: %v", err)
	}
}

func TestRecurringScopes(t *testing.T) {
	e := open(t)
	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour).UTC()
	week := 7 * 24 * time.Hour
	r, _ := e.Create(store.LocalCalendarID, calendar.Event{Summary: "Standup", Start: start, End: start.Add(time.Hour),
		Recurrence: []string{"RRULE:FREQ=WEEKLY;COUNT=6"}}, OriginHuman)
	window := func() []string { return summaries(t, start.Add(-time.Hour), start.Add(8*week)) }

	// This one only.
	if _, err := e.Update(r.EventID, ScopeThis, start.Add(week), calendar.Event{Summary: "Standup (moved)",
		Start: start.Add(week + 2*time.Hour), End: start.Add(week + 3*time.Hour)}, OriginHuman); err != nil {
		t.Fatal(err)
	}
	// Delete the third.
	if _, err := e.Delete(r.EventID, ScopeThis, start.Add(2*week), OriginHuman); err != nil {
		t.Fatal(err)
	}
	// This and following, from the fifth.
	if _, err := e.Update(r.EventID, ScopeFollowing, start.Add(4*week), calendar.Event{Summary: "Standup v2",
		Start: start.Add(4 * week), End: start.Add(4*week + 30*time.Minute)}, OriginHuman); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(window(), "|")
	want := "Standup|Standup (moved)|Standup|Standup v2|Standup v2"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestRespondByMailQueuesAReply(t *testing.T) {
	e := open(t)
	if err := store.SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	m := &message.Message{ID: "m1", AccountID: "acct", MessageID: "<inv@x>", ContentHash: "h1", Subject: "Invitation",
		From: "ada@example.com", Date: time.Now(), InternalDate: time.Now()}
	if err := store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour).UTC()
	id, _ := store.UpsertEvent(store.MailCalendarID, calendar.Event{UID: "inv", Summary: "Engine review", Start: start,
		End: start.Add(time.Hour), Organizer: "ada@example.com", Sequence: 1,
		Attendees: []calendar.Attendee{{Address: "me@example.com", Status: "needs-action"}}},
		store.EventMeta{SourceMessageID: "m1", Origin: "mail"})

	r, err := e.Respond(id, "accepted", "See you there", OriginHuman)
	if err != nil {
		t.Fatal(err)
	}
	if r.DraftID == "" || !strings.Contains(r.Note, "iql outbox approve "+r.DraftID) {
		t.Fatalf("result %+v", r)
	}
	ev, _ := store.GetEvent(id)
	if ev.MyStatus != "accepted" {
		t.Errorf("my status %q", ev.MyStatus)
	}
	d, err := store.GetDraft(r.DraftID)
	if err != nil || d.Status != store.DraftStatusQueued || d.To[0] != "ada@example.com" || d.InReplyTo != "<inv@x>" {
		t.Fatalf("draft %+v %v", d, err)
	}
	acc, _ := store.GetAccount("acct")
	msg, err := mailer.FromDraftWithCalendar(d, acc)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(msg.Render())
	for _, must := range []string{"multipart/alternative", "text/calendar; charset=utf-8; method=REPLY", "METHOD:REPLY",
		"PARTSTAT=ACCEPTED", "See you there", "SEQUENCE:1"} {
		if !strings.Contains(wire, must) {
			t.Errorf("rendered reply lacks %q:\n%s", must, wire)
		}
	}
	// An agent's answer is only a proposal.
	if r, _ := e.Respond(id, "declined", "", OriginAgent); r.State != "proposed" {
		t.Errorf("agent rsvp %q", r.State)
	}
	if ev, _ := store.GetEvent(id); ev.MyStatus != "accepted" {
		t.Errorf("agent changed the answer to %q", ev.MyStatus)
	}
}
