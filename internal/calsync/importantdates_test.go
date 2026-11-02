package calsync

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/store"
)

func TestImportantDateReminders(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	must := func(d store.ImportantDate) {
		if err := store.SaveImportantDate(&d); err != nil {
			t.Fatal(err)
		}
	}
	must(store.ImportantDate{Title: "Q4 report", Date: "2026-11-10"})
	must(store.ImportantDate{Title: "Passport", Date: "2027-01-14", LeadDays: 42})
	must(store.ImportantDate{Title: "Suggested", Date: "2026-11-10", Origin: "agent"})

	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.Local) }
	window := func(from, to time.Time) []Reminder {
		got, err := dueImportantDates(from, to)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// A week before, at eight in the morning.
	got := window(at(2026, 11, 3, 7), at(2026, 11, 3, 9))
	if len(got) != 1 || got[0].Summary != "Q4 report" || got[0].Note != "in 7 days" {
		t.Fatalf("a week before: %+v", got)
	}
	// The suggestion reminds nobody, the morning of.
	got = window(at(2026, 11, 10, 7), at(2026, 11, 10, 9))
	if len(got) != 1 || got[0].Note != "today" {
		t.Fatalf("the morning of: %+v", got)
	}
	// The lead time starting, six weeks before.
	got = window(at(2026, 12, 3, 7), at(2026, 12, 3, 9))
	if len(got) != 1 || got[0].Summary != "Passport" || got[0].Minutes != 42*1440 {
		t.Fatalf("lead time: %+v", got)
	}
	// Nothing at noon.
	if got = window(at(2026, 11, 3, 11), at(2026, 11, 3, 13)); len(got) != 0 {
		t.Fatalf("noon: %+v", got)
	}
	if (Reminder{Summary: "Q4 report", Note: "in 7 days"}).Text() != "Q4 report — in 7 days" {
		t.Error("text")
	}
}
