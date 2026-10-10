// Package serverlock makes a data directory serve from one process at a time.
//
// # Why
//
// Nothing stopped two servers opening one mailbox. Installed as a service,
// that stops being hypothetical: the service starts at login, somebody types
// `iql start` in a terminal, and two sync loops write the same mail. And a
// newer CLI, opening a database an older server is serving, migrates the schema
// out from under it.
//
// So a server holds an operating-system lock on a file in the data directory
// for as long as it runs, and writes who it is into that file. The lock is the
// kernel's, not a PID in a file: it is released when the process dies however
// it dies, so a crash never leaves a mailbox locked.
package serverlock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the lock file inside a data directory.
const FileName = "server.lock"

// Info is what a running server says about itself.
type Info struct {
	PID     int       `json:"pid"`
	URL     string    `json:"url"`
	Addr    string    `json:"addr"`
	Version string    `json:"version"`
	Schema  int       `json:"schema"`
	Started time.Time `json:"started"`
}

// ErrHeld is returned by Acquire when another process serves the directory.
var ErrHeld = errors.New("another InboxQL server is using this data directory")

// Lock is a held lock. Release it, or let the process exit.
type Lock struct {
	f *os.File
}

// Acquire takes the lock and records info in it. When another process holds
// it, the error is ErrHeld and the returned Info is what that process wrote.
func Acquire(dataDir string, info Info) (*Lock, *Info, error) {
	f, err := os.OpenFile(filepath.Join(dataDir, FileName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := tryLock(f); err != nil {
		f.Close()
		other, _ := Read(dataDir)
		return nil, other, ErrHeld
	}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := f.Truncate(0); err != nil {
		unlock(f)
		f.Close()
		return nil, nil, err
	}
	if _, err := f.WriteAt(append(b, '\n'), 0); err != nil {
		unlock(f)
		f.Close()
		return nil, nil, err
	}
	_ = f.Sync()
	return &Lock{f: f}, nil, nil
}

// Release gives the lock up.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	// Emptied before unlocking, so a reader never sees a stale server in a
	// file whose lock is free.
	_ = l.f.Truncate(0)
	unlock(l.f)
	l.f.Close()
	l.f = nil
}

// Held reports whether a server holds the directory, and what it wrote.
//
// It asks the kernel by trying for the lock itself, never by reading the PID:
// a PID outlives nothing and is reused by everything.
func Held(dataDir string) (*Info, bool) {
	f, err := os.OpenFile(filepath.Join(dataDir, FileName), os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if err := tryLock(f); err == nil {
		unlock(f)
		return nil, false
	}
	info, err := Read(dataDir)
	if err != nil {
		return &Info{}, true
	}
	return info, true
}

// Read parses what the lock file says, held or not.
func Read(dataDir string) (*Info, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if err != nil {
		return nil, err
	}
	info := &Info{}
	if err := json.Unmarshal(b, info); err != nil {
		return nil, err
	}
	return info, nil
}
