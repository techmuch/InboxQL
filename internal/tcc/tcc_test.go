package tcc

import (
	"os"
	"runtime"
	"testing"
)

func TestBundleOfTakesTheOutermostApp(t *testing.T) {
	cases := map[string]string{
		"/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal":                                  "/System/Applications/Utilities/Terminal.app",
		"/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper.app/Contents/MacOS/Code Helper": "/Applications/Visual Studio Code.app",
		"/Users/me/.iql/bin/iql": "",
		"/bin/zsh":               "",
	}
	for exe, want := range cases {
		if got := bundleOf(exe); got != want {
			t.Errorf("bundleOf(%q) = %q, want %q", exe, got, want)
		}
	}
}

// Started from a terminal: the terminal app is responsible, however many
// shells are in between.
func TestResponsibleIsTheTerminalThatStartedIt(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	tree := map[int]struct {
		ppid int
		exe  string
	}{
		os.Getppid(): {500, "/bin/zsh"},
		500:          {400, "login"},
		400:          {1, "/Applications/iTerm.app/Contents/MacOS/iTerm2"},
	}
	parentOf = func(pid int) (int, string, bool) { n, ok := tree[pid]; return n.ppid, n.exe, ok }
	defer func() { parentOf = defaultParentOf }()

	g := Responsible("/Users/me/.iql/bin/iql")
	if g.Kind != "app" || g.Name != "iTerm" || g.Path != "/Applications/iTerm.app" {
		t.Errorf("got %+v", g)
	}
}

// The login service: launchd started it, so the program itself.
func TestResponsibleUnderTheServiceIsTheBinary(t *testing.T) {
	SetService(true)
	defer SetService(false)
	g := Responsible("/Users/me/.iql/bin/iql")
	if g.Kind != "binary" || g.Path != "/Users/me/.iql/bin/iql" {
		t.Errorf("got %+v", g)
	}
}

// Reparented to launchd without being the service — tmux, screen, SSH. It
// says it cannot tell, rather than naming the binary and being wrong.
func TestResponsibleUnderAMultiplexerIsUnsure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	parentOf = func(pid int) (int, string, bool) {
		if pid == os.Getppid() {
			return 77, "/bin/zsh", true
		}
		return 1, "/opt/homebrew/bin/tmux", true
	}
	defer func() { parentOf = defaultParentOf }()
	g := Responsible("/Users/me/.iql/bin/iql")
	if g.Kind != "unsure" || g.Binary != "/Users/me/.iql/bin/iql" {
		t.Errorf("got %+v", g)
	}
}
