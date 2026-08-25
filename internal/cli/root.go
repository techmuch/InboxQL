package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/user/inboxql/internal/cli/ui"
)

// Subcommands each command accepts, used only to offer completions and to
// list them in help. Dispatch still happens inside the command's own Run, so
// this list is descriptive rather than enforcing.
var subcommands = map[string][]string{
	"account":     {"add", "list", "remove", "verify", "sync"},
	"user":        {"list", "add", "passwd"},
	"vault":       {"status", "rotate"},
	"llm":         {"status", "configure", "test", "disable"},
	"maintenance": {"vacuum", "analyze", "integrity", "checkpoint"},
	"import":      {"sources", "mailboxes", "scan", "run", "eml"},
	"draft":       {"create", "list", "show", "delete"},
	"outbox":      {"list", "show", "approve", "reject"},
}

// globalFlag is a flag that means the same thing wherever it appears.
//
// The old dispatcher parsed these with the stdlib flag package, which stops at
// the first non-flag token — so they worked only *before* the command name.
// `iql doctor --data ./data` failed, and, worse, the tool printed exactly that
// form as its own next-step guidance after `init`. Accepting them anywhere is
// what a person expects and what every other CLI does.
type globalFlag struct {
	name      string
	takesArg  bool
	apply     func(ctx *Context, value string)
	shorthand string
}

var globalFlags = []globalFlag{
	{name: "data", takesArg: true, apply: func(c *Context, v string) { c.DataDir = v }},
	{name: "json", apply: func(c *Context, _ string) { c.JSON = true }},
	{name: "verbose", shorthand: "V", apply: func(c *Context, _ string) { c.Verbose = true }},
	{name: "no-color", apply: func(c *Context, _ string) { c.NoColour = true }},
	{name: "no-colour", apply: func(c *Context, _ string) { c.NoColour = true }},
}

func lookupGlobal(token string) (globalFlag, bool) {
	name := strings.TrimLeft(token, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	for _, g := range globalFlags {
		if g.name == name || (g.shorthand != "" && g.shorthand == name) {
			return g, true
		}
	}
	return globalFlag{}, false
}

// splitGlobals removes global flags from args wherever they appear, applying
// each to ctx, and returns what is left for the command itself.
//
// A bare `--` ends the scan: everything after it belongs to the command, even
// if it looks like a global. That matters for `draft create -- --not-a-flag`.
func splitGlobals(ctx *Context, args []string) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			rest = append(rest, arg)
			continue
		}

		g, ok := lookupGlobal(arg)
		if !ok {
			rest = append(rest, arg)
			continue
		}

		value := ""
		if eq := strings.IndexByte(arg, '='); eq >= 0 {
			value = arg[eq+1:]
		} else if g.takesArg {
			if i+1 >= len(args) {
				// Leave it for the command to reject, so the error names the
				// flag rather than vanishing here.
				rest = append(rest, arg)
				continue
			}
			i++
			value = args[i]
		}
		g.apply(ctx, value)
	}
	return rest
}

// wantsHelp reports whether the user asked for help rather than for the
// command to run.
//
// Needed because these commands parse their own flags, so cobra never sees
// --help. Without this, `iql search --help` fell through to the stdlib flag
// package and printed `-account string` — single dashes, alphabetical, no
// prose — while the carefully written page was reachable only as
// `iql help search`. The conventional spelling now reaches the good page.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--help" || a == "-h" || a == "-help" {
			return true
		}
	}
	return false
}

// group titles, in the order a reader meets them.
const (
	groupStart = "start"
	groupAdmin = "admin"
	groupAgent = "agent"
)

// commandOrder is the sequence commands appear in help, chosen to match the
// order someone meets them rather than the alphabet.
var commandOrder = []string{
	"init", "start", "version", "doctor",
	"account", "user", "vault", "llm", "maintenance", "backup", "restore",
	"import", "export", "errors",
	"search", "read", "analyze", "draft", "send", "outbox",
}

func listedInOrder(name string) bool {
	for _, n := range commandOrder {
		if n == name {
			return true
		}
	}
	return false
}

var commandGroup = map[string]string{
	"init": groupStart, "start": groupStart, "version": groupStart, "doctor": groupStart,
	"account": groupAdmin, "user": groupAdmin, "vault": groupAdmin, "llm": groupAdmin,
	"maintenance": groupAdmin, "backup": groupAdmin, "restore": groupAdmin,
	"import": groupAdmin, "export": groupAdmin, "errors": groupAdmin,
	"search": groupAgent, "read": groupAgent, "analyze": groupAgent,
	"draft": groupAgent, "send": groupAgent, "outbox": groupAgent,
}

// NewRootCommand assembles the command tree.
func NewRootCommand(ctx *Context) *cobra.Command {
	root := &cobra.Command{
		Use:   "iql",
		Short: "InboxQL: Email for Engineers",
		Long: `iql — InboxQL: Email for Engineers

Self-hosted email analytics. The same binary serves the dashboard and provides
the command surface below.

Global flags may appear before or after the command: ` + "`iql doctor --data ./data`" + `
and ` + "`iql --data ./data doctor`" + ` are the same invocation.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `iql` should explain itself and exit non-zero, because no
		// work was requested.
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return Fail(ExitUsage, "")
		},
	}

	root.PersistentFlags().StringVar(&ctx.DataDir, "data",
		envOr("INBOXQL_DATA", "./data"), "path to the InboxQL data directory")
	root.PersistentFlags().BoolVar(&ctx.JSON, "json", false,
		"emit machine-readable JSON where supported")
	root.PersistentFlags().BoolVarP(&ctx.Verbose, "verbose", "V", false,
		"show database and diagnostic logging")
	root.PersistentFlags().BoolVar(&ctx.NoColour, "no-color", false,
		"never colourise output (also honours $NO_COLOR)")

	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Getting started:"},
		&cobra.Group{ID: groupAdmin, Title: "Administration:"},
		&cobra.Group{ID: groupAgent, Title: "Agent tools:"},
	)

	// Commands is a map, so iterating it directly would order the help
	// randomly; cobra's own sorting would then order it alphabetically, which
	// puts `doctor` before `init`. Both are worse than the order a reader
	// actually needs, so ordering is explicit and sorting is off.
	cobra.EnableCommandSorting = false
	for _, name := range commandOrder {
		if c, ok := Commands[name]; ok {
			root.AddCommand(newCobraCommand(ctx, c))
		}
	}
	// Anything registered but not listed above still appears, so a new command
	// cannot go missing from help by being forgotten here.
	for name, c := range Commands {
		if !listedInOrder(name) {
			root.AddCommand(newCobraCommand(ctx, c))
		}
	}

	root.SetHelpCommand(&cobra.Command{
		Use:    "help [command]",
		Short:  "show help for a command",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				return Fail(ExitUsage, "unknown command %q", strings.Join(args, " "))
			}
			return target.Help()
		},
	})

	registerCompletions(ctx, root)

	root.SetVersionTemplate("{{.Version}}\n")
	return root
}

// newCobraCommand wraps one registered Command.
//
// Flag parsing stays switched off: each command still parses its own flags
// with the stdlib flag package, which accepts forms pflag would reject and
// which the whole test suite is written against. Cobra is here for the tree,
// the help routing, the completions and the typo suggestions — not to re-parse
// twenty commands' worth of flags that already work.
func newCobraCommand(ctx *Context, c *Command) *cobra.Command {
	use := c.Name
	if subs, ok := subcommands[c.Name]; ok {
		use = c.Name + " <" + strings.Join(subs, "|") + ">"
	}

	cmd := &cobra.Command{
		Use:     use,
		Aliases: c.Aliases,
		Short:   c.Summary,
		Long:    strings.TrimSpace(c.Usage),
		GroupID: commandGroup[c.Name],

		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args = splitGlobals(ctx, args)
			if wantsHelp(args) {
				return cmd.Help()
			}
			ctx.resolve()
			return c.Run(ctx, args)
		},
	}
	// Cobra prints Use verbatim in the usage line; the angle-bracket form
	// above is for the listing, so give the usage line the real one.
	cmd.SetUsageTemplate(usageTemplate)
	return cmd
}

// normaliseSingleDash rewrites `-flag` as `--flag` for multi-letter names.
//
// The stdlib flag package treats the two spellings as identical, so both are
// documented and both are in people's muscle memory. pflag does not: it reads
// `-version` as the cluster -v -e -r -s -i -o -n and rejects it. Rewriting
// keeps every invocation that used to work working. Single-letter flags are
// left alone so real shorthands still behave, and everything after a bare `--`
// is untouched.
func normaliseSingleDash(args []string) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			a = "-" + a
		}
		out = append(out, a)
	}
	return out
}

// Execute runs the CLI and returns a process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx := &Context{Stdin: stdin, Stdout: stdout, Stderr: stderr}
	args = normaliseSingleDash(args)

	root := NewRootCommand(ctx)
	root.Version = Version
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.Execute()
	if err == nil {
		return ExitOK
	}

	name := commandName(args)

	var cliErr *Error
	if errors.As(err, &cliErr) {
		if msg := cliErr.Err.Error(); msg != "" {
			if name != "" {
				fmt.Fprintf(stderr, "iql %s: %s\n", name, msg)
			} else {
				fmt.Fprintf(stderr, "iql: %s\n", msg)
			}
		}
		return cliErr.Code
	}

	// Anything cobra itself rejected — an unknown command, a bad global flag —
	// is a usage error, not a runtime one.
	fmt.Fprintf(stderr, "iql: %v\n", err)
	if strings.Contains(err.Error(), "unknown command") || strings.Contains(err.Error(), "unknown flag") {
		return ExitUsage
	}
	if name != "" {
		return ExitError
	}
	return ExitUsage
}

// commandName finds the first non-flag token, for error prefixes.
func commandName(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if g, ok := lookupGlobal(a); ok && g.takesArg && !strings.Contains(a, "=") {
			i++
		}
	}
	return ""
}

// Printer builds a ui.Printer for this context's stdout, honouring --no-color.
func (c *Context) Printer() *ui.Printer {
	if c.NoColour {
		return ui.NewWithColour(c.Stdout, false)
	}
	return ui.New(c.Stdout)
}

// usageTemplate is cobra's default with the flag sections removed: these
// commands parse their own flags, so cobra has none to list and an empty
// "Flags:" heading would be a lie. The real flags are documented in Long.
const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
