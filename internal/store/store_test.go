package store

import (
	"os"
	"testing"
)

func TestApplyFilters(t *testing.T) {
	baseQuery := "SELECT * FROM messages"
	filter := AnalyticsFilter{
		Date:  "2026-02-25",
		From:  "alice@tech.com",
		Topic: "status",
	}

	var args []interface{}
	finalQuery, finalArgs := applyFilters(baseQuery, filter, args)

	expectedQuery := "SELECT * FROM messages WHERE strftime('%Y-%m-%d', date / 1000, 'unixepoch') = ? AND from_addr = ? AND subject LIKE ?"
	if finalQuery != expectedQuery {
		t.Errorf("Expected %q, got %q", expectedQuery, finalQuery)
	}

	if len(finalArgs) != 3 {
		t.Errorf("Expected 3 arguments, got %d", len(finalArgs))
	}
}

func TestApplyFiltersEmpty(t *testing.T) {
	baseQuery := "SELECT * FROM messages WHERE active = 1"
	filter := AnalyticsFilter{}

	var args []interface{}
	finalQuery, finalArgs := applyFilters(baseQuery, filter, args)

	expectedQuery := "SELECT * FROM messages WHERE active = 1"
	if finalQuery != expectedQuery {
		t.Errorf("Expected %q, got %q", expectedQuery, finalQuery)
	}

	if len(finalArgs) != 0 {
		t.Errorf("Expected 0 arguments, got %d", len(finalArgs))
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
