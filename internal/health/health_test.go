package health

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

func check(rep *Report, name string) *Check {
	for i := range rep.Checks {
		if rep.Checks[i].Name == name {
			return &rep.Checks[i]
		}
	}
	return nil
}

func openFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := store.InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)
	return dir
}

func TestRunReportsOnAFreshDatabase(t *testing.T) {
	dir := openFixture(t)

	rep := Run(Options{DataDir: dir, DBPath: filepath.Join(dir, store.DBNAME), SkipNetwork: true})

	for _, name := range []string{
		"data directory", "database", "schema version", "database integrity",
		"account credentials", "llm provider",
	} {
		if check(rep, name) == nil {
			t.Errorf("no %q check in the report", name)
		}
	}
	if rep.NotConfigured {
		t.Error("an initialised directory reported as not configured")
	}
	// A fresh database has no accounts and no model, which are warnings rather
	// than faults: neither stops the app working.
	if rep.Failed() {
		for _, c := range rep.Checks {
			if c.Status == StatusFail {
				t.Errorf("unexpected failure: %s — %s", c.Name, c.Detail)
			}
		}
	}
}

// A directory that was never initialised is a different answer from one that
// is broken: the first is fixed by running init, the second by investigating.
func TestNotConfigured(t *testing.T) {
	rep := Run(Options{DataDir: filepath.Join(t.TempDir(), "never-made"), SkipNetwork: true})

	if !rep.NotConfigured {
		t.Error("a missing data directory did not report as not configured")
	}
	if c := check(rep, "data directory"); c == nil || c.Status != StatusFail {
		t.Errorf("data directory check = %+v, want a failure", c)
	}
	// It stops there rather than reporting on a database it cannot reach.
	if check(rep, "schema version") != nil {
		t.Error("checks continued past an unusable data directory")
	}
}

func TestOpenStoreFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("cannot open the database")

	rep := Run(Options{
		DataDir:       dir,
		SkipNetwork:   true,
		OpenStore:     func() error { return boom },
		NotConfigured: func(err error) bool { return errors.Is(err, boom) },
	})

	c := check(rep, "database")
	if c == nil || c.Status != StatusFail {
		t.Fatalf("database check = %+v, want a failure", c)
	}
	if !strings.Contains(c.Detail, "cannot open") {
		t.Errorf("detail %q does not carry the reason", c.Detail)
	}
	if !rep.NotConfigured {
		t.Error("the caller said this error means not-configured and it was ignored")
	}
}

// The whole point of Job: a UI puts a button on a check without knowing
// anything about what the check means, and never by parsing the remedy text.
func TestFixableChecksNameTheirJob(t *testing.T) {
	dir := openFixture(t)
	rep := Run(Options{DataDir: dir, DBPath: filepath.Join(dir, store.DBNAME), SkipNetwork: true})

	known := map[string]bool{}
	for _, j := range Jobs {
		known[j] = true
	}
	for _, c := range rep.Checks {
		if c.Job == "" {
			continue
		}
		if !known[c.Job] {
			t.Errorf("check %q names job %q, which is not a known kind", c.Name, c.Job)
		}
		// A button with nothing to say is a button nobody understands.
		if c.Detail == "" {
			t.Errorf("check %q offers a job but says nothing", c.Name)
		}
	}
}

func TestVaultKeyPermissions(t *testing.T) {
	dir := openFixture(t)
	keyPath := filepath.Join(dir, "vault.key")

	// World-readable: the key that decrypts every stored password.
	if err := os.WriteFile(keyPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := Run(Options{DataDir: dir, DBPath: filepath.Join(dir, store.DBNAME), SkipNetwork: true})
	if c := check(rep, "vault key"); c == nil || c.Status != StatusWarn {
		t.Errorf("vault key check = %+v, want a warning about the mode", c)
	}

	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	rep = Run(Options{DataDir: dir, DBPath: filepath.Join(dir, store.DBNAME), SkipNetwork: true})
	if c := check(rep, "vault key"); c == nil || c.Status != StatusOK {
		t.Errorf("vault key check = %+v, want ok at mode 600", c)
	}
}

func TestFailedReflectsTheChecks(t *testing.T) {
	rep := &Report{}
	if rep.Failed() {
		t.Error("an empty report reports failure")
	}
	rep.add("a", StatusOK, "fine")
	rep.add("b", StatusWarn, "worth knowing")
	if rep.Failed() {
		t.Error("warnings counted as failures — they are not the same thing")
	}
	rep.add("c", StatusFail, "broken")
	if !rep.Failed() {
		t.Error("a failure was not reported")
	}
}
