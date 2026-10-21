package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "mail",
		Summary: "archive, label, trash, mark read: change messages",
		Usage: `iql mail <move|label|unlabel|read|unread|star|unstar> ... <message-id>...

Changes messages. Each change is made here at once and queued for the
account's server, which the running server applies within seconds; an
account with no server (an imported archive) keeps it here only.

  archive <id>...           take Inbox off
  inbox <id>...             back to the inbox, out of Spam and Trash
  trash <id>...             to Trash (never deleted from the server)
  untrash <id>...           out of Trash, back to the inbox
  spam <id>... | notspam    to Spam, or back out of it
  label <label> <id>...     add a label, creating it if it is new
  unlabel <label> <id>...   remove a label
  read | unread <id>...     mark read or unread
  star | unstar <id>...     star or unstar

Flags:
  --origin human|agent      who is making the change (default human); an
                            agent should say so
  --query "<query>"         every message the query matches, instead of ids:
                            iql mail archive --query "folder:inbox importance:low"
  --dry-run                 with --query: how many it would reach, changing nothing

A change by --query answers with "undo": pass that back to undo exactly it:
  iql --json mail archive --query "…" > done.json
  iql mail undo < done.json

  iql mail archive 6f1c2a…
  iql mail label Receipts 6f1c2a… 0b9e44…
  iql --json labels`,
		Run: runMail,
	})
	register(&Command{
		Name:    "labels",
		Aliases: []string{"label"},
		Summary: "the mail labels, with counts",
		Usage: `iql labels [list|create <name>|delete <name>] [--account <id>]

Mail labels: Inbox, Sent, Spam, Trash and your own, as Gmail has them. A
message carries any number of them. ` + "`label:`" + ` in a query matches these and
annotator labels alike.

  list            every label with total and unread (the default)
  create <name>   a label before anything carries it
  delete <name>   remove a label from every message and forget it; on Gmail
                  the label is removed there too. System labels cannot be
                  deleted.`,
		Run: runLabels,
	})
}

func runMail(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	fs := flag.NewFlagSet("mail "+sub, flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	originFlag := fs.String("origin", "human", "who is making the change: human or agent")
	queryFlag := fs.String("query", "", "every message this query matches, instead of ids")
	dryRun := fs.Bool("dry-run", false, "with --query: count, and change nothing")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	pos := fs.Args()
	source := store.LabelSourceHuman
	if strings.EqualFold(*originFlag, "agent") {
		source = store.LabelSourceAgent
	}

	if sub == "undo" {
		return mailUndo(ctx)
	}
	if *queryFlag != "" {
		return mailByQuery(ctx, sub, pos, *queryFlag, source, *dryRun)
	}

	var change *store.LabelChange
	var flagName string
	var flagOn bool
	switch sub {
	case "archive", "inbox", "trash", "untrash", "spam", "notspam":
		m := store.Moves[sub]
		change = &m
	case "label", "unlabel":
		if len(pos) < 2 {
			return Fail(ExitUsage, "usage: iql mail %s <label> <message-id>...", sub)
		}
		label := pos[0]
		pos = pos[1:]
		if sub == "label" {
			change = &store.LabelChange{Add: []string{label}}
		} else {
			change = &store.LabelChange{Remove: []string{label}}
		}
	case "read", "unread":
		flagName, flagOn = store.FlagSeen, sub == "read"
	case "star", "unstar":
		flagName, flagOn = store.FlagFlagged, sub == "star"
	case "":
		return Fail(ExitUsage, "which change? (archive, inbox, trash, spam, label, unlabel, read, unread, star, unstar)")
	default:
		return Fail(ExitUsage, "%q is not a change (archive, inbox, trash, untrash, spam, notspam, label, unlabel, read, unread, star, unstar)", sub)
	}
	if len(pos) == 0 {
		return Fail(ExitUsage, "which messages? give one or more message ids")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	for _, id := range pos {
		m, err := store.GetMessageByID(id)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if m == nil {
			return Fail(ExitNotFound, "no message with id %q", id)
		}
	}

	var n int
	var err error
	if change != nil {
		change.Source = source
		n, err = store.ChangeLabels(pos, *change)
	} else {
		n, err = store.SetMessageFlag(pos, flagName, flagOn)
	}
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	labels, _ := store.MailLabels(pos)
	if ctx.JSON {
		out := map[string]any{"change": sub, "changed": n, "labels": labels}
		return ctx.EmitJSON(out)
	}
	ctx.Printf("%s: %d of %d changed\n", sub, n, len(pos))
	for _, id := range pos {
		ctx.Printf("  %s  %s\n", id, strings.Join(labels[id], ", "))
	}
	return nil
}

func runLabels(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if strings.HasPrefix(sub, "-") {
		sub, rest = "list", args
	}
	fs := flag.NewFlagSet("labels", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	acct := fs.String("account", "", "count one account only")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "", "list":
		counts, err := store.LabelCounts(*acct)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			if counts == nil {
				counts = []store.LabelCount{}
			}
			return ctx.EmitJSON(counts)
		}
		p := ctx.Printer()
		for _, c := range counts {
			name := c.Name
			if c.System {
				name = p.Dim(name)
			}
			ctx.Printf("  %-32s %8d  %s\n", name, c.Total, p.Dim(fmt.Sprintf("%d unread", c.Unread)))
		}
		return nil
	case "create":
		if len(fs.Args()) != 1 {
			return Fail(ExitUsage, "usage: iql labels create <name>")
		}
		if err := store.CreateLabel(fs.Args()[0]); err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"created": fs.Args()[0]})
		}
		ctx.Printf("created %s\n", fs.Args()[0])
		return nil
	case "delete":
		if len(fs.Args()) != 1 {
			return Fail(ExitUsage, "usage: iql labels delete <name>")
		}
		name := fs.Args()[0]
		if !store.LabelExists(name) {
			return Fail(ExitNotFound, "no label named %q", name)
		}
		n, err := store.DeleteLabel(name)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"deleted": name, "messages": n})
		}
		ctx.Printf("deleted %s from %d messages\n", name, n)
		return nil
	}
	return Fail(ExitUsage, "%q is not a labels command (list, create, delete)", sub)
}

// mailByQuery makes a change to every message a query matches.
func mailByQuery(ctx *Context, sub string, pos []string, q, source string, dry bool) error {
	label := ""
	if sub == "label" || sub == "unlabel" {
		if len(pos) < 1 {
			return Fail(ExitUsage, "usage: iql mail %s <label> --query \"…\"", sub)
		}
		label = pos[0]
	} else if len(pos) > 0 {
		return Fail(ExitUsage, "give ids or --query, not both")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if dry {
		n, err := store.QueryMessageCount(q)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"change": sub, "matched": n, "dryRun": true})
		}
		ctx.Printf("%s would reach %d message(s). Nothing was changed.\n", sub, n)
		return nil
	}
	out, err := store.BulkApplyAs(q, sub, label, source)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"change": sub, "matched": out.Matched, "changed": out.Changed,
			"capped": out.Capped, "undo": out.Undo})
	}
	ctx.Printf("%s: %d of %d matching changed", sub, out.Changed, out.Matched)
	if out.Capped {
		ctx.Printf(" (the newest %d; run it again for the rest)", store.BulkMax)
	}
	ctx.Printf(".\nTo undo, run it with --json and pass the result to: iql mail undo\n")
	return nil
}

// mailUndo reverses a change made by --query, from its --json answer on stdin.
func mailUndo(ctx *Context) error {
	data, err := io.ReadAll(ctx.Stdin)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	var wrapped struct {
		Undo *store.BulkUndo `json:"undo"`
	}
	var undo store.BulkUndo
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Undo != nil {
		undo = *wrapped.Undo
	} else if err := json.Unmarshal(data, &undo); err != nil {
		return Fail(ExitUsage, "give the --json answer of a mail change on stdin: %v", err)
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()
	n, err := store.BulkUndoApply(undo)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"undone": n})
	}
	ctx.Printf("Undone: %d message(s) back as they were.\n", n)
	return nil
}
