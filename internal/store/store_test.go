package store

import (
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
