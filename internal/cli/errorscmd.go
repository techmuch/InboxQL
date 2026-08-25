package cli

import (
	"flag"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "errors",
		Summary: "show failures recorded by imports and other operations",
		Usage: `iql errors [--category <name>] [--job <id>] [--limit <n>] [--clear] [--json]

Per-item failures are recorded rather than only counted, because "3 failed"
says nothing about which three or why.

Flags:
  --category <name>  filter by category (currently only "import")
  --job <id>         only failures from one import job
  --limit <n>        maximum entries, newest first (default 50)
  --clear            delete the matching entries instead of listing them

The same records back the Error Log tab in the web interface.`,
		Run: runErrors,
	})
}

func runErrors(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("errors", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
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

	query := store.ErrorQuery{Category: *category, JobID: *jobID, Limit: *limit}

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
	total, err := store.CountErrors(store.ErrorQuery{Category: *category, JobID: *jobID})
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"total": total, "count": len(entries), "entries": entries,
		})
	}

	if total == 0 {
		ctx.Printf("No errors recorded.\n")
		return nil
	}

	// An error record is a paragraph, not a row: the message is free text and
	// routinely long. The header line is tabulated so the timestamps and
	// categories still line up down the page.
	p := ctx.Printer()
	t := p.NewTable("WHEN", "CATEGORY", "REFERENCE")
	for _, e := range entries {
		t.Row(e.CreatedAt.Format(time.RFC3339), t.Cell(ui.Bad, e.Category), e.Reference)
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
