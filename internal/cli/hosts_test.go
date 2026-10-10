package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/machine"
)

func TestHostsSetRecordsTheNameAndWritesTheFile(t *testing.T) {
	withHome(t, &machine.Settings{DataDir: t.TempDir()})
	hosts := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(hosts, []byte("127.0.0.1\tlocalhost\n"), 0o644)
	t.Setenv("INBOXQL_HOSTS_FILE", hosts)
	t.Setenv("SUDO_USER", "")

	var errOut bytes.Buffer
	if code := Execute([]string{"hosts", "set", "inboxql.localhost"}, nil, &bytes.Buffer{}, &errOut); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(hosts)
	if !strings.Contains(string(b), "127.0.0.1\tinboxql.localhost") || !strings.HasPrefix(string(b), "127.0.0.1\tlocalhost\n") {
		t.Errorf("hosts file:\n%s", b)
	}
	s, _, _ := machine.Load()
	if s.Hostname != "inboxql.localhost" {
		t.Errorf("settings hostname %q", s.Hostname)
	}

	if code := Execute([]string{"hosts", "remove"}, nil, &bytes.Buffer{}, &errOut); code != ExitOK {
		t.Fatalf("remove exit %d", code)
	}
	b, _ = os.ReadFile(hosts)
	if string(b) != "127.0.0.1\tlocalhost\n" {
		t.Errorf("after remove:\n%q", b)
	}
}

// Without administrator rights the name is still recorded, and the command
// says exactly how to finish.
func TestHostsWithoutPermissionSaysHowToFinish(t *testing.T) {
	withHome(t, &machine.Settings{DataDir: t.TempDir()})
	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts")
	os.WriteFile(hosts, []byte(""), 0o444)
	t.Setenv("INBOXQL_HOSTS_FILE", hosts)
	t.Setenv("SUDO_USER", "")
	if os.Getuid() == 0 {
		t.Skip("root can write a read-only file")
	}

	var errOut bytes.Buffer
	code := Execute([]string{"hosts", "set", "inboxql.localhost"}, nil, &bytes.Buffer{}, &errOut)
	if code == ExitOK || !strings.Contains(errOut.String(), "sudo iql hosts set inboxql.localhost") {
		t.Errorf("exit %d, %q", code, errOut.String())
	}
	if s, _, _ := machine.Load(); s.Hostname != "inboxql.localhost" {
		t.Error("the name was not recorded for the elevated run to finish")
	}
}

func TestHostsRefusesDotLocal(t *testing.T) {
	withHome(t, &machine.Settings{DataDir: t.TempDir()})
	var errOut bytes.Buffer
	if code := Execute([]string{"hosts", "set", "inboxql.local"}, nil, &bytes.Buffer{}, &errOut); code != ExitUsage {
		t.Errorf("exit %d, %q; want a usage refusal", code, errOut.String())
	}
}
