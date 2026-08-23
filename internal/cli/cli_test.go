package cli

import (
	"flag"
	"io"
	"testing"
)

// Go's flag package stops at the first positional, so `uea read m1 --thread`
// would silently drop --thread. That is the failure parseArgs exists to
// prevent, and it is worth pinning: a dropped flag produces wrong output with
// no error at all.
func TestParseArgsAcceptsFlagsAfterPositionals(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantThread bool
		wantPrompt string
		wantPos    []string
	}{
		{"flag after positional", []string{"m1", "--thread"}, true, "", []string{"m1"}},
		{"flag before positional", []string{"--thread", "m1"}, true, "", []string{"m1"}},
		{"value flag after positional", []string{"m1", "--prompt", "what now"}, false, "what now", []string{"m1"}},
		{"equals form after positional", []string{"m1", "--prompt=what now"}, false, "what now", []string{"m1"}},
		{"both kinds interleaved", []string{"--thread", "m1", "--prompt", "x"}, true, "x", []string{"m1"}},
		{"positional only", []string{"m1"}, false, "", []string{"m1"}},
		{"single dash short form", []string{"m1", "-thread"}, true, "", []string{"m1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			thread := fs.Bool("thread", false, "")
			prompt := fs.String("prompt", "", "")

			if err := parseArgs(fs, tc.args); err != nil {
				t.Fatalf("parseArgs(%q): %v", tc.args, err)
			}
			if *thread != tc.wantThread {
				t.Errorf("thread = %v, want %v", *thread, tc.wantThread)
			}
			if *prompt != tc.wantPrompt {
				t.Errorf("prompt = %q, want %q", *prompt, tc.wantPrompt)
			}
			if got := fs.Args(); len(got) != len(tc.wantPos) {
				t.Errorf("positionals = %q, want %q", got, tc.wantPos)
			} else {
				for i := range got {
					if got[i] != tc.wantPos[i] {
						t.Errorf("positional %d = %q, want %q", i, got[i], tc.wantPos[i])
					}
				}
			}
		})
	}
}

// A value that looks like a flag must not be hoisted out of its flag.
func TestParseArgsKeepsFlagValuesAttached(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	prompt := fs.String("prompt", "", "")
	thread := fs.Bool("thread", false, "")

	if err := parseArgs(fs, []string{"id", "--prompt", "-not-a-flag", "--thread"}); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if *prompt != "-not-a-flag" {
		t.Errorf("prompt = %q, want %q", *prompt, "-not-a-flag")
	}
	if !*thread {
		t.Error("--thread after a flag value was not parsed")
	}
}

// Everything after a bare -- is positional, so a message id that begins with a
// dash can still be addressed.
func TestParseArgsDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	thread := fs.Bool("thread", false, "")

	if err := parseArgs(fs, []string{"--thread", "--", "-weird-id"}); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !*thread {
		t.Error("--thread before -- was not parsed")
	}
	if got := fs.Args(); len(got) != 1 || got[0] != "-weird-id" {
		t.Errorf("positionals = %q, want [-weird-id]", got)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("thread", false, "")

	if err := parseArgs(fs, []string{"id", "--nope"}); err == nil {
		t.Error("expected an unknown flag to be rejected rather than silently ignored")
	}
}

// The global flagset must keep stopping at the command name; hoisting a
// subcommand's flags to the top level is exactly the regression that broke
// every subcommand flag during development.
func TestGlobalParseStopsAtCommandName(t *testing.T) {
	code := Run([]string{"--data", t.TempDir(), "search", "--unread"}, nil, io.Discard, io.Discard)
	if code == ExitUsage {
		t.Error("subcommand flag --unread was rejected by the global parser")
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	if code := Run([]string{"nonsense"}, nil, io.Discard, io.Discard); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestNoArgsIsUsageError(t *testing.T) {
	if code := Run(nil, nil, io.Discard, io.Discard); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
}

// Commands that need a data directory must say so rather than silently
// creating an empty database wherever they happen to be run.
func TestMissingDataDirIsNotConfigured(t *testing.T) {
	code := Run([]string{"--data", t.TempDir() + "/absent", "account", "list"}, nil, io.Discard, io.Discard)
	if code != ExitNotConfigured {
		t.Errorf("exit code = %d, want %d (ExitNotConfigured)", code, ExitNotConfigured)
	}
}

func TestVersionSucceeds(t *testing.T) {
	if code := Run([]string{"version"}, nil, io.Discard, io.Discard); code != ExitOK {
		t.Errorf("exit code = %d, want %d", code, ExitOK)
	}
}

// Every registered command needs help text; an undocumented command is a
// command nobody can use.
func TestEveryCommandHasUsage(t *testing.T) {
	for name, cmd := range Commands {
		if cmd.Summary == "" {
			t.Errorf("command %q has no Summary", name)
		}
		if cmd.Usage == "" {
			t.Errorf("command %q has no Usage", name)
		}
		if cmd.Run == nil {
			t.Errorf("command %q has no Run", name)
		}
	}
}

// The help groups drive the top-level listing, so a command missing from them
// is invisible to anyone reading `uea` with no arguments.
func TestEveryCommandAppearsInAGroup(t *testing.T) {
	grouped := map[string]bool{}
	for _, g := range groups {
		for _, n := range g.Names {
			grouped[n] = true
		}
	}
	for name := range Commands {
		if !grouped[name] {
			t.Errorf("command %q is not listed in any help group", name)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Test Mailbox":     "test-mailbox",
		"  Spaced  Out  ":  "spaced-out",
		"weird!!chars??":   "weird-chars",
		"already-a-slug":   "already-a-slug",
		"Q3 Budget (2026)": "q3-budget-2026",
		"":                 "",
		"!!!":              "",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" a@x.com , b@x.com ,, c@x.com ")
	want := []string{"a@x.com", "b@x.com", "c@x.com"}
	if len(got) != len(want) {
		t.Fatalf("splitList = %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d = %q, want %q", i, got[i], want[i])
		}
	}
	if splitList("  ") != nil {
		t.Error("a blank list should yield nil, not an empty-string element")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 8); got != "hello w…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("short", 20); got != "short" {
		t.Errorf("truncate should leave short strings alone, got %q", got)
	}
	// Counted in runes, not bytes, so multi-byte text is not cut mid-character.
	if got := truncate("héllo wörld", 7); len([]rune(got)) != 7 {
		t.Errorf("truncate = %q, want 7 runes", got)
	}
}
