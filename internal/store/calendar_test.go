package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/calendar"
)

func weekly(uid string, start time.Time) calendar.Event {
	return calendar.Event{UID: uid, Summary: "Standup", Start: start, End: start.Add(30 * time.Minute),
		TZ: "Europe/London", Recurrence: []string{"RRULE:FREQ=WEEKLY;COUNT=4"},
		Attendees: []calendar.Attendee{{Address: "Ada@Example.com", Status: "accepted"}}}
}

func TestCalendarDefaultsExist(t *testing.T) {
	setupTestDB(t)
	cals, err := ListCalendars()
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 || cals[0].ID != LocalCalendarID || !cals[0].Writable() || cals[1].Writable() {
		t.Fatalf("calendars %+v", cals)
	}
}

func TestUpsertExpandsAndOverrides(t *testing.T) {
	setupTestDB(t)
	start := time.Now().Truncate(time.Hour).Add(24 * time.Hour).UTC()
	id, err := UpsertEvent(LocalCalendarID, weekly("s1", start), EventMeta{})
	if err != nil {
		t.Fatal(err)
	}
	occ, _ := ListOccurrences(start.Add(-time.Hour), start.Add(60*24*time.Hour), nil)
	if len(occ) != 4 || occ[0].EventID != id || !occ[0].Recurring {
		t.Fatalf("occurrences %d %+v", len(occ), occ)
	}
	// Move the second occurrence; re-upserting is idempotent.
	o := weekly("s1", start)
	o.Recurrence = nil
	o.RecurrenceID = start.Add(7 * 24 * time.Hour)
	o.Start, o.End = o.RecurrenceID.Add(2*time.Hour), o.RecurrenceID.Add(3*time.Hour)
	o.Summary = "Standup (moved)"
	for i := 0; i < 2; i++ {
		if _, err := UpsertEvent(LocalCalendarID, o, EventMeta{}); err != nil {
			t.Fatal(err)
		}
	}
	occ, _ = ListOccurrences(start.Add(-time.Hour), start.Add(60*24*time.Hour), nil)
	if len(occ) != 4 || occ[1].Summary != "Standup (moved)" || !occ[1].Start.Equal(o.Start) {
		t.Fatalf("after override %+v", occ)
	}
	ev, _ := GetEvent(id)
	if len(ev.Attendees) != 1 || ev.Attendees[0].Address != "ada@example.com" || ev.Repeats == "" {
		t.Errorf("event %+v", ev)
	}
	// Deleting the override restores the occurrence; deleting the series removes all.
	DeleteEvent(LocalCalendarID, "s1", o.RecurrenceID)
	occ, _ = ListOccurrences(start.Add(-time.Hour), start.Add(60*24*time.Hour), nil)
	if len(occ) != 4 || occ[1].Summary != "Standup" {
		t.Errorf("after deleting override %+v", occ)
	}
	DeleteEvent(LocalCalendarID, "s1", time.Time{})
	if occ, _ = ListOccurrences(start.Add(-time.Hour), start.Add(60*24*time.Hour), nil); len(occ) != 0 {
		t.Errorf("after deleting series %d", len(occ))
	}
}

// An invitation that is also in a synced calendar shows once, from there.
func TestMailCopyHiddenWhenSynced(t *testing.T) {
	setupTestDB(t)
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour).UTC()
	ev := calendar.Event{UID: "inv", Summary: "Review", Start: start, End: start.Add(time.Hour)}
	UpsertEvent(MailCalendarID, ev, EventMeta{Origin: "mail"})
	occ, _ := ListOccurrences(start.Add(-time.Hour), start.Add(2*time.Hour), nil)
	if len(occ) != 1 || occ[0].CalendarID != MailCalendarID {
		t.Fatalf("mail only %+v", occ)
	}
	SaveCalendar(CalendarInfo{ID: "g1", Provider: ProviderGoogle, Name: "Work", Access: "owner"})
	UpsertEvent("g1", ev, EventMeta{Origin: "sync"})
	occ, _ = ListOccurrences(start.Add(-time.Hour), start.Add(2*time.Hour), nil)
	if len(occ) != 1 || occ[0].CalendarID != "g1" {
		t.Fatalf("synced %+v", occ)
	}
}

func TestAccountSecretIsSealed(t *testing.T) {
	setupTestDB(t)
	if err := SaveCalendarAccount(CalendarAccount{ID: "a1", Provider: ProviderGoogle, Name: "Work",
		Config: map[string]string{"clientId": "x"}}, "refresh-token"); err != nil {
		t.Fatal(err)
	}
	var raw string
	db.QueryRow(`SELECT secret FROM calendar_accounts WHERE id = 'a1'`).Scan(&raw)
	if raw == "refresh-token" || raw == "" {
		t.Errorf("secret stored as %q", raw)
	}
	if s, _ := CalendarAccountSecret("a1"); s != "refresh-token" {
		t.Errorf("secret = %q", s)
	}
	// Saving without a secret keeps it.
	SaveCalendarAccount(CalendarAccount{ID: "a1", Provider: ProviderGoogle, Name: "Renamed"}, "")
	if s, _ := CalendarAccountSecret("a1"); s != "refresh-token" {
		t.Errorf("secret lost: %q", s)
	}
	accts, _ := ListCalendarAccounts()
	if len(accts) != 1 || accts[0].Name != "Renamed" || !accts[0].HasSecret {
		t.Errorf("accounts %+v", accts)
	}
}

func TestAgentChangesStartProposed(t *testing.T) {
	setupTestDB(t)
	id, _ := QueueCalendarChange(CalendarChange{CalendarID: LocalCalendarID, Op: "create", Origin: "agent"})
	c, _ := GetCalendarChange(id)
	if c.State != "proposed" {
		t.Errorf("agent change state %q", c.State)
	}
	id2, _ := QueueCalendarChange(CalendarChange{CalendarID: LocalCalendarID, Op: "create"})
	if c, _ := GetCalendarChange(id2); c.State != "pending" {
		t.Errorf("human change state %q", c.State)
	}
}

func setupTestDB(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
}
