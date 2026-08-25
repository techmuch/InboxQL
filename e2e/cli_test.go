//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Exit codes are a published contract in AGENTS.md that agents and scripts
// branch on. Nothing checked them through the real binary before.
func TestExitCodeContract(t *testing.T) {
	e := newEnv(t)

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"account", "list"}, 0},
		{"unknown command", []string{"frobnicate"}, 2},
		{"unknown flag", []string{"search", "--nope"}, 2},
		{"missing argument", []string{"read"}, 2},
		{"no such message", []string{"read", "definitely-not-a-message"}, 3},
		{"bare invocation", []string{}, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.run(tc.args...).ExitCode; got != tc.want {
				t.Errorf("iql %s exited %d, want %d", strings.Join(tc.args, " "), got, tc.want)
			}
		})
	}
}

// Exit 5 means "not configured", which tells a caller to run init rather than
// to investigate a failure. It needs a directory that was never initialised,
// so it cannot use the shared environment.
func TestNotConfiguredExitCode(t *testing.T) {
	e := &env{t: t, dataDir: t.TempDir() + "/never-initialised", bin: binary(t)}

	if got := e.run("account", "list").ExitCode; got != 5 {
		t.Errorf("account list on an uninitialised directory exited %d, want 5", got)
	}
	if got := e.run("doctor").ExitCode; got != 5 {
		t.Errorf("doctor on an uninitialised directory exited %d, want 5", got)
	}
}

// Global flags parse in any position. This was broken until recently, and the
// broken form was the one the tool printed as its own next-step guidance.
func TestGlobalFlagsInAnyPosition(t *testing.T) {
	e := newEnv(t)
	bin := binary(t)

	forms := [][]string{
		{"--data", e.dataDir, "account", "list"},
		{"account", "list", "--data", e.dataDir},
		{"account", "--data", e.dataDir, "list"},
		{"account", "list", "--data=" + e.dataDir},
	}

	for _, args := range forms {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			env2 := &env{t: t, dataDir: e.dataDir, bin: bin}
			r := env2.runWithStdin(nil, args...)
			if r.ExitCode != 0 {
				t.Errorf("iql %s exited %d\n%s", strings.Join(args, " "), r.ExitCode, r.Stderr)
			}
		})
	}
}

// Every --json command must put a JSON document on stdout and nothing else.
// Diagnostics belong on stderr; an agent parsing stdout depends on this.
func TestJSONOutputIsCleanOnStdout(t *testing.T) {
	e := newEnv(t)

	for _, args := range [][]string{
		{"--json", "account", "list"},
		{"--json", "user", "list"},
		{"--json", "draft", "list"},
		{"--json", "outbox", "list"},
		{"--json", "errors", "list"},
		{"--json", "vault", "status"},
		{"--json", "llm", "status"},
		{"--json", "version"},
		{"--json", "search", "--query", "anything"},
		{"--json", "import", "sources"},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			r := e.run(args...)
			var any any
			r.JSON(t, &any)
		})
	}
}

// Routine database logging is suppressed; a person running a command should
// see its result and nothing else.
func TestQuietByDefaultVerboseOnRequest(t *testing.T) {
	e := newEnv(t)

	quiet := e.run("account", "list")
	if strings.Contains(quiet.Stderr, "schema version") {
		t.Errorf("database chatter appeared without --verbose:\n%s", quiet.Stderr)
	}

	loud := e.run("--verbose", "account", "list")
	if !strings.Contains(loud.Stderr, "schema version") {
		t.Errorf("--verbose did not restore database logging:\n%s", loud.Stderr)
	}
}

// The written help must be reachable by the spelling people actually type.
// `iql search --help` used to fall through to the stdlib flag package and
// print a bare alphabetical dump instead.
func TestHelpIsReachableBothWays(t *testing.T) {
	e := newEnv(t)

	for _, cmd := range []string{"search", "account", "import", "draft", "outbox", "backup"} {
		t.Run(cmd, func(t *testing.T) {
			viaHelp := e.run("help", cmd)
			viaFlag := e.run(cmd, "--help")

			if viaHelp.ExitCode != 0 || viaFlag.ExitCode != 0 {
				t.Fatalf("help exited %d / %d", viaHelp.ExitCode, viaFlag.ExitCode)
			}
			if viaHelp.Stdout != viaFlag.Stdout {
				t.Errorf("`iql help %s` and `iql %s --help` print different pages", cmd, cmd)
			}
			if strings.Contains(viaFlag.Stdout, "Usage of "+cmd) {
				t.Errorf("`iql %s --help` fell through to the stdlib flag dump", cmd)
			}
		})
	}
}

// Human output must carry no escape sequences when it is not a terminal, so
// piping into grep or awk yields plain text.
func TestNoAnsiEscapesWhenPiped(t *testing.T) {
	e := newEnv(t)

	for _, args := range [][]string{
		{"account", "list"},
		{"doctor"},
		{"search", "--limit", "5"},
		{"import", "sources"},
	} {
		r := e.run(args...)
		if strings.Contains(r.Stdout, "\033[") {
			t.Errorf("iql %s emitted ANSI escapes to a pipe", strings.Join(args, " "))
		}
	}
}

// init must print next steps that actually work. Three of them used to fail.
func TestInitGuidanceIsRunnable(t *testing.T) {
	e := newEnv(t)

	r := e.run("doctor")
	// doctor exits 1 when checks fail, which is fine on a fresh install with
	// no accounts; what matters is that it ran rather than rejecting the flag.
	if strings.Contains(r.Stderr, "flag provided but not defined") {
		t.Errorf("the command init suggests was rejected:\n%s", r.Stderr)
	}
}
