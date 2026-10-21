package backups

import (
	"archive/tar"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/store"
)

// Options are what a new backup is.
type Options struct {
	DataDir string
	Reason  string
	// Version is the InboxQL making it.
	Version string
	// Dest is a path chosen by the caller; empty means the backups folder,
	// named for its reason.
	Dest string
	// Key copies the vault key beside it; Attachments archives the
	// attachment files beside it.
	Key         bool
	Attachments bool
	// NoCheck skips reading the new backup back. Checking is the default:
	// a backup that will not open is worse than none, because it is trusted.
	NoCheck bool
	// Progress, when set, is told how far the copy has got.
	Progress func(done, total int64, stage string)
}

// Made is what making a backup did.
type Made struct {
	Backup Backup `json:"backup"`
	// MadeRoom are backups removed first to make room — ones the keep rules
	// would have removed once this one existed.
	MadeRoom []Backup `json:"madeRoom,omitempty"`
	// Pruned are removed afterwards by the keep rules.
	Pruned []Backup `json:"pruned,omitempty"`
	// AttachmentsLeftOut counts attachment files not in this backup.
	AttachmentsLeftOut int `json:"attachmentsLeftOut,omitempty"`
}

// NoRoom is a backup refused for want of disk space, before anything was
// written.
type NoRoom struct {
	Need, Free int64
	// Could is what removing every backup the keep rules allow would free;
	// still not enough, or the backup would have gone ahead.
	Could int64
}

func (e *NoRoom) Error() string {
	return fmt.Sprintf("not enough disk space for a backup: it needs about %s and %s is free%s",
		Human(e.Need), Human(e.Free), func() string {
			if e.Could > 0 {
				return fmt.Sprintf(" (%s more after removing the backups the keep rules allow)", Human(e.Could))
			}
			return ""
		}())
}

// DatabaseBytes is the mailbox database's size, its write-ahead log included.
func DatabaseBytes(dataDir string) int64 {
	var n int64
	for _, s := range []string{"", "-wal"} {
		if fi, err := os.Stat(filepath.Join(dataDir, store.DBNAME+s)); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// Need is the room a backup of this mailbox needs: its size, and a margin so
// the disk is not left with nothing at all.
func Need(dataDir string, attachments bool) int64 {
	n := DatabaseBytes(dataDir)
	if attachments {
		if _, b, err := blobstore.New(dataDir).Usage(); err == nil {
			n += b
		}
	}
	return n + n/20 + 512<<20
}

// Make takes a backup. The store must be open: the copy is SQLite's online
// backup, consistent while the server keeps running.
func Make(o Options) (*Made, error) {
	if o.Reason == "" {
		o.Reason = ReasonManual
	}
	out := &Made{}
	inFolder := o.Dest == ""
	dest := o.Dest
	if inFolder {
		if err := os.MkdirAll(Dir(o.DataDir), 0o755); err != nil {
			return nil, fmt.Errorf("cannot create %s: %w", Dir(o.DataDir), err)
		}
		dest = unique(filepath.Join(Dir(o.DataDir), nameFor(o.Reason, o.Version, time.Now())))
	} else if _, err := os.Stat(dest); err == nil {
		// Refuse rather than overwrite: a backup someone named is never
		// one they meant to lose.
		return nil, fmt.Errorf("%s already exists", dest)
	}

	// Room first, before a byte is written.
	need := Need(o.DataDir, o.Attachments)
	free, err := freeSpace(filepath.Dir(dest))
	if err == nil && free < need {
		var room []Backup
		var could int64
		if inFolder {
			all, err := List(o.DataDir)
			if err != nil {
				return nil, err
			}
			room = excess(all, LoadPolicy(o.DataDir), o.Reason, 1)
			for _, b := range room {
				could += b.Bytes + b.Extra
			}
		}
		if free+could < need {
			return nil, &NoRoom{Need: need, Free: free, Could: could}
		}
		// Only what makes the room, oldest first.
		for i := len(room) - 1; i >= 0 && free < need; i-- {
			if err := Delete(room[i]); err != nil {
				return nil, err
			}
			free += room[i].Bytes + room[i].Extra
			out.MadeRoom = append(out.MadeRoom, room[i])
		}
	}

	// Written under another name and renamed when whole, so a backup being
	// made — or one cut off part-way — is never listed as a backup.
	total := DatabaseBytes(o.DataDir)
	partial := dest + ".partial"
	stop := watch(partial, total, o.Progress)
	err = store.BackupTo(partial)
	stop()
	if err == nil {
		err = os.Rename(partial, dest)
	}
	if err != nil {
		_ = os.Remove(partial)
		return nil, err
	}

	s := sidecar{Reason: o.Reason, At: time.Now(), Version: o.Version}
	if o.Key {
		data, err := os.ReadFile(filepath.Join(o.DataDir, "vault.key"))
		if err == nil {
			err = os.WriteFile(keyPath(dest), data, 0o600)
		}
		if err != nil {
			return nil, fmt.Errorf("the database was backed up but the vault key was not: %w", err)
		}
		s.Key = true
	}
	blobs := blobstore.New(o.DataDir)
	count, _, _ := blobs.Usage()
	switch {
	case count == 0:
	case o.Attachments:
		if o.Progress != nil {
			o.Progress(total, total, "attachments")
		}
		if err := TarDirectory(blobs.Root(), tarPath(dest)); err != nil {
			return nil, fmt.Errorf("the database was backed up but the attachments were not: %w", err)
		}
		s.Attachments = true
	default:
		out.AttachmentsLeftOut = count
	}
	if err := writeSidecar(dest, s); err != nil {
		return nil, err
	}

	if !o.NoCheck {
		if o.Progress != nil {
			o.Progress(total, total, "checking")
		}
		if _, err := Check(dest); err != nil {
			b, _ := describe(dest)
			out.Backup = b
			// A backup that fails its check is not a reason to remove a good
			// one, so nothing is pruned after it.
			return out, fmt.Errorf("the backup was written but failed its check: %w", err)
		}
	}
	if inFolder {
		if out.Pruned, err = Prune(o.DataDir, false); err != nil {
			return nil, err
		}
	}
	b, _ := describe(dest)
	out.Backup = b
	return out, nil
}

// watch reports a copy's progress by the size of the file being written: the
// backup runs in one step, holding its read lock, so it has nothing to tell.
func watch(dest string, total int64, progress func(done, total int64, stage string)) func() {
	if progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if fi, err := os.Stat(dest); err == nil {
					progress(min(fi.Size(), total), total, "copying")
				}
			}
		}
	}()
	return func() { close(done) }
}

func nameFor(reason, version string, at time.Time) string {
	switch reason {
	case ReasonUpdate:
		return fmt.Sprintf("inboxql-pre-%s-%s.db", strings.TrimPrefix(version, "v"), at.Format("20060102-150405"))
	case ReasonScheduled:
		return "scheduled-" + at.Format("20060102-150405") + ".db"
	case ReasonRestore:
		return "before-restore-" + at.Format("20060102-150405") + ".db"
	}
	return "iql-" + at.UTC().Format("20060102T150405Z") + ".db"
}

// unique steps a generated name aside from one already there: names are
// second-granular, and two backups a moment apart would otherwise collide.
func unique(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	base := strings.TrimSuffix(path, ".db")
	for i := 2; ; i++ {
		p := fmt.Sprintf("%s-%d.db", base, i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// Check reads a backup back: SQLite's quick_check, and the schema version it
// holds. The answer is written beside it, so the list can say so.
func Check(path string) (schema int, err error) {
	schema, note, err := inspect(path)
	s, ok := readSidecar(path)
	if !ok {
		b, _ := describe(path)
		s = sidecar{Reason: b.Reason, At: b.At, Version: b.Version}
	}
	now := time.Now()
	s.CheckedAt, s.Schema = &now, schema
	if err != nil {
		s.Checked, s.CheckNote = "failed", err.Error()
	} else {
		s.Checked, s.CheckNote = "ok", note
	}
	if werr := writeSidecar(path, s); werr != nil && err == nil {
		err = werr
	}
	return schema, err
}

// inspect opens a database read-only and asks it whether it is whole.
func inspect(path string) (schema int, note string, err error) {
	if _, err := os.Stat(path); err != nil {
		return 0, "", err
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return 0, "", err
	}
	defer db.Close()
	if err := db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
		return 0, "", fmt.Errorf("not a database InboxQL can read: %w", err)
	}
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return schema, "", err
	}
	if result != "ok" {
		return schema, "", fmt.Errorf("quick_check: %s", result)
	}
	return schema, fmt.Sprintf("schema v%d", schema), nil
}

// TarDirectory archives a directory tree into dest, refusing to overwrite.
func TarDirectory(root, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	w := tar.NewWriter(out)
	defer w.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if err := w.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

// Human is a byte count as a person reads it.
func Human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
