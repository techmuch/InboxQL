//go:build !sqlite_fts5

package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/message"
)

// A build without FTS5 must still store mail in a mailbox a build with it
// indexed. v0.0.101 to v0.0.103 were released without the tag: the index's
// table already existed, so the binary believed it had the module, and the
// triggers on messages failed every insert with "no such module: fts5".
func TestMailboxIndexedElsewhereStillTakesMail(t *testing.T) {
	openQueryFixture(t)
	if FullTextCompiled() {
		t.Fatal("FullTextCompiled in a build without the sqlite_fts5 tag")
	}

	// What a build with FTS5 leaves behind: triggers into an index this
	// binary cannot open (SQLite resolves a trigger's tables when it fires),
	// and the index's document counts.
	for _, stmt := range []string{
		`CREATE TABLE messages_fts_docsize(id INTEGER PRIMARY KEY, sz BLOB)`,
		`INSERT INTO messages_fts_docsize(id) VALUES (1)`,
		`CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, subject) VALUES (new.rowid, new.subject);
		END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := ensureFullTextIndex(db); err != nil {
		t.Fatalf("ensureFullTextIndex: %v", err)
	}
	if FullTextAvailable() {
		t.Fatal("full text reported available without the module")
	}

	now := time.Now()
	if err := SaveMessage(&message.Message{ID: "late", AccountID: "acct", MessageID: "<late@x>",
		From: "a@x.com", Subject: "arrived later", Body: "b", Date: now, InternalDate: now,
		ContentHash: "late", Mailbox: "INBOX"}); err != nil {
		t.Fatalf("a message could not be stored: %v", err)
	}

	// Emptied, so a build with FTS5 sees an index missing documents and
	// rebuilds it rather than trusting one that missed this message.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages_fts_docsize`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("document counts = %d (%v), want emptied", n, err)
	}
}
