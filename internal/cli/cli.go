// Package cli implements the uea command-line interface.
//
// # Design
//
// Every command is a [Command] in the [Commands] registry, dispatched by
// [Run]. Dispatch is plain stdlib `flag` — with this many commands a framework
// dependency would buy little, and the binary is meant to stay easy to build.
//
// # Two audiences
//
// Some commands are for a person at a terminal; others exist so an LLM agent
// can drive UEA as a tool. The agent-facing ones (search, read, analyze, draft,
// send) all support `--json` and never prompt. The split matters most around
// [outbox approval], which is deliberately reachable only from a terminal.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/user/uea/internal/store"
)

// Exit codes. These are part of the contract with scripts and agents, so they
// are documented in AGENTS.md and must not be renumbered casually.
const (
	ExitOK            = 0 // success
	ExitError         = 1 // something went wrong
	ExitUsage         = 2 // bad arguments; the command was never attempted
	ExitNotFound      = 3 // the requested account, message or draft does not exist
	ExitNeedsApprove  = 4 // refused: requires human approval at a terminal
	ExitNotConfigured = 5 // a prerequisite is missing (no LLM provider, no data dir)
)

// Error carries an exit code alongside the message.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Fail builds an Error with the given exit code.
func Fail(code int, format string, args ...any) *Error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Context is the environment a command runs in. It is passed rather than
// reached for globally so commands stay testable.
type Context struct {
	DataDir string
	JSON    bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Command is one uea subcommand.
type Command struct {
	Name    string
	Summary string
	// Usage is printed under `uea help <name>`. The first line should be the
	// invocation form.
	Usage string
	Run   func(ctx *Context, args []string) error
}

// Commands is the registry, populated by each command file's init.
var Commands = map[string]*Command{}

func register(c *Command) { Commands[c.Name] = c }

// Printf writes human-readable output. Commands should skip it in JSON mode.
func (c *Context) Printf(format string, args ...any) {
	fmt.Fprintf(c.Stdout, format, args...)
}

// EmitJSON writes v as indented JSON followed by a newline.
//
// Indented rather than compact: these outputs are read by people debugging as
// often as by programs, and every consumer of JSON handles whitespace.
func (c *Context) EmitJSON(v any) error {
	enc := json.NewEncoder(c.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// dbPath is where the SQLite database lives for this data directory.
func (c *Context) dbPath() string {
	return filepath.Join(c.DataDir, store.DBNAME)
}

// OpenStore opens the database, refusing if the directory was never
// initialised.
//
// `uea init` is the only command that may create a data directory. Everything
// else fails loudly instead, because silently creating an empty database in
// the wrong working directory produces a UEA that looks broken rather than
// misconfigured — the old behaviour, when the path was hardcoded relative to
// the process's cwd.
func (c *Context) OpenStore() error {
	if _, err := os.Stat(c.dbPath()); err != nil {
		if os.IsNotExist(err) {
			return Fail(ExitNotConfigured,
				"no UEA data directory at %s\n\nRun `uea init --data %s` to create one.",
				c.DataDir, c.DataDir)
		}
		return Fail(ExitError, "cannot access %s: %v", c.dbPath(), err)
	}
	if _, err := store.InitDB(c.DataDir); err != nil {
		return Fail(ExitError, "failed to open database: %v", err)
	}
	return nil
}

// Run parses global flags, dispatches to a command, and returns an exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx := &Context{Stdin: stdin, Stdout: stdout, Stderr: stderr}

	fs := flag.NewFlagSet("uea", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&ctx.DataDir, "data", envOr("UEA_DATA", "./data"),
		"path to the UEA data directory")
	fs.BoolVar(&ctx.JSON, "json", false, "emit machine-readable JSON")
	fs.Usage = func() { usage(stderr) }

	// Plain Parse, deliberately: it stops at the first non-flag token, which is
	// the command name. The interleaving parseArgs used by subcommands must not
	// be applied here, or a subcommand's own flags get hoisted to the top level
	// and rejected as unknown globals.
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr)
		return ExitUsage
	}

	name := rest[0]
	if name == "help" {
		return help(ctx, rest[1:])
	}

	cmd, ok := Commands[name]
	if !ok {
		fmt.Fprintf(stderr, "uea: unknown command %q\n\n", name)
		usage(stderr)
		return ExitUsage
	}

	// Resolve to an absolute path once, so error messages and the vault key
	// location do not shift with the working directory mid-run.
	if abs, err := filepath.Abs(ctx.DataDir); err == nil {
		ctx.DataDir = abs
	}

	if err := cmd.Run(ctx, rest[1:]); err != nil {
		var cliErr *Error
		if errors.As(err, &cliErr) {
			fmt.Fprintf(stderr, "uea %s: %s\n", name, cliErr.Err)
			return cliErr.Code
		}
		fmt.Fprintf(stderr, "uea %s: %v\n", name, err)
		return ExitError
	}
	return ExitOK
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// groups orders the help output by what the reader is trying to do, rather
// than alphabetically across twenty unrelated commands.
var groups = []struct {
	Title string
	Names []string
}{
	{"Getting started", []string{"init", "serve", "version", "doctor"}},
	{"Administration", []string{"account", "user", "vault", "llm", "maintenance", "backup", "restore", "export"}},
	{"Agent tools", []string{"search", "read", "analyze", "draft", "send", "outbox"}},
}

func usage(w io.Writer) {
	fmt.Fprint(w, `uea — Universal Email Analytics

Usage:
  uea [global flags] <command> [flags]

Global flags:
  --data <dir>   data directory (default "./data", or $UEA_DATA)
  --json         emit machine-readable JSON where supported

`)
	seen := map[string]bool{}
	for _, g := range groups {
		fmt.Fprintf(w, "%s:\n", g.Title)
		for _, n := range g.Names {
			if c, ok := Commands[n]; ok {
				fmt.Fprintf(w, "  %-12s %s\n", c.Name, c.Summary)
				seen[n] = true
			}
		}
		fmt.Fprintln(w)
	}

	var other []string
	for n := range Commands {
		if !seen[n] {
			other = append(other, n)
		}
	}
	if len(other) > 0 {
		sort.Strings(other)
		fmt.Fprintf(w, "Other:\n")
		for _, n := range other {
			fmt.Fprintf(w, "  %-12s %s\n", n, Commands[n].Summary)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprint(w, "Run `uea help <command>` for details.\nAgents: see AGENTS.md for the JSON contract and exit codes.\n")
}

func help(ctx *Context, args []string) int {
	if len(args) == 0 {
		usage(ctx.Stdout)
		return ExitOK
	}
	cmd, ok := Commands[args[0]]
	if !ok {
		fmt.Fprintf(ctx.Stderr, "uea: unknown command %q\n", args[0])
		return ExitUsage
	}
	fmt.Fprintf(ctx.Stdout, "%s\n", strings.TrimSpace(cmd.Usage))
	return ExitOK
}

// subcommand pulls the leading verb from args, e.g. `account add`.
func subcommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}

// parseArgs parses fs while allowing flags and positionals to be interleaved.
//
// Go's flag package stops at the first non-flag argument, so `uea read m1
// --thread` would parse "m1" and then silently treat "--thread" as another
// positional — the flag has no effect and nothing complains. That is the
// natural way to type the command, and an ignored flag is a worse failure than
// an error, so arguments are reordered before parsing.
func parseArgs(fs *flag.FlagSet, args []string) error {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			// Everything after a bare -- is positional by convention.
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(arg) > 1 && strings.HasPrefix(arg, "-"):
			flags = append(flags, arg)
			// `--name value` needs its value kept adjacent; `--name=value` and
			// boolean flags do not take one.
			if !strings.Contains(arg, "=") && flagNeedsValue(fs, arg) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, arg)
		}
	}

	// The "--" terminator is re-inserted rather than carried through the loop:
	// without it, a positional that begins with a dash (a message id, say) would
	// be re-read as a flag once the two groups are concatenated.
	reordered := append(flags, "--")
	reordered = append(reordered, positional...)
	return fs.Parse(reordered)
}

// flagNeedsValue reports whether the named flag consumes the following token.
func flagNeedsValue(fs *flag.FlagSet, arg string) bool {
	f := fs.Lookup(strings.TrimLeft(arg, "-"))
	if f == nil {
		// Unknown flag: leave it for Parse to report properly.
		return false
	}
	boolFlag, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !(ok && boolFlag.IsBoolFlag())
}
