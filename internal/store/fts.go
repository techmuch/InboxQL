package store

import (
	"database/sql"
	"log"
	"strings"
	"sync/atomic"
)

// ftsEnabled records whether this binary can use the full-text index.
//
// FTS5 is a compile-time option in mattn/go-sqlite3: a build without the
// `sqlite_fts5` tag cannot create or query the virtual table, and asking it to
// fails with "no such module: fts5". Releases are built with the tag (see the
// Makefile), but `go test ./...` and `go run` are not, and refusing to open a
// database in that case would make the project untestable without remembering
// a flag.
//
// So the index is a capability, not a schema version. When it is absent the
// query compiler falls back to substring matching, which is what search did
// before the index existed — slower and token-blind, but correct.
var ftsEnabled atomic.Bool

// FullTextAvailable reports whether queries will use the full-text index.
//
// Surfaced through `iql doctor`, because "search is slow" and "this binary has
// no index" are the same fact and only one of them is visible.
func FullTextAvailable() bool { return ftsEnabled.Load() }

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	subject,
	body,
	normalized_body,
	from_addr,
	content='messages',
	content_rowid='rowid',
	tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, subject, body, normalized_body, from_addr)
	VALUES (new.rowid, new.subject, new.body, new.normalized_body, new.from_addr);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, subject, body, normalized_body, from_addr)
	VALUES ('delete', old.rowid, old.subject, old.body, old.normalized_body, old.from_addr);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, subject, body, normalized_body, from_addr)
	VALUES ('delete', old.rowid, old.subject, old.body, old.normalized_body, old.from_addr);
	INSERT INTO messages_fts(rowid, subject, body, normalized_body, from_addr)
	VALUES (new.rowid, new.subject, new.body, new.normalized_body, new.from_addr);
END;
`

// ensureFullTextIndex creates the index when the build supports it.
//
// Safe to call on a database that already has one, and safe to call from a
// build that cannot: the missing-module error is the answer, not a failure.
func ensureFullTextIndex(db *sql.DB) error {
	if _, err := db.Exec(ftsSchema); err != nil {
		if isMissingFTS5(err) {
			ftsEnabled.Store(false)
			log.Printf("Full-text search unavailable: this binary was built without FTS5. " +
				"Text queries will use substring matching. Rebuild with `-tags sqlite_fts5` for the index.")
			return nil
		}
		return err
	}

	// An index that exists but was never populated answers every query with
	// nothing, which is worse than not having one. Rebuild is idempotent and
	// cheap when the index is already current.
	if err := rebuildFullTextIndex(db); err != nil {
		return err
	}

	ftsEnabled.Store(true)
	return nil
}

// rebuildFullTextIndex reindexes every message.
//
// Exposed through `iql maintenance` as well: the triggers keep the index
// current, but a database restored from a backup taken by a build without
// FTS5 arrives with no index at all.
func rebuildFullTextIndex(db *sql.DB) error {
	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&total); err != nil {
		return err
	}

	indexed, err := indexedDocuments(db)
	if err != nil {
		return err
	}
	if indexed == total {
		return nil
	}

	if _, err := db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild');`); err != nil {
		return err
	}
	if total > 0 {
		log.Printf("Built the full-text index over %d message(s).", total)
	}
	return nil
}

// indexedDocuments counts what the full-text index actually holds.
//
// Not `SELECT COUNT(*) FROM messages_fts`. This is an external-content table,
// so querying it reads through to `messages` — that count equals the message
// count whether the index holds everything or nothing. Using it as the guard
// meant the rebuild never ran, and the failure was silent and inverted: a
// `text:` search matched nothing, so `-text:` matched everything, and a
// negation confidently returned messages containing the very word it excluded.
//
// `%_docsize` is a real shadow table with one row per indexed document, so it
// reports what is there rather than what could be read through to.
func indexedDocuments(db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(*) FROM messages_fts_docsize").Scan(&n)
	if err != nil {
		// The shadow table is absent only if the index was created with
		// columnsize=0, which this schema does not do. Rebuilding is the safe
		// answer to not knowing: it costs time, where guessing costs
		// correctness.
		return -1, nil
	}
	return n, nil
}

// FullTextCoverage reports how much of the mailbox the index actually holds.
//
// Surfaced by `iql doctor` because a partial index fails silently and
// inverted: text searches match too little, so their negations match too much.
// A count nobody checks is how that stays invisible.
func FullTextCoverage() (indexed, total int64, err error) {
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&total); err != nil {
		return 0, 0, err
	}
	if !ftsEnabled.Load() {
		return 0, total, nil
	}
	indexed, err = indexedDocuments(db)
	return indexed, total, err
}

// RebuildFullTextIndex reindexes on demand, for `iql maintenance`.
func RebuildFullTextIndex() error {
	if !ftsEnabled.Load() {
		return nil
	}
	return rebuildFullTextIndex(db)
}

func isMissingFTS5(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such module")
}
