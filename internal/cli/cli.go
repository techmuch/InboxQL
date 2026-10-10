// Package cli implements the iql command-line interface.
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
// can drive InboxQL as a tool. The agent-facing ones (search, read, analyze, draft,
// send) all support `--json` and never prompt. The split matters most around
// [outbox approval], which is deliberately reachable only from a terminal.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/inboxql/internal/logging"
	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/modelpath"
	"github.com/user/inboxql/internal/serverlock"
	"github.com/user/inboxql/internal/store"
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
	// DataSource says why DataDir is what it is: "flag", "env", "settings"
	// or "local". `iql where` reports it, because "which mailbox is this"
	// should never need guessing.
	DataSource string
	// Machine is the machine settings, when ~/.iql/settings.json exists.
	Machine *machine.Settings
	// machineErr is a settings file that exists and could not be read. Kept
	// rather than fatal at resolve time, so `iql where` can say what is wrong
	// with it; anything that opens the store refuses.
	machineErr error
	// suppressInitNext is set by `iql setup`, which prints its own next step.
	suppressInitNext bool
	JSON             bool
	// Verbose surfaces the database and migration logging that is otherwise
	// suppressed. Off by default: three lines of schema chatter before every
	// command's real output is noise for a person and clutter in a terminal.
	Verbose bool
	// NoColour forces plain output even on a terminal.
	NoColour bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// resolve normalises the context once the global flags are known.
//
// The data directory becomes absolute so error messages and the vault key
// path do not shift if something changes the working directory mid-run.
func (c *Context) resolve() {
	configureLogging(c.Verbose, c.Stderr)

	settings, found, err := machine.Load()
	switch {
	case err != nil:
		c.machineErr = err
	case found:
		c.Machine = settings
		// Models are the machine's whatever mailbox is in use: a --data
		// pointing at a test mailbox still finds the weights already on disk.
		modelpath.SetRoot(settings.Models)
	}

	// Which mailbox, first match wins. The explicit ways of saying so come
	// first; the settings file is the machine's answer; ./data — what every
	// command assumed before there were machine settings — is used only when
	// there are none.
	switch {
	case c.DataDir != "":
		c.DataSource = "flag"
	case strings.TrimSpace(os.Getenv("INBOXQL_DATA")) != "":
		c.DataDir, c.DataSource = os.Getenv("INBOXQL_DATA"), "env"
	case c.Machine != nil:
		c.DataDir, c.DataSource = c.Machine.DataDir, "settings"
	default:
		c.DataDir, c.DataSource = "./data", "local"
	}
	if abs, err := filepath.Abs(machine.Expand(c.DataDir)); err == nil {
		c.DataDir = abs
	}
	c.warnAboutLocalData()
}

// warnAboutLocalData says so when a mailbox sits in this folder and another
// one is in use.
//
// Running `iql start` inside a project folder used to mean that folder's
// ./data. Once machine settings exist it means the machine's mailbox, which is
// what was asked for — and is also exactly the change that would otherwise go
// unnoticed until somebody wondered where their mail went.
func (c *Context) warnAboutLocalData() {
	if c.DataSource != "settings" && c.DataSource != "env" {
		return
	}
	local, err := filepath.Abs("./data")
	if err != nil || local == c.DataDir {
		return
	}
	if _, err := os.Stat(filepath.Join(local, store.DBNAME)); err != nil {
		return
	}
	fmt.Fprintf(c.Stderr, "iql: using %s (from %s), not the mailbox in ./data — pass --data ./data for that one\n",
		c.DataDir, sourceWords(c.DataSource))
}

// sourceWords says where a data directory came from, in words.
func sourceWords(source string) string {
	switch source {
	case "flag":
		return "--data"
	case "env":
		return "$INBOXQL_DATA"
	case "settings":
		return machine.Path()
	default:
		return "the current folder, because there are no machine settings"
	}
}

// defaultAddr is where a server listens, or is looked for, unless told:
// $INBOXQL_ADDR, then the machine settings, then the built-in default.
func (c *Context) defaultAddr() string {
	if a := strings.TrimSpace(os.Getenv("INBOXQL_ADDR")); a != "" {
		return a
	}
	if c.Machine != nil && c.Machine.Addr != "" {
		return c.Machine.Addr
	}
	return machine.DefaultAddr
}

// Command is one iql subcommand.
type Command struct {
	Name    string
	Aliases []string
	Summary string
	// Usage is printed under `iql help <name>`. The first line should be the
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
// `iql init` is the only command that may create a data directory. Everything
// else fails loudly instead, because silently creating an empty database in
// the wrong working directory produces an InboxQL that looks broken rather than
// misconfigured — the old behaviour, when the path was hardcoded relative to
// the process's cwd.
func (c *Context) OpenStore() error {
	if c.machineErr != nil && c.DataSource != "flag" && c.DataSource != "env" {
		return Fail(ExitNotConfigured, "%v", c.machineErr)
	}
	// A server holding this mailbox with an older schema would have the
	// database migrated out from under it. Refused: stop it, or update it.
	if info, held := serverlock.Held(c.DataDir); held && info.Schema > 0 && info.Schema < store.SchemaVersion {
		return Fail(ExitError,
			"a server is running for %s with an older version of InboxQL (schema %d; this is %d).\n"+
				"Opening it here would upgrade the database underneath it. Stop that server first —\n"+
				"`iql service stop` if it is the service — or run `iql update`.",
			c.DataDir, info.Schema, store.SchemaVersion)
	}
	_ = store.MigrateLegacyDatabase(c.DataDir)
	if _, err := os.Stat(c.dbPath()); err != nil {
		if os.IsNotExist(err) {
			return Fail(ExitNotConfigured,
				"no InboxQL data directory at %s\n\nRun `iql init --data %s` to create one.",
				c.DataDir, c.DataDir)
		}
		return Fail(ExitError, "cannot access %s: %v", c.dbPath(), err)
	}
	if _, err := store.InitDB(c.DataDir); err != nil {
		return Fail(ExitError, "failed to open database: %v", err)
	}
	// Only now: the log is a table, so nothing can be written to it until the
	// database is open and migrated. Everything before this point — including
	// the migrations themselves — reaches stderr and not the log, which is the
	// honest answer rather than a gap worth pretending about.
	logging.Start()
	logging.SetLevel(storedLevel())
	return nil
}

// storedLevel reads the configured floor, defaulting to info.
//
// From the settings table rather than a flag: the level is changed when
// something has already gone wrong, and a flag would mean reproducing it.
func storedLevel() string {
	if v, err := store.GetSetting(logLevelSetting); err == nil && v != "" {
		return v
	}
	return store.LevelInfo
}

// logLevelSetting is where the floor is stored.
const logLevelSetting = "log.level"

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
// Go's flag package stops at the first non-flag argument, so `iql read m1
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

// readServerLock reports the server holding a data directory, if any.
func readServerLock(dataDir string) (*serverlock.Info, bool) {
	return serverlock.Held(dataDir)
}
