package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "query",
		Aliases: []string{"q"},
		Summary: "search and aggregate with the query language",
		Usage: `iql query <expression> [--limit n] [--offset n] [--explain] [--json]

Runs an InboxQL query. An expression is a filter, optionally followed by
pipeline stages that reshape the result.

  iql query "from:stripe after:2026-01-01"
  iql query "is:unread -from:*@acme.com"
  iql query "label:invoice | sum amount by month"

Filter terms
  from: to: cc: bcc: anyone:   addresses; substring by default
  subject: body:              words in the text
  account: folder: mailbox:   where it lives
  is:unread is:starred is:reply is:answered is:junk is:deleted
  has:attachment has:file has:label
  after: before: on:          2026-08-15, 2026-08, 2026, today, 7d
  larger: smaller:            5mb, 500kb, a byte count
  label: unlabeled: conf>     annotator results
  extract:name.field>10       extracted structured data
  thread:<message-id>         every message in that conversation
  a bare word                 full-text search

Match modes
  from:acme        contains "acme" — also matches notacme@x.com
  from:=a@x.com    exactly that address
  from:*@acme.com  glob; * matches any run of characters

Negation
  -from:x  or  NOT from:x     and it composes: -(from:x after:2026-01)

  -label:x means the annotator evaluated this message and said no. It does
  NOT include messages the annotator has never seen — ask for those with
  unlabeled:x, or write "-label:x OR unlabeled:x" for the loose reading.

Pipeline stages
  | count [by <field>]        totals, or totals per group
  | top <field> [n]           the n largest groups
  | sort <field> [asc|desc]   reorder messages
  | limit <n>                 cap the rows
  | thread                    expand to whole conversations
  | participants              who appears, and how often
  | extract <annotator>       read that annotator's structured output
  | series <field> by <bucket>       an extracted value over time
  | sum|avg|min|max <field> [by <bucket>]

Group and bucket names
  from domain to cc account mailbox label thread subject
  hour day week month year

Flags
  --limit n    rows to return (default 50)
  --offset n   skip n rows; messages only
  --explain    print the compiled SQL instead of running it
  --count      return only how many messages match`,
		Run: runQuery,
	})

	register(&Command{
		Name:    "sql",
		Summary: "run a read-only SQL statement",
		Usage: `iql sql <statement> [--limit n] [--json]
iql sql --schema

The escape hatch. The query language will not cover everything, and this is
what to reach for when it does not.

The connection is opened read-only, so no statement here can modify the
database whatever it says.

  iql sql "SELECT from_addr, COUNT(*) FROM messages GROUP BY 1 ORDER BY 2 DESC LIMIT 10"
  iql sql --schema

Tables worth knowing:
  messages              one row per message; date is epoch milliseconds
  message_participants  (message_id, role, address) — from/to/cc/bcc, normalised
  message_refs          (message_id, ref, ordinal) — the References chain
  annotators            label and extraction definitions
  annotations           their results, with status, source and confidence
  attachments, accounts, drafts, import_jobs, error_log

Flags
  --schema     list tables and views with their definitions
  --limit n    rows to return (default 200)`,
		Run: runSQL,
	})
}

// parseQueryArgs parses flags while leaving the query expression alone.
//
// The general parseArgs treats every dash-prefixed token as a flag, which is
// right everywhere else and wrong here: `-from:alice` is negation, the most
// basic thing this command does, and the flag package would reject it as an
// unknown flag before the query was ever seen. Requiring `iql query -- "-x"`
// for that would make the documented syntax unusable as documented.
//
// So only tokens this command actually defines are treated as flags. An
// unknown dash token is part of the expression, where the query parser will
// report it with a position if it is genuinely wrong.
func parseQueryArgs(fs *flag.FlagSet, args []string) (string, error) {
	var flags, expr []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			expr = append(expr, args[i+1:]...)
			i = len(args)
		case len(arg) > 1 && strings.HasPrefix(arg, "-") && isDefinedFlag(fs, arg):
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && flagNeedsValue(fs, arg) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			expr = append(expr, arg)
		}
	}

	if err := fs.Parse(flags); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(expr, " ")), nil
}

// isDefinedFlag reports whether this command declared the flag.
func isDefinedFlag(fs *flag.FlagSet, arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if i := strings.Index(name, "="); i >= 0 {
		name = name[:i]
	}
	return fs.Lookup(name) != nil
}

func runQuery(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	limit := fs.Int("limit", 50, "rows to return")
	offset := fs.Int("offset", 0, "rows to skip")
	explain := fs.Bool("explain", false, "print the compiled SQL without running it")
	countOnly := fs.Bool("count", false, "return only the number of matches")
	expr, err := parseQueryArgs(fs, args)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *explain {
		res, err := store.ExplainQuery(expr)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(res)
		}
		ctx.Printf("%s\n", res.SQL)
		if len(res.Args) > 0 {
			ctx.Printf("\nparameters: %v\n", res.Args)
		}
		return nil
	}

	if *countOnly {
		n, err := store.CountQuery(expr)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"query": expr, "count": n})
		}
		ctx.Printf("%d\n", n)
		return nil
	}

	res, err := store.RunQuery(expr, *limit, *offset)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	if ctx.JSON {
		// The compiled SQL is useful when debugging a query but noise in a
		// pipeline, so it is dropped unless --explain asked for it.
		res.SQL, res.Args = "", nil
		return ctx.EmitJSON(res)
	}

	return printQueryResult(ctx, res)
}

func printQueryResult(ctx *Context, res *store.QueryResult) error {
	p := ctx.Printer()

	switch res.Kind {
	case "count":
		ctx.Printf("%d\n", res.Total)
		return nil

	case "groups":
		if len(res.Groups) == 0 {
			ctx.Printf("No results.\n")
			return nil
		}
		t := p.NewTable("GROUP", "VALUE")
		for _, g := range res.Groups {
			label := g.Label
			if label == "" {
				label = "(none)"
			}
			t.Row(ui.Truncate(label, 60), formatNumber(g.Value))
		}
		return t.Flush()

	default:
		if len(res.Messages) == 0 {
			ctx.Printf("No messages matched.\n")
			return nil
		}
		t := p.NewTable("ID", "DATE", "FROM", "SUBJECT")
		for _, m := range res.Messages {
			subject := m.Subject
			if subject == "" {
				subject = "(no subject)"
			}
			t.Row(
				ui.Truncate(m.ID, 12),
				m.Date.Format("2006-01-02"),
				ui.Truncate(m.From, 32),
				ui.Truncate(subject, 52),
			)
		}
		if err := t.Flush(); err != nil {
			return err
		}
		ctx.Printf("\n%s\n", p.Dim(count(len(res.Messages), "message", "messages")))
		return nil
	}
}

// formatNumber prints a count as an integer and a measurement as a decimal.
//
// An aggregate over extracted data is not a count — `| sum amount by month`
// is currency — and rendering 1420.5 as 1420 would be a quiet lie.
func formatNumber(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

func runSQL(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	limit := fs.Int("limit", 200, "rows to return")
	schema := fs.Bool("schema", false, "list tables and views")
	statement, err := parseQueryArgs(fs, args)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *schema {
		res, err := store.SchemaOverview()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(res)
		}
		p := ctx.Printer()
		for _, row := range res.Rows {
			ctx.Printf("%s %s\n", p.Dim(fmt.Sprint(row[0])), p.Bold(fmt.Sprint(row[1])))
			for _, line := range strings.Split(fmt.Sprint(row[2]), "\n") {
				if s := strings.TrimSpace(line); s != "" {
					ctx.Printf("    %s\n", p.Dim(s))
				}
			}
			ctx.Printf("\n")
		}
		return nil
	}

	if statement == "" {
		return Fail(ExitUsage, "give a statement, or --schema to list what there is to query")
	}

	res, err := store.RunRawSQL(ctx.DataDir, statement, *limit)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(res)
	}

	if len(res.Rows) == 0 {
		ctx.Printf("No rows.\n")
		return nil
	}

	p := ctx.Printer()
	headers := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		headers[i] = strings.ToUpper(c)
	}
	t := p.NewTable(headers...)
	for _, row := range res.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			if c == nil {
				cells[i] = "NULL"
				continue
			}
			cells[i] = ui.Truncate(strings.ReplaceAll(fmt.Sprint(c), "\n", " "), 48)
		}
		t.Row(cells...)
	}
	if err := t.Flush(); err != nil {
		return err
	}
	if res.Truncated {
		ctx.Printf("\n%s\n", p.Yellow(fmt.Sprintf("Stopped at %d rows; pass --limit for more.", len(res.Rows))))
	}
	return nil
}
