package cli

import (
	"flag"
	"fmt"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "contact",
		Aliases: []string{"contacts"},
		Summary: "the people and systems in your mailbox",
		Usage: `iql contact <list|show|set|classify> [flags]

A contact is an address you have corresponded with. Every message creates one
for every address it touches, so this is a view of the mailbox rather than an
address book you have to maintain.

  list      contacts matching a query (default: everyone, busiest first)
  show      one contact, with what is known and where it came from
  set       correct a contact by hand; a human ruling outranks every rule
  classify  mark contacts as person or system from headers and addressing
  enrich    fold an extractor's output into contacts and topics
  topics    what a contact is associated with, most distinctive first
  for       who to talk to about a topic

What is counted — messages, sent, received, first and last seen — is derived
from the participant edges on every read, never cached, so it cannot disagree
with the mailbox.

Contact fields are query terms, so the same language filters both:

  iql contact list --query "kind:system"
  iql query "in:contacts messages>10 -kind:system"
  iql query "in:contacts | top domain 10"

classify flags:
  --dry-run   report what would be marked and write nothing

set flags:
  --name --first --last --phone --org --title --kind`,
		Run: runContact,
	})
}

func runContact(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "list", "":
		fs := flag.NewFlagSet("contact list", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		q := fs.String("query", "", "narrow with a contact query")
		limit := fs.Int("limit", 50, "rows to return")
		if err := parseArgs(fs, rest); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}
		expr := "in:contacts"
		if *q != "" {
			expr += " " + *q
		}
		res, err := store.RunQuery(expr, *limit, 0)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(res.Contacts)
		}
		return printQueryResult(ctx, res)

	case "show":
		address, _ := subcommand(rest)
		if address == "" {
			return Fail(ExitUsage, "which contact?")
		}
		c, err := store.GetContact(address)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if c == nil {
			return Fail(ExitError, "no contact for %q", address)
		}
		if ctx.JSON {
			return ctx.EmitJSON(c)
		}

		p := ctx.Printer()
		ctx.Printf("%s\n", p.Bold(c.Name()))
		ctx.Printf("%s\n\n", p.Dim(c.Address))
		row := func(label, value string) {
			if value != "" {
				ctx.Printf("  %-12s %s\n", label, value)
			}
		}
		row("kind", c.Kind+dimSource(p, c.KindSource))
		row("first name", c.FirstName)
		row("last name", c.LastName)
		row("phone", c.Phone)
		row("org", c.Org)
		row("title", c.Title)
		row("header name", c.HeaderName)
		if c.EnrichedBy != "" {
			row("enriched by", c.EnrichedBy)
		}
		ctx.Printf("\n")
		row("messages", fmt.Sprintf("%d", c.Messages))
		row("sent", fmt.Sprintf("%d", c.Sent))
		row("received", fmt.Sprintf("%d", c.Received))
		if !c.FirstSeen.IsZero() {
			row("first seen", c.FirstSeen.Format("2006-01-02"))
			row("last seen", c.LastSeen.Format("2006-01-02"))
		}
		ctx.Printf("\n%s\n", p.Dim("Their mail:  iql query \"anyone:"+c.Address+"\""))
		return nil

	case "set":
		address, flags := subcommand(rest)
		if address == "" {
			return Fail(ExitUsage, "which contact?")
		}
		fs := flag.NewFlagSet("contact set", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		name := fs.String("name", "", "what to call them, overriding every other source")
		first := fs.String("first", "", "first name")
		last := fs.String("last", "", "last name")
		phone := fs.String("phone", "", "phone number")
		org := fs.String("org", "", "organisation")
		title := fs.String("title", "", "job title")
		kind := fs.String("kind", "", "person, organization or system")
		if err := parseArgs(fs, flags); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}
		if *kind != "" && !validContactKind(*kind) {
			return Fail(ExitUsage, "kind must be person, organization, system or unknown")
		}

		c := &store.Contact{
			Address: address, DisplayName: *name, FirstName: *first, LastName: *last,
			Phone: *phone, Org: *org, Title: *title, Kind: *kind,
		}
		// Recorded as a human ruling, which no later rule or model run undoes.
		if err := store.SaveContact(c, store.ContactFromHuman); err != nil {
			return Fail(ExitError, "%v", err)
		}
		saved, _ := store.GetContact(address)
		if ctx.JSON {
			return ctx.EmitJSON(saved)
		}
		ctx.Printf("Updated %s.\n", ctx.Printer().Bold(saved.Name()))
		return nil

	case "classify":
		fs := flag.NewFlagSet("contact classify", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		dryRun := fs.Bool("dry-run", false, "report what would change and write nothing")
		if err := parseArgs(fs, rest); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}
		out, err := store.ClassifyContacts(*dryRun)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(out)
		}
		verb := "Marked"
		if *dryRun {
			verb = "Would mark"
		}
		ctx.Printf("%s %d as system and %d as person, of %d examined.\n",
			verb, out.System, out.Person, out.Examined)
		if out.Skipped > 0 {
			ctx.Printf("%s\n", ctx.Printer().Dim(fmt.Sprintf(
				"%d left unknown: not enough evidence without reading a body.", out.Skipped)))
		}
		return nil

	case "enrich":
		name, flags := subcommand(rest)
		if name == "" {
			return Fail(ExitUsage, "which extractor? see `iql annotate list`")
		}
		fs := flag.NewFlagSet("contact enrich", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		dryRun := fs.Bool("dry-run", false, "report what would change and write nothing")
		if err := parseArgs(fs, flags); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}
		out, err := store.EnrichContacts(name, *dryRun)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(out)
		}
		verb := "Updated"
		if *dryRun {
			verb = "Would update"
		}
		ctx.Printf("%s %d contacts and recorded %d topics, from %d extractions.\n",
			verb, out.Contacts, out.Topics, out.Read)
		return nil

	case "topics":
		address, _ := subcommand(rest)
		if address == "" {
			return Fail(ExitUsage, "which contact?")
		}
		topics, err := store.TopicsFor(address, 20)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(topics)
		}
		if len(topics) == 0 {
			ctx.Printf("No topics recorded for %s.\n\n", address)
			ctx.Printf("%s\n", ctx.Printer().Dim(
				"Topics come from an extractor. See `iql contact enrich <annotator>`."))
			return nil
		}
		p := ctx.Printer()
		t := p.NewTable("TOPIC", "MESSAGES", "SHARE", "LIFT")
		for _, ct := range topics {
			t.Row(ui.Truncate(ct.Topic, 36),
				fmt.Sprintf("%d", ct.Messages),
				fmt.Sprintf("%.0f%%", ct.Share*100),
				fmt.Sprintf("%.1fx", ct.Lift))
		}
		if err := t.Flush(); err != nil {
			return err
		}
		ctx.Printf("\n%s\n", p.Dim(
			"Lift is how much more they discuss it than the mailbox does. Ranked by lift, "+
				"because ranking by volume answers every question with whoever you email most."))
		return nil

	case "for":
		topic, _ := subcommand(rest)
		if topic == "" {
			return Fail(ExitUsage, "which topic?")
		}
		contacts, err := store.ContactsForTopic(topic, 10)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(contacts)
		}
		if len(contacts) == 0 {
			ctx.Printf("Nobody is associated with %q.\n", topic)
			return nil
		}
		p := ctx.Printer()
		t := p.NewTable("NAME", "ADDRESS", "KIND", "MSGS")
		for _, c := range contacts {
			t.Row(ui.Truncate(c.Name(), 28), ui.Truncate(c.Address, 34), c.Kind,
				fmt.Sprintf("%d", c.Messages))
		}
		return t.Flush()

	default:
		return Fail(ExitUsage,
			"unknown subcommand %q (want list, show, set, classify, enrich, topics or for)", sub)
	}
}

func validContactKind(k string) bool {
	switch k {
	case store.KindPerson, store.KindOrganization, store.KindSystem, store.KindUnknown:
		return true
	}
	return false
}

// dimSource annotates a value with where the claim came from.
func dimSource(p *ui.Printer, source string) string {
	if source == "" {
		return ""
	}
	return p.Dim("  (" + source + ")")
}
