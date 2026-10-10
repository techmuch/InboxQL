package service

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records commands and answers from a script.
type fakeRunner struct {
	calls   []string
	answers map[string]struct {
		out string
		err error
	}
}

func (f *fakeRunner) run(name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	for prefix, a := range f.answers {
		if strings.HasPrefix(line, prefix) {
			return a.out, a.err
		}
	}
	return "", nil
}

func (f *fakeRunner) answer(prefix, out string, err error) {
	if f.answers == nil {
		f.answers = map[string]struct {
			out string
			err error
		}{}
	}
	f.answers[prefix] = struct {
		out string
		err error
	}{out, err}
}

func spec(t *testing.T) Spec {
	return Spec{Binary: "/Users/me/.iql/bin/iql", Home: filepath.Join(t.TempDir(), ".iql")}
}

func TestLaunchAgentIsWellFormedAndRunsTheService(t *testing.T) {
	home := t.TempDir()
	f := &fakeRunner{}
	m, _ := For("darwin", f.run, home)
	s := spec(t)
	s.HomeOverride = "/custom/iql & home"
	if err := m.Install(s); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	// Well-formed XML, including the escaped ampersand: a malformed plist is
	// silently ignored by launchd, and the service simply never starts.
	if err := xml.Unmarshal(b, new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("plist is not well-formed XML: %v", err)
	}
	plist := string(b)
	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/Users/me/.iql/bin/iql</string>", "<string>start</string>", "<string>--service</string>",
		"<key>RunAtLoad</key>", "<key>SuccessfulExit</key>\n\t\t<false/>",
		"<string>/custom/iql &amp; home</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	// No data directory: the service reads the machine settings.
	if strings.Contains(plist, "--data") {
		t.Error("the service names a data directory instead of reading the settings")
	}
	if got := strings.Join(f.calls, "\n"); !strings.Contains(got, "launchctl bootstrap gui/") {
		t.Errorf("did not load the agent:\n%s", got)
	}
}

// Stop must unload, not kill: launchd restarts a process that dies on a
// signal, so a kill would not stay stopped.
func TestStoppingALaunchAgentUnloadsIt(t *testing.T) {
	f := &fakeRunner{}
	m, _ := For("darwin", f.run, t.TempDir())
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "launchctl bootout gui/") {
		t.Errorf("stop ran %v", f.calls)
	}
}

func TestLaunchAgentStatusReadsLaunchctl(t *testing.T) {
	f := &fakeRunner{}
	f.answer("launchctl print", "gui/501/io.github.techmuch.inboxql = {\n\tstate = running\n\tpid = 4242\n}", nil)
	m, _ := For("darwin", f.run, t.TempDir())
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || !strings.Contains(st.Detail, "4242") {
		t.Errorf("status %+v", st)
	}

	f2 := &fakeRunner{}
	f2.answer("launchctl print", "Could not find service", errors.New("exit 113"))
	m2, _ := For("darwin", f2.run, t.TempDir())
	if st, _ := m2.Status(); st.Running {
		t.Error("an unloaded agent reads as running")
	}
}

func TestSystemdUnitRestartsOnlyOnFailure(t *testing.T) {
	f := &fakeRunner{}
	m, _ := For("linux", f.run, t.TempDir())
	s := spec(t)
	s.Binary = "/home/me/bin dir/iql"
	if err := m.Install(s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.File())
	unit := string(b)
	for _, want := range []string{
		`ExecStart="/home/me/bin dir/iql" start --service`,
		"Restart=on-failure", "WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	got := strings.Join(f.calls, "\n")
	if !strings.Contains(got, "systemctl --user daemon-reload") ||
		!strings.Contains(got, "systemctl --user enable --now "+UnitName) {
		t.Errorf("install ran:\n%s", got)
	}
}

func TestSystemdStatusReadsIsActive(t *testing.T) {
	f := &fakeRunner{}
	f.answer("systemctl --user is-active", "inactive", errors.New("exit 3"))
	m, _ := For("linux", f.run, t.TempDir())
	st, _ := m.Status()
	if st.Running || st.Detail != "inactive" {
		t.Errorf("status %+v", st)
	}
}

func TestWindowsTaskRunsHiddenAtLogon(t *testing.T) {
	f := &fakeRunner{}
	m, _ := For("windows", f.run, t.TempDir())
	s := spec(t)
	s.Binary = `C:\Users\me\.iql\bin\iql.exe`
	if err := m.Install(s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.File())
	vbs := string(b)
	// Window style 0 is hidden; the quotes around the path are doubled, as
	// VBScript requires.
	if !strings.Contains(vbs, `shell.Run """C:\Users\me\.iql\bin\iql.exe"" start --service", 0, False`) {
		t.Errorf("launcher script:\n%s", vbs)
	}
	got := strings.Join(f.calls, "\n")
	if !strings.Contains(got, "schtasks /Create /F /TN InboxQL") || !strings.Contains(got, "/SC ONLOGON") {
		t.Errorf("install ran:\n%s", got)
	}
}

// The task has exited by the time iql runs, so stopping goes by the server's
// own PID.
func TestStoppingTheWindowsTaskKillsTheServer(t *testing.T) {
	f := &fakeRunner{}
	m, _ := For("windows", f.run, t.TempDir())
	m.(interface{ SetServerPID(func() int) }).SetServerPID(func() int { return 777 })
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "taskkill /PID 777 /F" {
		t.Errorf("stop ran %v", f.calls)
	}
}

func TestAnUnsupportedPlatformSaysSo(t *testing.T) {
	if _, err := For("plan9", nil, "/"); err == nil {
		t.Error("plan9 was given a service manager")
	}
}

func TestALaunchAgentBeingSpawnedIsStarting(t *testing.T) {
	f := &fakeRunner{}
	f.answer("launchctl print", "\tstate = xpcproxy\n\tpid = 9\n", nil)
	m, _ := For("darwin", f.run, t.TempDir())
	st, _ := m.Status()
	if !st.Starting || st.Running {
		t.Errorf("status %+v, want starting", st)
	}
}
