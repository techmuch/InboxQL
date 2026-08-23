package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// DB exposes the open handle for callers that need raw access. Prefer the
// typed helpers; this exists for maintenance operations that are pure SQL.
func DB() *sql.DB { return db }

// SchemaVersionOnDisk reads PRAGMA user_version from the open database.
//
// Distinct from the SchemaVersion constant, which is what this binary expects:
// comparing the two is how doctor detects a database from a different build.
func SchemaVersionOnDisk() (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version;").Scan(&v); err != nil {
		return 0, fmt.Errorf("cannot read schema version: %w", err)
	}
	return v, nil
}

// IntegrityCheck runs SQLite's integrity_check and returns an error describing
// any corruption found.
func IntegrityCheck() error {
	rows, err := db.Query("PRAGMA integrity_check;")
	if err != nil {
		return fmt.Errorf("integrity_check failed to run: %w", err)
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		// A healthy database returns the single row "ok".
		if !strings.EqualFold(line, "ok") {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("integrity_check reported %d problem(s): %s",
			len(problems), strings.Join(problems, "; "))
	}
	return nil
}

// Vacuum rebuilds the database, reclaiming space freed by deletions.
func Vacuum() error {
	_, err := db.Exec("VACUUM;")
	return err
}

// Analyze refreshes the query planner's statistics.
func Analyze() error {
	_, err := db.Exec("ANALYZE;")
	return err
}

// Checkpoint flushes the write-ahead log back into the main database file.
//
// Worth doing before copying the database by hand: without it, recent writes
// live only in the -wal sidecar and a naive file copy loses them.
func Checkpoint() error {
	_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// BackupTo writes a consistent copy of the database to destPath while the
// application keeps running.
//
// Uses SQLite's online backup API rather than copying the file, which is what
// requirements.md 2.4 asks for: a plain copy of a live WAL database can catch
// a torn write, and copying uea.db without its -wal sidecar silently loses
// whatever had not been checkpointed.
func BackupTo(destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("cannot create backup directory: %w", err)
	}
	// Refuse rather than overwrite: a backup command that can destroy an
	// earlier backup by name collision is a trap.
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("%s already exists", destPath)
	}

	destDB, err := sql.Open("sqlite3", destPath)
	if err != nil {
		return fmt.Errorf("cannot create backup database: %w", err)
	}
	defer destDB.Close()

	destConn, err := destDB.Conn(context.Background())
	if err != nil {
		return err
	}
	defer destConn.Close()

	srcConn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer srcConn.Close()

	return destConn.Raw(func(destRaw any) error {
		return srcConn.Raw(func(srcRaw any) error {
			dest, ok := destRaw.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected destination driver connection")
			}
			src, ok := srcRaw.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected source driver connection")
			}

			backup, err := dest.Backup("main", src, "main")
			if err != nil {
				return fmt.Errorf("cannot start backup: %w", err)
			}
			// -1 copies every remaining page in one step.
			if _, err := backup.Step(-1); err != nil {
				backup.Finish()
				return fmt.Errorf("backup failed: %w", err)
			}
			return backup.Finish()
		})
	})
}
