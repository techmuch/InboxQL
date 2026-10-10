package cli

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/modelpath"
)

func init() {
	register(&Command{
		Name:    "setup",
		Summary: "make this machine's mailbox: write ~/.iql/settings.json and prepare it",
		Usage: `iql setup [--data <dir>] [--addr <host:port>] [--force]

Writes the machine settings, ~/.iql/settings.json, and prepares the mailbox
they name. After this every iql command — and the service — uses that mailbox
unless told otherwise with --data or $INBOXQL_DATA.

  --data <dir>         adopt this mailbox rather than making ~/.iql/data —
                       an existing data directory is used as it is
  --addr <host:port>   where the server listens (default 127.0.0.1:8420)
  --force              rewrite settings that already exist

Nothing is deleted or moved. Pointing the settings at an existing mailbox
uses it where it is.`,
		Run: runSetup,
	})
	register(&Command{
		Name:    "where",
		Summary: "say which mailbox iql is using, and why",
		Usage: `iql where [--json]

Prints the data directory every command will use from here, where that answer
came from (--data, $INBOXQL_DATA, ~/.iql/settings.json, or this folder), the
machine settings, the models folder and the listen address.`,
		Run: runWhere,
	})
}

func runSetup(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	addr := fs.String("addr", "", "where the server listens")
	force := fs.Bool("force", false, "rewrite existing settings")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	existing, found, err := machine.Load()
	if err != nil && !*force {
		return Fail(ExitNotConfigured, "%v\n\nFix it, or re-run with --force to replace it.", err)
	}
	if found && existing != nil && !*force && ctx.DataSource != "flag" && *addr == "" {
		// Already set up and nothing new asked for: say what it is rather
		// than rewriting it, and make sure the mailbox it names exists.
		ctx.DataDir = existing.DataDir
		return finishSetup(ctx, existing, false)
	}

	s := machine.Defaults()
	if found && existing != nil {
		s = existing
	}
	// --data here means "this is the machine's mailbox". It is the way to
	// adopt one that already exists — a checkout's ./data, an archive — as
	// it is, without copying or moving anything.
	if ctx.DataSource == "flag" {
		s.DataDir = ctx.DataDir
	}
	if *addr != "" {
		s.Addr = *addr
	}
	if err := machine.Save(s); err != nil {
		return Fail(ExitError, "writing %s: %v", machine.Path(), err)
	}
	ctx.Machine, ctx.DataDir, ctx.DataSource = s, s.DataDir, "settings"
	modelpath.SetRoot(s.Models)
	return finishSetup(ctx, s, true)
}

// finishSetup prepares the mailbox the settings name, if it is not ready yet.
func finishSetup(ctx *Context, s *machine.Settings, wrote bool) error {
	_, statErr := os.Stat(ctx.dbPath())
	created := false
	if os.IsNotExist(statErr) {
		ctx.suppressInitNext = true
		if err := runInit(ctx, nil); err != nil {
			return err
		}
		created = true
	}
	if err := os.MkdirAll(s.Models, 0o755); err != nil {
		return Fail(ExitError, "creating %s: %v", s.Models, err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"settings": machine.Path(), "written": wrote, "dataDir": s.DataDir,
			"created": created, "addr": s.Addr, "models": s.Models,
		})
	}
	p := ctx.Printer()
	if wrote {
		ctx.Printf("Wrote %s.\n", p.Bold(machine.Path()))
	} else {
		ctx.Printf("Already set up: %s.\n", p.Bold(machine.Path()))
	}
	ctx.Printf("  %-9s %s\n", p.Dim("mailbox"), s.DataDir)
	ctx.Printf("  %-9s %s\n", p.Dim("models"), s.Models)
	ctx.Printf("  %-9s http://%s\n", p.Dim("address"), s.Addr)
	ctx.Printf("\nEvery iql command now uses this mailbox. Next: %s\n", p.Bold("iql service install"))
	return nil
}

func runWhere(ctx *Context, args []string) error {
	type answer struct {
		DataDir    string `json:"dataDir"`
		Source     string `json:"source"`
		Why        string `json:"why"`
		Exists     bool   `json:"exists"`
		Settings   string `json:"settings"`
		HasMachine bool   `json:"machineSettings"`
		Problem    string `json:"settingsProblem,omitempty"`
		Models     string `json:"models"`
		Addr       string `json:"addr"`
		Hostname   string `json:"hostname,omitempty"`
		Server     string `json:"server,omitempty"`
	}
	a := answer{
		DataDir: ctx.DataDir, Source: ctx.DataSource, Why: sourceWords(ctx.DataSource),
		Settings: machine.Path(), HasMachine: ctx.Machine != nil,
		Models: modelpath.For(ctx.DataDir, ""), Addr: ctx.defaultAddr(),
	}
	if _, err := os.Stat(filepath.Join(ctx.DataDir, "inboxql.db")); err == nil {
		a.Exists = true
	}
	if ctx.machineErr != nil {
		a.Problem = ctx.machineErr.Error()
	}
	if ctx.Machine != nil {
		a.Hostname = ctx.Machine.Hostname
	}
	if info, held := readServerLock(ctx.DataDir); held {
		a.Server = info.URL
	}

	if ctx.JSON {
		return ctx.EmitJSON(a)
	}
	p := ctx.Printer()
	ctx.Printf("%s\n", p.Bold(a.DataDir))
	ctx.Printf("  %-9s %s\n", p.Dim("because"), a.Why)
	if !a.Exists {
		ctx.Printf("  %s\n", p.Yellow("no mailbox here yet — `iql setup` or `iql init`"))
	}
	if a.Problem != "" {
		ctx.Printf("  %s %s\n", p.Yellow("settings:"), a.Problem)
	} else if !a.HasMachine {
		ctx.Printf("  %-9s none at %s — `iql setup` makes them\n", p.Dim("machine"), a.Settings)
	}
	ctx.Printf("  %-9s %s\n", p.Dim("models"), a.Models)
	ctx.Printf("  %-9s %s\n", p.Dim("address"), a.Addr)
	if a.Hostname != "" {
		ctx.Printf("  %-9s %s\n", p.Dim("hostname"), a.Hostname)
	}
	if a.Server != "" {
		ctx.Printf("  %-9s running at %s\n", p.Dim("server"), a.Server)
	}
	return nil
}
