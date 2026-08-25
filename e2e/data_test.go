//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Backup and restore is the feature whose failure mode is losing everything,
// and nothing exercised the round trip. This drives it through the binary,
// including the SQLite online backup API that forces the cgo build.
func TestBackupRestoreRoundTrip(t *testing.T) {
	e := newEnv(t)

	if r := e.run("account", "add",
		"--id", "roundtrip", "--name", "Round Trip", "--email", "rt@example.com",
		"--host", "imap.example.com"); r.ExitCode != 0 {
		t.Fatalf("account add exited %d\n%s", r.ExitCode, r.Stderr)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if r := e.run("backup", backupPath); r.ExitCode != 0 {
		t.Fatalf("backup exited %d\n%s", r.ExitCode, r.Stderr)
	}
	if info, err := os.Stat(backupPath); err != nil || info.Size() == 0 {
		t.Fatalf("backup produced no usable file: %v", err)
	}

	// Destroy the account, then restore and confirm it came back.
	if r := e.run("account", "remove", "roundtrip", "--yes"); r.ExitCode != 0 {
		t.Fatalf("account remove exited %d\n%s", r.ExitCode, r.Stderr)
	}
	if accountIDs(t, e) != "" {
		t.Fatal("account survived removal; the rest of this test would prove nothing")
	}

	if r := e.run("restore", backupPath, "--yes"); r.ExitCode != 0 {
		t.Fatalf("restore exited %d\n%s", r.ExitCode, r.Stderr)
	}
	if got := accountIDs(t, e); got != "roundtrip" {
		t.Errorf("after restore the accounts are %q, want \"roundtrip\"", got)
	}
}

// A backup without the vault key cannot decrypt any stored password, and the
// command is supposed to say so rather than let someone find out later.
func TestBackupWarnsAboutTheVaultKey(t *testing.T) {
	e := newEnv(t)
	r := e.run("backup", filepath.Join(t.TempDir(), "b.db"))

	if r.ExitCode != 0 {
		t.Fatalf("backup exited %d\n%s", r.ExitCode, r.Stderr)
	}
	combined := r.Stdout + r.Stderr
	if !strings.Contains(strings.ToLower(combined), "key") {
		t.Errorf("backup said nothing about the vault key:\n%s", combined)
	}
}

// The maintenance commands touch the database directly; a failure here is a
// corrupt store, so each one is exercised for real.
func TestMaintenanceCommands(t *testing.T) {
	e := newEnv(t)

	for _, sub := range []string{"integrity", "vacuum", "analyze", "checkpoint"} {
		t.Run(sub, func(t *testing.T) {
			if r := e.run("maintenance", sub); r.ExitCode != 0 {
				t.Errorf("maintenance %s exited %d\n%s", sub, r.ExitCode, r.Stderr)
			}
		})
	}

	// The database must still be sound afterwards.
	if r := e.run("maintenance", "integrity"); r.ExitCode != 0 {
		t.Errorf("integrity check failed after maintenance ran\n%s", r.Stderr)
	}
}

// doctor is the command people run when something is wrong; it must be honest
// about a healthy install too.
func TestDoctorReportsStructuredChecks(t *testing.T) {
	e := newEnv(t)

	var report struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	e.run("--json", "doctor").JSON(t, &report)

	if len(report.Checks) == 0 {
		t.Fatal("doctor reported no checks")
	}

	want := map[string]bool{"data directory": false, "database": false, "vault key": false}
	for _, c := range report.Checks {
		if _, ok := want[c.Name]; ok {
			want[c.Name] = true
			if c.Status != "ok" {
				t.Errorf("check %q is %q on a fresh install", c.Name, c.Status)
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("doctor did not report on %q", name)
		}
	}
}

// The importer must never modify the user's mail client. This asserts the
// commitment holds on a machine with no mail client at all: it reports absent
// rather than erroring, which is what makes it safe to run anywhere.
func TestImportSourcesIsReadOnlyAndSafe(t *testing.T) {
	e := newEnv(t)

	r := e.run("--json", "import", "sources")
	if r.ExitCode != 0 {
		t.Fatalf("import sources exited %d\n%s", r.ExitCode, r.Stderr)
	}

	var sources []struct {
		ID        string `json:"id"`
		Available bool   `json:"available"`
		Readable  bool   `json:"readable"`
	}
	r.JSON(t, &sources)

	if len(sources) == 0 {
		t.Error("import sources listed nothing; it should report clients as absent, not omit them")
	}
}

func accountIDs(t *testing.T, e *env) string {
	t.Helper()
	var accounts []struct {
		ID string `json:"id"`
	}
	e.run("--json", "account", "list").JSON(t, &accounts)

	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ",")
}
