package query

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// The week label is the Monday the week starts on, for every day of it.
func TestWeekBucketIsItsMonday(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expr, err := bucketExpr("?", "week")
	if err != nil {
		t.Fatal(err)
	}
	for day, want := range map[string]string{
		"2026-10-12 12:00": "2026-10-12", // Monday
		"2026-10-13 12:00": "2026-10-12",
		"2026-10-18 12:00": "2026-10-12", // Sunday
		"2026-10-19 12:00": "2026-10-19",
	} {
		var got string
		// localtime is applied inside the expression, so feed it a local noon.
		if err := db.QueryRow("SELECT "+expr, mustLocalMillis(t, db, day)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s -> %s, want %s", day, got, want)
		}
	}
}

func mustLocalMillis(t *testing.T, db *sql.DB, local string) int64 {
	var ms int64
	if err := db.QueryRow("SELECT CAST(strftime('%s', ?, 'utc') AS INTEGER) * 1000", local).Scan(&ms); err != nil {
		t.Fatal(err)
	}
	return ms
}
