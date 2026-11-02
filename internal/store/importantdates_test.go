package store

import (
	"testing"
	"time"
)

func TestImportantDatesRoundTrip(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)

	d := &ImportantDate{Title: "Passport expires", Date: "2027-01-14", LeadDays: 42}
	if err := SaveImportantDate(d); err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Origin != "human" || d.CreatedAt == 0 {
		t.Fatalf("defaults not filled: %+v", d)
	}
	if err := SaveImportantDate(&ImportantDate{Title: "Anniversary", Date: "2026-12-12", Yearly: true}); err != nil {
		t.Fatal(err)
	}
	got, err := ListImportantDates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Anniversary" || !got[0].Yearly || got[1].LeadDays != 42 {
		t.Fatalf("listed %+v", got)
	}

	d.Title = "Passport expires (renew)"
	if err := SaveImportantDate(d); err != nil {
		t.Fatal(err)
	}
	if err := DeleteImportantDate(got[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ = ListImportantDates()
	if len(got) != 1 || got[0].Title != "Passport expires (renew)" {
		t.Fatalf("after update and delete: %+v", got)
	}
}

func TestImportantDatesRefuseWhatIsWrong(t *testing.T) {
	for _, d := range []ImportantDate{
		{Title: "", Date: "2026-12-12"},
		{Title: "x", Date: "12/12/2026"},
		{Title: "x", Date: "2026-12-12", LeadDays: -1},
		{Title: "x", Date: "2026-12-12", RefKind: "folder"},
		{Title: "x", Date: "2026-12-12", Origin: "robot"},
	} {
		if err := d.validate(); err == nil {
			t.Errorf("accepted %+v", d)
		}
	}
}

func TestNextOccurrence(t *testing.T) {
	from := time.Date(2026, 11, 1, 15, 0, 0, 0, time.Local)
	once := ImportantDate{Date: "2026-11-10"}
	if got, ok := once.NextOccurrence(from); !ok || got.Day() != 10 {
		t.Errorf("one-off ahead: %v %v", got, ok)
	}
	if _, ok := (ImportantDate{Date: "2026-10-01"}).NextOccurrence(from); ok {
		t.Error("a passed one-off date has no next occurrence")
	}
	yearly := ImportantDate{Date: "2019-03-05", Yearly: true}
	if got, _ := yearly.NextOccurrence(from); got.Year() != 2027 || got.Month() != 3 {
		t.Errorf("yearly past this year: %v", got)
	}
	today := ImportantDate{Date: "2020-11-01", Yearly: true}
	if got, _ := today.NextOccurrence(from); got.Year() != 2026 {
		t.Errorf("yearly falling today counts today: %v", got)
	}
}

func TestTopicCountsOnAnEmptyMailbox(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)
	ts, err := TopicCounts(10)
	if err != nil || ts == nil || len(ts) != 0 {
		t.Fatalf("got %v, %v; want an empty list, not nil", ts, err)
	}
}
