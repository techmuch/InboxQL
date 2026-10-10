package cli

import (
	"flag"

	"github.com/user/inboxql/internal/service"
)

// browse opens a URL in the default browser. A variable so tests can see
// what would have been opened without opening it.
var browse = openBrowser

func init() {
	register(&Command{
		Name:    "open",
		Summary: "open InboxQL in your browser",
		Usage: `iql open [--print] [--json]

Opens the running server for this mailbox in a new browser tab — at the
address it is serving, so a name set with ` + "`iql hosts set`" + ` is the one used.

Nothing is started. When no server is running, this says what to run:
` + "`iql service start`" + ` if the login service is installed, ` + "`iql start --open`" + `
otherwise.

  --print   print the address instead of opening it — for scripts, and for a
            session over SSH where there is no browser to open

For an agent: this adds a tab with no explanation. To show a person
something, use ` + "`iql ui query <window> <query> --note <why>`" + `, which says why.`,
		Run: runOpen,
	})
}

func runOpen(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	printOnly := fs.Bool("print", false, "print the address rather than opening it")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	info, held := readServerLock(ctx.DataDir)
	if !held || info == nil || info.URL == "" {
		next := "iql start --open"
		if mgr, err := service.ForThisMachine(); err == nil {
			if st, err := mgr.Status(); err == nil && st.Installed {
				next = "iql service start"
			}
		}
		if ctx.JSON {
			_ = ctx.EmitJSON(map[string]any{"running": false, "dataDir": ctx.DataDir, "next": next})
		}
		return Fail(ExitNotConfigured, "InboxQL is not running for %s. Start it with `%s`.", ctx.DataDir, next)
	}

	opened := false
	if !*printOnly {
		if err := browse(info.URL); err != nil {
			return Fail(ExitError, "could not open a browser (%v). The address is %s", err, info.URL)
		}
		opened = true
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"running": true, "url": info.URL, "opened": opened, "pid": info.PID})
	}
	if *printOnly {
		ctx.Printf("%s\n", info.URL)
	} else {
		ctx.Printf("Opened %s\n", info.URL)
	}
	return nil
}
