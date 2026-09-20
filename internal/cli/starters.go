package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/annotate"
	"github.com/user/inboxql/internal/store"
)

// starterRow is one starter as a listing shows it.
type starterRow struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Engine    string `json:"engine"`
	Scope     string `json:"scope,omitempty"`
	About     string `json:"about"`
	Installed bool   `json:"installed"`
	// Matches is how much of this mailbox it would touch. A rule matching
	// nothing here is not worth installing, and an extractor scoped to three
	// thousand messages is worth knowing about before it starts.
	Matches int64 `json:"matches"`
	// Gate names the label this one is scoped by, when that label has not run
	// yet. Without it the listing reads "0 messages" for every extractor and
	// looks like they are useless, when it means the gate has not been asked.
	Gate string `json:"gate,omitempty"`
}

// annotateStarters lists or installs the starter pack.
//
// Creating is not running. Six extractors over a mailbox is an hour of CPU on
// a small one and a day on a real one, so they arrive with coverage at zero
// and the command says what to run next rather than running it.
func annotateStarters(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("annotate starters", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	install := fs.Bool("install", false, "create the ones that do not exist yet")
	only := fs.String("only", "", "comma-separated names, instead of all of them")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var names []string
	for _, n := range strings.Split(*only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}

	if !*install {
		return listStarters(ctx, names)
	}

	made, skipped, err := annotate.InstallStarters(names)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"created": made, "skipped": skipped})
	}

	p := ctx.Printer()
	if len(made) == 0 && len(skipped) > 0 {
		ctx.Printf("Nothing to create; all of them exist already.\n")
		return nil
	}
	ctx.Printf("Created %s.\n", count(int64(len(made)), "annotator", "annotators"))
	for _, n := range made {
		ctx.Printf("  %s\n", n)
	}
	if len(skipped) > 0 {
		ctx.Printf("%s\n", p.Dim("Left alone, because they already exist: "+strings.Join(skipped, ", ")))
	}

	// Said rather than done. A rule pass is milliseconds over a whole mailbox;
	// the extractors are the ones worth deciding about.
	ctx.Printf("\n%s\n", p.Dim("Nothing has run yet. The labels are free — start with those:"))
	ctx.Printf("  iql annotate run money\n")
	ctx.Printf("%s\n", p.Dim("Then an extractor, which reads about 20 seconds per message:"))
	ctx.Printf("  iql annotate plan receipts\n")
	return nil
}

// hasRun reports whether an annotator has evaluated anything yet.
//
// An installed annotator that has never run is, for the purpose of scoping
// something else, the same as one that does not exist.
func hasRun(a *store.Annotator) bool {
	if a == nil {
		return false
	}
	p, err := store.Progress(a, "")
	return err == nil && p.Evaluated > 0
}

func listStarters(ctx *Context, only []string) error {
	want := map[string]bool{}
	for _, n := range only {
		want[n] = true
	}

	existing := map[string]*store.Annotator{}
	if list, err := store.ListAnnotators(); err == nil {
		for _, a := range list {
			existing[a.Name] = a
		}
	}

	var rows []starterRow
	for _, s := range annotate.Starters {
		if len(want) > 0 && !want[s.Name] {
			continue
		}
		r := starterRow{
			Name: s.Name, Kind: s.Kind, Engine: s.Engine,
			Scope: s.Scope, About: s.About,
		}
		_, r.Installed = existing[s.Name]

		// How much of this mailbox each one would touch. A rule matching
		// nothing here is not worth installing, and an extractor scoped to
		// three thousand messages is worth knowing about before it starts.
		probe := s.Scope
		if s.Engine == store.EngineRule {
			probe = s.Instructions
		}

		// An extractor gated by a label that has not run matches nothing yet,
		// which is true and useless to show: it would read as "this finds
		// nothing" when it means "nobody has asked the gate". Count what the
		// gate would reach instead, and say that is what this is.
		if s.Needs != "" && !hasRun(existing[s.Needs]) {
			if gate, ok := annotate.StarterByName(s.Needs); ok {
				probe = gate.Instructions
				r.Gate = s.Needs
			}
		}

		if probe != "" {
			if n, err := store.CountQuery(probe); err == nil {
				r.Matches = n
			}
		}
		rows = append(rows, r)
	}

	if ctx.JSON {
		return ctx.EmitJSON(rows)
	}

	p := ctx.Printer()
	ctx.Printf("%s\n\n", p.Dim(
		"Starting points, meant to be edited. Each extractor is scoped by a cheap\n"+
			"rule label, because a span engine costs ~20s a message and a query costs nothing."))

	for _, r := range rows {
		mark := " "
		if r.Installed {
			mark = "·"
		}
		kind := "extract"
		if r.Kind == store.KindLabel {
			kind = "label"
		}
		ctx.Printf("%s %-11s %-7s %-7s %s\n", mark, p.Bold(r.Name), kind, r.Engine, r.About)
		if r.Scope != "" {
			ctx.Printf("    %-9s %s\n", p.Dim("scope"), r.Scope)
		}
		if r.Gate != "" {
			ctx.Printf("    %-9s %s %s\n", p.Dim("here"),
				count(r.Matches, "message", "messages"),
				p.Dim("once `"+r.Gate+"` has run"))
		} else {
			ctx.Printf("    %-9s %s\n", p.Dim("here"),
				count(r.Matches, "message", "messages"))
		}
	}

	ctx.Printf("\n%s\n", p.Dim("· already exists. Create the rest with `iql annotate starters --install`."))
	ctx.Printf("%s\n", p.Dim(
		"Read them back at a confidence floor — `extract:receipts.amount@0.6`. Below\n"+
			"about 0.6 a span extractor asked for reference numbers starts offering\n"+
			"card-shaped strings it found on the page."))
	return nil
}

// annotateSweep runs everything waiting on a trigger.
//
// The manual face of what a trigger does automatically, so the behaviour can
// be seen and tested without waiting for a sync or a timer.
func annotateSweep(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("annotate sweep", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	when := fs.String("trigger", store.TriggerAfterSync, "which trigger to honour")
	dryRun := fs.Bool("dry-run", false, "report what would run and do nothing")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if !store.ValidTrigger(*when) {
		return Fail(ExitUsage, "--trigger must be one of: %s", strings.Join(store.Triggers, ", "))
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	due, err := annotate.Due(*when)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	p := ctx.Printer()
	if len(due) == 0 {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"trigger": *when, "due": []string{}})
		}
		ctx.Printf("Nothing is waiting on %s.\n", *when)
		return nil
	}

	if *dryRun {
		type row struct {
			Name    string `json:"name"`
			Engine  string `json:"engine"`
			Scope   string `json:"scope,omitempty"`
			Pending int64  `json:"pending"`
		}
		var rows []row
		for _, a := range due {
			r := row{Name: a.Name, Engine: a.Engine, Scope: a.Scope}
			if pr, err := store.Progress(a, a.Scope); err == nil {
				r.Pending = pr.Total - pr.Evaluated
			}
			rows = append(rows, r)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"trigger": *when, "due": rows})
		}
		ctx.Printf("%s would run:\n", *when)
		for _, r := range rows {
			ctx.Printf("  %-12s %-7s %s\n", r.Name, r.Engine,
				count(r.Pending, "message", "messages"))
			if r.Scope != "" {
				ctx.Printf("    %s %s\n", p.Dim("scope"), r.Scope)
			}
		}
		// A pass is capped on purpose; saying so stops "it did not finish"
		// being read as a failure.
		ctx.Printf("%s\n", p.Dim(fmt.Sprintf(
			"At most %d messages per annotator per pass. The rest stay pending.",
			annotate.TriggeredLimit)))
		return nil
	}

	out, err := annotate.Sweep(context.Background(), *when, ctx.DataDir,
		func(name string, done, total int64) {
			if !ctx.JSON && total > 0 {
				p.Printf("\r  %s %s %d/%d", p.Dim("running"), name, done, total)
			}
		})
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(out)
	}

	ctx.Printf("\r%-48s\n", "")
	for _, o := range out.Ran {
		ctx.Printf("  %-12s evaluated %d, %d record(s)\n", o.Annotator, o.Evaluated, o.Records)
	}
	for _, f := range out.Failed {
		ctx.Printf("  %s %s\n", p.Yellow("failed"), f)
	}
	if out.Remaining > 0 {
		ctx.Printf("%s\n", p.Dim(fmt.Sprintf(
			"%d still pending — a pass is capped at %d each; run it again.",
			out.Remaining, annotate.TriggeredLimit)))
	}
	ctx.Printf("%s\n", p.Dim("done in "+out.Duration))
	return nil
}

// reportWaitingAnnotators says what a sync has left for the annotators.
//
// Said, not done. `iql account sync` is documented as synchronous and safe in
// a cron job; silently gaining twenty minutes of span extraction would make
// that false. The UI starts the job instead, because it can show progress and
// offer to stop — see the after-sync trigger in the maintenance panel.
func reportWaitingAnnotators(ctx *Context) {
	due, err := annotate.Due(store.TriggerAfterSync)
	if err != nil || len(due) == 0 {
		return
	}
	names := make([]string, 0, len(due))
	for _, a := range due {
		names = append(names, a.Name)
	}
	p := ctx.Printer()
	ctx.Printf("%s\n", p.Dim(fmt.Sprintf(
		"%s waiting on after-sync: %s",
		count(int64(len(due)), "annotator is", "annotators are"),
		strings.Join(names, ", "))))
	ctx.Printf("%s\n", p.Dim("  iql annotate sweep"))
}
