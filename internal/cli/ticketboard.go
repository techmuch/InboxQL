package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
)

// The board's own furniture: lists, boards, labels and rules. Each answers
// with what it now is, so a harness can act and read the result in one call.

const listsUsage = `iql ticket lists                                 the lists, in order
iql ticket lists add <title> [--closed] [--after <list>|--first]
iql ticket lists rename <list> <title>
iql ticket lists move <list> --after <list> | --first | --last
iql ticket lists open|close <list>             open work, or finished
iql ticket lists archive|restore <list>
iql ticket lists delete <list> [--move-to <list>]`

func ticketLists(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket lists", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	closed := fs.Bool("closed", false, "a list of finished cards")
	after := fs.String("after", "", "the list it goes after")
	first := fs.Bool("first", false, "put it first")
	last := fs.Bool("last", false, "put it last")
	moveTo := fs.String("move-to", "", "where a deleted list's cards go")
	all := fs.Bool("all", false, "include archived lists")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	verb, ref := fs.Arg(0), fs.Arg(1)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	resolve := func() (string, error) {
		if ref == "" {
			return "", fmt.Errorf("which list?")
		}
		l, err := store.GetTicketList(ref)
		if err != nil {
			return "", err
		}
		return l.ID, nil
	}
	place := func() (string, error) {
		switch {
		case *first:
			return "^", nil
		case *last || *after == "":
			return "", nil
		}
		l, err := store.GetTicketList(*after)
		if err != nil {
			return "", err
		}
		return l.ID, nil
	}

	var err error
	switch verb {
	case "", "list", "ls":
	case "add", "new", "create":
		title := strings.Join(fs.Args()[1:], " ")
		kind := store.ListOpen
		if *closed {
			kind = store.ListClosed
		}
		var p string
		if p, err = place(); err == nil {
			_, err = store.CreateTicketList(title, kind, p)
		}
	case "rename":
		var id string
		if id, err = resolve(); err == nil {
			err = store.RenameTicketList(id, strings.Join(fs.Args()[2:], " "))
		}
	case "move":
		var id, p string
		if id, err = resolve(); err == nil {
			if !*first && !*last && *after == "" {
				return Fail(ExitUsage, "move it where? --after <list>, --first or --last")
			}
			if p, err = place(); err == nil {
				err = store.MoveTicketList(id, p)
			}
		}
	case "open", "close":
		var id string
		if id, err = resolve(); err == nil {
			kind := store.ListOpen
			if verb == "close" {
				kind = store.ListClosed
			}
			err = store.SetTicketListKind(id, kind)
		}
	case "archive", "restore":
		var id string
		if id, err = resolve(); err == nil {
			err = store.ArchiveTicketList(id, verb == "archive")
		}
	case "delete", "rm", "remove":
		var id string
		if id, err = resolve(); err == nil {
			dest := ""
			if *moveTo != "" {
				var l *store.TicketList
				if l, err = store.GetTicketList(*moveTo); err == nil {
					dest = l.ID
				}
			}
			if err == nil {
				err = store.DeleteTicketList(id, dest)
			}
		}
	default:
		return Fail(ExitUsage, "%s", listsUsage)
	}
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	lists, err := store.TicketLists(*all || verb == "archive" || verb == "restore")
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(lists)
	}
	p := ctx.Printer()
	t := p.NewTable("LIST", "TITLE", "KIND", "CARDS", "")
	for _, l := range lists {
		note := ""
		if l.ArchivedAt != nil {
			note = "archived"
		}
		t.Row(l.ID, l.Title, l.Kind, fmt.Sprint(l.Cards), note)
	}
	return t.Flush()
}

const boardsUsage = `iql ticket boards                              the boards
iql ticket boards add <title> --filter "<query>"
iql ticket boards edit <board> [--title <t>] [--filter "<query>"]
iql ticket boards move <board> --after <board> | --first | --last
iql ticket boards delete <board>

A board is a named filter over the lists: "Home" might be label:home. A card
added on it (iql ticket new … --board home) is given the labels it asks for.`

func ticketBoards(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket boards", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	filter := fs.String("filter", "", "which cards it shows, in the query language")
	title := fs.String("title", "", "its title")
	after := fs.String("after", "", "the board it goes after")
	first := fs.Bool("first", false, "put it first")
	last := fs.Bool("last", false, "put it last")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	verb, ref := fs.Arg(0), fs.Arg(1)
	filterSet := false
	fs.Visit(func(f *flag.Flag) { filterSet = filterSet || f.Name == "filter" })

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var err error
	switch verb {
	case "", "list", "ls":
	case "add", "new", "create":
		_, err = store.CreateTicketBoard(strings.Join(fs.Args()[1:], " "), *filter)
	case "edit":
		var b *store.TicketBoard
		if b, err = store.GetTicketBoard(ref); err == nil {
			t, f := b.Title, b.Filter
			if *title != "" {
				t = *title
			}
			if filterSet {
				f = *filter
			}
			err = store.UpdateTicketBoard(b.ID, t, f)
		}
	case "move":
		var b *store.TicketBoard
		if b, err = store.GetTicketBoard(ref); err == nil {
			place := ""
			switch {
			case *first:
				place = "^"
			case *after != "":
				var o *store.TicketBoard
				if o, err = store.GetTicketBoard(*after); err == nil {
					place = o.ID
				}
			case !*last:
				return Fail(ExitUsage, "move it where? --after <board>, --first or --last")
			}
			if err == nil {
				err = store.MoveTicketBoard(b.ID, place)
			}
		}
	case "delete", "rm", "remove":
		var b *store.TicketBoard
		if b, err = store.GetTicketBoard(ref); err == nil {
			err = store.DeleteTicketBoard(b.ID)
		}
	default:
		return Fail(ExitUsage, "%s", boardsUsage)
	}
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	boards, err := store.TicketBoards()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(boards)
	}
	p := ctx.Printer()
	t := p.NewTable("BOARD", "TITLE", "FILTER")
	for _, b := range boards {
		f := b.Filter
		if f == "" {
			f = p.Dim("(every card)")
		}
		t.Row(b.ID, b.Title, f)
	}
	return t.Flush()
}

func ticketLabels(ctx *Context, args []string) error {
	verb, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch verb {
	case "", "list", "ls":
	case "color", "colour":
		if len(rest) < 1 {
			return Fail(ExitUsage, "usage: iql ticket labels color <label> [%s]",
				strings.Join(store.LabelColors, "|"))
		}
		color := ""
		if len(rest) > 1 {
			color = rest[1]
		}
		if err := store.SetLabelColor(rest[0], color); err != nil {
			return Fail(ExitUsage, "%v", err)
		}
	default:
		return Fail(ExitUsage, "usage: iql ticket labels [color <label> <colour>]")
	}

	labels, err := store.BoardLabels()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(labels)
	}
	p := ctx.Printer()
	t := p.NewTable("LABEL", "COLOUR", "CARDS", "MESSAGES")
	for _, l := range labels {
		t.Row(l.Name, l.Color, fmt.Sprint(l.Cards), fmt.Sprint(l.Messages))
	}
	return t.Flush()
}

// actionsFlag collects --do flags: an action, or action=value.
type actionsFlag []store.RuleAction

func (a *actionsFlag) String() string { return "" }
func (a *actionsFlag) Set(v string) error {
	do, value, _ := strings.Cut(v, "=")
	*a = append(*a, store.RuleAction{Do: do, Value: value})
	return nil
}

const rulesUsage = `iql ticket rules                                  the rules
iql ticket rules add --when enters:<list> --do <action>[=value]...
iql ticket rules add --when mail --query "<mail query>" [--list <list>] [--do …]
iql ticket rules on|off|delete <rule>
iql ticket rules run [<rule>] [--backfill]

Actions: check-all, add-label=<l>, remove-label=<l>, set-due=<3d|date>,
clear-due, set-priority=<p>, archive.

A mail rule makes one card per conversation in its list (default: Proposed),
from mail that arrives after the rule was made; --backfill on run covers
older mail too. Mail rules also run after every sync while iql is serving.

  iql ticket rules add --when enters:done --do check-all --do archive
  iql ticket rules add --when mail --query "from:landlord@x.com" --list todo --do add-label=home`

func ticketRules(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ticket rules", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	when := fs.String("when", "", "enters:<list>, or mail")
	q := fs.String("query", "", "for a mail rule: which mail")
	list := fs.String("list", "", "for a mail rule: where its cards go")
	title := fs.String("title", "", "a name for the rule")
	backfill := fs.Bool("backfill", false, "run: include mail from before the rule was made")
	var actions actionsFlag
	fs.Var(&actions, "do", "an action, or action=value (repeatable)")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	verb, ref := fs.Arg(0), fs.Arg(1)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var err error
	switch verb {
	case "", "list", "ls":
	case "add", "new", "create":
		r := &store.TicketRule{Title: *title, Query: *q, List: *list, Actions: actions}
		switch {
		case strings.HasPrefix(*when, "enters:"):
			r.Trigger, r.List = store.RuleEnterList, strings.TrimPrefix(*when, "enters:")
		case *when == "mail":
			r.Trigger = store.RuleMail
		default:
			return Fail(ExitUsage, "--when enters:<list> or --when mail\n\n%s", rulesUsage)
		}
		if err = store.CreateTicketRule(r); err == nil && ctx.JSON {
			return ctx.EmitJSON(r)
		}
	case "on", "off", "enable", "disable":
		var r *store.TicketRule
		if r, err = store.GetTicketRule(ref); err == nil {
			r.Enabled = verb == "on" || verb == "enable"
			err = store.UpdateTicketRule(r)
		}
	case "delete", "rm", "remove":
		var r *store.TicketRule
		if r, err = store.GetTicketRule(ref); err == nil {
			err = store.DeleteTicketRule(r.ID)
		}
	case "run":
		id := ""
		if ref != "" {
			var r *store.TicketRule
			if r, err = store.GetTicketRule(ref); err != nil {
				return Fail(ExitNotFound, "%v", err)
			}
			id = r.ID
		}
		out, err := store.RunMailRules(id, *backfill)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(out)
		}
		ctx.Printf("%d rule(s): %d message(s) matched, %d card(s) made, %d added to cards already made.\n",
			out.Rules, out.Matched, out.Created, out.Attached)
		for _, e := range out.Errors {
			ctx.Printf("  %s %s\n", ctx.Printer().Yellow("failed:"), e)
		}
		return nil
	default:
		return Fail(ExitUsage, "%s", rulesUsage)
	}
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	rules, err := store.TicketRules()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(rules)
	}
	if len(rules) == 0 {
		ctx.Printf("No rules.\n\n%s\n", rulesUsage)
		return nil
	}
	p := ctx.Printer()
	t := p.NewTable("ID", "ON", "RULE")
	for _, r := range rules {
		on := "yes"
		if !r.Enabled {
			on = "no"
		}
		t.Row(r.ID[:8], on, ui.Truncate(r.Title, 80))
	}
	return t.Flush()
}
