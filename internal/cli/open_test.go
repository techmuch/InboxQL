package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/serverlock"
)

// open goes to the address the running server recorded, which is the one
// with the site name in it.
func TestOpenUsesTheRunningServersAddress(t *testing.T) {
	dir := t.TempDir()
	lock, _, err := serverlock.Acquire(dir, serverlock.Info{PID: 1, URL: "http://inboxql.localhost:8420"})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	var opened string
	browse = func(u string) error { opened = u; return nil }
	defer func() { browse = openBrowser }()

	var out bytes.Buffer
	if code := Execute([]string{"open", "--data", dir}, nil, &out, &out); code != ExitOK {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if opened != "http://inboxql.localhost:8420" {
		t.Errorf("opened %q", opened)
	}

	opened, out = "", bytes.Buffer{}
	if code := Execute([]string{"open", "--print", "--data", dir}, nil, &out, &out); code != ExitOK || opened != "" {
		t.Errorf("--print exit %d, opened %q", code, opened)
	}
	if strings.TrimSpace(out.String()) != "http://inboxql.localhost:8420" {
		t.Errorf("--print printed %q", out.String())
	}
}

// With nothing running it starts nothing, and says what would.
func TestOpenWithNothingRunning(t *testing.T) {
	called := false
	browse = func(string) error { called = true; return nil }
	defer func() { browse = openBrowser }()
	var out bytes.Buffer
	code := Execute([]string{"open", "--data", t.TempDir()}, nil, &out, &out)
	if code != ExitNotConfigured || called {
		t.Fatalf("exit %d, browsed %v: %s", code, called, out.String())
	}
	if !strings.Contains(out.String(), "not running") {
		t.Errorf("message = %q", out.String())
	}
}
