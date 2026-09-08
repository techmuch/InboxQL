package store

import (
	"os"
	"testing"
)

// The dashboard's widgets are queries now.
//
// These used to be GetTemporalVolume, GetTopSenders and GetTopicStats, three
// functions with three copies of the filter logic that disagreed about what
// `from` meant. What is asserted here is not that the SQL looks a particular
// way — the old tests did that, and passed while the three implementations
// diverged — but that the results are right.
func TestDashboardWidgetsAreQueries(t *testing.T) {
	openQueryFixture(t)

	volume, err := RunQuery("| count by day", 0, 0)
	if err != nil {
		t.Fatalf("volume: %v", err)
	}
	if len(volume.Groups) != 5 {
		t.Errorf("volume returned %d days, want 5", len(volume.Groups))
	}
	for _, g := range volume.Groups {
		if g.Value != 1 {
			t.Errorf("day %s counted %v, want 1", g.Label, g.Value)
		}
	}

	// Top senders excludes the account's own addresses, which is what me()
	// means. The fixture account is me@example.com, which never sends.
	senders, err := RunQuery("-from:me() | top from 10", 0, 0)
	if err != nil {
		t.Fatalf("senders: %v", err)
	}
	if len(senders.Groups) != 5 {
		t.Errorf("senders returned %d, want 5", len(senders.Groups))
	}

	topics, err := RunQuery("| top topic 50", 0, 0)
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	labels := map[string]bool{}
	for _, g := range topics.Groups {
		labels[g.Label] = true
	}
	// The first word of the subject line, which is what this widget has always
	// shown.
	for _, want := range []string{"quarterly", "lunch", "weekly"} {
		if !labels[want] {
			t.Errorf("topics did not include %q: %v", want, labels)
		}
	}

	// The ignore list is part of the language now, not something the dashboard
	// applied on its own. It used to strip these while `| top topic` in Desk
	// showed them, so the same question had two answers depending on where it
	// was asked — "re:" and "fwd:" led the list in one place and were absent
	// in the other. "your" is in the seeded default list.
	for _, ignored := range []string{"your", "re:", "fwd:", "the"} {
		if labels[ignored] {
			t.Errorf("topics included the ignored word %q: %v", ignored, labels)
		}
	}
}

// A cross-filter and a folder have to compose, which is the thing three
// separate filter implementations could not reliably do.
func TestFilterAndAggregateCompose(t *testing.T) {
	openQueryFixture(t)

	res, err := RunQuery("from:*@acme.com | count by day", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Groups) != 3 {
		t.Errorf("acme mail spans %d days, want 3", len(res.Groups))
	}

	res, err = RunQuery("on:2026-03-01 | count", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("one day holds %d messages, want 1", res.Total)
	}
}

func TestMigrateLegacyDatabase(t *testing.T) {
	tempDir := t.TempDir()
	oldDB := tempDir + "/uea.db"
	oldWAL := tempDir + "/uea.db-wal"
	if err := os.WriteFile(oldDB, []byte("sqlite header"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldWAL, []byte("wal data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLegacyDatabase(tempDir); err != nil {
		t.Fatalf("MigrateLegacyDatabase failed: %v", err)
	}

	newDB := tempDir + "/" + DBNAME
	newWAL := tempDir + "/" + DBNAME + "-wal"
	if _, err := os.Stat(newDB); err != nil {
		t.Errorf("expected %s to exist, got %v", newDB, err)
	}
	if _, err := os.Stat(newWAL); err != nil {
		t.Errorf("expected %s to exist, got %v", newWAL, err)
	}
	if _, err := os.Stat(oldDB); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, got %v", oldDB, err)
	}
}
