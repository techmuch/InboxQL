package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/inboxql/internal/laya"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "laya",
		Summary: "manage the decision model",
		Usage: `iql laya <install|status|remove> [--repo name] [--yes]

The model behind ` + "`--engine laya`" + ` annotators. It answers a yes-or-no
question about a message by scoring the two answers, so the only thing it can
return is one of the options it was given.

  iql laya status     say whether a model is installed, and where
  iql laya install    download it and prepare it for this machine
  iql laya calibrate  fit an annotator's scores to your own corrections
  iql laya remove     delete it

flags:
  --repo name   model repository to install (default ` + laya.DefaultRepo + `)
  --yes         skip the confirmation

## What it is for

A rule label is a query: free, exact, and unable to answer anything the query
language cannot say. A span extractor reads values out of a message and cannot
label it at all. So a label that needs judgement — "is this actually about a
purchase, or a newsletter that mentions a price" — had only one home, the LLM,
which is the engine that will invent a field rather than leave it out.

This one emits no tokens. It is about two seconds a message, against roughly
forty for a local generative model.

## Why installing is not just downloading

What arrives is float16, which is right for the runtime it was exported for and
not for this one: the pure Go backend normalises in float32, so the published
file parses, builds a graph, and then fails hundreds of nodes in with an error
about a dtype. Installing widens the weights once, here. No value changes —
every float16 is exactly a float32 — it only costs disk, about 600 MB becoming
about 1.2 GB.

The conversion happens on this machine, from the file the publisher published,
rather than being fetched pre-converted from somewhere with no particular claim
to be trusted.

## What it does not promise

The published checkpoint is not calibrated: it ships with every temperature at
1.0, and its authors measure its expected calibration error at 0.466. So the
probability on an annotation orders messages correctly — a 0.95 is more likely
right than a 0.6 — and is not a frequency. ` + "`label:x@0.9`" + ` against these
scores is a ranking cut, not "ninety percent of these are right".

## Where the mail goes

Nowhere. The model runs in this process and reads only the mailbox already on
this disk. The one network request this feature ever makes is the download.`,
		Run: runLayaCmd,
	})
}

func runLayaCmd(ctx *Context, args []string) error {
	sub, rest := subcommand(args)

	fs := flag.NewFlagSet("laya", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	repo := fs.String("repo", laya.DefaultRepo, "model repository to install")
	yes := fs.Bool("yes", false, "skip the confirmation")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	switch sub {
	case "status", "":
		return layaStatus(ctx)
	case "install":
		return layaInstall(ctx, *repo, *yes)
	case "calibrate":
		return layaCalibrate(ctx, rest)
	case "remove":
		return layaRemove(ctx, *yes)
	default:
		return Fail(ExitUsage, "unknown subcommand %q: use install, status, calibrate or remove", sub)
	}
}

func layaStatus(ctx *Context) error {
	dir := laya.Dir(ctx.DataDir)
	p := ctx.Printer()

	if !laya.Installed(ctx.DataDir) {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"installed": false, "dir": dir})
		}
		ctx.Printf("No decision model installed.\n")
		ctx.Printf("%s\n", p.Dim("Install one with `iql laya install`."))
		return nil
	}

	var size int64
	for _, f := range laya.ModelFiles {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil {
			size += st.Size()
		}
	}
	card := laya.ReadCard(ctx.DataDir)

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"installed": true, "dir": dir, "bytes": size,
			"repo": card.Repo, "digest": card.Digest, "installedAt": card.Installed,
			"calibrated": false,
		})
	}
	ctx.Printf("Decision model installed.\n")
	ctx.Printf("  %-10s %s\n", p.Dim("where"), dir)
	ctx.Printf("  %-10s %s\n", p.Dim("size"), humanBytes(size))
	if card.Repo != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("model"), card.Repo)
	}
	if card.Digest != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("digest"), card.Digest[:16])
	} else {
		ctx.Printf("  %-10s %s\n", p.Dim("digest"),
			p.Yellow("unrecorded — this model was not put here by `iql laya install`"))
	}
	// Said every time it is asked about, because the number on every
	// annotation looks exactly like a calibrated one and is not.
	ctx.Printf("  %-10s %s\n", p.Dim("scores"),
		p.Yellow("ordering only — this checkpoint ships uncalibrated"))
	ctx.Printf("%s\n", p.Dim("Use it with `iql annotate create <name> --kind label --engine laya`."))
	return nil
}

func layaInstall(ctx *Context, repo string, yes bool) error {
	p := ctx.Printer()
	ctx.Printf("Downloading %s into %s.\n", repo, laya.Dir(ctx.DataDir))
	ctx.Printf("%s\n", p.Dim(
		"About 620 MB, which becomes about 1.2 GB once widened for this machine. "+
			"It is fetched once and then runs entirely here; no mail is sent anywhere."))

	if !yes && !ctx.Confirm("Go ahead?") {
		return Fail(ExitError, "cancelled")
	}

	last := ""
	started := time.Now()
	err := laya.Install(context.Background(), ctx.DataDir, repo,
		func(file string, done, total int64) {
			if ctx.JSON {
				return
			}
			var line string
			if total > 0 {
				line = fmt.Sprintf("  %s %d%%", file, done*100/total)
			} else {
				line = fmt.Sprintf("  %s %s", file, humanBytes(done))
			}
			if line != last {
				ctx.Printf("\r%-40s", line)
				last = line
			}
		})
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	if !ctx.JSON {
		ctx.Printf("\r%-40s\n", "")
	}
	ctx.Printf("Installed in %s.\n", time.Since(started).Round(time.Second))
	ctx.Printf("%s\n", p.Dim(
		`Next: iql annotate create purchases --kind label --engine laya \`+"\n"+
			`        --instructions "this message is about a purchase the reader made"`))
	return nil
}

func layaRemove(ctx *Context, yes bool) error {
	dir := laya.Dir(ctx.DataDir)
	if !laya.Installed(ctx.DataDir) {
		ctx.Printf("Nothing installed.\n")
		return nil
	}
	ctx.Printf("This deletes %s.\n", dir)
	ctx.Printf("%s\n", ctx.Printer().Dim(
		"Existing annotations are kept — they are results, not the model."))
	if !yes && !ctx.Confirm("Go ahead?") {
		return Fail(ExitError, "cancelled")
	}
	if err := laya.Remove(ctx.DataDir); err != nil {
		return Fail(ExitError, "%v", err)
	}
	ctx.Printf("Removed.\n")
	return nil
}

// layaCalibrate fits a temperature from the corrections somebody has made.
//
// # What this does and does not buy
//
// It does not make the model more accurate. Temperature scaling divides the
// logits by one number, which cannot reorder anything, so every answer stays
// the answer it was. What changes is whether the number attached to it means
// what it looks like it means — whether, of the answers returned at 0.9, about
// nine in ten are right.
//
// That is the difference between `label:x@0.9` being a threshold and being a
// ranking cut with a misleading name.
func layaCalibrate(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which annotator? `iql laya calibrate <name>`")
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
	if a.Engine != store.EngineLaya {
		return Fail(ExitUsage,
			"%q runs on the %s engine. Only a decision annotator has scores to calibrate",
			name, a.Engine)
	}

	rulings, err := store.HumanRulings(a.ID)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	obs := make([]laya.Observation, 0, len(rulings))
	for _, r := range rulings {
		// The quantity being calibrated is the confidence in the answer that
		// was given, so a "no" at 0.2 reported true is a 0.8 confident "no".
		p := r.Confidence
		if !r.Said {
			p = 1 - p
		}
		obs = append(obs, laya.Observation{Probability: p, Correct: r.Said == r.Ruled})
	}

	t, err := laya.FitTemperature(obs)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if err := laya.SaveTemperature(ctx.DataDir, a.Name, t); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"annotator": a.Name, "temperature": t})
	}

	p := ctx.Printer()
	ctx.Printf("Fitted %s on %d of your rulings.\n", p.Bold(a.Name), t.N)
	ctx.Printf("  %-12s %.2f %s\n", p.Dim("temperature"), t.Value,
		p.Dim(temperatureReads(t.Value)))
	ctx.Printf("  %-12s %.3f → %.3f\n", p.Dim("calibration"), t.ECEBefore, t.ECEAfter)
	ctx.Printf("\n%s\n", p.Dim(
		"Applied to new runs. Existing annotations keep the scores they were given;\n"+
			"re-run to rescore them."))
	return nil
}

func temperatureReads(v float64) string {
	switch {
	case v > 1.05:
		return "(it was over-confident; scores are softened)"
	case v < 0.95:
		return "(it was under-confident; scores are sharpened)"
	default:
		return "(it was already about right)"
	}
}
