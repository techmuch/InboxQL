package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Tags made before the rename survive it, attributed to a person — which is
// the only thing that could have put them there.
func TestContactTagsBecomeLabelsInTheMigration(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if err := SaveContact(&Contact{Address: "alice@acme.com"}, "test"); err != nil {
		t.Fatal(err)
	}
	CloseDB()

	// Wind the database back to before v40, with a tag in the old table.
	raw, err := sql.Open("sqlite3", filepath.Join(dir, DBNAME))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP TABLE contact_labels`,
		`CREATE TABLE contact_tags (address TEXT NOT NULL, tag TEXT NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY (address, tag))`,
		`INSERT INTO contact_tags VALUES ('alice@acme.com', 'vip', 1)`,
		`PRAGMA user_version = 39`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	raw.Close()

	if _, err := InitDB(dir); err != nil {
		t.Fatalf("re-opening: %v", err)
	}
	t.Cleanup(CloseDB)

	labels, err := GetContactLabels("alice@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0] != "vip" {
		t.Errorf("labels %v, want [vip]", labels)
	}
	var source string
	if err := db.QueryRow(`SELECT source FROM contact_labels WHERE address = 'alice@acme.com'`).Scan(&source); err != nil || source != "human" {
		t.Errorf("source %q (%v), want human", source, err)
	}
	if tableExists(db, "contact_tags") {
		t.Error("the old table is still there")
	}
}
