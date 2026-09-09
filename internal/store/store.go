package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/hasher"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/vault"
)

const (
	// DBNAME is the default name for the SQLite database file.
	DBNAME = "inboxql.db"
	// SchemaVersion is the current version of the database schema.
	SchemaVersion = 28
)

var (
	db     *sql.DB
	dbOnce sync.Once
)

// Agent represents an AI Agent configuration.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SchemaJSON  string `json:"schemaJson"`
}

// User represents a system user.
type User struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	PasswordHash    string `json:"-"`
	DisplayName     string `json:"displayName"`
	Email           string `json:"email"`
	ProfileImageURL string `json:"profileImageUrl"`
}

// Session represents a user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// MailboxSyncState represents the synchronization state for a specific mailbox.
type MailboxSyncState struct {
	ID         string `json:"id"`
	AccountID  string `json:"accountId"`
	Name       string `json:"name"`
	LastUID    uint32 `json:"lastUid"`
	LastMODSEQ uint64 `json:"lastModseq"`
}

// AnalyticsData represents a point in a time series.
type AnalyticsData struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// MigrateLegacyDatabase automatically renames uea.db (and WAL/SHM) to inboxql.db if present.
func MigrateLegacyDatabase(dataDir string) error {
	newDB := filepath.Join(dataDir, DBNAME)
	oldDB := filepath.Join(dataDir, "uea.db")

	if _, err := os.Stat(newDB); os.IsNotExist(err) {
		if _, errOld := os.Stat(oldDB); errOld == nil {
			log.Printf("Migrating legacy database %s -> %s", oldDB, newDB)
			if err := os.Rename(oldDB, newDB); err != nil {
				return fmt.Errorf("failed to migrate %s to %s: %w", oldDB, newDB, err)
			}
			for _, ext := range []string{"-wal", "-shm"} {
				oldAux := oldDB + ext
				newAux := newDB + ext
				if _, errAux := os.Stat(oldAux); errAux == nil {
					_ = os.Rename(oldAux, newAux)
				}
			}
		}
	}
	return nil
}

// InitDB initializes the SQLite database connection and sets up the schema.
func InitDB(dataDir string) (*sql.DB, error) {
	var err error
	dbOnce.Do(func() {
		if err = os.MkdirAll(dataDir, 0755); err != nil {
			err = fmt.Errorf("failed to create data directory: %w", err)
			return
		}

		_ = MigrateLegacyDatabase(dataDir)

		dbPath := filepath.Join(dataDir, DBNAME)
		log.Printf("Initializing database at: %s", dbPath)

		// busy_timeout makes concurrent writers wait for the lock instead of
		// failing instantly with "database is locked". WAL allows one writer at
		// a time, and sync goroutines write messages concurrently, so without
		// this a second account syncing is an error rather than a short wait.
		db, err = sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_foreign_keys=on")
		if err != nil {
			err = fmt.Errorf("failed to open database: %w", err)
			return
		}

		// SQLite permits exactly one writer. Letting database/sql open an
		// unbounded pool of connections that then contend for that single write
		// lock produces lock contention under concurrent sync, so the pool is
		// capped deliberately rather than left at the default.
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(4)
		db.SetConnMaxLifetime(time.Hour)

		_, err = db.Exec("PRAGMA journal_mode=WAL;")
		if err != nil {
			err = fmt.Errorf("failed to set WAL journal mode: %w", err)
			return
		}
		_, err = db.Exec("PRAGMA synchronous=NORMAL;")
		if err != nil {
			err = fmt.Errorf("failed to set synchronous mode: %w", err)
			return
		}

		err = migrateDB(db)
		if err != nil {
			err = fmt.Errorf("failed to run database migrations: %w", err)
			return
		}

		// The credential vault lives alongside the database, and account rows
		// are unreadable without it, so it is initialised here rather than
		// leaving each caller to remember.
		if err = vault.Init(dataDir); err != nil {
			err = fmt.Errorf("failed to initialise credential vault: %w", err)
			return
		}

		if err = MigrateAccountPasswords(); err != nil {
			err = fmt.Errorf("failed to encrypt stored account passwords: %w", err)
			return
		}

		// Whether a full-text index can exist depends on how this binary was
		// built, not on the database, so it is checked on every open rather
		// than recorded as a schema version. A database written by a build
		// with FTS5 must still open in one without it.
		if err = ensureFullTextIndex(db); err != nil {
			err = fmt.Errorf("failed to prepare the full-text index: %w", err)
			return
		}
	})

	return db, err
}

// MigrateAccountPasswords rewrites any account password still held as plaintext
// into the vault envelope.
//
// This is a data migration rather than a schema one, so it is deliberately not
// part of the PRAGMA user_version ladder: it must run on every start, since a
// row can arrive as plaintext from an older binary or a restored backup at any
// point, not only at the moment the schema changes.
func MigrateAccountPasswords() error {
	rows, err := db.Query("SELECT id, password FROM accounts")
	if err != nil {
		return err
	}

	type pending struct{ id, password string }
	var todo []pending

	for rows.Next() {
		var id string
		var password sql.NullString
		if err := rows.Scan(&id, &password); err != nil {
			rows.Close()
			return err
		}
		if password.Valid && password.String != "" && !vault.IsEncrypted(password.String) {
			todo = append(todo, pending{id: id, password: password.String})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// Closed explicitly rather than deferred: the UPDATEs below need the write
	// lock, and on SQLite an open read cursor can still be holding it.
	rows.Close()

	if len(todo) == 0 {
		return nil
	}

	for _, item := range todo {
		encrypted, err := vault.Encrypt(item.password)
		if err != nil {
			return fmt.Errorf("account %s: %w", item.id, err)
		}
		if _, err := db.Exec("UPDATE accounts SET password = ? WHERE id = ?", encrypted, item.id); err != nil {
			return fmt.Errorf("account %s: %w", item.id, err)
		}
	}

	log.Printf("Encrypted %d previously-plaintext account password(s) at rest.", len(todo))
	return nil
}

// migrateDB runs database migrations.
func migrateDB(db *sql.DB) error {
	var currentVersion int
	row := db.QueryRow("PRAGMA user_version;")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	log.Printf("Current database schema version: %d", currentVersion)

	if currentVersion < 1 {
		log.Println("Applying schema migration v1...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS accounts (
				id TEXT PRIMARY KEY,
				host TEXT NOT NULL,
				port INTEGER NOT NULL,
				user TEXT NOT NULL,
				password TEXT NOT NULL,
				ssl BOOLEAN NOT NULL
			);

			CREATE TABLE IF NOT EXISTS mailboxes (
				id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				name TEXT NOT NULL,
				last_uid INTEGER DEFAULT 0,
				last_modseq INTEGER DEFAULT 0,
				FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
				UNIQUE(account_id, name)
			);

			CREATE INDEX IF NOT EXISTS idx_mailboxes_account_id ON mailboxes(account_id);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v1: %w", err)
		}
		_, err = db.Exec("PRAGMA user_version = 1;")
		if err != nil {
			return err
		}
		currentVersion = 1
	}

	if currentVersion < 2 {
		log.Println("Applying schema migration v2 (messages table)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS messages (
				id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				uid INTEGER NOT NULL,
				message_id TEXT NOT NULL,
				content_hash TEXT NOT NULL,
				normalized_body TEXT NOT NULL,
				from_addr TEXT NOT NULL,
				to_addrs TEXT NOT NULL,
				cc_addrs TEXT NOT NULL,
				bcc_addrs TEXT NOT NULL,
				subject TEXT NOT NULL,
				date INTEGER NOT NULL,
				body TEXT NOT NULL,
				html_body TEXT NOT NULL,
				header BLOB NOT NULL,
				flags TEXT NOT NULL,
				size INTEGER NOT NULL,
				internal_date INTEGER NOT NULL,
				FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_messages_account_id ON messages(account_id);
			CREATE INDEX IF NOT EXISTS idx_messages_message_id ON messages(message_id);
			CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_content_hash ON messages(content_hash);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v2: %w", err)
		}
		_, err = db.Exec("PRAGMA user_version = 2;")
		if err != nil {
			return err
		}
		currentVersion = 2
	}

	if currentVersion < 3 {
		log.Println("Applying schema migration v3...")
		_, err := db.Exec(`
			ALTER TABLE accounts ADD COLUMN name TEXT;
			ALTER TABLE accounts ADD COLUMN smtp_host TEXT;
			ALTER TABLE accounts ADD COLUMN smtp_port INTEGER;
		`)
		if err != nil {
			log.Printf("Warning v3: %v", err)
		}
		_, err = db.Exec("PRAGMA user_version = 3;")
		if err != nil {
			return err
		}
		currentVersion = 3
	}

	if currentVersion < 4 {
		log.Println("Applying schema migration v4...")
		_, err := db.Exec(`ALTER TABLE accounts ADD COLUMN email TEXT;`)
		if err != nil {
			log.Printf("Warning v4: %v", err)
		}
		_, err = db.Exec("PRAGMA user_version = 4;")
		if err != nil {
			return err
		}
		currentVersion = 4
	}

	if currentVersion < 5 {
		log.Println("Applying schema migration v5 (users and sessions)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS users (
				id TEXT PRIMARY KEY,
				username TEXT UNIQUE NOT NULL,
				password_hash TEXT NOT NULL,
				display_name TEXT,
				email TEXT,
				profile_image_url TEXT
			);

			CREATE TABLE IF NOT EXISTS sessions (
				id TEXT PRIMARY KEY,
				user_id TEXT NOT NULL,
				expires_at DATETIME NOT NULL,
				FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v5: %w", err)
		}
		_, err = db.Exec("PRAGMA user_version = 5;")
		if err != nil {
			return err
		}
		currentVersion = 5
	}

	if currentVersion < 6 {
		log.Println("Applying schema migration v6 (account status columns)...")
		_, err := db.Exec(`
			ALTER TABLE accounts ADD COLUMN last_sync_status TEXT DEFAULT 'idle';
			ALTER TABLE accounts ADD COLUMN last_sync_error TEXT;
		`)
		if err != nil {
			log.Printf("Warning v6: %v", err)
		}
		_, err = db.Exec("PRAGMA user_version = 6;")
		if err != nil {
			return err
		}
		currentVersion = 6
	}

	if currentVersion < 7 {
		log.Println("Applying schema migration v7 (app_settings and date fix)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS app_settings (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL
			);

			INSERT OR IGNORE INTO app_settings (key, value) VALUES ('ignore_words', 're:,fwd:,the,and,for,this,that,with,from,your,have,status,update,alert,notification');

			UPDATE messages SET date = date * 1000 WHERE date < 100000000000;
			UPDATE messages SET internal_date = internal_date * 1000 WHERE internal_date < 100000000000;
		`)
		if err != nil {
			log.Printf("Warning v7: %v", err)
		}
		_, err = db.Exec("PRAGMA user_version = 7;")
		if err != nil {
			return err
		}
		currentVersion = 7
	}

	if currentVersion < 8 {
		log.Println("Applying schema migration v8 (agents table)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS agents (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				description TEXT,
				schema_json TEXT NOT NULL
			);
		`)
		if err != nil {
			log.Printf("Warning v8: %v", err)
		}
		_, err = db.Exec("PRAGMA user_version = 8;")
		if err != nil {
			return err
		}
		currentVersion = 8
	}

	if currentVersion < 9 {
		log.Println("Applying schema migration v9 (drafts and outbox)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS drafts (
				id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				in_reply_to TEXT,
				to_addrs TEXT NOT NULL DEFAULT '[]',
				cc_addrs TEXT NOT NULL DEFAULT '[]',
				bcc_addrs TEXT NOT NULL DEFAULT '[]',
				subject TEXT NOT NULL DEFAULT '',
				body TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'draft',
				origin TEXT NOT NULL DEFAULT 'human',
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				queued_at INTEGER,
				sent_at INTEGER,
				last_error TEXT,
				FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_drafts_status ON drafts(status);
			CREATE INDEX IF NOT EXISTS idx_drafts_account ON drafts(account_id);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v9: %w", err)
		}
		_, err = db.Exec("PRAGMA user_version = 9;")
		if err != nil {
			return err
		}
		currentVersion = 9
	}

	if currentVersion < 10 {
		log.Println("Applying schema migration v10 (per-account content hash)...")
		// The old index was UNIQUE(content_hash) across every account, and the
		// hash covered only the body. Every body-less message therefore hashed
		// to SHA-256 of the empty string, so the first calendar invite stored
		// and INSERT OR IGNORE silently discarded the rest. Rehashing has to
		// happen with no unique index in place, since rows pass through
		// intermediate states as they are rewritten one at a time.
		if _, err := db.Exec(`DROP INDEX IF EXISTS idx_messages_content_hash;`); err != nil {
			return fmt.Errorf("failed to drop the old content hash index: %w", err)
		}
		if err := rehashMessages(db); err != nil {
			return fmt.Errorf("failed to recompute content hashes: %w", err)
		}
		// Rehashing separates messages that used to collide, but rows lost to
		// the old index are gone and genuine duplicates may remain; clear them
		// before asking SQLite to enforce uniqueness.
		if _, err := db.Exec(`
			DELETE FROM messages
			WHERE rowid NOT IN (SELECT MIN(rowid) FROM messages GROUP BY account_id, content_hash);

			CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_account_content
				ON messages(account_id, content_hash);
		`); err != nil {
			return fmt.Errorf("failed to apply schema v10: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 10;"); err != nil {
			return err
		}
		currentVersion = 10
	}

	if currentVersion < 11 {
		log.Println("Applying schema migration v11 (attachments)...")
		// storage_path is NULL for a part that was not kept — too large, or
		// attachments disabled for that import. The row still exists so the
		// message records what it carried rather than appearing to have had
		// nothing.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS attachments (
				id            TEXT PRIMARY KEY,
				message_id    TEXT NOT NULL,
				filename      TEXT NOT NULL DEFAULT '',
				mime_type     TEXT NOT NULL DEFAULT '',
				size          INTEGER NOT NULL DEFAULT 0,
				content_hash  TEXT NOT NULL DEFAULT '',
				storage_path  TEXT,
				inline        BOOLEAN NOT NULL DEFAULT 0,
				content_id    TEXT,
				skipped       TEXT,
				created_at    INTEGER NOT NULL,
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_attachments_message ON attachments(message_id);
			CREATE INDEX IF NOT EXISTS idx_attachments_hash    ON attachments(content_hash);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v11: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 11;"); err != nil {
			return err
		}
		currentVersion = 11
	}

	if currentVersion < 12 {
		log.Println("Applying schema migration v12 (import jobs)...")
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS import_jobs (
				id            TEXT PRIMARY KEY,
				source        TEXT NOT NULL,
				account_id    TEXT NOT NULL,
				mailboxes     TEXT NOT NULL DEFAULT '[]',
				options       TEXT NOT NULL DEFAULT '{}',
				status        TEXT NOT NULL DEFAULT 'pending',
				dry_run       BOOLEAN NOT NULL DEFAULT 0,
				total         INTEGER,
				scanned       INTEGER NOT NULL DEFAULT 0,
				imported      INTEGER NOT NULL DEFAULT 0,
				duplicates    INTEGER NOT NULL DEFAULT 0,
				skipped       INTEGER NOT NULL DEFAULT 0,
				failed        INTEGER NOT NULL DEFAULT 0,
				attachments   INTEGER NOT NULL DEFAULT 0,
				bytes         INTEGER NOT NULL DEFAULT 0,
				current       TEXT,
				last_error    TEXT,
				created_at    INTEGER NOT NULL,
				started_at    INTEGER,
				finished_at   INTEGER
			);

			CREATE INDEX IF NOT EXISTS idx_import_jobs_status ON import_jobs(status);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v12: %w", err)
		}
		// A job left running is a job whose process died. Nothing is going to
		// finish it, so mark it rather than letting the UI wait forever on
		// progress that will never arrive.
		if _, err := db.Exec(`
			UPDATE import_jobs
			SET status = 'interrupted',
			    last_error = 'the server stopped while this import was running',
			    finished_at = ?
			WHERE status IN ('running', 'pending');
		`, time.Now().UnixMilli()); err != nil {
			return fmt.Errorf("failed to reconcile interrupted import jobs: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 12;"); err != nil {
			return err
		}
		currentVersion = 12
	}

	if currentVersion < 13 {
		log.Println("Applying schema migration v13 (error log)...")
		// Per-item failures were only ever counted in memory and printed once.
		// A count with no detail is not actionable: "3 failed" tells you
		// nothing about which three, or why.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS error_log (
				id         TEXT PRIMARY KEY,
				category   TEXT NOT NULL,
				job_id     TEXT,
				account_id TEXT,
				context    TEXT,
				reference  TEXT,
				message    TEXT NOT NULL,
				created_at INTEGER NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx_error_log_created  ON error_log(created_at DESC);
			CREATE INDEX IF NOT EXISTS idx_error_log_job      ON error_log(job_id);
			CREATE INDEX IF NOT EXISTS idx_error_log_category ON error_log(category);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v13: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 13;"); err != nil {
			return err
		}
		currentVersion = 13
	}

	if currentVersion < 14 {
		log.Println("Applying schema migration v14 (mailbox attribution)...")
		// Sync fetches INBOX and Sent and wrote both into one flat table with
		// no record of which was which, and the importer discarded the folder
		// it read from. Without this column "Sent" can only ever be inferred
		// from the sender address, which is wrong for anything you were CC'd
		// on and for mail you sent from another client.
		//
		// Existing rows keep NULL and fall back to that inference.
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN mailbox TEXT;`); err != nil {
			log.Printf("Warning v14: %v", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_mailbox ON messages(mailbox);`); err != nil {
			return fmt.Errorf("failed to apply schema v14: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 14;"); err != nil {
			return err
		}
		currentVersion = 14
	}

	if currentVersion < 15 {
		log.Println("Applying schema migration v15 (participant edges)...")
		// Recipients lived only as a JSON array in to_addrs/cc_addrs/bcc_addrs,
		// which can be substring-matched but not negated: "no recipient is
		// alice" is a NOT EXISTS over rows, and there were no rows. Every
		// multi-valued field needs this shape before the query language can
		// offer negation that means what it says.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS message_participants (
				message_id TEXT NOT NULL,
				role       TEXT NOT NULL,
				address    TEXT NOT NULL,
				name       TEXT,
				PRIMARY KEY (message_id, role, address),
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_participants_address ON message_participants(address);
			CREATE INDEX IF NOT EXISTS idx_participants_role    ON message_participants(role, address);
			CREATE INDEX IF NOT EXISTS idx_participants_message ON message_participants(message_id);

			-- Date filtering was strftime() over every row, which no index can
			-- serve. The query language compiles dates to epoch comparisons
			-- against this column instead.
			CREATE INDEX IF NOT EXISTS idx_messages_date ON messages(date DESC);
			CREATE INDEX IF NOT EXISTS idx_messages_size ON messages(size);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v15: %w", err)
		}
		if err := backfillParticipants(db); err != nil {
			return fmt.Errorf("failed to backfill participants: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 15;"); err != nil {
			return err
		}
		currentVersion = 15
	}

	if currentVersion < 16 {
		log.Println("Applying schema migration v16 (annotators)...")
		// Labels and extractions are one mechanism. A label is an annotator
		// whose schema is a boolean; an extraction is one whose schema has
		// fields. They share every hard part — versioning, incremental re-run,
		// confidence, provenance, consent — so they share a table.
		//
		// `status` is what makes negation honest: a missing row means "never
		// evaluated", which is a different answer from "evaluated, no match".
		// Without that distinction -label:x silently returns every message the
		// annotator has not reached yet.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS annotators (
				id           TEXT PRIMARY KEY,
				name         TEXT NOT NULL,
				kind         TEXT NOT NULL,
				engine       TEXT NOT NULL,
				version      INTEGER NOT NULL DEFAULT 1,
				instructions TEXT NOT NULL,
				schema_json  TEXT NOT NULL DEFAULT '{}',
				model        TEXT,
				allow_remote INTEGER NOT NULL DEFAULT 0,
				created_at   INTEGER NOT NULL,
				updated_at   INTEGER NOT NULL
			);

			CREATE TABLE IF NOT EXISTS annotations (
				id                TEXT PRIMARY KEY,
				message_id        TEXT NOT NULL,
				annotator_id      TEXT NOT NULL,
				annotator_version INTEGER NOT NULL,
				seq               INTEGER NOT NULL DEFAULT 0,
				status            TEXT NOT NULL,
				source            TEXT NOT NULL,
				data_json         TEXT NOT NULL DEFAULT '{}',
				confidence        REAL,
				model             TEXT,
				error             TEXT,
				created_at        INTEGER NOT NULL,
				FOREIGN KEY (message_id)   REFERENCES messages(id)   ON DELETE CASCADE,
				FOREIGN KEY (annotator_id) REFERENCES annotators(id) ON DELETE CASCADE,
				UNIQUE(message_id, annotator_id, annotator_version, seq)
			);

			CREATE INDEX IF NOT EXISTS idx_annotations_lookup  ON annotations(annotator_id, annotator_version, status);
			CREATE INDEX IF NOT EXISTS idx_annotations_message ON annotations(message_id);
			CREATE INDEX IF NOT EXISTS idx_annotations_source  ON annotations(annotator_id, source);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v16: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 16;"); err != nil {
			return err
		}
		currentVersion = 16
	}

	if currentVersion < 17 {
		log.Println("Applying schema migration v17 (reference graph)...")
		// Threading grouped by normalised subject, so two unrelated messages
		// that share a subject line landed in one conversation. In-Reply-To and
		// References were captured in the raw header blob and never parsed;
		// this is the edge table that makes real threading a graph walk.
		for _, stmt := range []string{
			`ALTER TABLE messages ADD COLUMN in_reply_to TEXT;`,
			// thread_key is the conversation a message belongs to, maintained
			// on write. RFC 5322 puts the whole ancestry in References in
			// order, so its first entry is the thread root; a message with no
			// References is its own root. That makes a thread a single indexed
			// equality rather than a recursive walk on every query.
			`ALTER TABLE messages ADD COLUMN thread_key TEXT;`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				log.Printf("Warning v17: %v", err)
			}
		}
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS message_refs (
				message_id TEXT NOT NULL,
				ref        TEXT NOT NULL,
				ordinal    INTEGER NOT NULL,
				PRIMARY KEY (message_id, ref),
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_message_refs_ref     ON message_refs(ref);
			CREATE INDEX IF NOT EXISTS idx_message_refs_message ON message_refs(message_id);
			CREATE INDEX IF NOT EXISTS idx_messages_in_reply_to ON messages(in_reply_to);
			CREATE INDEX IF NOT EXISTS idx_messages_thread_key  ON messages(thread_key);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v17: %w", err)
		}
		if err := backfillRefs(db); err != nil {
			return fmt.Errorf("failed to backfill reference graph: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 17;"); err != nil {
			return err
		}
		currentVersion = 17
	}

	if currentVersion < 18 {
		log.Println("Applying schema migration v18 (saved queries)...")
		// A saved query is not a bookmark. `saved:invoices` is a term in the
		// language, so a saved query is a building block other queries compose
		// with — and a board column, later, is a query that names one.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS saved_queries (
				id          TEXT PRIMARY KEY,
				name        TEXT NOT NULL UNIQUE,
				title       TEXT NOT NULL,
				query       TEXT NOT NULL,
				description TEXT,
				pinned      INTEGER NOT NULL DEFAULT 0,
				created_at  INTEGER NOT NULL,
				updated_at  INTEGER NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx_saved_queries_pinned ON saved_queries(pinned DESC, name);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v18: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 18;"); err != nil {
			return err
		}
		currentVersion = 18
	}

	if currentVersion < 19 {
		log.Println("Applying schema migration v19 (tickets)...")
		// A ticket is an entity; an annotation is evidence.
		//
		// This distinction is the whole design. An email is an immutable event
		// and an annotation is derived from it — versioned, and invalidated
		// when the annotator's instruction changes. That is right for a label
		// and catastrophic for a ticket: editing an extraction prompt would
		// wipe the board. So tickets live in their own table with their own
		// lifecycle, seeded by annotations and never owned by them, and
		// ticket_sources records which messages are the evidence.
		//
		// State a person set is theirs. A re-run may add evidence and propose
		// new tickets; it never rewrites status, priority or due.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS tickets (
				id           TEXT PRIMARY KEY,
				thread_key   TEXT,
				title        TEXT NOT NULL,
				body         TEXT,
				status       TEXT NOT NULL DEFAULT 'proposed',
				priority     TEXT,
				due_at       INTEGER,
				origin       TEXT NOT NULL DEFAULT 'human',
				annotator_id TEXT,
				confidence   REAL,
				created_at   INTEGER NOT NULL,
				updated_at   INTEGER NOT NULL,
				closed_at    INTEGER,
				FOREIGN KEY (annotator_id) REFERENCES annotators(id) ON DELETE SET NULL
			);

			CREATE TABLE IF NOT EXISTS ticket_sources (
				ticket_id     TEXT NOT NULL,
				message_id    TEXT NOT NULL,
				annotation_id TEXT,
				created_at    INTEGER NOT NULL,
				PRIMARY KEY (ticket_id, message_id),
				FOREIGN KEY (ticket_id)  REFERENCES tickets(id)  ON DELETE CASCADE,
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_tickets_status     ON tickets(status);
			CREATE INDEX IF NOT EXISTS idx_tickets_due        ON tickets(due_at);
			CREATE INDEX IF NOT EXISTS idx_tickets_thread     ON tickets(thread_key);
			CREATE INDEX IF NOT EXISTS idx_ticket_sources_msg ON ticket_sources(message_id);

			-- One ticket per conversation per annotator. This is what makes a
			-- re-run idempotent: the second pass finds the ticket it already
			-- proposed rather than proposing it again.
			CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_thread_annotator
				ON tickets(thread_key, annotator_id)
				WHERE thread_key IS NOT NULL AND annotator_id IS NOT NULL;
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v19: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 19;"); err != nil {
			return err
		}
		currentVersion = 19
	}

	if currentVersion < 20 {
		log.Println("Applying schema migration v20 (ticket events)...")
		// A ticket's history, because the ticket only records its present.
		//
		// status, priority and due are current values; nothing says when they
		// changed or what they were. That is fine for a board, which only ever
		// asks what is true now, and useless for a timeline, which is a
		// narrative: "raised on the 2nd, started on the 6th, done on the 8th".
		//
		// This has to exist before anything reads it. Every other derived
		// thing in this system can be rebuilt from the mail — annotations,
		// participants, thread keys, the FTS index all have a backfill — but a
		// transition that was never recorded is gone. So the table lands
		// first, ahead of the timeline that wants it, and the backfill below
		// seeds what little can be honestly reconstructed.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS ticket_events (
				id         TEXT PRIMARY KEY,
				ticket_id  TEXT NOT NULL,
				kind       TEXT NOT NULL,
				from_value TEXT,
				to_value   TEXT,
				actor      TEXT NOT NULL DEFAULT 'human',
				at         INTEGER NOT NULL,
				FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_ticket_events_ticket ON ticket_events(ticket_id, at);

			-- Existing tickets get the one event that is a fact rather than a
			-- guess: they were raised, at created_at, by whatever raised them.
			-- Their intermediate moves are unrecoverable and are not invented.
			INSERT INTO ticket_events (id, ticket_id, kind, from_value, to_value, actor, at)
			SELECT lower(hex(randomblob(16))), id, 'created', NULL, status,
			       COALESCE(origin, 'human'), created_at
			FROM tickets
			WHERE NOT EXISTS (
				SELECT 1 FROM ticket_events e WHERE e.ticket_id = tickets.id AND e.kind = 'created'
			);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v20: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 20;"); err != nil {
			return err
		}
		currentVersion = 20
	}

	if currentVersion < 21 {
		log.Println("Applying schema migration v21 (draft thread keys)...")
		// A draft belongs to a conversation, and until now only messages and
		// tickets could say which one.
		//
		// Drafts already carried in_reply_to — the Message-ID of what they
		// answer — so the conversation was always derivable by a join. Storing
		// it makes the key uniform across all three entities, which is what
		// lets a timeline ask one question of one column instead of a
		// different question per table.
		//
		// The join needs normalising on both sides: messages.message_id keeps
		// the angle brackets the server sent, and it is the same mismatch that
		// once left every reply in a thread of its own.
		// ADD COLUMN is the one statement here with no IF NOT EXISTS, so it is
		// run on its own and a failure is logged rather than fatal — the same
		// shape v17 uses, and what makes re-running the ladder harmless.
		if _, err := db.Exec(`ALTER TABLE drafts ADD COLUMN thread_key TEXT;`); err != nil {
			log.Printf("Warning v21: %v", err)
		}
		_, err := db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_drafts_thread_key ON drafts(thread_key);

			UPDATE drafts SET thread_key = (
				SELECT m.thread_key FROM messages m
				WHERE trim(m.message_id, '<>') = trim(drafts.in_reply_to, '<>')
				LIMIT 1
			)
			WHERE in_reply_to IS NOT NULL AND trim(in_reply_to, '<>') != '';
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v21: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 21;"); err != nil {
			return err
		}
		currentVersion = 21
	}

	if currentVersion < 22 {
		log.Println("Applying schema migration v22 (LLM profiles)...")
		// One gateway became several.
		//
		// The provider settings were seven rows in app_settings, which allows
		// exactly one model to be configured at a time. That was already at
		// odds with the rest of the system: `annotate create --model` has
		// shipped for a while and overrides the model string but not the
		// provider, endpoint or key — so naming a cloud model on a machine
		// configured for a local runtime asks the local runtime for a model it
		// has never heard of.
		//
		// A profile is the whole address of a model: where it runs, which
		// model, and what credential reaches it. Work names a profile, which
		// is also what makes "is this remote?" answerable per annotator rather
		// than once globally.
		//
		// Exactly one profile is the default, enforced by a partial unique
		// index rather than by application code, because "somehow two
		// defaults" is the kind of state that is unfixable from the UI.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS llm_profiles (
				id             TEXT PRIMARY KEY,
				name           TEXT NOT NULL UNIQUE,
				provider       TEXT NOT NULL,
				model          TEXT NOT NULL,
				endpoint       TEXT NOT NULL,
				api_key        TEXT,
				is_default     INTEGER NOT NULL DEFAULT 0,
				auto_start     INTEGER NOT NULL DEFAULT 0,
				launch_mode    TEXT NOT NULL DEFAULT 'headless',
				lifecycle_mode TEXT NOT NULL DEFAULT 'managed',
				created_at     INTEGER NOT NULL,
				updated_at     INTEGER NOT NULL
			);

			CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_profiles_default
				ON llm_profiles(is_default) WHERE is_default = 1;
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v22: %w", err)
		}
		if err := migrateLLMSettingsToProfile(db); err != nil {
			return fmt.Errorf("failed to migrate the LLM settings into a profile: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 22;"); err != nil {
			return err
		}
		currentVersion = 22
	}

	if currentVersion < 23 {
		log.Println("Applying schema migration v23 (annotators name a model profile)...")
		// An annotator's `model` column was a bare model name, which could
		// only ever mean "that model, on whatever gateway is configured
		// globally". Naming a profile instead means naming the gateway too,
		// and it is what lets the consent check ask "is *this* annotator
		// remote" rather than "is the machine remote".
		//
		// model is deliberately kept. It still overrides the profile's model
		// for the case that motivated it — same gateway, different size of
		// model — and dropping a column on upgrade destroys information.
		if _, err := db.Exec(`ALTER TABLE annotators ADD COLUMN profile TEXT;`); err != nil {
			log.Printf("Warning v23: %v", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 23;"); err != nil {
			return err
		}
		currentVersion = 23
	}

	if currentVersion < 24 {
		log.Println("Applying schema migration v24 (recover participant names)...")
		// message_participants has always had a `name` column and it has always
		// been empty, because both parsers threw the display name away: the
		// .eml path kept only from[0].Address and the IMAP path rebuilt the
		// address from mailbox@host, discarding PersonalName.
		//
		// The names were never lost, only unread — every message keeps its raw
		// header. So this is a re-read rather than a re-import, and it is the
		// entire reason a contact can have a name without asking a model to
		// guess one from the body.
		if err := backfillParticipantNames(db); err != nil {
			return fmt.Errorf("failed to recover participant names: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 24;"); err != nil {
			return err
		}
		currentVersion = 24
	}

	if currentVersion < 25 {
		log.Println("Applying schema migration v25 (contacts)...")
		// A contact is an address someone has corresponded with, and every
		// message already creates the edges that prove it — message_participants
		// has held them since v15.
		//
		// # What is stored here and what is not
		//
		// Stored: identity, names, and anything a person or a model asserted —
		// facts that cannot be recomputed from the mailbox.
		//
		// NOT stored: how many messages, when first seen, when last seen, who
		// they appear alongside. Those are derived from message_participants on
		// every read. Caching them here would be a second source of truth that
		// goes stale the first time a message is deleted or re-imported, and
		// the two would then disagree with nobody noticing — which is this
		// project's most familiar failure.
		//
		// # Why the rows exist at all, then
		//
		// So there is somewhere to put a name, a phone number, a kind, and a
		// human correction. A derived view has no room for an assertion.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS contacts (
				address      TEXT PRIMARY KEY,
				-- The display name headers give this address, most recent wins.
				header_name  TEXT,
				-- What a person typed, which outranks every other source.
				display_name TEXT,
				first_name   TEXT,
				last_name    TEXT,
				phone        TEXT,
				org          TEXT,
				title        TEXT,
				-- person | organization | system | unknown
				kind         TEXT NOT NULL DEFAULT 'unknown',
				-- header | rule | llm | human, so a weaker source never
				-- overwrites a stronger one.
				kind_source  TEXT,
				enriched_by  TEXT,
				enriched_at  INTEGER,
				confidence   REAL,
				created_at   INTEGER NOT NULL,
				updated_at   INTEGER NOT NULL
			);

			CREATE INDEX IF NOT EXISTS idx_contacts_kind ON contacts(kind);
			CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(header_name);

			-- Every address already seen becomes a contact, with the name the
			-- v24 backfill just recovered. The most frequent spelling wins:
			-- a long thread names the same person several ways.
			INSERT INTO contacts (address, header_name, created_at, updated_at)
			SELECT p.address,
			       (SELECT p2.name FROM message_participants p2
			         WHERE p2.address = p.address AND p2.name IS NOT NULL AND p2.name != ''
			         GROUP BY p2.name ORDER BY COUNT(*) DESC, p2.name LIMIT 1),
			       CAST(strftime('%s','now') AS INTEGER) * 1000,
			       CAST(strftime('%s','now') AS INTEGER) * 1000
			FROM message_participants p
			GROUP BY p.address
			ON CONFLICT(address) DO NOTHING;
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v25: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 25;"); err != nil {
			return err
		}
		currentVersion = 25
	}

	if currentVersion < 26 {
		log.Println("Applying schema migration v26 (message topics)...")
		// What a message is about, as rows rather than as a guess.
		//
		// `| top topic` has always grouped by the first word of the subject
		// line, which the code that does it has always described as a
		// placeholder. On a real archive that put 68% of the mail into two
		// buckets called "fwd:" and "re:". This is where a real answer goes.
		//
		// One message has several topics, so this is a table and not a column.
		// Source records what produced each one, because a topic asserted by a
		// model and a topic derived from a subject line are not equally
		// trustworthy and a reader deserves to know which they are looking at.
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS message_topics (
				message_id TEXT NOT NULL,
				topic      TEXT NOT NULL,
				source     TEXT NOT NULL,
				confidence REAL,
				created_at INTEGER NOT NULL,
				PRIMARY KEY (message_id, topic),
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_message_topics_topic ON message_topics(topic);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v26: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 26;"); err != nil {
			return err
		}
		currentVersion = 26
	}

	if currentVersion < 27 {
		log.Println("Applying schema migration v27 (embeddings)...")
		// A profile is for chat or for embedding, and vectors carry the width
		// of the model that made them.
		//
		// Both facts have to be recorded rather than discovered. A profile
		// pointed at the wrong kind of model fails with a provider error at
		// the moment of use, and vectors of different widths cannot be
		// compared at all — so a stored embedding that does not say which
		// model produced it is unusable the first time the model changes.
		for _, stmt := range []string{
			`ALTER TABLE llm_profiles ADD COLUMN purpose TEXT NOT NULL DEFAULT 'chat';`,
			`ALTER TABLE llm_profiles ADD COLUMN dimensions INTEGER;`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				log.Printf("Warning v27: %v", err)
			}
		}
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS message_embeddings (
				message_id TEXT PRIMARY KEY,
				-- The profile, model and width that produced this vector.
				-- Changing any of them invalidates it, the same way changing
				-- an annotator's instruction invalidates its results.
				profile    TEXT NOT NULL,
				model      TEXT NOT NULL,
				dimensions INTEGER NOT NULL,
				-- float32 little-endian, dimensions wide. A BLOB rather than a
				-- vector extension: sqlite-vec is a cgo extension and a
				-- distribution burden, and a brute-force scan in Go is fast
				-- enough well past the size of a personal mailbox.
				vector     BLOB NOT NULL,
				created_at INTEGER NOT NULL,
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_embeddings_model ON message_embeddings(model, dimensions);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v27: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 27;"); err != nil {
			return err
		}
		currentVersion = 27
	}

	if currentVersion < 28 {
		log.Println("Migrating database schema to version 28 (contact notes and tags)...")
		if _, err := db.Exec(`ALTER TABLE contacts ADD COLUMN notes TEXT NOT NULL DEFAULT '';`); err != nil {
			log.Printf("Warning v28: %v", err)
		}
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS contact_tags (
				address    TEXT NOT NULL REFERENCES contacts(address) ON DELETE CASCADE,
				tag        TEXT NOT NULL,
				created_at INTEGER NOT NULL,
				PRIMARY KEY (address, tag)
			);

			CREATE INDEX IF NOT EXISTS idx_contact_tags_tag ON contact_tags(tag);
			CREATE INDEX IF NOT EXISTS idx_contact_tags_address ON contact_tags(address);
		`)
		if err != nil {
			return fmt.Errorf("failed to apply schema v28: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 28;"); err != nil {
			return err
		}
		currentVersion = 28
	}

	log.Printf("Database schema is up to date (version %d).", SchemaVersion)
	return nil
}

// CloseDB closes the database and allows InitDB to open one again.
//
// Resetting the once matters beyond tidiness: without it, InitDB could only
// ever run once per process, so any second caller silently received the
// already-closed handle and every query failed with "database is closed".
// That made the store untestable from more than one test in a package, and
// forced test files to share a single database opened in TestMain. Production
// opens once and closes at exit, so the reset changes nothing there.
func CloseDB() {
	if db != nil {
		db.Close()
		db = nil
	}
	dbOnce = sync.Once{}
}

// Agent functions
func SaveAgent(a *Agent) error {
	_, err := db.Exec(`
		INSERT INTO agents (id, name, description, schema_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			schema_json = EXCLUDED.schema_json;
	`, a.ID, a.Name, a.Description, a.SchemaJSON)
	return err
}

func GetAgent(id string) (*Agent, error) {
	a := &Agent{}
	err := db.QueryRow("SELECT id, name, description, schema_json FROM agents WHERE id = ?", id).
		Scan(&a.ID, &a.Name, &a.Description, &a.SchemaJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func ListAgents() ([]*Agent, error) {
	rows, err := db.Query("SELECT id, name, description, schema_json FROM agents")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []*Agent
	for rows.Next() {
		a := &Agent{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.SchemaJSON); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func DeleteAgent(id string) error {
	_, err := db.Exec("DELETE FROM agents WHERE id = ?", id)
	return err
}

// App Settings functions
func GetSetting(key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM app_settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func UpdateSetting(key, value string) error {
	_, err := db.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value;
	`, key, value)
	return err
}

// User functions
func SaveUser(u *User) error {
	_, err := db.Exec(`
		INSERT INTO users (id, username, password_hash, display_name, email, profile_image_url)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = EXCLUDED.username,
			password_hash = EXCLUDED.password_hash,
			display_name = EXCLUDED.display_name,
			email = EXCLUDED.email,
			profile_image_url = EXCLUDED.profile_image_url;
	`, u.ID, u.Username, u.PasswordHash, u.DisplayName, u.Email, u.ProfileImageURL)
	return err
}

func GetUserByUsername(username string) (*User, error) {
	u := &User{}
	err := db.QueryRow("SELECT id, username, password_hash, display_name, email, profile_image_url FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Email, &u.ProfileImageURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func GetUserByID(id string) (*User, error) {
	u := &User{}
	err := db.QueryRow("SELECT id, username, password_hash, display_name, email, profile_image_url FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Email, &u.ProfileImageURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func SaveSession(s *Session) error {
	_, err := db.Exec("INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)", s.ID, s.UserID, s.ExpiresAt)
	return err
}

func GetSession(id string) (*Session, error) {
	s := &Session{}
	err := db.QueryRow("SELECT id, user_id, expires_at FROM sessions WHERE id = ?", id).
		Scan(&s.ID, &s.UserID, &s.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func DeleteSession(id string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// Account functions

// SaveAccount persists an account, encrypting its password at rest.
//
// The caller passes a plaintext password and never has to think about the
// vault; encryption happens here so there is exactly one path into the
// accounts table and no way to accidentally write a plaintext row.
func SaveAccount(acc *account.Account) error {
	encrypted, err := vault.Encrypt(acc.Password)
	if err != nil {
		return fmt.Errorf("failed to encrypt account password: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO accounts (id, name, email, host, port, user, password, ssl, smtp_host, smtp_port, last_sync_status, last_sync_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			user = EXCLUDED.user,
			password = EXCLUDED.password,
			ssl = EXCLUDED.ssl,
			smtp_host = EXCLUDED.smtp_host,
			smtp_port = EXCLUDED.smtp_port,
			last_sync_status = EXCLUDED.last_sync_status,
			last_sync_error = EXCLUDED.last_sync_error;
	`, acc.ID, acc.Name, acc.Email, acc.Host, acc.Port, acc.User, encrypted, acc.SSL, acc.SMTPHost, acc.SMTPPort, acc.LastSyncStatus, acc.LastSyncError)
	return err
}

// GetAccount loads an account with its password decrypted, ready to use for an
// IMAP login.
func GetAccount(id string) (*account.Account, error) {
	acc := &account.Account{}
	err := db.QueryRow("SELECT id, name, email, host, port, user, password, ssl, smtp_host, smtp_port, last_sync_status, last_sync_error FROM accounts WHERE id = ?", id).
		Scan(&acc.ID, &acc.Name, &acc.Email, &acc.Host, &acc.Port, &acc.User, &acc.Password, &acc.SSL, &acc.SMTPHost, &acc.SMTPPort, &acc.LastSyncStatus, &acc.LastSyncError)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	decrypted, err := vault.Decrypt(acc.Password)
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", id, err)
	}
	acc.Password = decrypted
	return acc, nil
}

func ListAccounts() ([]*account.Account, error) {
	rows, err := db.Query("SELECT id, name, email, host, port, user, password, ssl, smtp_host, smtp_port, last_sync_status, last_sync_error FROM accounts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accs []*account.Account
	for rows.Next() {
		acc := &account.Account{}
		if err := rows.Scan(&acc.ID, &acc.Name, &acc.Email, &acc.Host, &acc.Port, &acc.User, &acc.Password, &acc.SSL, &acc.SMTPHost, &acc.SMTPPort, &acc.LastSyncStatus, &acc.LastSyncError); err != nil {
			return nil, err
		}
		decrypted, err := vault.Decrypt(acc.Password)
		if err != nil {
			// One unreadable row must not blank out the whole account list, so
			// the error is logged and that account is returned without a
			// password rather than aborting the query.
			log.Printf("WARN: could not decrypt password for account %s: %v", acc.ID, err)
			acc.Password = ""
		} else {
			acc.Password = decrypted
		}
		accs = append(accs, acc)
	}
	return accs, nil
}

func DeleteAccount(id string) error {
	_, err := db.Exec("DELETE FROM accounts WHERE id = ?", id)
	return err
}

func UpdateAccountStatus(id string, status string, lastError string) error {
	_, err := db.Exec("UPDATE accounts SET last_sync_status = ?, last_sync_error = ? WHERE id = ?", status, lastError, id)
	return err
}

// Message functions
func SaveMessage(m *message.Message) error {
	to, _ := json.Marshal(m.To)
	cc, _ := json.Marshal(m.Cc)
	bcc, _ := json.Marshal(m.Bcc)
	flags, _ := json.Marshal(m.Flags)

	// header is BLOB NOT NULL, and a nil []byte binds as NULL. Combined with
	// INSERT OR IGNORE — which is here to make re-imports idempotent against
	// the (account_id, content_hash) index — that turned a message with no
	// header into a silent no-op: zero rows written, nil error returned. The
	// duplicate case is the only one OR IGNORE should ever swallow.
	header := m.Header
	if header == nil {
		header = []byte{}
	}

	res, err := db.Exec(`
		INSERT OR IGNORE INTO messages (id, account_id, uid, message_id, content_hash, normalized_body, from_addr, to_addrs, cc_addrs, bcc_addrs, subject, date, body, html_body, header, flags, size, internal_date, mailbox)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, m.ID, m.AccountID, m.UID, m.MessageID, m.ContentHash, m.NormalizedBody, m.From, string(to), string(cc), string(bcc), m.Subject, m.Date.UnixMilli(), m.Body, m.HTMLBody, header, string(flags), m.Size, m.InternalDate.UnixMilli(), nullIfEmpty(m.Mailbox))
	if err != nil {
		return err
	}

	// Only index a row that was actually written. OR IGNORE above collapses a
	// duplicate onto the existing row, whose id is not this message's id, so
	// writing edges here would point at a message that does not exist and
	// trip the foreign key.
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}

	// The participant and reference edges are derived data, but they are
	// derived once here rather than recomputed per query: negation over
	// recipients is a NOT EXISTS against these rows, and threading walks them.
	if err := writeParticipants(db, m); err != nil {
		return fmt.Errorf("indexing participants for %s: %w", m.ID, err)
	}
	if err := writeRefs(db, m); err != nil {
		return fmt.Errorf("indexing references for %s: %w", m.ID, err)
	}
	// Every message creates a contact for every address it touches. Here
	// rather than in a nightly pass so a contact exists the moment its first
	// message does — and so the two can never be out of step.
	if err := ensureContacts(db, m); err != nil {
		return fmt.Errorf("recording contacts for %s: %w", m.ID, err)
	}
	return nil
}

// ListMessages returns an account's messages, newest first.
//
// A thin wrapper over the query compiler rather than its own SQL: the
// hand-written variant this replaced had already drifted, omitting the mailbox
// column so the list could not tell which folder a message came from.
func ListMessages(accountID string, limit, offset int) ([]*message.Message, error) {
	q := ""
	if accountID != "" {
		q = "account:" + accountID
	}
	res, err := RunQuery(q, limit, offset)
	if err != nil {
		return nil, err
	}
	return res.Messages, nil
}

func GetMessageByID(id string) (*message.Message, error) {
	m := &message.Message{}
	var to, cc, bcc, flags string
	var date, internalDate int64
	err := db.QueryRow("SELECT id, account_id, uid, message_id, content_hash, normalized_body, from_addr, to_addrs, cc_addrs, bcc_addrs, subject, date, body, html_body, header, flags, size, internal_date FROM messages WHERE id = ?", id).
		Scan(&m.ID, &m.AccountID, &m.UID, &m.MessageID, &m.ContentHash, &m.NormalizedBody, &m.From, &to, &cc, &bcc, &m.Subject, &date, &m.Body, &m.HTMLBody, &m.Header, &flags, &m.Size, &internalDate)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(to), &m.To)
	json.Unmarshal([]byte(cc), &m.Cc)
	json.Unmarshal([]byte(bcc), &m.Bcc)
	json.Unmarshal([]byte(flags), &m.Flags)
	m.Date = time.UnixMilli(date)
	m.InternalDate = time.UnixMilli(internalDate)
	return m, nil
}

func MessageExistsByMessageID(messageID string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE message_id = ?", messageID).Scan(&count)
	return count > 0, err
}

// Mailbox Sync State
func SaveMailboxSyncState(s *MailboxSyncState) error {
	_, err := db.Exec(`
		INSERT INTO mailboxes (id, account_id, name, last_uid, last_modseq)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_uid = EXCLUDED.last_uid,
			last_modseq = EXCLUDED.last_modseq;
	`, s.ID, s.AccountID, s.Name, s.LastUID, s.LastMODSEQ)
	return err
}

func GetMailboxSyncState(id string) (*MailboxSyncState, error) {
	s := &MailboxSyncState{}
	err := db.QueryRow("SELECT id, account_id, name, last_uid, last_modseq FROM mailboxes WHERE id = ?", id).
		Scan(&s.ID, &s.AccountID, &s.Name, &s.LastUID, &s.LastMODSEQ)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func GetAccountStats(accountID string) (*account.AccountStats, error) {
	stats := &account.AccountStats{}
	err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE account_id = ?", accountID).Scan(&stats.TotalMessages)
	if err != nil {
		return nil, err
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE account_id = ? AND flags NOT LIKE '%\\Seen%'", accountID).Scan(&stats.UnreadMessages)
	if err != nil {
		return nil, err
	}
	err = db.QueryRow("SELECT COALESCE(SUM(size), 0) FROM messages WHERE account_id = ?", accountID).Scan(&stats.StorageSize)
	if err != nil {
		return nil, err
	}
	if stats.TotalMessages > 0 {
		var lastDate int64
		err = db.QueryRow("SELECT MAX(date) FROM messages WHERE account_id = ?", accountID).Scan(&lastDate)
		if err == nil {
			stats.LastSync = time.UnixMilli(lastDate).Format(time.RFC3339)
		}
	} else {
		stats.LastSync = "Never"
	}
	return stats, nil
}

// ListUsers returns every login, ordered by username.
func ListUsers() ([]*User, error) {
	rows, err := db.Query("SELECT id, username, password_hash, display_name, email, profile_image_url FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Email, &u.ProfileImageURL); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetDefaultUser returns the primary administrator user, or the first available user.
func GetDefaultUser() (*User, error) {
	users, err := ListUsers()
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	for _, u := range users {
		if strings.HasPrefix(u.Username, "admin") {
			return u, nil
		}
	}
	return users[0], nil
}

// DeleteSessionsForUser invalidates every session belonging to a user.
func DeleteSessionsForUser(userID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

// rehashMessages recomputes content_hash for every stored message using
// [hasher.MessageHash].
//
// Run once by migration v10. Rows are read in full first and written in a
// single transaction: rewriting while a cursor is open on the same table risks
// the read seeing its own writes, and one failed row should not leave the table
// half-rehashed.
func rehashMessages(db *sql.DB) error {
	type row struct {
		id, messageID, from, subject, body string
	}

	rows, err := db.Query("SELECT id, message_id, from_addr, subject, body FROM messages")
	if err != nil {
		return err
	}

	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.messageID, &r.from, &r.subject, &r.body); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(all) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE messages SET content_hash = ? WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range all {
		h := hasher.MessageHash(r.messageID, r.from, r.subject, r.body)
		if _, err := stmt.Exec(h, r.id); err != nil {
			return fmt.Errorf("message %s: %w", r.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("Recomputed content hashes for %d message(s).", len(all))
	return nil
}

// EraseSyncedData removes all synchronized messages, mailboxes, attachments, and import jobs.
// Accounts, user profiles, settings, and local drafts are preserved.
func EraseSyncedData() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{"attachments", "messages", "mailboxes", "import_jobs", "error_log"}
	for _, table := range tables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table, err)
		}
	}

	return tx.Commit()
}
