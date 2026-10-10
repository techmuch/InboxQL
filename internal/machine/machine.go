// Package machine is InboxQL's view of the computer it is installed on: where
// the machine-wide settings live, and what they say.
//
// # Why this exists
//
// The data directory used to default to `./data`, relative to wherever a
// command happened to run. That is fine for a developer in a checkout and
// wrong for an installed service: the service starts in one folder, a terminal
// opens in another, and the two quietly read different mailboxes.
//
// So a machine has one settings file, ~/.iql/settings.json, and when it exists
// it is where every command — and the service — finds the mailbox. A checkout
// still works exactly as before when the file is absent, or with --data.
package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultPort is where InboxQL listens unless told otherwise.
//
// Not 8080, which is the most used development port there is — a web server
// being worked on and the mail client in the background would fight over it.
// 8420 is unremarkable enough to be free and stable enough to bookmark.
const DefaultPort = "8420"

// DefaultAddr is the listen address: loopback only, on DefaultPort.
const DefaultAddr = "127.0.0.1:" + DefaultPort

// Settings is the machine-wide configuration.
//
// Kept deliberately small. Everything that belongs to a mailbox — accounts,
// annotators, the log level — lives in that mailbox's database, so moving a
// data directory moves all of it. This file only says which mailbox, and how
// this machine serves it.
type Settings struct {
	// DataDir is the mailbox. "~" is expanded.
	DataDir string `json:"dataDir"`
	// Addr is the listen address for `iql start` and the service.
	Addr string `json:"addr,omitempty"`
	// Models is where model weights are kept, once for the whole machine
	// rather than once per mailbox — they are one to two gigabytes each.
	Models string `json:"models,omitempty"`
	// Hostname is an optional name for the local site, such as
	// inboxql.localhost, set up with `iql hosts set`.
	Hostname string `json:"hostname,omitempty"`
	// HeavyWorkOnBattery lets automatic background work — the annotation
	// sweep after a sync — run on a laptop's battery. Off by default: a
	// twenty-minute extraction pass is not something to start unasked on
	// battery.
	HeavyWorkOnBattery bool `json:"heavyWorkOnBattery,omitempty"`
}

// Home is the machine's InboxQL folder: $INBOXQL_HOME, or ~/.iql.
//
// The environment variable exists for tests above all. A test suite that read
// the developer's real ~/.iql would find their real mailbox — the one thing a
// test must never touch.
func Home() string {
	if h := strings.TrimSpace(os.Getenv("INBOXQL_HOME")); h != "" {
		return Expand(h)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".iql")
	}
	return ".iql"
}

// Path is the settings file.
func Path() string { return filepath.Join(Home(), "settings.json") }

// Defaults are what `iql setup` writes when nothing else is said.
func Defaults() *Settings {
	h := Home()
	return &Settings{
		DataDir: filepath.Join(h, "data"),
		Addr:    DefaultAddr,
		Models:  filepath.Join(h, "models"),
	}
}

// Load reads the settings. found is false, with no error, when there is no
// settings file — which is the ordinary state of a checkout.
func Load() (s *Settings, found bool, err error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s = &Settings{}
	if err := json.Unmarshal(b, s); err != nil {
		// Refused rather than defaulted: a settings file that will not parse
		// is somebody's half-finished edit, and silently ignoring it would
		// send every command back to ./data — a different mailbox.
		return nil, true, fmt.Errorf("%s is not valid JSON: %w", Path(), err)
	}
	if strings.TrimSpace(s.DataDir) == "" {
		return nil, true, fmt.Errorf("%s has no dataDir; it is the one thing the file is for", Path())
	}
	s.DataDir = Expand(s.DataDir)
	s.Models = Expand(s.Models)
	if s.Addr == "" {
		s.Addr = DefaultAddr
	}
	return s, true, nil
}

// Save writes the settings, creating the machine folder if need be.
//
// Written to a temporary file and renamed, so a crash part-way leaves the old
// settings rather than half of new ones.
func Save(s *Settings) error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Expand resolves a leading "~" to the home directory.
func Expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
