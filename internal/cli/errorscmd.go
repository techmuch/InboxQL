package cli

import (
	"flag"
	"time"

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
		ctx.Printf("Cleared %d entr%s.\n", removed, plural(removed, "y", "ies"))
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

	for _, e := range entries {
		ctx.Printf("%s  %-8s %s\n", e.CreatedAt.Format(time.RFC3339), e.Category, e.Reference)
		if e.Context != "" {
			ctx.Printf("  in %s\n", e.Context)
		}
		ctx.Printf("  %s\n\n", e.Message)
	}
	if total > len(entries) {
		ctx.Printf("Showing %d of %d. Raise --limit to see more.\n", len(entries), total)
	}
	return nil
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
