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
  iql query "status:todo | timeline"

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
  saved:<name>                everything a saved query matches
  a bare word                 full-text search

Shorthands
  from:(alice OR bob)         a group scoped to one field
  from:me()                   your own configured addresses
  has:cc                      somebody was copied; -has:cc means nobody was

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
  | timeline                  conversations, with the tickets and drafts
                              they produced, interleaved by time
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
  --count      return only how many messages match
  --complete n list what may be typed at cursor position n`,
		Run: runQuery,
	})

	register(&Command{
		Name:    "saved",
		Aliases: []string{"queries"},
		Summary: "name and reuse queries",
		Usage: `iql saved <list|show|save|delete> [flags]

A saved query is a building block, not a bookmark: ` + "`saved:<name>`" + ` is a term
in the language, so a saved query composes with everything else.

  iql saved save "Acme invoices" --query "from:*@acme.com subject:invoice"
  iql query "saved:acme-invoices after:7d"
  iql saved list

save flags:
  --query <expr>       the query text; - reads stdin
  --name <slug>        the handle used by saved: (default: a slug of the title)
  --description <text> what it is for
  --pinned             sort it to the top

The query is compiled before it is stored, so a saved query that does not work
is rejected at the point you write it rather than from wherever you use it.

Saved queries do not nest: one cannot reference another.`,
		Run: runSaved,
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

func runSaved(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "list", "":
		return savedList(ctx, rest)
	case "show":
		return savedShow(ctx, rest)
	case "save", "add", "create":
		return savedSave(ctx, rest)
	case "delete", "rm", "remove":
		return savedDelete(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, show, save or delete)", sub)
	}
}

func savedList(ctx *Context, args []string) error {
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	queries, err := store.ListSavedQueries()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(queries)
	}
	if len(queries) == 0 {
		ctx.Printf("No saved queries. Create one with `iql saved save \"Name\" --query \"...\"`\n")
		return nil
	}

	p := ctx.Printer()
	t := p.NewTable("NAME", "TITLE", "QUERY")
	for _, q := range queries {
		title := q.Title
		if q.Pinned {
			title = "* " + title
		}
		t.Row(q.Name, ui.Truncate(title, 28), ui.Truncate(q.Query, 48))
	}
	return t.Flush()
}

func savedShow(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which saved query?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	q, err := store.GetSavedQuery(name)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if q == nil {
		return Fail(ExitNotFound, "no saved query named %q", name)
	}
	if ctx.JSON {
		return ctx.EmitJSON(q)
	}

	p := ctx.Printer()
	ctx.Printf("%s %s\n\n", p.Bold(q.Title), p.Dim("("+q.Name+")"))
	ctx.Printf("  %s\n", q.Query)
	if q.Description != "" {
		ctx.Printf("\n  %s\n", p.Dim(q.Description))
	}
	ctx.Printf("\nUse it: %s\n", p.Dim("iql query \"saved:"+q.Name+"\""))
	return nil
}

func savedSave(ctx *Context, args []string) error {
	title, rest := subcommand(args)
	if title == "" {
		return Fail(ExitUsage, "give the saved query a name")
	}

	fs := flag.NewFlagSet("saved save", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	queryText := fs.String("query", "", "the query text; - reads stdin")
	name := fs.String("name", "", "the handle saved: will use")
	description := fs.String("description", "", "what it is for")
	pinned := fs.Bool("pinned", false, "sort it to the top")
	// The query itself may begin with a dash, so flags are separated the same
	// way `iql query` separates them.
	expr, err := parseQueryArgs(fs, rest)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	text := *queryText
	if text == "" {
		text = expr
	}
	if text == "-" {
		b, err := readAll(ctx.Stdin)
		if err != nil {
			return Fail(ExitError, "reading the query from stdin: %v", err)
		}
		text = strings.TrimSpace(string(b))
	}
	if text == "" {
		return Fail(ExitUsage, "--query is required")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	q := &store.SavedQuery{
		Title: title, Name: *name, Query: text,
		Description: *description, Pinned: *pinned,
	}
	if err := store.SaveQuery(q); err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(q)
	}
	p := ctx.Printer()
	ctx.Printf("Saved %s as %s.\n\n", p.Bold(q.Title), p.Bold(q.Name))
	ctx.Printf("Use it: %s\n", p.Dim("iql query \"saved:"+q.Name+"\""))
	return nil
}

func savedDelete(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which saved query?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if err := store.DeleteSavedQuery(name); err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"deleted": name})
	}
	ctx.Printf("Deleted %s. Queries referencing it will now fail rather than match nothing.\n", name)
	return nil
}

func runQuery(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	limit := fs.Int("limit", 50, "rows to return")
	offset := fs.Int("offset", 0, "rows to skip")
	explain := fs.Bool("explain", false, "print the compiled SQL without running it")
	countOnly := fs.Bool("count", false, "return only the number of matches")
	complete := fs.Int("complete", -1, "list what may be typed at this cursor position")
	expr, err := parseQueryArgs(fs, args)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *complete >= 0 {
		c, err := store.Complete(expr, *complete)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(c)
		}
		p := ctx.Printer()
		ctx.Printf("%s\n\n", p.Dim(string(c.Context)+" completion"))
		for _, cand := range c.Candidates {
			if cand.Detail != "" {
				ctx.Printf("  %-28s %s\n", cand.Value, p.Dim(cand.Detail))
			} else {
				ctx.Printf("  %s\n", cand.Value)
			}
		}
		return nil
	}

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

	case "tickets":
		if len(res.Tickets) == 0 {
			ctx.Printf("No tickets matched.\n")
			return nil
		}
		t := p.NewTable("ID", "STATUS", "DUE", "TITLE")
		for _, tk := range res.Tickets {
			due := ""
			if tk.DueAt != nil {
				due = tk.DueAt.Format("2006-01-02")
			}
			t.Row(ui.Truncate(tk.ID, 8), tk.Status, due, ui.Truncate(tk.Title, 56))
		}
		return t.Flush()

	case "drafts":
		if len(res.Drafts) == 0 {
			ctx.Printf("No drafts matched.\n")
			return nil
		}
		t := p.NewTable("ID", "UPDATED", "STATUS", "TO", "SUBJECT")
		for _, d := range res.Drafts {
			t.Row(
				ui.Truncate(d.ID, 8),
				d.UpdatedAt.Format("2006-01-02"),
				d.Status,
				ui.Truncate(strings.Join(d.To, ", "), 28),
				ui.Truncate(d.Subject, 44),
			)
		}
		return t.Flush()

	case "threads":
		return printThreads(ctx, res)

	case "contacts":
		if len(res.Contacts) == 0 {
			ctx.Printf("No contacts matched.\n")
			return nil
		}
		t := p.NewTable("NAME", "ADDRESS", "KIND", "MSGS", "SENT", "LAST SEEN")
		for _, c := range res.Contacts {
			last := ""
			if !c.LastSeen.IsZero() {
				last = c.LastSeen.Format("2006-01-02")
			}
			kind := c.Kind
			if kind == store.KindSystem {
				kind = p.Yellow(kind)
			}
			t.Row(
				ui.Truncate(c.Name(), 28),
				ui.Truncate(c.Address, 34),
				kind,
				fmt.Sprintf("%d", c.Messages),
				fmt.Sprintf("%d", c.Sent),
				last,
			)
		}
		if err := t.Flush(); err != nil {
			return err
		}
		ctx.Printf("\n%s\n", p.Dim(count(len(res.Contacts), "contact", "contacts")))
		return nil

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

// printThreads renders conversations as indented timelines.
//
// Not a table: a timeline is nested, and flattening it into rows loses the one
// thing it exists to show — that these events belong to the same conversation.
// So the conversation is a heading and its moments are indented under it.
func printThreads(ctx *Context, res *store.QueryResult) error {
	p := ctx.Printer()

	if len(res.Threads) == 0 {
		ctx.Printf("No conversations matched.\n")
		return nil
	}

	for i, th := range res.Threads {
		if i > 0 {
			ctx.Printf("\n")
		}
		subject := th.Subject
		if subject == "" {
			subject = "(no subject)"
		}
		ctx.Printf("%s\n", ui.Truncate(subject, 72))
		ctx.Printf("%s\n", p.Dim(threadSummaryLine(th)))

		for _, e := range th.Entries {
			ctx.Printf("  %s  %-6s  %s\n",
				e.At.Format("2006-01-02"),
				entryTag(e.Kind),
				ui.Truncate(entryLine(e), 62))
		}
	}

	ctx.Printf("\n%s\n", p.Dim(count(len(res.Threads), "conversation", "conversations")))
	return nil
}

// threadSummaryLine describes a conversation in one dim line.
func threadSummaryLine(th *store.Thread) string {
	parts := []string{count(th.MessageCount, "message", "messages")}
	if th.TicketCount > 0 {
		parts = append(parts, count(th.TicketCount, "ticket", "tickets"))
	}
	if th.DraftCount > 0 {
		parts = append(parts, count(th.DraftCount, "draft", "drafts"))
	}
	if len(th.Participants) > 0 {
		parts = append(parts, strings.Join(th.Participants, ", "))
	}
	return ui.Truncate(strings.Join(parts, " · "), 76)
}

// entryTag is a short, fixed-width word for an entry's type.
//
// Words rather than symbols: this output is piped, grepped and pasted into
// issues, and a glyph that renders as a box in someone's terminal is worse
// than four plain letters.
func entryTag(kind string) string {
	switch kind {
	case store.EntryMessage:
		return "mail"
	case store.EntryTicket, store.EntryEvent:
		return "task"
	case store.EntryDraft:
		return "draft"
	case store.EntryAnnotation:
		return "label"
	}
	return kind
}

// entryLine is what one moment reads as.
func entryLine(e store.ThreadEntry) string {
	switch e.Kind {
	case store.EntryMessage:
		from, _ := store.NormaliseAddress(e.Actor)
		if from == "" {
			from = e.Actor
		}
		return from + "  " + e.Summary
	case store.EntryAnnotation:
		if e.Annotation != nil && e.Annotation.DataJSON != "" {
			return e.Summary + "  " + ui.Truncate(e.Annotation.DataJSON, 40)
		}
		return e.Summary
	}
	return e.Summary
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
