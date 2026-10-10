package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/serverlock"
	"github.com/user/inboxql/internal/store"
)

// withHome gives a test its own machine folder, optionally with settings.
func withHome(t *testing.T, s *machine.Settings) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("INBOXQL_HOME", home)
	t.Setenv("INBOXQL_DATA", "")
	if s != nil {
		if err := machine.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func where(t *testing.T, args ...string) (map[string]any, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(append([]string{"--json", "where"}, args...), nil, &out, &errOut)
	if code != ExitOK {
		t.Fatalf("where exited %d: %s", code, errOut.String())
	}
	var a map[string]any
	if err := json.Unmarshal(out.Bytes(), &a); err != nil {
		t.Fatalf("where output %q: %v", out.String(), err)
	}
	return a, errOut.String()
}

// The order, first match wins: --data, $INBOXQL_DATA, the machine settings,
// then ./data.
func TestWhichMailboxIsUsed(t *testing.T) {
	machineData := filepath.Join(t.TempDir(), "machine")
	withHome(t, &machine.Settings{DataDir: machineData})

	a, _ := where(t)
	if a["source"] != "settings" || a["dataDir"] != machineData {
		t.Errorf("with settings: %v from %v, want %s from settings", a["dataDir"], a["source"], machineData)
	}

	envData := filepath.Join(t.TempDir(), "env")
	t.Setenv("INBOXQL_DATA", envData)
	a, _ = where(t)
	if a["source"] != "env" || a["dataDir"] != envData {
		t.Errorf("with env: %v from %v", a["dataDir"], a["source"])
	}

	flagData := filepath.Join(t.TempDir(), "flag")
	a, _ = where(t, "--data", flagData)
	if a["source"] != "flag" || a["dataDir"] != flagData {
		t.Errorf("with --data: %v from %v", a["dataDir"], a["source"])
	}
}

// Without machine settings nothing changes: ./data, as every command assumed
// before there were any.
func TestWithoutSettingsTheFolderStillDecides(t *testing.T) {
	withHome(t, nil)
	dir := t.TempDir()
	t.Chdir(dir)

	a, _ := where(t)
	if a["source"] != "local" || a["dataDir"] != filepath.Join(dir, "data") {
		t.Errorf("got %v from %v, want ./data from local", a["dataDir"], a["source"])
	}
}

// The change that would otherwise go unnoticed: a mailbox in this folder, and
// another one in use.
func TestAnUnusedLocalMailboxIsMentioned(t *testing.T) {
	withHome(t, &machine.Settings{DataDir: filepath.Join(t.TempDir(), "machine")})
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", store.DBNAME), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := where(t)
	if !strings.Contains(stderr, "not the mailbox in ./data") {
		t.Errorf("no warning about ./data; stderr was %q", stderr)
	}
}

// A settings file that will not parse is somebody's half-finished edit.
// Falling back to ./data would quietly open a different mailbox.
func TestBrokenSettingsAreRefusedNotIgnored(t *testing.T) {
	home := withHome(t, nil)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	code := Execute([]string{"account", "list"}, nil, &bytes.Buffer{}, &errOut)
	if code != ExitNotConfigured || !strings.Contains(errOut.String(), "not valid JSON") {
		t.Errorf("exit %d, %q; want 5 naming the file", code, errOut.String())
	}
}

func TestSetupWritesSettingsAndAMailbox(t *testing.T) {
	home := withHome(t, nil)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--json", "setup"}, nil, &out, &errOut); code != ExitOK {
		t.Fatalf("setup exited %d: %s", code, errOut.String())
	}
	s, found, err := machine.Load()
	if err != nil || !found {
		t.Fatalf("settings not written: %v", err)
	}
	if s.DataDir != filepath.Join(home, "data") || s.Addr != machine.DefaultAddr {
		t.Errorf("settings %+v", s)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, store.DBNAME)); err != nil {
		t.Errorf("no mailbox made: %v", err)
	}

	// Run again: nothing rewritten, and said so.
	out.Reset()
	if code := Execute([]string{"--json", "setup"}, nil, &out, &errOut); code != ExitOK {
		t.Fatalf("second setup exited %d", code)
	}
	if !strings.Contains(out.String(), `"written": false`) {
		t.Errorf("second setup rewrote the settings: %s", out.String())
	}
}

// Adopting a mailbox that already exists uses it where it is.
func TestSetupCanAdoptAnExistingMailbox(t *testing.T) {
	withHome(t, nil)
	existing := t.TempDir()
	if code := Execute([]string{"--data", existing, "init"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); code != ExitOK {
		t.Fatalf("init exited %d", code)
	}
	if code := Execute([]string{"--data", existing, "setup"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); code != ExitOK {
		t.Fatalf("setup exited %d", code)
	}
	s, _, _ := machine.Load()
	if s == nil || s.DataDir != existing {
		t.Errorf("settings point at %v, want %s", s, existing)
	}
}

// A server with an older schema holds the mailbox: opening it here would
// migrate the database out from under it.
func TestAnOlderServerBlocksAMigration(t *testing.T) {
	withHome(t, nil)
	data := t.TempDir()
	if code := Execute([]string{"--data", data, "init"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); code != ExitOK {
		t.Fatalf("init exited %d", code)
	}
	lock, _, err := serverlock.Acquire(data, serverlock.Info{PID: 1, Schema: store.SchemaVersion - 1})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	var errOut bytes.Buffer
	code := Execute([]string{"--data", data, "account", "list"}, nil, &bytes.Buffer{}, &errOut)
	if code != ExitError || !strings.Contains(errOut.String(), "older version") {
		t.Errorf("exit %d, %q; want a refusal naming the older server", code, errOut.String())
	}
}
