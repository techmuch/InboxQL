package cli

import (
	"bytes"
	"flag"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Go's flag package stops at the first positional, so `iql read m1 --thread`
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
	code := Execute([]string{"--data", t.TempDir(), "search", "--unread"}, nil, io.Discard, io.Discard)
	if code == ExitUsage {
		t.Error("subcommand flag --unread was rejected by the global parser")
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	if code := Execute([]string{"nonsense"}, nil, io.Discard, io.Discard); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestNoArgsIsUsageError(t *testing.T) {
	if code := Execute(nil, nil, io.Discard, io.Discard); code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
}

// Commands that need a data directory must say so rather than silently
// creating an empty database wherever they happen to be run.
func TestMissingDataDirIsNotConfigured(t *testing.T) {
	code := Execute([]string{"--data", t.TempDir() + "/absent", "account", "list"}, nil, io.Discard, io.Discard)
	if code != ExitNotConfigured {
		t.Errorf("exit code = %d, want %d (ExitNotConfigured)", code, ExitNotConfigured)
	}
}

func TestVersionSucceeds(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"subcommand", []string{"version"}},
		{"long flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
		{"single dash version", []string{"-version"}},
		{"json with flag", []string{"--json", "--version"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := Execute(tc.args, nil, io.Discard, io.Discard); code != ExitOK {
				t.Errorf("Run(%q) exit code = %d, want %d", tc.args, code, ExitOK)
			}
		})
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

// Every command must land in a help group and in the explicit ordering.
// A command in neither is still reachable, but nobody reading `iql --help`
// would ever discover it.
func TestEveryCommandAppearsInAGroup(t *testing.T) {
	for name := range Commands {
		if commandGroup[name] == "" {
			t.Errorf("command %q is not in any help group", name)
		}
		if !listedInOrder(name) {
			t.Errorf("command %q is missing from commandOrder, so help lists it last and unsorted", name)
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

// Global flags used to work only before the command name, because dispatch
// used the stdlib flag package, which stops at the first non-flag token. The
// failure was not merely inconvenient: `iql init` printed three suggested next
// commands, all of which put --data after the command, and all of which failed.
func TestGlobalFlagsWorkAfterTheCommand(t *testing.T) {
	dir := t.TempDir() + "/absent"

	before := Execute([]string{"--data", dir, "account", "list"}, nil, io.Discard, io.Discard)
	after := Execute([]string{"account", "list", "--data", dir}, nil, io.Discard, io.Discard)

	if before != after {
		t.Errorf("--data before the command exits %d but after it exits %d; they must be the same invocation", before, after)
	}
	if after != ExitNotConfigured {
		t.Errorf("exit code = %d, want %d — the flag was not applied", after, ExitNotConfigured)
	}
}

func TestGlobalFlagAcceptsEqualsForm(t *testing.T) {
	dir := t.TempDir() + "/absent"
	if code := Execute([]string{"account", "list", "--data=" + dir}, nil, io.Discard, io.Discard); code != ExitNotConfigured {
		t.Errorf("exit code = %d, want %d", code, ExitNotConfigured)
	}
}

// Everything after a bare -- belongs to the command, even when it looks like
// a global flag.
func TestDoubleDashEndsGlobalScanning(t *testing.T) {
	ctx := &Context{}
	rest := splitGlobals(ctx, []string{"--json", "--", "--data", "/should/not/apply"})
	if !ctx.JSON {
		t.Error("--json before -- was not applied")
	}
	if ctx.DataDir != "" {
		t.Errorf("--data after -- was applied as a global: %q", ctx.DataDir)
	}
	if len(rest) != 3 || rest[0] != "--" {
		t.Errorf("rest = %q, want the -- and everything after it", rest)
	}
}

// The hand-written help was reachable only as `iql help <cmd>`; the spelling
// people actually type fell through to the stdlib flag package and printed a
// bare alphabetical flag dump instead.
func TestHelpFlagReachesTheWrittenHelp(t *testing.T) {
	for _, args := range [][]string{
		{"search", "--help"},
		{"search", "-h"},
		{"help", "search"},
	} {
		var out bytes.Buffer
		if code := Execute(args, nil, &out, &out); code != ExitOK {
			t.Errorf("Execute(%q) exit code = %d, want 0", args, code)
		}
		// A line from the prose, which the stdlib flag dump does not contain.
		if !strings.Contains(out.String(), "Substring matching over subject") {
			t.Errorf("Execute(%q) did not print the written help:\n%s", args, out.String())
		}
	}
}

// stdlib flag treated -flag and --flag alike, so both spellings are in the
// docs and in muscle memory. pflag reads -version as a cluster of shorthands.
func TestSingleDashLongFlagsStillWork(t *testing.T) {
	if got := normaliseSingleDash([]string{"-version"}); got[0] != "--version" {
		t.Errorf("normaliseSingleDash(-version) = %q, want --version", got[0])
	}
	// Real shorthands must survive untouched.
	if got := normaliseSingleDash([]string{"-v"}); got[0] != "-v" {
		t.Errorf("a single-letter shorthand was rewritten: %q", got[0])
	}
	// Nothing after -- is rewritten.
	got := normaliseSingleDash([]string{"--", "-literal"})
	if got[1] != "-literal" {
		t.Errorf("argument after -- was rewritten: %q", got[1])
	}
}

// Routine database chatter is suppressed, but a schema migration changes the
// user's database on disk and must never happen silently.
func TestLoggingKeepsConsequentialLines(t *testing.T) {
	cases := map[string]bool{
		"Initializing database at: /tmp/x":                              false,
		"Current database schema version: 14":                           false,
		"Database schema is up to date (v14).":                          false,
		"Applying schema migration v14 (mailbox)":                       true,
		"WARN: vault key has permissions 0644":                          true,
		"Encrypted 3 previously-plaintext account password(s) at rest.": true,
		"PANIC during sync of account x":                                true,
	}
	for line, want := range cases {
		if got := consequential(line + "\n"); got != want {
			t.Errorf("consequential(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestQuietLoggingFiltersButVerboseDoesNot(t *testing.T) {
	var quiet, loud bytes.Buffer

	configureLogging(false, &quiet)
	log.Println("Current database schema version: 14")
	log.Println("Applying schema migration v14")

	configureLogging(true, &loud)
	log.Println("Current database schema version: 14")

	if strings.Contains(quiet.String(), "schema version") {
		t.Errorf("routine chatter survived the quiet logger: %q", quiet.String())
	}
	if !strings.Contains(quiet.String(), "migration") {
		t.Errorf("a migration was suppressed: %q", quiet.String())
	}
	if !strings.Contains(loud.String(), "schema version") {
		t.Errorf("--verbose did not restore routine logging: %q", loud.String())
	}
}

// Completion runs against commands that parse their own flags, so cobra hands
// the completion function the global flags as though they were positional
// arguments. Missing that made every completion position off by however many
// globals the user had typed.
func TestCompletionIgnoresGlobalFlags(t *testing.T) {
	ctx := &Context{}
	got := positional(ctx, []string{"--data", "/tmp/x", "sync", "--json"})
	if len(got) != 1 || got[0] != "sync" {
		t.Errorf("positional = %q, want [sync]", got)
	}
	if ctx.DataDir != "/tmp/x" {
		t.Errorf("--data was not applied while completing: %q", ctx.DataDir)
	}
}

func TestSubcommandCompletion(t *testing.T) {
	ctx := &Context{}
	root := NewRootCommand(ctx)

	var account *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "account" {
			account = c
		}
	}
	if account == nil {
		t.Fatal("account command not registered")
	}
	if account.ValidArgsFunction == nil {
		t.Fatal("account has no completion function")
	}

	got, _ := account.ValidArgsFunction(account, nil, "")
	if len(got) != len(subcommands["account"]) {
		t.Errorf("completion offered %q, want %q", got, subcommands["account"])
	}

	// A prefix must narrow the list.
	got, _ = account.ValidArgsFunction(account, nil, "re")
	if len(got) != 1 || got[0] != "remove" {
		t.Errorf("prefix completion = %q, want [remove]", got)
	}
}

// Every command that dispatches on a subcommand must advertise those names, or
// completion silently offers nothing where it is most useful.
func TestSubcommandListsAreComplete(t *testing.T) {
	// Commands known to take a subcommand verb rather than positional args.
	for _, name := range []string{"account", "user", "vault", "llm", "maintenance", "import", "draft", "outbox"} {
		if len(subcommands[name]) == 0 {
			t.Errorf("command %q has no subcommand list for completion", name)
		}
	}
}

// A setting that switches off an authentication requirement must never be
// enabled by a value nobody meant as true.
func TestEnvBoolFailsClosed(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on", " 1 "}
	falsy := []string{"", "0", "false", "no", "off", "banana", "2", "trueish", "y"}

	for _, v := range truthy {
		t.Setenv("INBOXQL_TEST_BOOL", v)
		if !envBool("INBOXQL_TEST_BOOL") {
			t.Errorf("envBool(%q) = false, want true", v)
		}
	}
	for _, v := range falsy {
		t.Setenv("INBOXQL_TEST_BOOL", v)
		if envBool("INBOXQL_TEST_BOOL") {
			t.Errorf("envBool(%q) = true; an unrecognised value must not enable a security setting", v)
		}
	}

	if envBool("INBOXQL_DEFINITELY_UNSET_VARIABLE") {
		t.Error("an unset variable read as true")
	}
}

// The auth posture is decided once at startup from the listen address, so this
// table is the whole security model in one place.
func TestTrustDecision(t *testing.T) {
	cases := []struct {
		name            string
		addr            string
		requirePassword bool
		forceTrust      bool
		wantTrust       bool
	}{
		// A desktop install: localhost only, so reaching the port means being
		// at the machine. No password.
		{"default loopback bind", "127.0.0.1:8080", false, false, true},
		{"loopback by name", "localhost:8080", false, false, true},
		{"IPv6 loopback", "[::1]:8080", false, false, true},

		// Serving beyond localhost widens the audience past the person at the
		// keyboard, so the password comes back.
		{"all interfaces", ":8080", false, false, false},
		{"explicit public address", "0.0.0.0:8080", false, false, false},
		{"a LAN address", "192.168.1.10:8080", false, false, false},

		// Both overrides, in both directions.
		{"forced on a public address", ":8080", false, true, true},
		{"required on loopback", "127.0.0.1:8080", true, false, false},
		// require-password wins; it is the safer of the two.
		{"both flags", ":8080", true, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := trustDecision(tc.addr, tc.requirePassword, tc.forceTrust)
			if got != tc.wantTrust {
				t.Errorf("trustDecision(%q, requirePassword=%v, forceTrust=%v) = %v, want %v",
					tc.addr, tc.requirePassword, tc.forceTrust, got, tc.wantTrust)
			}
			if reason == "" {
				t.Error("no reason given; the startup banner would say nothing")
			}
		})
	}
}

func TestBoundToLoopback(t *testing.T) {
	loopback := []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.42:9000"}
	public := []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "10.0.0.5:80", "[::]:8080"}

	for _, addr := range loopback {
		if !boundToLoopback(addr) {
			t.Errorf("boundToLoopback(%q) = false, want true", addr)
		}
	}
	for _, addr := range public {
		if boundToLoopback(addr) {
			t.Errorf("boundToLoopback(%q) = true; a public bind must withdraw passwordless access", addr)
		}
	}
}
