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
		Name:    "ticket",
		Aliases: []string{"tickets"},
		Summary: "work the tickets derived from your mail",
		Usage: `iql ticket <list|show|new|move|accept|reject|merge|delete|propose|board> [flags]

A ticket is an entity with state that changes; a message is an immutable event.
Tickets are seeded by extractors and owned by you: a re-run attaches more
evidence, it never rewrites a status you set.

  list      tickets matching a query (default: everything not done)
  show      one ticket and the mail behind it
  new       raise one by hand
  move      set the status — what dragging a card does
  accept    move a proposal into the working set
  reject    record that a proposal was wrong
  merge     fold one ticket's evidence into another
  delete    remove a ticket, leaving its mail untouched
  propose   raise tickets from an extractor's results
  board     the columns, with their tickets

Ticket fields are query terms, so the same language filters both:

  iql ticket list --query "status:todo due:7d"
  iql ticket list --query "status:doing from:*@acme.com"
  iql query "status:* | count by status"

A query naming a message field inside a ticket query asks about the ticket's
evidence: "tickets whose mail came from Acme".

new flags:
  --status --priority --due   the ticket's own fields
  --message <id>              the mail it came from; also files the ticket in
                              that message's conversation, so it appears on the
                              thread's timeline
  --thread <key>              file it in a conversation directly

propose flags:
  --auto-accept <0-1>  confidence at or above which a ticket skips the queue
  --dry-run            report what would be raised and write nothing

A conversation and everything it caused, in one time-ordered view:

  iql query "status:todo | timeline"`,
		Run: runTicket,
	})
}

func runTicket(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "list", "":
		return ticketList(ctx, rest)
	case "show":
		return ticketShow(ctx, rest)
	case "new", "add", "create":
		return ticketNew(ctx, rest)
	case "move":
		return ticketMove(ctx, rest)
	case "accept":
		return ticketTransition(ctx, rest, store.TicketTodo, "accepted")
	case "reject":
		return ticketTransition(ctx, rest, store.TicketRejected, "rejected")
	case "merge":
		return ticketMerge(ctx, rest)
	case "delete", "rm", "remove":
		return ticketDelete(ctx, rest)
	case "propose":
		return ticketPropose(ctx, rest)
	case "board":
		return ticketBoard(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q", sub)
	}
}

// openTickets is the default scope: everything still live.
//
// Done and rejected tickets are history. A list that showed them by default
// would grow without bound and stop being the thing you look at each morning.
const openTickets = "-status:done -status:rejected"

func ticketList(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket list", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	q := fs.String("query", "", "filter with the query language")
	limit := fs.Int("limit", 50, "rows to return")
	expr, err := parseQueryArgs(fs, args)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if *q != "" {
		expr = *q
	}
	if strings.TrimSpace(expr) == "" {
		expr = openTickets
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	res, err := store.RunQuery(expr, *limit, 0)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if res.Kind != "tickets" {
		return Fail(ExitUsage, "that query returns %s, not tickets; name a ticket field such as status:", res.Kind)
	}

	if ctx.JSON {
		return ctx.EmitJSON(res.Tickets)
	}
	if len(res.Tickets) == 0 {
		ctx.Printf("No tickets matched.\n")
		return nil
	}

	p := ctx.Printer()
	t := p.NewTable("ID", "STATUS", "DUE", "TITLE", "FROM")
	for _, tk := range res.Tickets {
		due := ""
		if tk.DueAt != nil {
			due = tk.DueAt.Format("2006-01-02")
		}
		origin := ""
		if len(tk.Sources) > 0 {
			origin = tk.Sources[0].From
		}
		t.Row(ui.Truncate(tk.ID, 8), tk.Status, due,
			ui.Truncate(tk.Title, 48), ui.Truncate(origin, 26))
	}
	return t.Flush()
}

func ticketShow(ctx *Context, args []string) error {
	id, _ := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := findTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(tk)
	}

	p := ctx.Printer()
	ctx.Printf("%s\n\n", p.Bold(tk.Title))
	ctx.Printf("  %-10s %s\n", p.Dim("status"), tk.Status)
	if tk.Priority != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("priority"), tk.Priority)
	}
	if tk.DueAt != nil {
		ctx.Printf("  %-10s %s\n", p.Dim("due"), tk.DueAt.Format("2006-01-02"))
	}
	ctx.Printf("  %-10s %s\n", p.Dim("raised by"), tk.Origin)
	if tk.Body != "" {
		ctx.Printf("\n%s\n", tk.Body)
	}

	// The evidence, always. A ticket you cannot trace is a task in a worse
	// task manager.
	ctx.Printf("\n%s\n", p.Dim("From this mail:"))
	for _, s := range tk.Sources {
		ctx.Printf("  %s  %-28s %s\n", p.Dim(s.Date.Format("2006-01-02")),
			ui.Truncate(s.From, 28), ui.Truncate(s.Subject, 50))
		ctx.Printf("  %s\n", p.Dim("iql read "+s.MessageID))
	}
	return nil
}

// findTicket resolves a full id or a unique prefix.
//
// Ticket ids are UUIDs and nobody types one in full; the list prints eight
// characters, so those eight have to be enough to act on.
func findTicket(prefix string) (*store.Ticket, error) {
	if tk, err := store.GetTicket(prefix); err == nil && tk != nil {
		return tk, nil
	}
	// A glob over the status enum is how the language says "every ticket".
	res, err := store.RunQuery("status:*", 1000, 0)
	if err != nil {
		return nil, err
	}
	var found *store.Ticket
	for _, tk := range res.Tickets {
		if strings.HasPrefix(tk.ID, prefix) {
			if found != nil {
				return nil, fmt.Errorf("%q matches more than one ticket; use more of the id", prefix)
			}
			found = tk
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no ticket matching %q", prefix)
	}
	return found, nil
}

func ticketNew(ctx *Context, args []string) error {
	title, rest := subcommand(args)
	if title == "" {
		return Fail(ExitUsage, "give the ticket a title")
	}
	fs := flag.NewFlagSet("ticket new", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	status := fs.String("status", store.TicketTodo, "starting status")
	priority := fs.String("priority", "", "priority")
	due := fs.String("due", "", "due date")
	message := fs.String("message", "", "message id this came from")
	thread := fs.String("thread", "", "conversation this belongs to")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk := &store.Ticket{Title: title, Status: *status, Priority: *priority, Origin: store.OriginHumanTicket}
	if *message != "" {
		// A ticket raised from a message belongs to that message's
		// conversation, not to nothing. Attaching the evidence without setting
		// the key left the ticket invisible on its own thread's timeline, and
		// made a later extraction propose a duplicate for the same thread.
		key, err := store.MessageThreadKey(*message)
		if err != nil {
			return Fail(ExitUsage, "--message: %v", err)
		}
		tk.ThreadKey = key
	}
	if *thread != "" {
		tk.ThreadKey = *thread
	}
	if *due != "" {
		when, err := store.ParseTicketDue(*due)
		if err != nil {
			return Fail(ExitUsage, "--due: %v", err)
		}
		tk.DueAt = when
	}
	if err := store.SaveTicket(tk); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if *message != "" {
		if err := store.AttachSource(tk.ID, *message, ""); err != nil {
			return Fail(ExitError, "attaching the message: %v", err)
		}
	}

	if ctx.JSON {
		return ctx.EmitJSON(tk)
	}
	ctx.Printf("Raised %s.\n", ctx.Printer().Bold(tk.ID[:8]))
	return nil
}

func ticketMove(ctx *Context, args []string) error {
	id, rest := subcommand(args)
	status, _ := subcommand(rest)
	if id == "" || status == "" {
		return Fail(ExitUsage, "usage: iql ticket move <id> <status>")
	}
	return applyStatus(ctx, id, status, "moved to "+status)
}

func ticketTransition(ctx *Context, args []string, status, verb string) error {
	id, _ := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	return applyStatus(ctx, id, status, verb)
}

func applyStatus(ctx *Context, id, status, verb string) error {
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := findTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if err := store.SetTicketStatus(tk.ID, status); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"id": tk.ID, "status": status})
	}
	ctx.Printf("%s — %s\n", ui.Truncate(tk.Title, 56), verb)
	return nil
}

func ticketMerge(ctx *Context, args []string) error {
	into, rest := subcommand(args)
	from, _ := subcommand(rest)
	if into == "" || from == "" {
		return Fail(ExitUsage, "usage: iql ticket merge <into-id> <from-id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	target, err := findTicket(into)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	source, err := findTicket(from)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if err := store.MergeTickets(target.ID, source.ID); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"into": target.ID, "merged": source.ID})
	}
	ctx.Printf("Merged. %s now carries the mail from both.\n", ui.Truncate(target.Title, 50))
	return nil
}

func ticketDelete(ctx *Context, args []string) error {
	id, _ := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := findTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if err := store.DeleteTicket(tk.ID); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"deleted": tk.ID})
	}
	ctx.Printf("Deleted. The mail behind it is untouched.\n")
	return nil
}

func ticketPropose(ctx *Context, args []string) error {
	name, rest := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which extractor?")
	}
	fs := flag.NewFlagSet("ticket propose", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	// The sentinel means "whatever the stored preference is", resolved after
	// the store is open. A literal default here would silently ignore the
	// setting the UI writes.
	autoAccept := fs.Float64("auto-accept", -1, "confidence at or above which a ticket skips the queue")
	dryRun := fs.Bool("dry-run", false, "report what would be raised and write nothing")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	threshold := *autoAccept
	if threshold < 0 {
		threshold = store.DefaultTicketAutoAccept()
	}

	out, err := store.ProposeTickets(name, threshold, *dryRun)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(out)
	}

	p := ctx.Printer()
	ctx.Printf("  %-16s %d\n", p.Dim("raised"), out.Proposed)
	ctx.Printf("  %-16s %d\n", p.Dim("auto-accepted"), out.Accepted)
	ctx.Printf("  %-16s %d\n", p.Dim("already raised"), out.Existing)
	if out.DryRun {
		ctx.Printf("\n%s\n", p.Dim("Nothing was written. Drop --dry-run to raise them."))
		return nil
	}
	if out.Proposed > out.Accepted {
		ctx.Printf("\nReview them: %s\n", p.Dim(`iql ticket list --query "status:proposed"`))
	}
	return nil
}

// ticketBoard prints the columns.
//
// A board is a filter plus a column field. Keeping those separate is what makes
// moving a card mean something: it writes the column field, which is the
// inverse of the query that defined the column. Conflating them leaves dragging
// undefined — there is no meaning to moving a card from `due:7d` to `from:x`.
func ticketBoard(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket board", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	q := fs.String("query", "", "which tickets appear on the board")
	limit := fs.Int("limit", 10, "tickets shown per column")
	expr, err := parseQueryArgs(fs, args)
	if err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if *q != "" {
		expr = *q
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	columns, err := store.BoardColumns(expr, *limit)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(columns)
	}

	p := ctx.Printer()
	for _, col := range columns {
		ctx.Printf("\n%s %s\n", p.Bold(strings.ToUpper(col.Status)),
			p.Dim(fmt.Sprintf("(%d)", col.Total)))
		if len(col.Tickets) == 0 {
			ctx.Printf("  %s\n", p.Dim("—"))
			continue
		}
		for _, tk := range col.Tickets {
			due := ""
			if tk.DueAt != nil {
				due = " " + p.Dim("due "+tk.DueAt.Format("Jan 2"))
			}
			ctx.Printf("  %s  %s%s\n", p.Dim(tk.ID[:8]), ui.Truncate(tk.Title, 56), due)
		}
		if col.Total > int64(len(col.Tickets)) {
			ctx.Printf("  %s\n", p.Dim(fmt.Sprintf("… %d more", col.Total-int64(len(col.Tickets)))))
		}
	}
	ctx.Printf("\n%s\n", p.Dim("Move one: iql ticket move <id> <status>"))
	return nil
}
