// Package backups is the mailbox's backups as something to manage: what each
// one is, how much room they take, which to keep, and whether one can be
// trusted to restore from.
//
// # Why a catalogue
//
// A backup used to be a file and nothing else. Nothing said why it was made,
// whether it would open, or whether it held the key its passwords need — and
// nothing ever removed one, so every update left a full copy of the mailbox
// behind until the disk filled. Each backup now carries a small sidecar, and
// the keep rules here decide which go.
package backups

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Why a backup was made.
const (
	// ReasonUpdate is the copy `iql update` takes before replacing the binary.
	ReasonUpdate = "update"
	// ReasonManual is one somebody asked for. Never pruned.
	ReasonManual = "manual"
	// ReasonScheduled is one the schedule made.
	ReasonScheduled = "scheduled"
	// ReasonRestore is the database a restore set aside.
	ReasonRestore = "restore"
)

// Folder is where backups live under the data directory.
const Folder = "backups"

// Backup is one backup of the mailbox's database.
type Backup struct {
	// Name is the file's name, which is how a backup is referred to.
	Name string `json:"name"`
	Path string `json:"path"`
	// At is when it was made.
	At     time.Time `json:"at"`
	Bytes  int64     `json:"bytes"`
	Reason string    `json:"reason"`
	// Version is the InboxQL that made it, when known; Schema is the
	// database's schema version, read from the file when it is checked.
	Version string `json:"version,omitempty"`
	Schema  int    `json:"schema,omitempty"`
	// Key and Attachments say what came with it, beside it.
	Key         bool  `json:"key"`
	Attachments bool  `json:"attachments"`
	Extra       int64 `json:"extraBytes,omitempty"`
	// Checked is "ok", "failed" or "" for not yet; CheckedAt is when.
	Checked   string     `json:"checked,omitempty"`
	CheckNote string     `json:"checkNote,omitempty"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// Legacy is a file from before the catalogue, recognised by its name.
	Legacy bool `json:"legacy,omitempty"`
}

// sidecar is what a backup's .json beside it holds.
type sidecar struct {
	Reason      string     `json:"reason"`
	At          time.Time  `json:"at"`
	Version     string     `json:"version,omitempty"`
	Schema      int        `json:"schema,omitempty"`
	Key         bool       `json:"key,omitempty"`
	Attachments bool       `json:"attachments,omitempty"`
	Checked     string     `json:"checked,omitempty"`
	CheckNote   string     `json:"checkNote,omitempty"`
	CheckedAt   *time.Time `json:"checkedAt,omitempty"`
}

// Dir is the backups folder of a data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, Folder) }

// freeSpace is FreeBytes, replaceable so a test can play a full disk.
var freeSpace = FreeBytes

func sidecarPath(path string) string { return path + ".json" }
func keyPath(path string) string     { return path + ".vault.key" }
func tarPath(path string) string     { return path + ".attachments.tar" }

func writeSidecar(path string, s sidecar) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sidecarPath(path), b, 0o600)
}

func readSidecar(path string) (sidecar, bool) {
	var s sidecar
	b, err := os.ReadFile(sidecarPath(path))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return s, false
	}
	return s, true
}

// Names before the catalogue, and what they meant.
var (
	legacyUpdate  = regexp.MustCompile(`^inboxql-pre-([0-9][0-9.]*)-\d{8}-\d{6}\.db$`)
	legacyManual  = regexp.MustCompile(`^iql-\d{8}T\d{6}Z(-\d+)?\.db$`)
	legacyRestore = regexp.MustCompile(`^inboxql\.db\.\d{8}T\d{6}Z\.bak$`)
)

// List returns every backup, newest first: the backups folder, and the
// databases a restore set aside in the data folder itself before restores
// filed them in the folder.
func List(dataDir string) ([]Backup, error) {
	var out []Backup
	entries, err := os.ReadDir(Dir(dataDir))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !isDatabase(e.Name()) {
			continue
		}
		if b, ok := describe(filepath.Join(Dir(dataDir), e.Name())); ok {
			out = append(out, b)
		}
	}
	root, err := os.ReadDir(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range root {
		if !e.IsDir() && legacyRestore.MatchString(e.Name()) {
			if b, ok := describe(filepath.Join(dataDir, e.Name())); ok {
				out = append(out, b)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// isDatabase is a backup's own file, not one of its companions.
func isDatabase(name string) bool {
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".bak")
}

func describe(path string) (Backup, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return Backup{}, false
	}
	b := Backup{Name: filepath.Base(path), Path: path, At: fi.ModTime(), Bytes: fi.Size()}
	if s, ok := readSidecar(path); ok {
		b.Reason, b.Version, b.Schema = s.Reason, s.Version, s.Schema
		b.Checked, b.CheckNote, b.CheckedAt = s.Checked, s.CheckNote, s.CheckedAt
		if !s.At.IsZero() {
			b.At = s.At
		}
	} else {
		b.Legacy = true
		switch {
		case legacyUpdate.MatchString(b.Name):
			b.Reason = ReasonUpdate
			b.Version = legacyUpdate.FindStringSubmatch(b.Name)[1]
		case legacyRestore.MatchString(b.Name):
			b.Reason = ReasonRestore
		default:
			b.Reason = ReasonManual
		}
	}
	if fi, err := os.Stat(keyPath(path)); err == nil {
		b.Key, b.Extra = true, b.Extra+fi.Size()
	}
	if fi, err := os.Stat(tarPath(path)); err == nil {
		b.Attachments, b.Extra = true, b.Extra+fi.Size()
	}
	return b, true
}

// Find resolves a backup by name, or by a unique prefix of one.
func Find(dataDir, name string) (*Backup, error) {
	all, err := List(dataDir)
	if err != nil {
		return nil, err
	}
	var hits []Backup
	for _, b := range all {
		if b.Name == name || b.Path == name {
			return &b, nil
		}
		if strings.HasPrefix(b.Name, name) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return &hits[0], nil
	case 0:
		return nil, fmt.Errorf("no backup named %q; iql backup list shows them", name)
	}
	return nil, fmt.Errorf("%q matches %d backups; use more of the name", name, len(hits))
}

// Delete removes a backup and everything beside it: its sidecar, key and
// attachments archive.
func Delete(b Backup) error {
	if err := os.Remove(b.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, p := range []string{sidecarPath(b.Path), keyPath(b.Path), tarPath(b.Path),
		b.Path + "-wal", b.Path + "-shm"} {
		_ = os.Remove(p)
	}
	return nil
}

// Total is the room backups take, companions included.
func Total(all []Backup) int64 {
	var n int64
	for _, b := range all {
		n += b.Bytes + b.Extra
	}
	return n
}
