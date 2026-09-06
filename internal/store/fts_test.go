//go:build sqlite_fts5

package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// These tests need a real FTS5 index, so they only build with the tag. The
// fallback path is covered by running the rest of the suite without it.

// dropFullTextIndex puts the database back into the state a build without
// FTS5 leaves it in: messages, no index, no triggers.
func dropFullTextIndex(t *testing.T) {
	t.Helper()
	for _, stmt := range []string{
		"DROP TRIGGER IF EXISTS messages_fts_insert",
		"DROP TRIGGER IF EXISTS messages_fts_delete",
		"DROP TRIGGER IF EXISTS messages_fts_update",
		"DROP TABLE IF EXISTS messages_fts",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	ftsEnabled.Store(false)
}

func indexedCount(t *testing.T) int64 {
	t.Helper()
	n, err := indexedDocuments(db)
	if err != nil {
		t.Fatalf("indexedDocuments: %v", err)
	}
	return n
}

// The bug this exists for.
//
// Every other test in this package starts from a fresh database, so the
// triggers populate the index as messages are inserted and the rebuild path is
// never reached. The failing case is the upgrade: a database whose messages
// predate the index, which is every existing user's database the first time
// they run a build with FTS5.
//
// It failed silently and inverted. `text:x` matched nothing, so `-text:x`
// matched everything, and a negation confidently returned messages containing
// the very word it was told to exclude.
func TestIndexIsBuiltForMessagesThatPredateIt(t *testing.T) {
	openQueryFixture(t)

	total, err := CountQuery("")
	if err != nil {
		t.Fatalf("CountQuery: %v", err)
	}

	// Rewind to "messages exist, index does not".
	dropFullTextIndex(t)

	if err := ensureFullTextIndex(db); err != nil {
		t.Fatalf("ensureFullTextIndex: %v", err)
	}
	if !FullTextAvailable() {
		t.Fatal("the index was not enabled after being created")
	}

	if got := indexedCount(t); got != total {
		t.Fatalf("index holds %d documents for %d messages; the rebuild did not run", got, total)
	}

	// The symptom, asserted directly.
	if ids := ids(t, "invoice"); len(ids) == 0 {
		t.Error("a word plainly present in the fixture matched nothing")
	}
	pos, neg := countOf(t, "invoice"), countOf(t, "-invoice")
	if pos == 0 {
		t.Error("text search matched nothing after the rebuild")
	}
	if pos+neg != total {
		t.Errorf("%d matching + %d not-matching = %d, want %d", pos, neg, pos+neg, total)
	}
	// The specific shape reported: a negation must not return a message that
	// contains the excluded word.
	for _, id := range ids(t, "-invoice") {
		if id == "m1" || id == "m5" {
			t.Errorf("-invoice returned %s, whose subject contains the word", id)
		}
	}
}

// The guard has to count what the index holds, not what it can read through to.
//
// `SELECT COUNT(*) FROM messages_fts` is the trap: this is an external-content
// table, so that query answers from `messages` and reports a full index even
// when the index is empty. Using it made the rebuild unreachable.
func TestIndexedCountReportsTheIndexNotTheContent(t *testing.T) {
	openQueryFixture(t)

	total, err := CountQuery("")
	if err != nil {
		t.Fatalf("CountQuery: %v", err)
	}

	// Recreate the table without populating it: the state the guard has to
	// recognise.
	dropFullTextIndex(t)
	if _, err := db.Exec(ftsSchema); err != nil {
		t.Fatalf("creating the index: %v", err)
	}

	var readThrough int64
	if err := db.QueryRow("SELECT COUNT(*) FROM messages_fts").Scan(&readThrough); err != nil {
		t.Fatalf("counting through the content table: %v", err)
	}
	if readThrough != total {
		t.Skipf("SQLite answered COUNT(*) on the virtual table as %d, not %d; "+
			"the trap this guards against no longer exists in this build", readThrough, total)
	}

	if got := indexedCount(t); got != 0 {
		t.Errorf("an empty index reported %d documents; the guard is reading through "+
			"to the content table and the rebuild will never run", got)
	}
}

// A rebuild on an index that is already current must not be wasted work, or
// every open of a large mailbox pays to reindex it.
func TestRebuildIsSkippedWhenTheIndexIsCurrent(t *testing.T) {
	openQueryFixture(t)

	before := indexedCount(t)
	if before == 0 {
		t.Fatal("the fixture left an empty index")
	}
	if err := rebuildFullTextIndex(db); err != nil {
		t.Fatalf("rebuildFullTextIndex: %v", err)
	}
	if after := indexedCount(t); after != before {
		t.Errorf("index went from %d to %d documents on a no-op rebuild", before, after)
	}
}

// The triggers have to keep the index in step as mail arrives and leaves, or
// the same silent-wrong-answer returns by a different route.
func TestIndexFollowsInsertsAndDeletes(t *testing.T) {
	openQueryFixture(t)

	before := indexedCount(t)

	m := &message.Message{
		ID: "new1", AccountID: "acct", MessageID: "<new1@test>",
		ContentHash: "new1", From: "zed@example.com",
		Subject: "Turnip harvest schedule", Body: "Turnips are ready.",
		Date: time.Now(), InternalDate: time.Now(), Mailbox: "INBOX",
	}
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if got := indexedCount(t); got != before+1 {
		t.Errorf("index holds %d after an insert, want %d", got, before+1)
	}
	if n := countOf(t, "turnip"); n != 1 {
		t.Errorf("the new message is not searchable: turnip matched %d", n)
	}

	if _, err := db.Exec("DELETE FROM messages WHERE id = ?", m.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if got := indexedCount(t); got != before {
		t.Errorf("index holds %d after a delete, want %d", got, before)
	}
	if n := countOf(t, "turnip"); n != 0 {
		t.Errorf("a deleted message is still searchable: turnip matched %d", n)
	}
}

// Erasing an account's mail must take its text out of the index too, or a
// search still returns rows the store can no longer produce.
func TestErasingDataClearsTheIndex(t *testing.T) {
	openQueryFixture(t)

	if n := countOf(t, "invoice"); n == 0 {
		t.Fatal("the fixture has nothing to erase")
	}
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me"}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	if err := EraseSyncedData(); err != nil {
		t.Fatalf("EraseSyncedData: %v", err)
	}

	if got := indexedCount(t); got != 0 {
		t.Errorf("index still holds %d documents after erasing every message", got)
	}
	if n := countOf(t, "invoice"); n != 0 {
		t.Errorf("erased mail is still searchable: invoice matched %d", n)
	}
}

// Whatever state the index is in, positive and negative text searches must
// still partition the mailbox. This is the invariant the reported bug broke.
func TestTextSearchPartitionsEvenAfterARebuild(t *testing.T) {
	openQueryFixture(t)

	total, err := CountQuery("")
	if err != nil {
		t.Fatalf("CountQuery: %v", err)
	}

	dropFullTextIndex(t)
	if err := ensureFullTextIndex(db); err != nil {
		t.Fatalf("ensureFullTextIndex: %v", err)
	}

	for _, word := range []string{"invoice", "lunch", "payment", "nothingmatchesthis"} {
		pos := countOf(t, word)
		neg := countOf(t, "-"+word)
		if pos+neg != total {
			t.Errorf("%q: %d + %d = %d, want %d", word, pos, neg, pos+neg, total)
		}
	}
}
