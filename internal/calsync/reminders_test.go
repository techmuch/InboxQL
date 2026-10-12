package calsync

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/store"
)

func TestDueReminders(t *testing.T) {
	open(t)
	now := time.Now().Truncate(time.Second)
	mk := func(uid string, start time.Time, alarm int, meta store.EventMeta) {
		if _, err := store.UpsertEvent(store.LocalCalendarID, calendar.Event{UID: uid, Summary: uid, Start: start,
			End: start.Add(time.Hour), Alarms: []int{alarm}}, meta); err != nil {
			t.Fatal(err)
		}
	}
	mk("due", now.Add(5*time.Minute), 10, store.EventMeta{})                          // came due 5 minutes ago
	mk("asleep", now.Add(-20*time.Minute), 10, store.EventMeta{})                     // came due 30 minutes ago
	mk("later", now.Add(2*time.Hour), 10, store.EventMeta{})                          // not yet
	mk("declined", now.Add(5*time.Minute), 10, store.EventMeta{MyStatus: "declined"}) // not going
	mk("proposed", now.Add(5*time.Minute), 10, store.EventMeta{Proposed: true})       // not real yet

	due, err := DueReminders(now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Summary != "due" || due[0].Minutes != 10 {
		t.Fatalf("due %+v", due)
	}
	if txt := due[0].Text(); txt == "" {
		t.Error("no text")
	}
	// Asked again for the same window, nothing new is due: the cursor moves.
	if again, _ := DueReminders(now, now.Add(time.Second)); len(again) != 0 {
		t.Errorf("delivered twice: %+v", again)
	}
}
