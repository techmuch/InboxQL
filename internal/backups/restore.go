package backups

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/user/inboxql/internal/store"
)

// Restored is what a restore did.
type Restored struct {
	From string `json:"from"`
	// SetAside is the database that was replaced, now a backup of its own.
	SetAside string `json:"setAside,omitempty"`
	Schema   int    `json:"schema"`
	// Upgrade says the restored database is older and will be brought up to
	// date when it is opened.
	Upgrade bool `json:"upgrade,omitempty"`
}

// CanRestore says whether a backup can be restored by this InboxQL: it opens,
// it is whole, and it is not from a newer version than this one.
func CanRestore(path string) (schema int, err error) {
	schema, _, err = inspect(path)
	if err != nil {
		return schema, fmt.Errorf("this backup cannot be restored: %w", err)
	}
	if schema > store.SchemaVersion {
		return schema, fmt.Errorf("this backup was made by a newer InboxQL (schema v%d; this one reads up to v%d) — update first",
			schema, store.SchemaVersion)
	}
	return schema, nil
}

// RestoreNow replaces the mailbox's database with a backup. Nothing may have
// the database open: the CLI refuses while a server holds the mailbox, and
// the server applies a staged restore before it opens it.
//
// The current database is set aside as a backup of its own, never deleted —
// if this was the wrong backup, the way back is one more restore.
func RestoreNow(dataDir, source, version string) (*Restored, error) {
	schema, err := CanRestore(source)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(dataDir, store.DBNAME)
	out := &Restored{From: source, Schema: schema, Upgrade: schema < store.SchemaVersion}

	src, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if free, err := freeSpace(dataDir); err == nil && free < src.Size()+256<<20 {
		return nil, &NoRoom{Need: src.Size() + 256<<20, Free: free}
	}

	if _, err := os.Stat(target); err == nil {
		// Fold the write-ahead log into the file first, so what is set aside
		// is the whole of the current mailbox and not most of it.
		if err := checkpoint(target); err != nil {
			return nil, fmt.Errorf("cannot settle the current database before setting it aside: %w", err)
		}
		if err := os.MkdirAll(Dir(dataDir), 0o755); err != nil {
			return nil, err
		}
		aside := unique(filepath.Join(Dir(dataDir), nameFor(ReasonRestore, version, time.Now())))
		current, _, _ := inspect(target)
		if err := os.Rename(target, aside); err != nil {
			return nil, fmt.Errorf("cannot set the current database aside: %w", err)
		}
		os.Remove(target + "-wal")
		os.Remove(target + "-shm")
		_ = writeSidecar(aside, sidecar{Reason: ReasonRestore, At: time.Now(), Version: version, Schema: current})
		out.SetAside = aside
	}

	// Copied in a stream, not read whole: a mailbox is gigabytes. Written to
	// a temporary name and renamed, so a copy that fails part-way never sits
	// where the database should be.
	tmp := target + ".restoring"
	if err := copyFile(source, tmp); err != nil {
		os.Remove(tmp)
		if out.SetAside != "" {
			_ = os.Rename(out.SetAside, target) // put things back
		}
		return nil, fmt.Errorf("copying the backup: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		if out.SetAside != "" {
			_ = os.Rename(out.SetAside, target)
		}
		return nil, err
	}
	return out, Prune0(dataDir)
}

// Prune0 prunes and logs, for callers with nowhere to report it.
func Prune0(dataDir string) error {
	gone, err := Prune(dataDir, false)
	for _, b := range gone {
		log.Printf("[backups] removed %s (keep rules)", b.Name)
	}
	return err
}

func checkpoint(path string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pending is a restore asked for while the server had the database open.
type pending struct {
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

func pendingPath(dataDir string) string { return filepath.Join(dataDir, "restore-pending.json") }

// Stage asks for a restore the next time the server starts, which is the
// only moment nothing has the database open.
func Stage(dataDir, source string) error {
	if _, err := CanRestore(source); err != nil {
		return err
	}
	b, _ := json.Marshal(pending{Source: source, At: time.Now()})
	return os.WriteFile(pendingPath(dataDir), b, 0o600)
}

// Staged is the restore waiting for a restart, if there is one.
func Staged(dataDir string) string {
	var p pending
	b, err := os.ReadFile(pendingPath(dataDir))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return ""
	}
	return p.Source
}

// Unstage drops a staged restore.
func Unstage(dataDir string) error {
	err := os.Remove(pendingPath(dataDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ApplyStaged carries out a staged restore, before the database is opened.
// The request is removed first, whatever happens: a restore that fails is
// reported once, not retried at every start until somebody notices.
func ApplyStaged(dataDir, version string) (*Restored, error) {
	source := Staged(dataDir)
	if source == "" {
		return nil, nil
	}
	if err := Unstage(dataDir); err != nil {
		return nil, err
	}
	return RestoreNow(dataDir, source, version)
}
