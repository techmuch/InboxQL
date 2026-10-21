package cli

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "ticket",
		Aliases: []string{"tickets", "card", "cards"},
		Summary: "work the board: cards, lists, boards, labels and rules",
		Usage: `iql ticket <command> [flags]

A ticket — a card — is something to do. It can come from your mail or from
you, and it is yours: a re-run attaches more evidence, it never rewrites what
you set.

Cards:
  list      cards matching a query (default: everything not finished)
  show      one card: description, checklist, comments, history, its mail
  new       add a card
  edit      change a card's title, description, priority or dates
  move      put a card in a list, at a place
  archive   take a card off the board (restore puts it back)
  label     add labels (unlabel takes them off)
  checklist add, tick and edit checklist items
  comment   add a comment
  accept    move a proposal into the first open list
  reject    record that a proposal was wrong
  merge     fold one card's evidence into another
  delete    remove a card, leaving its mail untouched
  propose   raise cards from an extractor's results

The board:
  board     the lists, with their cards
  lists     add, rename, reorder, archive or delete lists
  boards    named filters over the lists ("Home" is label:home)
  labels    the labels in use, and their colours
  rules     automation: when a card enters a list, or mail arrives

Card fields are query terms, so the same language filters both:

  iql ticket list --query "status:todo due:7d"
  iql ticket list --query "label:home state:open"
  iql ticket list --query "status:doing from:*@acme.com"
  iql query "status:* | count by status"

A mail field inside a ticket query asks about the card's evidence: "cards
whose mail came from Acme". Archived cards are left out unless the query
says archived:.

new flags:
  --list <list>               where it goes (default: the first open list)
  --priority --due --start    its fields; dates are 2026-10-24, 3d, 2w…
  --label a,b                 labels (labels are topics)
  --body <text|->  --body-file <path>   the description, in Markdown
  --board <board>             add the labels that board's filter asks for
  --message <id>              the mail it came from; also files the card in
                              that message's conversation
  --thread <key>              file it in a conversation directly

edit flags: --title --body --body-file --priority --due --start
  (--due none clears a date)

move: iql ticket move <id> <list> [--after <card> | --top]

checklist:
  iql ticket checklist <id>                       the items
  iql ticket checklist <id> add "text" [--checklist name]
  iql ticket checklist <id> done|undo|remove <item>
  iql ticket checklist <id> edit <item> "text"
  iql ticket checklist <id> rename <checklist> <new name>
  iql ticket checklist <id> delete <checklist>
  (<item> is its text, or its id or a prefix of it)

comment: iql ticket comment <id> "text" [--origin agent]   (- reads stdin)

propose flags:
  --auto-accept <0-1>  confidence at or above which a card skips the queue
  --dry-run            report what would be raised and write nothing`,
		Run: runTicket,
	})
}

func runTicket(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "list", "ls", "":
		return ticketList(ctx, rest)
	case "show":
		return ticketShow(ctx, rest)
	case "new", "add", "create":
		return ticketNew(ctx, rest)
	case "edit", "set":
		return ticketEdit(ctx, rest)
	case "move", "mv":
		return ticketMove(ctx, rest)
	case "archive":
		return ticketArchive(ctx, rest, true)
	case "restore", "unarchive":
		return ticketArchive(ctx, rest, false)
	case "label":
		return ticketLabel(ctx, rest, true)
	case "unlabel":
		return ticketLabel(ctx, rest, false)
	case "checklist", "check":
		return ticketChecklist(ctx, rest)
	case "comment":
		return ticketComment(ctx, rest)
	case "accept":
		return ticketTransition(ctx, rest, "", "accepted")
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
	case "lists":
		return ticketLists(ctx, rest)
	case "boards":
		return ticketBoards(ctx, rest)
	case "labels":
		return ticketLabels(ctx, rest)
	case "rules", "rule":
		return ticketRules(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q", sub)
	}
}

// openTickets is the default scope: everything not finished.
//
// Finished and rejected cards are history. A list that showed them by default
// would grow without bound and stop being the thing you look at each morning.
// state: rather than status:done, so a closed list of your own counts too.
const openTickets = "-state:closed -state:rejected"

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
	// A query that names no card field is still about cards here.
	expr = "in:tickets " + expr

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
	t := p.NewTable("ID", "LIST", "DUE", "TITLE", "LABELS")
	for _, tk := range res.Tickets {
		t.Row(ui.Truncate(tk.ID, 8), tk.Status, day(tk.DueAt),
			ui.Truncate(tk.Title, 48), ui.Truncate(strings.Join(tk.Labels, ","), 24))
	}
	return t.Flush()
}

func day(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
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

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	d, err := store.GetTicketDetail(tk.ID)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(d)
	}

	p := ctx.Printer()
	ctx.Printf("%s\n\n", p.Bold(d.Title))
	ctx.Printf("  %-10s %s\n", p.Dim("id"), d.ID)
	list := d.Status
	if d.ListTitle != "" {
		list = d.ListTitle + " " + p.Dim("("+d.Status+")")
	}
	if d.ArchivedAt != nil {
		list += " " + p.Dim("· archived")
	}
	ctx.Printf("  %-10s %s\n", p.Dim("list"), list)
	if d.Priority != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("priority"), d.Priority)
	}
	if d.StartAt != nil {
		ctx.Printf("  %-10s %s\n", p.Dim("start"), day(d.StartAt))
	}
	if d.DueAt != nil {
		ctx.Printf("  %-10s %s\n", p.Dim("due"), day(d.DueAt))
	}
	if len(d.Labels) > 0 {
		ctx.Printf("  %-10s %s\n", p.Dim("labels"), strings.Join(d.Labels, ", "))
	}
	ctx.Printf("  %-10s %s\n", p.Dim("raised by"), d.Origin)
	if d.Body != "" {
		ctx.Printf("\n%s\n", d.Body)
	}

	group := ""
	for _, it := range d.Checklist {
		if it.Checklist != group {
			group = it.Checklist
			ctx.Printf("\n%s\n", p.Dim(group+":"))
		}
		box := "[ ]"
		if it.Done {
			box = "[x]"
		}
		ctx.Printf("  %s %s  %s\n", box, it.Text, p.Dim(it.ID[:8]))
	}

	if len(d.Notes) > 0 {
		ctx.Printf("\n%s\n", p.Dim("Comments:"))
		for _, c := range d.Notes {
			ctx.Printf("  %s %s\n  %s\n", p.Dim(c.CreatedAt.Format("2006-01-02 15:04")),
				p.Dim(c.Actor), strings.ReplaceAll(c.Body, "\n", "\n  "))
		}
	}

	// The evidence, always. A card you cannot trace is a task in a worse
	// task manager.
	if len(d.Sources) > 0 {
		ctx.Printf("\n%s\n", p.Dim("From this mail:"))
		for _, s := range d.Sources {
			ctx.Printf("  %s  %-28s %s\n", p.Dim(s.Date.Format("2006-01-02")),
				ui.Truncate(s.From, 28), ui.Truncate(s.Subject, 50))
			ctx.Printf("  %s\n", p.Dim("iql read "+s.MessageID))
		}
	}
	return nil
}

// dateFlag reads a date flag: "" leaves it, "none" clears it, anything else
// is a date the due: grammar accepts.
func dateFlag(name, v string, current *time.Time) (*time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return current, nil
	case "none", "clear", "-":
		return nil, nil
	}
	t, err := store.ParseTicketDue(v)
	if err != nil {
		return nil, fmt.Errorf("--%s: %v", name, err)
	}
	return t, nil
}

func ticketNew(ctx *Context, args []string) error {
	title, rest := subcommand(args)
	if title == "" {
		return Fail(ExitUsage, "give the ticket a title")
	}
	fs := flag.NewFlagSet("ticket new", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	list := fs.String("list", "", "the list it goes in (default: the first open list)")
	status := fs.String("status", "", "the same as --list")
	priority := fs.String("priority", "", "priority")
	due := fs.String("due", "", "due date")
	start := fs.String("start", "", "start date")
	labels := fs.String("label", "", "labels, comma separated")
	body := fs.String("body", "", "description in Markdown, or - for stdin")
	bodyFile := fs.String("body-file", "", "read the description from a file")
	board := fs.String("board", "", "give it the labels this board's filter asks for")
	message := fs.String("message", "", "message id this came from")
	thread := fs.String("thread", "", "conversation this belongs to")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk := &store.Ticket{Title: title, Priority: *priority, Origin: store.OriginHumanTicket}
	if ref := firstOf(*list, *status); ref != "" {
		l, err := store.GetTicketList(ref)
		if err != nil {
			return Fail(ExitUsage, "--list: %v", err)
		}
		tk.Status = l.ID
	}
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
	var err error
	if tk.DueAt, err = dateFlag("due", *due, nil); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if tk.StartAt, err = dateFlag("start", *start, nil); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if *body != "" || *bodyFile != "" {
		if tk.Body, err = ctx.ReadBody(*body, *bodyFile); err != nil {
			return err
		}
	}
	all := splitList(*labels)
	if *board != "" {
		b, err := store.GetTicketBoard(*board)
		if err != nil {
			return Fail(ExitUsage, "--board: %v", err)
		}
		all = append(all, b.Labels...)
	}

	if err := store.SaveTicket(tk); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if *message != "" {
		if err := store.AttachSource(tk.ID, *message, ""); err != nil {
			return Fail(ExitError, "attaching the message: %v", err)
		}
	}
	if err := store.AddTicketLabels(tk.ID, all); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		d, err := store.GetTicketDetail(tk.ID)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		return ctx.EmitJSON(d)
	}
	ctx.Printf("Added %s to %s.\n", ctx.Printer().Bold(tk.ID[:8]), tk.Status)
	return nil
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func ticketEdit(ctx *Context, args []string) error {
	id, rest := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	fs := flag.NewFlagSet("ticket edit", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "description in Markdown, or - for stdin")
	bodyFile := fs.String("body-file", "", "read the description from a file")
	priority := fs.String("priority", "", "priority (none clears it)")
	due := fs.String("due", "", "due date (none clears it)")
	start := fs.String("start", "", "start date (none clears it)")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if *title != "" {
		tk.Title = *title
	}
	if *body != "" || *bodyFile != "" {
		if tk.Body, err = ctx.ReadBody(*body, *bodyFile); err != nil {
			return err
		}
	}
	switch strings.ToLower(*priority) {
	case "":
	case "none", "clear", "-":
		tk.Priority = ""
	default:
		tk.Priority = *priority
	}
	if tk.DueAt, err = dateFlag("due", *due, tk.DueAt); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if tk.StartAt, err = dateFlag("start", *start, tk.StartAt); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if err := store.SaveTicket(tk); err != nil {
		return Fail(ExitError, "%v", err)
	}
	return emitCard(ctx, tk.ID, "Saved.")
}

// emitCard prints a card after a change: in full as JSON, or a line.
func emitCard(ctx *Context, id, said string) error {
	if ctx.JSON {
		d, err := store.GetTicketDetail(id)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		return ctx.EmitJSON(d)
	}
	ctx.Printf("%s\n", said)
	return nil
}

func ticketMove(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket move", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	after := fs.String("after", "", "the card it goes after")
	top := fs.Bool("top", false, "put it at the top of the list")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	id, list := fs.Arg(0), fs.Arg(1)
	if id == "" {
		return Fail(ExitUsage, "usage: iql ticket move <id> <list> [--after <card> | --top]")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	dest := tk.Status
	if list != "" {
		l, err := store.GetTicketList(list)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		dest = l.ID
	}
	place := ""
	switch {
	case *top:
		place = "^"
	case *after != "":
		other, err := store.FindTicket(*after)
		if err != nil {
			return Fail(ExitNotFound, "--after: %v", err)
		}
		place = other.ID
	}
	if err := store.MoveTicket(tk.ID, dest, place); err != nil {
		return Fail(ExitError, "%v", err)
	}
	return emitCard(ctx, tk.ID, ui.Truncate(tk.Title, 56)+" — moved to "+dest)
}

func ticketTransition(ctx *Context, args []string, status, verb string) error {
	id, _ := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if status == "" {
		err = store.AcceptTicket(tk.ID)
	} else {
		err = store.SetTicketStatus(tk.ID, status)
	}
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	return emitCard(ctx, tk.ID, ui.Truncate(tk.Title, 56)+" — "+verb)
}

func ticketArchive(ctx *Context, args []string, archived bool) error {
	id, _ := subcommand(args)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if err := store.ArchiveTicket(tk.ID, archived); err != nil {
		return Fail(ExitError, "%v", err)
	}
	said := "Archived. It is off the board; archived:yes finds it."
	if !archived {
		said = "Restored to " + tk.Status + "."
	}
	return emitCard(ctx, tk.ID, said)
}

func ticketLabel(ctx *Context, args []string, add bool) error {
	id, rest := subcommand(args)
	if id == "" || len(rest) == 0 {
		return Fail(ExitUsage, "usage: iql ticket label <id> <label>...")
	}
	var labels []string
	for _, r := range rest {
		labels = append(labels, splitList(r)...)
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if add {
		err = store.AddTicketLabels(tk.ID, labels)
	} else {
		err = store.RemoveTicketLabels(tk.ID, labels)
	}
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	d, err := store.GetTicketDetail(tk.ID)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(d)
	}
	ctx.Printf("Labels: %s\n", strings.Join(d.Labels, ", "))
	return nil
}

func ticketChecklist(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket checklist", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	name := fs.String("checklist", "", "which checklist (default: Checklist)")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	id, verb := fs.Arg(0), fs.Arg(1)
	if id == "" {
		return Fail(ExitUsage, "which ticket?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	item := func() (*store.ChecklistItem, error) {
		if fs.Arg(2) == "" {
			return nil, fmt.Errorf("which item?")
		}
		return store.FindChecklistItem(tk.ID, fs.Arg(2))
	}

	switch verb {
	case "", "list", "show":
	case "add":
		text := strings.Join(fs.Args()[2:], " ")
		_, err = store.AddChecklistItem(tk.ID, *name, text)
	case "done", "check", "tick":
		var it *store.ChecklistItem
		if it, err = item(); err == nil {
			err = store.SetChecklistItemDone(it.ID, true)
		}
	case "undo", "uncheck", "untick":
		var it *store.ChecklistItem
		if it, err = item(); err == nil {
			err = store.SetChecklistItemDone(it.ID, false)
		}
	case "edit":
		var it *store.ChecklistItem
		if it, err = item(); err == nil {
			err = store.EditChecklistItem(it.ID, strings.Join(fs.Args()[3:], " "))
		}
	case "remove", "rm":
		var it *store.ChecklistItem
		if it, err = item(); err == nil {
			err = store.DeleteChecklistItem(it.ID)
		}
	case "rename":
		err = store.RenameChecklist(tk.ID, fs.Arg(2), fs.Arg(3))
	case "delete":
		err = store.DeleteChecklist(tk.ID, fs.Arg(2))
	default:
		return Fail(ExitUsage, "checklist: add, done, undo, edit, remove, rename or delete")
	}
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	items, err := store.ChecklistItems(tk.ID)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(items)
	}
	p := ctx.Printer()
	group := ""
	for _, it := range items {
		if it.Checklist != group {
			group = it.Checklist
			ctx.Printf("%s\n", p.Dim(group+":"))
		}
		box := "[ ]"
		if it.Done {
			box = "[x]"
		}
		ctx.Printf("  %s %s  %s\n", box, it.Text, p.Dim(it.ID[:8]))
	}
	if len(items) == 0 {
		ctx.Printf("No checklist. Add one: iql ticket checklist %s add \"…\"\n", tk.ID[:8])
	}
	return nil
}

func ticketComment(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket comment", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	origin := fs.String("origin", store.OriginHumanTicket, "human or agent")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	id := fs.Arg(0)
	text := strings.Join(fs.Args()[min(1, fs.NArg()):], " ")
	if id == "" || text == "" {
		return Fail(ExitUsage, `usage: iql ticket comment <id> "text" (or - for stdin)`)
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	tk, err := store.FindTicket(id)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if text == "-" {
		if text, err = ctx.ReadBody("-", ""); err != nil {
			return err
		}
	}
	c, err := store.AddTicketComment(tk.ID, text, *origin)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(c)
	}
	ctx.Printf("Commented on %s.\n", ui.Truncate(tk.Title, 56))
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

	target, err := store.FindTicket(into)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	source, err := store.FindTicket(from)
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

	tk, err := store.FindTicket(id)
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

// ticketBoard prints the lists with their cards.
//
// A board is a filter plus the lists. Keeping those separate is what makes
// moving a card mean something: it writes the card's list, which is the
// inverse of the query that defined the column. Conflating them leaves
// dragging undefined — there is no meaning to moving a card from `due:7d` to
// `from:x`.
func ticketBoard(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket board", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	q := fs.String("query", "", "which cards appear on the board")
	board := fs.String("board", "", "a named board (iql ticket boards)")
	limit := fs.Int("limit", 10, "cards shown per list")
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

	if *board != "" {
		b, err := store.GetTicketBoard(*board)
		if err != nil {
			return Fail(ExitNotFound, "%v", err)
		}
		expr = strings.TrimSpace(b.Filter + " " + expr)
	}
	columns, err := store.BoardColumns(expr, *limit)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(columns)
	}

	p := ctx.Printer()
	for _, col := range columns {
		ctx.Printf("\n%s %s\n", p.Bold(col.Title), p.Dim(fmt.Sprintf("%s · %d", col.Status, col.Total)))
		if len(col.Tickets) == 0 {
			ctx.Printf("  %s\n", p.Dim("—"))
			continue
		}
		for _, tk := range col.Tickets {
			extra := ""
			if tk.DueAt != nil {
				extra += " " + p.Dim("due "+tk.DueAt.Format("Jan 2"))
			}
			if tk.ChecklistTotal > 0 {
				extra += " " + p.Dim(fmt.Sprintf("☑ %d/%d", tk.ChecklistDone, tk.ChecklistTotal))
			}
			if len(tk.Labels) > 0 {
				extra += " " + p.Dim("#"+strings.Join(tk.Labels, " #"))
			}
			ctx.Printf("  %s  %s%s\n", p.Dim(tk.ID[:8]), ui.Truncate(tk.Title, 56), extra)
		}
		if col.Total > int64(len(col.Tickets)) {
			ctx.Printf("  %s\n", p.Dim(fmt.Sprintf("… %d more", col.Total-int64(len(col.Tickets)))))
		}
	}
	ctx.Printf("\n%s\n", p.Dim("Move one: iql ticket move <id> <list>"))
	return nil
}
