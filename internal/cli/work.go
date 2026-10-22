package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/work"
)

func init() {
	register(&Command{
		Name:    "elements",
		Aliases: []string{"element"},
		Summary: "what is worth knowing about mail, for an AI harness to read and answer",
		Usage: `iql elements [list|show <name>|install [names...] [--replace]|tune <name> [--add-hint <text>|--instructions <text>|--reset]]

An element is something worth knowing later: receipts, travel, calendar events,
tasks, waiting, importance, sender and topics. Your AI harness answers them
with iql work; see iql work guide.

  list      the elements, installed or not, and how much each has answered
  show      one element in full: instructions, levels or fields
  install   add the catalog, or the named elements. An annotator with the same
            name is replaced only if it never answered anything, unless
            --replace is given.
  tune      customize an element's instructions without editing code:
            --add-hint <text>      append a custom rule/hint (preserves version)
            --instructions <text>  replace instructions (preserves version unless --bump-version)
            --reset                restore catalog instructions`,
		Run: runElements,
	})
	register(&Command{
		Name:    "work",
		Summary: "hand conversations to an AI harness and take its answers",
		Usage: `iql work <next|submit|status|metrics|audit|flag|unflag|targets|clear|guide> [flags]

  next      the newest conversations with something to answer, leased to you
            --limit n          how many (default 10)
            --only a,b         just these elements
            --query <q>        just conversations with mail this matches
            --model <id>       who will answer (recorded with the batch)
            --template <file>  write a pre-scaffolded answers JSON file
            --out <file>       write the full batch JSON to a file
            --digest           print a compact readable digest of all leased items
            --deep             read properly what was only guessed: flagged
                               conversations first, then untouched guesses;
                               the template guesses nothing
  submit    answers, from a file or standard input: one JSON object, an
            array of them, or one per line
            --base <file>      merge partial/sparse answers on top of a --template file
            --accept-tickets   put created task tickets straight on the board (status: todo)
            --input-tokens n --output-tokens n --cost-usd x --seconds x
                               what reading the batch cost, if you know
  status    what has been answered, by element and model
  metrics   how efficient and how accurate the loop is: --since 30d, --model <id>
  audit     check answers already given, reading them again blind:
              next   [--limit n] [--model <id>] [--template f] [--digest] [--only a,b]
              submit <file> [--base f]   the same answer shape as work submit
              list   [--all]             disagreements waiting for a person
              resolve <id> original|checker|neither
  flag      mark a conversation for a deep read: flag <thread> [--reason text]
  unflag    clear that mark
  targets   how right each element must be, and what a wrong answer costs:
            targets [set <element> --target 0.95 --cost high|medium|low]
  clear     undo answers given through work: --model <id>, --since 1d|2h|date,
            --only a,b
  guide     everything a harness needs to run the loop

  iql work guide`,
		Run: runWork,
	})
}

func runElements(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	fs := flag.NewFlagSet("elements", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	replace := fs.Bool("replace", false, "replace same-named annotators even if they have answers")
	addHint := fs.String("add-hint", "", "for tune: append a custom hint/rule to the element's instructions")
	instructions := fs.String("instructions", "", "for tune: set the element's instructions")
	reset := fs.Bool("reset", false, "for tune: restore the default catalog instructions")
	bumpVersion := fs.Bool("bump-version", false, "for tune: increment the element's version so existing answers are re-asked")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "", "list", "ls":
		return elementsList(ctx)
	case "show":
		if fs.NArg() != 1 {
			return Fail(ExitUsage, "usage: iql elements show <name>")
		}
		return elementsShow(ctx, fs.Arg(0))
	case "tune", "customize":
		if fs.NArg() != 1 {
			return Fail(ExitUsage, "usage: iql elements tune <name> [--add-hint <text>|--instructions <text>|--reset]")
		}
		return elementsTune(ctx, fs.Arg(0), *addHint, *instructions, *reset, *bumpVersion)
	case "install":
		made, replaced, skipped, err := elements.Install(fs.Args(), *replace)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"created": made, "replaced": replaced, "skipped": skipped})
		}
		if len(made) > 0 {
			ctx.Printf("Created: %s\n", strings.Join(made, ", "))
		}
		if len(replaced) > 0 {
			ctx.Printf("Replaced (they had never answered anything): %s\n", strings.Join(replaced, ", "))
		}
		if len(skipped) > 0 {
			ctx.Printf("%s\n", ctx.Printer().Dim("Left alone: "+strings.Join(skipped, ", ")+
				" — already elements, or annotators with answers (--replace to replace them)"))
		}
		ctx.Printf("\nNext: point your AI harness at `iql work guide`.\n")
		return nil
	}
	return Fail(ExitUsage, "unknown elements command %q", sub)
}

type elementRow struct {
	elements.Element
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
	Answered  int  `json:"answered"`
}

func elementsList(ctx *Context) error {
	counts, err := store.AgentAnswerCounts()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	answered := map[string]int{}
	for _, c := range counts {
		answered[c.Annotator] += c.Messages
	}
	var rows []elementRow
	for _, e := range elements.Catalog {
		r := elementRow{Element: e}
		if a, err := store.GetAnnotator(e.Name); err == nil && a != nil {
			if got, ok := elements.FromAnnotator(a); ok {
				r.Element, r.Installed, r.Enabled = got, true, a.Enabled
			}
		}
		r.Answered = answered[e.Name]
		rows = append(rows, r)
	}
	if ctx.JSON {
		return ctx.EmitJSON(rows)
	}
	p := ctx.Printer()
	t := p.NewTable("ELEMENT", "ABOUT", "UNIT", "STATE", "ANSWERED")
	for _, r := range rows {
		state := "not installed"
		switch {
		case r.Installed && r.Enabled:
			state = "on"
		case r.Installed:
			state = "off"
		}
		t.Row(r.Name, r.About, r.Unit, state, strconv.Itoa(r.Answered))
	}
	return t.Flush()
}

func elementsShow(ctx *Context, name string) error {
	e, ok := elements.ByName(name)
	if a, err := store.GetAnnotator(name); err == nil && a != nil {
		if got, isEl := elements.FromAnnotator(a); isEl {
			e, ok = got, true
		}
	}
	if !ok {
		return Fail(ExitNotFound, "no element named %q (iql elements list)", name)
	}
	if ctx.JSON {
		return ctx.EmitJSON(e)
	}
	ctx.Printf("%s — %s\n\nUnit: %s   Shape: %s", e.Name, e.About, e.Unit, e.Shape)
	if e.Target != "" {
		ctx.Printf("   Also goes to: %s", e.Target)
	}
	if e.Version > 0 {
		ctx.Printf("   Version: %d", e.Version)
	}
	ctx.Printf("\n\n%s\n", e.Instructions)
	if len(e.Levels) > 0 {
		ctx.Printf("\nLevels:\n")
		for _, l := range e.Levels {
			ctx.Printf("  %-14s %s\n", l.Label, l.Describe)
		}
	}
	if len(e.Fields) > 0 {
		ctx.Printf("\nFields:\n")
		printFields(ctx, e.Fields, "  ")
	}
	return nil
}

func elementsTune(ctx *Context, name, addHint, instructions string, reset, bumpVersion bool) error {
	cat, inCatalog := elements.ByName(name)
	a, err := store.GetAnnotator(name)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if a == nil {
		return Fail(ExitNotFound, "element %q is not installed (run `iql elements install` first)", name)
	}
	if _, isEl := elements.FromAnnotator(a); !isEl {
		return Fail(ExitUsage, "%q is not an agent element", name)
	}
	given := 0
	for _, set := range []bool{strings.TrimSpace(addHint) != "", strings.TrimSpace(instructions) != "", reset} {
		if set {
			given++
		}
	}
	if given != 1 {
		return Fail(ExitUsage, "specify exactly one of --add-hint <text>, --instructions <text>, or --reset")
	}

	var nextInst string
	switch {
	case reset:
		if !inCatalog {
			return Fail(ExitNotFound, "no built-in catalog instructions for %q", name)
		}
		nextInst = cat.Instructions
	case strings.TrimSpace(instructions) != "":
		nextInst = strings.TrimSpace(instructions)
	case strings.TrimSpace(addHint) != "":
		hint := strings.TrimSpace(addHint)
		nextInst = strings.TrimSpace(a.Instructions) + "\n\nCustom rule: " + hint
	}

	updated, err := store.TuneElement(name, nextInst, bumpVersion)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		el, _ := elements.FromAnnotator(updated)
		return ctx.EmitJSON(el)
	}
	ctx.Printf("Updated %s (version %d):\n\n%s\n", updated.Name, updated.Version, updated.Instructions)
	return nil
}

func printFields(ctx *Context, fields []elements.Field, indent string) {
	for _, f := range fields {
		req := ""
		if f.Required {
			req = " (required)"
		}
		enum := ""
		if len(f.Enum) > 0 {
			enum = " — one of " + strings.Join(f.Enum, ", ")
		}
		ctx.Printf("%s%-12s %-9s %s%s%s\n", indent, f.Name, f.Type, f.Describe, enum, req)
		if len(f.Of) > 0 {
			printFields(ctx, f.Of, indent+"    ")
		}
	}
}

func runWork(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if sub == "guide" || sub == "" {
		ctx.Printf("%s", work.Guide)
		return nil
	}
	auditSub := ""
	if sub == "audit" {
		auditSub, rest = subcommand(rest)
	}
	fs := flag.NewFlagSet("work", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	limit := fs.Int("limit", 10, "conversations to take")
	only := fs.String("only", "", "comma-separated element names")
	query := fs.String("query", "", "only conversations with mail this matches")
	model := fs.String("model", "", "model id (for template or clear)")
	since := fs.String("since", "", "for clear: only answers since this (1d, 2h, or a date)")
	templatePath := fs.String("template", "", "for next: write pre-scaffolded answers JSON to this file")
	basePath := fs.String("base", "", "for submit: merge partial/sparse answers on top of this template file")
	digestOut := fs.String("digest-out", "", "for next: write the readable digest to this file (defaults to <template>.digest.txt)")
	outPath := fs.String("out", "", "for next: write the full batch JSON to this file")
	digest := fs.Bool("digest", false, "for next: print a compact readable digest of all leased items")
	acceptTickets := fs.Bool("accept-tickets", false, "for submit/template: put created task tickets directly in todo status")
	deep := fs.Bool("deep", false, "for next: read properly what was only guessed")
	var usage store.WorkUsage
	fs.IntVar(&usage.InputTokens, "input-tokens", 0, "for submit: input tokens reading the batch took")
	fs.IntVar(&usage.OutputTokens, "output-tokens", 0, "for submit: output tokens reading the batch took")
	fs.Float64Var(&usage.CostUSD, "cost-usd", 0, "for submit: what reading the batch cost, in dollars")
	fs.Float64Var(&usage.Seconds, "seconds", 0, "for submit: how long reading the batch took")
	randomShare := fs.Float64("random-share", 0.5, "for audit next: share of the batch drawn at random")
	all := fs.Bool("all", false, "for audit list: settled disagreements too")
	reason := fs.String("reason", "", "for flag: why")
	target := fs.Float64("target", 0, "for targets set: the share of answers that must be right, 0.5 to 1")
	cost := fs.String("cost", "", "for targets set: what a wrong answer costs: high, medium or low")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	names := splitList(*only)
	switch sub {
	case "next":
		b, err := work.Next(work.Options{Limit: *limit, Only: names, Query: *query, Deep: *deep, Model: *model})
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		return emitBatch(ctx, b, *model, *templatePath, *digestOut, *outPath, *digest, *acceptTickets)

	case "submit":
		return workSubmit(ctx, fs.Args(), *basePath, *acceptTickets, usage, false)

	case "metrics":
		from := time.Now().AddDate(0, 0, -30)
		var err error
		if *since != "" {
			if *since == "all" {
				from = time.Time{}
			} else if from, err = parseSince(*since); err != nil {
				return Fail(ExitUsage, "%v", err)
			}
		}
		m, err := work.Measure(from, *model)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(m)
		}
		printMetrics(ctx, m)
		return nil

	case "audit":
		return workAudit(ctx, auditSub, fs.Args(), auditFlags{limit: *limit, model: *model, only: names,
			template: *templatePath, digestOut: *digestOut, out: *outPath, digest: *digest, base: *basePath,
			randomShare: *randomShare, all: *all, usage: usage})

	case "flag", "unflag":
		if len(fs.Args()) != 1 {
			return Fail(ExitUsage, "usage: iql work %s <thread>", sub)
		}
		thread := fs.Args()[0]
		if sub == "flag" {
			r := strings.TrimSpace(*reason)
			if r == "" {
				r = "flagged by hand"
			}
			err := store.FlagThread(thread, r, store.SourceHuman)
			if err != nil {
				return Fail(ExitError, "%v", err)
			}
		} else if err := store.UnflagThread(thread); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"thread": thread, "flagged": sub == "flag"})
		}
		if sub == "flag" {
			ctx.Printf("Flagged %s: `iql work next --deep` reads it first.\n", thread)
		} else {
			ctx.Printf("Unflagged %s.\n", thread)
		}
		return nil

	case "targets":
		if args := fs.Args(); len(args) > 0 {
			if args[0] != "set" || len(args) != 2 {
				return Fail(ExitUsage, "usage: iql work targets set <element> --target 0.95 --cost high")
			}
			if err := work.SetTarget(args[1], *target, *cost); err != nil {
				return Fail(ExitUsage, "%v", err)
			}
		}
		r, err := work.LoadRouting()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(r)
		}
		printRoutes(ctx, r)
		return nil
	}
	switch sub {
	case "status":
		counts, err := store.AgentAnswerCounts()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		leases, err := store.ActiveLeases()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"answers": counts, "leased": leases})
		}
		t := ctx.Printer().NewTable("ELEMENT", "MODEL", "MESSAGES", "ANSWERS", "LAST")
		for _, c := range counts {
			t.Row(c.Annotator, c.Model, strconv.Itoa(c.Messages), strconv.Itoa(c.Answers), c.Last.Format("2006-01-02 15:04"))
		}
		if err := t.Flush(); err != nil {
			return err
		}
		ctx.Printf("\n%d conversations leased now.\n", leases)
		return nil

	case "clear":
		var from time.Time
		if *since != "" {
			t, err := parseSince(*since)
			if err != nil {
				return Fail(ExitUsage, "%v", err)
			}
			from = t
		}
		var ids []string
		for _, n := range names {
			a, err := store.GetAnnotator(n)
			if err != nil || a == nil {
				return Fail(ExitNotFound, "no element named %q", n)
			}
			ids = append(ids, a.ID)
		}
		n, err := store.ClearAgentAnswers(*model, from, ids)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"cleared": n})
		}
		ctx.Printf("Cleared %d answers; those conversations are pending again.\n"+
			"Proposed tickets and calendar events stay proposed — reject any you do not want.\n", n)
		return nil
	}
	return Fail(ExitUsage, "unknown work command %q (next, submit, status, clear, guide)", sub)
}

// emitBatch writes a batch where it was asked for — the batch JSON, the
// template, the digest — and says what was leased.
func emitBatch(ctx *Context, b *work.Batch, model, templatePath, digestOut, outPath string, digest, acceptTickets bool) error {
	if templatePath != "" {
		tmpl := work.Template(b, model)
		if acceptTickets {
			for i := range tmpl {
				tmpl[i].AcceptTickets = true
			}
		}
		raw, err := json.MarshalIndent(tmpl, "", "  ")
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if err := os.WriteFile(templatePath, append(raw, '\n'), 0o644); err != nil {
			return Fail(ExitError, "%v", err)
		}
	}
	if outPath != "" {
		raw, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if err := os.WriteFile(outPath, append(raw, '\n'), 0o644); err != nil {
			return Fail(ExitError, "%v", err)
		}
	}
	dPath := digestOut
	if dPath == "" && templatePath != "" {
		dPath = templatePath + ".digest.txt"
	}
	var dText string
	if dPath != "" || digest || (templatePath != "" && !ctx.JSON) {
		dText = work.Digest(b)
	}
	if dPath != "" {
		if err := os.WriteFile(dPath, []byte(dText), 0o644); err != nil {
			return Fail(ExitError, "%v", err)
		}
	}
	if digest || (templatePath != "" && !ctx.JSON) {
		ctx.Printf("%s", dText)
		if dPath != "" && templatePath != "" {
			ctx.Printf("Digest saved to %s (read with view_file if stdout is truncated); template saved to %s.\n", dPath, templatePath)
		}
		return nil
	}
	if ctx.JSON {
		return ctx.EmitJSON(b)
	}
	if len(b.Items) == 0 {
		ctx.Printf("%s", work.NothingToRead(b))
		return nil
	}
	ctx.Printf("%d conversations leased for 30 minutes. Use --digest or --json to read them:\n", len(b.Items))
	for _, it := range b.Items {
		var asked []string
		for _, a := range it.Ask {
			asked = append(asked, a.Element)
		}
		ctx.Printf("  %s  %s  [%s]\n", it.Thread, it.Subject, strings.Join(asked, ", "))
	}
	return nil
}

// readSubmissions reads one submission, an array of them, or one per line,
// merged over a template when one is given.
func readSubmissions(ctx *Context, args []string, basePath string) ([]work.Submission, error) {
	var in io.Reader = ctx.Stdin
	if len(args) == 1 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return nil, Fail(ExitNotFound, "%v", err)
		}
		defer f.Close()
		in = f
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, Fail(ExitError, "%v", err)
	}
	subs, err := decodeSubmissions(raw)
	if err != nil {
		return nil, Fail(ExitUsage, "%v", err)
	}
	if basePath != "" {
		baseRaw, err := os.ReadFile(basePath)
		if err != nil {
			return nil, Fail(ExitNotFound, "%v", err)
		}
		baseSubs, err := decodeSubmissions(baseRaw)
		if err != nil {
			return nil, Fail(ExitUsage, "--base %s: %v", basePath, err)
		}
		subs = mergeBaseSubmissions(baseSubs, subs)
	}
	if len(subs) == 0 {
		return nil, Fail(ExitUsage, "no answers given")
	}
	return subs, nil
}

// workSubmit reads one submission, an array of them, or one per line.
func workSubmit(ctx *Context, args []string, basePath string, acceptTickets bool, usage store.WorkUsage, audit bool) error {
	subs, err := readSubmissions(ctx, args, basePath)
	if err != nil {
		return err
	}
	// Usage given on the command line is for the batch: it goes on the first
	// submission, which names the batch through its lease.
	if !usage.Empty() && subs[0].Usage == nil {
		subs[0].Usage = &usage
	}
	if audit {
		return auditSubmit(ctx, subs)
	}

	cal := calsync.New()
	var results []*work.Result
	failed := 0
	for _, s := range subs {
		if acceptTickets {
			s.AcceptTickets = true
		}
		r, err := work.Submit(s, cal)
		if err != nil {
			return Fail(ExitError, "%s: %v", s.Thread, err)
		}
		if !r.OK() {
			failed++
		}
		results = append(results, r)
	}
	if ctx.JSON {
		var labelsAdded, archived, contacts, tickets, events, alreadyStored int
		var ticketIDs, eventChangeIDs []string
		for _, r := range results {
			if r.AlreadyStored {
				alreadyStored++
			}
			labelsAdded += r.Labels
			archived += r.Archived
			contacts += r.Contacts
			tickets += r.Tickets
			ticketIDs = append(ticketIDs, r.TicketIDs...)
			events += r.Events
			eventChangeIDs = append(eventChangeIDs, r.EventChangeIDs...)
		}
		out := map[string]any{
			"stored":             len(results) - failed - alreadyStored,
			"alreadyStored":      alreadyStored,
			"refused":            failed,
			"labelsAdded":        labelsAdded,
			"archived":           archived,
			"contactsClassified": contacts,
			"ticketsProposed":    tickets,
			"ticketIds":          ticketIDs,
			"eventsProposed":     events,
			"eventChangeIds":     eventChangeIDs,
			"results":            results,
		}
		if err := ctx.EmitJSON(out); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			if r.OK() {
				ctx.Printf("✓ %s  %s\n", r.Thread, submitSummary(r))
				continue
			}
			ctx.Printf("✗ %s  nothing stored:\n", r.Thread)
			for _, p := range r.Problems {
				where := strings.TrimSpace(p.Element + " " + p.Target)
				if where != "" {
					where += ": "
				}
				ctx.Printf("    %s%s\n", where, p.Problem)
			}
		}
	}
	if failed > 0 {
		return &Error{Code: ExitUsage, Err: fmt.Errorf("%d of %d conversations refused; fix and resend them", failed, len(results))}
	}
	return nil
}

func submitSummary(r *work.Result) string {
	if r.AlreadyStored {
		return "already stored"
	}
	var parts []string
	for el, n := range r.Stored {
		parts = append(parts, fmt.Sprintf("%s %d", el, n))
	}
	sort.Strings(parts)
	if r.Tickets > 0 {
		parts = append(parts, fmt.Sprintf("%d tickets proposed", r.Tickets))
	}
	if r.Events > 0 {
		parts = append(parts, fmt.Sprintf("%d events proposed", r.Events))
	}
	if r.Labels > 0 {
		parts = append(parts, fmt.Sprintf("%d labeled", r.Labels))
	}
	if r.Archived > 0 {
		parts = append(parts, fmt.Sprintf("%d archived", r.Archived))
	}
	return strings.Join(parts, ", ")
}

func decodeSubmissions(raw []byte) ([]work.Submission, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '[' {
		var subs []work.Submission
		if err := json.Unmarshal(raw, &subs); err != nil {
			return nil, fmt.Errorf("not a JSON array of answers: %v", err)
		}
		return subs, nil
	}
	// One object, or one per line.
	var subs []work.Submission
	dec := json.NewDecoder(bufio.NewReader(bytes.NewReader(raw)))
	for dec.More() {
		var s work.Submission
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("not JSON answers: %v", err)
		}
		subs = append(subs, s)
	}
	return subs, nil
}

// mergeBaseSubmissions overlays sparse/partial overrides onto a base template
// slice so an agent only needs to emit the conversations and elements it
// wants to change, inheriting Lease, Model, Labels, and untouched element
// defaults from the base template.
func mergeBaseSubmissions(base, overrides []work.Submission) []work.Submission {
	byThread := map[string]int{}
	out := make([]work.Submission, len(base))
	for i, b := range base {
		ans := map[string]json.RawMessage{}
		for k, v := range b.Answers {
			ans[k] = v
		}
		b.Answers = ans
		out[i] = b
		byThread[b.Thread] = i
	}
	for _, ov := range overrides {
		idx, ok := byThread[ov.Thread]
		if !ok {
			out = append(out, ov)
			continue
		}
		cur := out[idx]
		if strings.TrimSpace(ov.Lease) != "" {
			cur.Lease = ov.Lease
		}
		if strings.TrimSpace(ov.Model) != "" {
			cur.Model = ov.Model
		}
		if len(ov.Labels) > 0 {
			cur.Labels = ov.Labels
		}
		if ov.Archive {
			cur.Archive = true
		}
		if ov.AcceptTickets {
			cur.AcceptTickets = true
		}
		for k, v := range ov.Answers {
			if prev, exists := cur.Answers[k]; exists {
				cur.Answers[k] = mergeRawAnswer(prev, v)
			} else {
				cur.Answers[k] = v
			}
		}
		out[idx] = cur
	}
	return out
}

// mergeRawAnswer merges a partial message/sender map (e.g. overriding only 1
// message ID in a 5-message thread) over the base template's map when both are
// target-keyed maps; otherwise returns the override directly.
func mergeRawAnswer(baseRaw, ovRaw json.RawMessage) json.RawMessage {
	var ovAns work.Answer
	if json.Unmarshal(ovRaw, &ovAns) == nil &&
		(ovAns.Level != "" || len(ovAns.Records) > 0 || len(ovAns.Tags) > 0 || ovAns.None != "" || ovAns.Skip != "") {
		return ovRaw
	}
	var baseMap, ovMap map[string]work.Answer
	// A base left null — the template would not guess it — has nothing to
	// merge into: the override is the whole answer.
	if json.Unmarshal(baseRaw, &baseMap) == nil && baseMap != nil && json.Unmarshal(ovRaw, &ovMap) == nil && len(ovMap) > 0 {
		for k, v := range ovMap {
			baseMap[k] = v
		}
		if merged, err := json.Marshal(baseMap); err == nil {
			return merged
		}
	}
	return ovRaw
}

// parseSince reads 1d, 12h, 30m or a date.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return time.Now().AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	for _, l := range []string{"2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--since is 1d, 12h or a date, not %q", s)
}
