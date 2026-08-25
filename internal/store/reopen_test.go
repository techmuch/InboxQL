package store

import "testing"

// InitDB is a sync.Once singleton; if CloseDB does not reset it, a second
// open in the same process silently hands back the closed handle. Every
// downstream package that wants an isolated database per test depends on this.
func TestDatabaseCanBeReopenedAfterClose(t *testing.T) {
	for i := 0; i < 3; i++ {
		if _, err := InitDB(t.TempDir()); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&n); err != nil {
			t.Fatalf("query after open %d: %v", i, err)
		}
		CloseDB()
	}
}
