package cli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/health"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "doctor",
		Summary: "diagnose the installation",
		Usage: `iql doctor [--json] [--skip-network]

Runs health checks and exits non-zero if any of them fail, so it is usable
from monitoring and CI.

Checks: data directory writable, database reachable and integral, schema
version matches the binary, vault key present with sane permissions, every
account password decryptable, and IMAP reachability per account.

Flags:
  --skip-network   skip IMAP reachability, which is the slow part

Exit codes:
  0  all checks passed (warnings may still be present)
  1  at least one check failed`,
		Run: runDoctor,
	})
}

func runDoctor(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	skipNetwork := fs.Bool("skip-network", false, "skip IMAP reachability checks")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	// The checks live in internal/health so the web UI runs the same ones
	// rather than forming a second opinion about the same questions. This
	// command is the terminal's way of rendering them.
	opened := false
	rep := health.Run(health.Options{
		DataDir:     ctx.DataDir,
		DBPath:      ctx.dbPath(),
		SkipNetwork: *skipNetwork,
		OpenStore: func() error {
			err := ctx.OpenStore()
			opened = err == nil
			return err
		},
		NotConfigured: func(err error) bool {
			var cliErr *Error
			return errors.As(err, &cliErr) && cliErr.Code == ExitNotConfigured
		},
	})
	if opened {
		defer store.CloseDB()
	}

	return finishDoctor(ctx, rep)
}

func finishDoctor(ctx *Context, rep *health.Report) error {
	if ctx.JSON {
		if err := ctx.EmitJSON(rep); err != nil {
			return err
		}
	} else {
		p := ctx.Printer()
		for _, c := range rep.Checks {
			state := ui.OK
			switch c.Status {
			case health.StatusWarn:
				state = ui.Warn
			case health.StatusFail:
				state = ui.Bad
			}
			p.Status(state, c.Name, c.Detail)
			if c.Remedy != "" && c.Status != health.StatusOK {
				// The remedy is the actionable half, so it is indented under
				// the finding rather than crammed onto the same line.
				p.Printf("        %s %s\n", p.Dim("try:"), c.Remedy)
			}
		}
	}
	if rep.NotConfigured {
		return Fail(ExitNotConfigured, "not initialised — run `iql init --data %s`", ctx.DataDir)
	}
	if rep.Failed() {
		return &Error{Code: ExitError, Err: fmt.Errorf("one or more checks failed")}
	}
	return nil
}
