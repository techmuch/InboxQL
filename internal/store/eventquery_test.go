package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/calendar"
)

func TestEventQueries(t *testing.T) {
	setupTestDB(t)
	SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"})
	now := time.Now().Truncate(time.Hour).UTC()
	acme := calendar.Event{UID: "acme", Summary: "Acme sync", Start: now.Add(24 * time.Hour), End: now.Add(26 * time.Hour),
		Organizer: "me@example.com", Recurrence: []string{"RRULE:FREQ=WEEKLY;COUNT=3"},
		Attendees: []calendar.Attendee{{Address: "ada@acme.com", Status: "accepted"}, {Address: "me@example.com", Status: "accepted"}}}
	solo := calendar.Event{UID: "solo", Summary: "Dentist", Start: now.Add(48 * time.Hour), End: now.Add(49 * time.Hour), Location: "High St"}
	call := calendar.Event{UID: "call", Summary: "Vendor call", Start: now.Add(72 * time.Hour), End: now.Add(73 * time.Hour),
		ConferenceURL: "https://meet.google.com/abc", Attendees: []calendar.Attendee{{Address: "bob@vendor.com"}}}
	for _, ev := range []calendar.Event{acme, solo, call} {
		if _, err := UpsertEvent(LocalCalendarID, ev, EventMeta{}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		q    string
		kind string
		want int
	}{
		{"in:events next:30d", "events", 5},
		{"in:events attendee:*@acme.com", "events", 3},
		{"in:events organizer:me()", "events", 3},
		{"in:events -has:conference next:30d", "events", 4},
		{"in:events is:recurring", "events", 3},
		{"in:events location:high", "events", 1},
		{"in:events dentist", "events", 1},
		{"in:events duration>90", "events", 3},
		{"in:events | count", "count", 1},
		{"in:events | count by attendee", "groups", 3},
	}
	for _, c := range cases {
		res, err := RunQuery(c.q, 100, 0)
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		if res.Kind != c.kind || res.Count != c.want {
			t.Errorf("%s: kind %s count %d, want %s %d", c.q, res.Kind, res.Count, c.kind, c.want)
		}
	}
	res, err := RunQuery("in:events | sum hours", 0, 0)
	if err != nil || res.Total != 8 {
		t.Errorf("sum hours = %v %v, want 8 (3×2 + 1 + 1)", res, err)
	}
	res, err = RunQuery("in:events attendee:*@acme.com | sum hours by attendee", 0, 0)
	if err != nil || len(res.Groups) != 2 || res.Groups[0].Value != 6 {
		t.Errorf("hours by attendee = %+v %v", res, err)
	}
	// duration alone still means the log, as it always has.
	if res, err := RunQuery("duration>1000", 0, 0); err != nil || res.Kind != "logs" {
		t.Errorf("duration>1000 = %v %v", res, err)
	}
}
