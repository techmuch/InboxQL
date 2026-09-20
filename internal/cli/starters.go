package cli

import (
	"flag"
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
