package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/logging"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "log",
		Aliases: []string{"errors"},
		Summary: "read what the application has been doing",
		Usage: `iql log [--level <name>] [--category <name>] [--job <id>] [--limit <n>] [--clear]
iql log level [debug|info|warn|error]

Everything the application records: syncs, imports, annotator runs, model calls
and the failures among them. Per-item failures are recorded rather than only
counted, because "3 failed" says nothing about which three or why.

Flags:
  --level <name>     keep this level and above (debug, info, warn, error)
  --slow             only lines that took longer than the configured threshold
  --category <name>  filter by subsystem — import, app, sync
  --job <id>         only lines from one import or maintenance run
  --limit <n>        maximum lines, newest first (default 50)
  --clear            delete the matching lines instead of listing them

## Setting the level

  iql log level          what it is now
  iql log level debug    turn it up

Changed at runtime and remembered, because logging is turned up after
something has already gone wrong — a flag would mean reproducing it first.

Debug is loud: a sync writes thousands of lines a minute, and the log is a
table in the same database the mail is in. Turn it up to find something, then
turn it back down.

## Timings

Every query is recorded with how long it took, and a slow one is a warning
carrying its SQL, so it can be pasted into "iql sql --explain". The --slow flag
shows those; "in:logs duration>1000" asks the same question in the language.

What is recorded, how slow is slow, and whether query text is written down are
in Settings under Logging.

## It is a query kind

The language reaches the same rows, which is where this gets useful:

  iql query "in:logs level>warn after:7d"
  iql query "in:logs | count by category"
  iql query "in:logs job:7f2a"

` + "`iql log`" + ` is the quick read; ` + "`in:logs`" + ` composes.`,
		Run: runErrors,
	})
}

func runErrors(ctx *Context, args []string) error {
	// `log level` is a subcommand, not a flag, because it sets rather than
	// filters and the two would be confused under one name.
	if sub, rest := subcommand(args); sub == "level" {
		return logLevel(ctx, rest)
	}

	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	level := fs.String("level", "", "keep this level and above")
	slow := fs.Bool("slow", false, "only lines slower than the threshold")
	category := fs.String("category", "", "filter by category")
	jobID := fs.String("job", "", "filter by import job")
	limit := fs.Int("limit", 50, "maximum entries")
	clear := fs.Bool("clear", false, "delete matching entries")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *level != "" && !slices.Contains(store.Levels, strings.ToLower(*level)) {
		return Fail(ExitUsage, "--level must be one of: %s", strings.Join(store.Levels, ", "))
	}
	query := store.ErrorQuery{
		Category: *category, JobID: *jobID, MinLevel: *level, Limit: *limit}
	if *slow {
		query.MinDuration = store.LoadLogSettings().SlowMs
	}

	if *clear {
		removed, err := store.ClearErrors(query)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"cleared": removed})
		}
		ctx.Printf("Cleared %s.\n", count(removed, "entry", "entries"))
		return nil
	}

	entries, err := store.ListErrors(query)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	total, err := store.CountErrors(store.ErrorQuery{
		Category: *category, JobID: *jobID, MinLevel: *level,
		MinDuration: query.MinDuration})
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"total": total, "count": len(entries), "entries": entries,
		})
	}

	if total == 0 {
		ctx.Printf("Nothing recorded at this level.\n")
		return nil
	}

	// An error record is a paragraph, not a row: the message is free text and
	// routinely long. The header line is tabulated so the timestamps and
	// categories still line up down the page.
	p := ctx.Printer()
	t := p.NewTable("WHEN", "LEVEL", "CATEGORY", "TOOK", "REFERENCE")
	for _, e := range entries {
		// The number the log exists to carry. Blank rather than "0ms" for a
		// line that is not about something taking time — a migration notice is
		// not an instantaneous query.
		took := ""
		if e.Duration != nil {
			took = fmt.Sprintf("%dms", *e.Duration)
		}
		// Coloured by level rather than by being present. Everything here used
		// to be a failure; now most of it is a record of ordinary work, and
		// painting it all red would make the failures harder to find, not
		// easier.
		t.Row(e.CreatedAt.Format(time.RFC3339),
			t.Cell(levelStyle(e.Level), e.Level), e.Category, took, e.Reference)
	}
	if err := t.Flush(); err != nil {
		return Fail(ExitError, "writing errors: %v", err)
	}
	p.Printf("\n")
	for _, e := range entries {
		p.Printf("%s %s\n", p.Dim(e.CreatedAt.Format(time.RFC3339)), e.Reference)
		if e.Context != "" {
			p.Printf("  in %s\n", e.Context)
		}
		p.Printf("  %s\n\n", e.Message)
	}
	if total > len(entries) {
		ctx.Printf("Showing %d of %d. Raise --limit to see more.\n", len(entries), total)
	}
	return nil
}

// levelStyle paints a level by severity.
func levelStyle(level string) ui.Status {
	switch level {
	case store.LevelError:
		return ui.Bad
	case store.LevelWarn:
		return ui.Warn
	default:
		// Not OK either: an info line is neither a success nor a failure, it
		// is a record that something happened.
		return ui.Note
	}
}

// logLevel reads or sets the floor.
func logLevel(ctx *Context, args []string) error {
	want, _ := subcommand(args)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if want == "" {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{
				"level": logging.Level(), "levels": store.Levels, "dropped": logging.Dropped(),
			})
		}
		ctx.Printf("%s\n", logging.Level())
		if n := logging.Dropped(); n > 0 {
			ctx.Printf("%s\n", ctx.Printer().Yellow(
				fmt.Sprintf("%d lines were dropped: the writer could not keep up", n)))
		}
		return nil
	}

	want = strings.ToLower(want)
	if !slices.Contains(store.Levels, want) {
		return Fail(ExitUsage, "%q is not a level (%s)", want, strings.Join(store.Levels, ", "))
	}
	// Both the running process and the stored value: setting one of them is
	// the version that reads as broken, either doing nothing until a restart
	// or forgetting at one.
	logging.SetLevel(want)
	if err := store.UpdateSetting(logLevelSetting, want); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"level": want})
	}
	ctx.Printf("Log level is now %s.\n", want)
	if want == store.LevelDebug {
		ctx.Printf("%s\n", ctx.Printer().Dim(
			"Debug is loud — a sync writes thousands of lines a minute. Turn it back down after."))
	}
	return nil
}
