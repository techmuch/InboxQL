package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/user/inboxql/internal/annotate"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "annotate",
		Aliases: []string{"label"},
		Summary: "define and run labels and extractors",
		Usage: `iql annotate <list|show|create|delete|run|plan|correct> [flags]

An annotator is a named, versioned instruction applied to messages. A label
answers yes or no; an extractor pulls structured records out of a body. They
are the same mechanism, so they version, re-run and query the same way.

  list      show annotators and how far each has got
  show      one annotator, its instruction and its progress
  create    define or update one
  delete    remove it and everything it produced
  run       apply it to messages that still need it
  plan      report what a run would do, without doing it
  correct   record a human ruling, which outranks the machine

create flags:
  --kind <label|extract>    yes/no, or structured records (default label)
  --engine <rule|llm>       a query expression, or a prompt (default rule)
  --instructions <text>     the rule or the prompt; - reads stdin
  --schema <file>           JSON schema for an extractor's records
  --time-field <name>       which extracted field is the record's own date
  --model <name>            override the configured LLM model
  --allow-remote            consent to sending bodies to a non-local provider

run flags:
  --scope <query>   only messages matching this filter
  --limit <n>       stop after n messages
  --dry-run         report what would happen and write nothing

Editing --instructions bumps the version, which marks every earlier result
stale without deleting it. A re-run then only visits what it has not answered
at the new version.

A rule is a query expression, so the language in ` + "`iql query`" + ` is also the
labelling language:

  iql annotate create billing --engine rule \
    --instructions "from:*@stripe.com OR subject:invoice"
  iql annotate run billing

An LLM annotator sends message bodies to whatever provider is configured. If
that provider is remote, a run refuses without --allow-remote.`,
		Run: runAnnotate,
	})
}

func runAnnotate(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "list", "":
		return annotateList(ctx, rest)
	case "show":
		return annotateShow(ctx, rest)
	case "create", "add", "update":
		return annotateCreate(ctx, rest)
	case "delete", "rm", "remove":
		return annotateDelete(ctx, rest)
	case "run":
		return annotateRun(ctx, rest)
	case "plan":
		return annotatePlan(ctx, rest)
	case "correct":
		return annotateCorrect(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, show, create, delete, run, plan or correct)", sub)
	}
}

func annotateList(ctx *Context, args []string) error {
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	annotators, err := store.ListAnnotators()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	type row struct {
		*store.Annotator
		Progress *store.AnnotatorProgress `json:"progress"`
	}
	out := make([]row, 0, len(annotators))
	for _, a := range annotators {
		p, err := store.Progress(a, "")
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		out = append(out, row{Annotator: a, Progress: p})
	}

	if ctx.JSON {
		return ctx.EmitJSON(out)
	}
	if len(out) == 0 {
		ctx.Printf("No annotators yet. Create one with `iql annotate create <name> --instructions ...`\n")
		return nil
	}

	p := ctx.Printer()
	t := p.NewTable("NAME", "KIND", "ENGINE", "VER", "EVALUATED", "MATCHED", "CORRECTIONS")
	for _, r := range out {
		coverage := fmt.Sprintf("%d/%d", r.Progress.Evaluated, r.Progress.Total)
		t.Row(r.Name, r.Kind, r.Engine, itoa(r.Version), coverage,
			fmt.Sprint(r.Progress.Matched), fmt.Sprint(r.Progress.Human))
	}
	return t.Flush()
}

func annotateShow(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which annotator?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	a, err := store.GetAnnotator(name)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if a == nil {
		return Fail(ExitNotFound, "no annotator named %q", name)
	}
	progress, err := store.Progress(a, "")
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"annotator": a, "progress": progress})
	}

	p := ctx.Printer()
	ctx.Printf("%s %s\n\n", p.Bold(a.Name), p.Dim(fmt.Sprintf("(%s, %s engine, v%d)", a.Kind, a.Engine, a.Version)))
	ctx.Printf("  %s\n", p.Dim("instructions"))
	for _, line := range strings.Split(a.Instructions, "\n") {
		ctx.Printf("    %s\n", line)
	}
	if a.SchemaJSON != "" && a.SchemaJSON != "{}" {
		ctx.Printf("\n  %s\n", p.Dim("schema"))
		ctx.Printf("    %s\n", a.SchemaJSON)
	}
	ctx.Printf("\n  %-14s %d of %d\n", p.Dim("evaluated"), progress.Evaluated, progress.Total)
	ctx.Printf("  %-14s %d\n", p.Dim("matched"), progress.Matched)
	ctx.Printf("  %-14s %d\n", p.Dim("no match"), progress.Empty)
	if progress.Failed > 0 {
		ctx.Printf("  %-14s %s\n", p.Dim("failed"), p.Yellow(fmt.Sprint(progress.Failed)))
	}
	if progress.Human > 0 {
		ctx.Printf("  %-14s %d %s\n", p.Dim("corrections"), progress.Human,
			p.Dim("(kept across re-runs)"))
	}
	if a.Engine == store.EngineLLM {
		ctx.Printf("  %-14s %v\n", p.Dim("remote ok"), a.AllowRemote)
	}
	return nil
}

func annotateCreate(ctx *Context, args []string) error {
	name, rest := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "give the annotator a name")
	}

	fs := flag.NewFlagSet("annotate create", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	kind := fs.String("kind", store.KindLabel, "label or extract")
	engine := fs.String("engine", store.EngineRule, "rule or llm")
	instructions := fs.String("instructions", "", "the rule expression or prompt; - reads stdin")
	schemaFile := fs.String("schema", "", "JSON schema file for an extractor")
	timeField := fs.String("time-field", "", "extracted field holding the record's own date")
	model := fs.String("model", "", "override the configured model")
	allowRemote := fs.Bool("allow-remote", false, "consent to sending bodies to a remote provider")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if *kind != store.KindLabel && *kind != store.KindExtract {
		return Fail(ExitUsage, "--kind must be label or extract")
	}
	if *engine != store.EngineRule && *engine != store.EngineLLM {
		return Fail(ExitUsage, "--engine must be rule or llm")
	}
	if *kind == store.KindExtract && *engine == store.EngineRule {
		return Fail(ExitUsage, "a rule can answer yes or no, but it cannot extract records; use --engine llm")
	}

	text := *instructions
	if text == "-" {
		b, err := readAll(ctx.Stdin)
		if err != nil {
			return Fail(ExitError, "reading instructions from stdin: %v", err)
		}
		text = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(text) == "" {
		return Fail(ExitUsage, "--instructions is required")
	}

	// A rule is a query, so it can be checked now rather than failing halfway
	// through a run over the whole mailbox.
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *engine == store.EngineRule {
		if err := store.ValidateQuery(text); err != nil {
			return Fail(ExitUsage, "the rule is not a valid query: %v", err)
		}
	}

	schemaJSON := "{}"
	if *schemaFile != "" {
		b, err := os.ReadFile(*schemaFile)
		if err != nil {
			return Fail(ExitError, "reading --schema: %v", err)
		}
		if !json.Valid(b) {
			return Fail(ExitUsage, "%s is not valid JSON", *schemaFile)
		}
		schemaJSON = string(b)
	}
	if *timeField != "" {
		var schema map[string]any
		if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if schema == nil {
			schema = map[string]any{}
		}
		schema["timeField"] = *timeField
		b, _ := json.Marshal(schema)
		schemaJSON = string(b)
	}

	existing, err := store.GetAnnotator(name)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	a := &store.Annotator{
		Name: name, Kind: *kind, Engine: *engine,
		Instructions: text, SchemaJSON: schemaJSON,
		Model: *model, AllowRemote: *allowRemote,
	}
	if err := store.SaveAnnotator(a); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(a)
	}

	p := ctx.Printer()
	switch {
	case existing == nil:
		ctx.Printf("Created annotator %s.\n\n", p.Bold(a.Name))
	case a.Version > existing.Version:
		ctx.Printf("Updated %s; the instruction changed, so it is now %s.\n",
			p.Bold(a.Name), p.Bold(fmt.Sprintf("v%d", a.Version)))
		ctx.Printf("%s\n\n", p.Dim("Earlier results are kept but no longer answer queries; re-run to refresh them."))
	default:
		ctx.Printf("Updated %s.\n\n", p.Bold(a.Name))
	}
	ctx.Printf("Next: %s\n", p.Dim(fmt.Sprintf("iql annotate run %s --dry-run", a.Name)))
	return nil
}

func annotateDelete(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which annotator?")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if err := store.DeleteAnnotator(name); err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"deleted": name})
	}
	ctx.Printf("Deleted %s and every result it produced.\n", name)
	return nil
}

func annotatePlan(ctx *Context, args []string) error {
	name, rest := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which annotator?")
	}
	fs := flag.NewFlagSet("annotate plan", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	scope := fs.String("scope", "", "only messages matching this query")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	plan, err := annotate.Describe(name, *scope)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(plan)
	}
	printPlan(ctx, plan)
	return nil
}

func printPlan(ctx *Context, plan *annotate.Plan) {
	p := ctx.Printer()
	ctx.Printf("%s v%d (%s engine)\n", p.Bold(plan.Annotator), plan.Version, plan.Engine)
	ctx.Printf("  %-12s %d of %d messages\n", p.Dim("to evaluate"), plan.Pending, plan.Total)
	if plan.Scope != "" {
		ctx.Printf("  %-12s %s\n", p.Dim("scope"), plan.Scope)
	}
	if plan.Provider != "" {
		ctx.Printf("  %-12s %s\n", p.Dim("provider"), plan.Provider)
	}
	if plan.Remote {
		ctx.Printf("\n  %s %s\n", p.Yellow("warning:"),
			fmt.Sprintf("this sends %s message bodies to %s.",
				plural(plan.Pending, "", ""), plan.Endpoint))
		ctx.Printf("  %s\n", p.Dim("Roughly "+formatChars(plan.EstimatedChars)+" of text will leave this machine."))
	} else if plan.Engine == store.EngineLLM {
		ctx.Printf("  %-12s %s\n", p.Dim("privacy"), p.Green("local provider; nothing leaves this machine"))
	}
}

func formatChars(n int64) string {
	switch {
	case n > 1_000_000:
		return fmt.Sprintf("%.1fM characters", float64(n)/1e6)
	case n > 1000:
		return fmt.Sprintf("%.0fk characters", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d characters", n)
	}
}

func annotateRun(ctx *Context, args []string) error {
	name, rest := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which annotator?")
	}

	fs := flag.NewFlagSet("annotate run", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	scope := fs.String("scope", "", "only messages matching this query")
	limit := fs.Int("limit", 0, "stop after n messages")
	dryRun := fs.Bool("dry-run", false, "report what would happen and write nothing")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if *scope != "" {
		if err := store.ValidateQuery(*scope); err != nil {
			return Fail(ExitUsage, "--scope is not a valid query: %v", err)
		}
	}

	plan, err := annotate.Describe(name, *scope)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}

	if *dryRun {
		if ctx.JSON {
			return ctx.EmitJSON(plan)
		}
		printPlan(ctx, plan)
		ctx.Printf("\n%s\n", ctx.Printer().Dim("Nothing was written. Drop --dry-run to run it."))
		return nil
	}

	// State the privacy posture before a bulk send, whether or not it was
	// asked for. The refusal itself lives in annotate.Run; this is so the
	// person watching knows what is happening while it happens.
	if plan.Remote && !ctx.JSON {
		printPlan(ctx, plan)
		ctx.Printf("\n")
	}

	opt := annotate.Options{Scope: *scope, Limit: *limit}
	if !ctx.JSON && plan.Engine == store.EngineLLM {
		p := ctx.Printer()
		opt.Progress = func(done, total int64) {
			if total > 0 {
				p.Printf("\r  %s %d/%d", p.Dim("annotating"), done, total)
			}
		}
	}

	outcome, err := annotate.Run(context.Background(), name, opt)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if opt.Progress != nil {
		ctx.Printf("\r%s\r", strings.Repeat(" ", 40))
	}

	if ctx.JSON {
		return ctx.EmitJSON(outcome)
	}

	p := ctx.Printer()
	ctx.Printf("%s v%d — %s\n\n", p.Bold(outcome.Annotator), outcome.Version, p.Dim(outcome.Duration))
	ctx.Printf("  %-12s %d\n", p.Dim("evaluated"), outcome.Evaluated)
	ctx.Printf("  %-12s %d\n", p.Dim("matched"), outcome.Matched)
	ctx.Printf("  %-12s %d\n", p.Dim("no match"), outcome.Empty)
	if outcome.Records > outcome.Matched {
		ctx.Printf("  %-12s %d\n", p.Dim("records"), outcome.Records)
	}
	if outcome.Failed > 0 {
		ctx.Printf("  %-12s %s\n", p.Dim("failed"), p.Yellow(fmt.Sprint(outcome.Failed)))
		ctx.Printf("\n%s\n", p.Dim("Failures are recorded, not retried. `iql query \"label:"+outcome.Annotator+"\"` shows what matched."))
	}
	ctx.Printf("\nQuery it: %s\n", p.Dim(fmt.Sprintf("iql query \"label:%s\"", outcome.Annotator)))
	return nil
}

func annotateCorrect(ctx *Context, args []string) error {
	name, rest := subcommand(args)
	messageID, rest := subcommand(rest)
	if name == "" || messageID == "" {
		return Fail(ExitUsage, "usage: iql annotate correct <name> <message-id> --yes|--no")
	}

	fs := flag.NewFlagSet("annotate correct", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	yes := fs.Bool("yes", false, "this message does match")
	no := fs.Bool("no", false, "this message does not match")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if *yes == *no {
		return Fail(ExitUsage, "pass exactly one of --yes or --no")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if err := store.SetHumanAnnotation(name, messageID, *yes, nil); err != nil {
		return Fail(ExitNotFound, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"annotator": name, "message": messageID, "matched": *yes, "source": store.SourceHuman,
		})
	}
	ctx.Printf("Recorded. This ruling outranks the annotator and survives every re-run.\n")
	return nil
}
