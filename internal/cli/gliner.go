package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/inboxql/internal/gliner"
)

func init() {
	register(&Command{
		Name:    "gliner",
		Summary: "manage the span-extraction model",
		Usage: `iql gliner <install|status|remove> [--repo name] [--yes]

The model behind ` + "`--engine gliner`" + ` annotators. It finds values by scoring
spans of the message itself, so every value it produces is a piece of that
message — it has no way to return one that is not.

  iql gliner status     say whether a model is installed, and where
  iql gliner install    download it and prepare it for this machine
  iql gliner remove     delete it

flags:
  --repo name   model repository to install (default ` + gliner.DefaultRepo + `)
  --yes         skip the confirmation

## Why installing is not just downloading

What arrives is the model as its authors published it. A published GLiNER
export cannot be compiled ahead of time — it works out where the words are
with an operation whose output shape depends on its input, which is fine for a
C++ runtime that discovers shapes as it goes and impossible for one that
decides them in advance. Installing rewrites those lookups into inputs, which
InboxQL can supply because it built them in the first place.

The rewrite happens here, on this machine, from the file the publisher
published. That is deliberate: a pre-rewritten model from somewhere else would
be a model binary from a host with no particular claim to be trusted.

## Where the mail goes

Nowhere. The model runs in this process, and the only thing it reads is the
mailbox already on this disk. Nothing is sent anywhere at annotation time —
the one network request this feature ever makes is the download below.`,
		Run: runGliner,
	})
}

func runGliner(ctx *Context, args []string) error {
	sub, rest := subcommand(args)

	fs := flag.NewFlagSet("gliner", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	repo := fs.String("repo", gliner.DefaultRepo, "model repository to install")
	yes := fs.Bool("yes", false, "skip the confirmation")
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	switch sub {
	case "status", "":
		return glinerStatus(ctx)
	case "install":
		return glinerInstall(ctx, *repo, *yes)
	case "remove":
		return glinerRemove(ctx, *yes)
	default:
		return Fail(ExitUsage, "unknown subcommand %q: use install, status or remove", sub)
	}
}

func glinerStatus(ctx *Context) error {
	dir := gliner.Dir(ctx.DataDir)
	p := ctx.Printer()

	if !gliner.Installed(ctx.DataDir) {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"installed": false, "dir": dir})
		}
		ctx.Printf("No span-extraction model installed.\n")
		ctx.Printf("%s\n", p.Dim("Install one with `iql gliner install`."))
		return nil
	}

	var size int64
	for _, f := range gliner.ModelFiles {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil {
			size += st.Size()
		}
	}
	// From the card rather than hashed again: it was recorded at install and
	// cannot have changed without the file changing, and hashing 750 MB to
	// print one line is not worth the wait.
	card := gliner.ReadCard(ctx.DataDir)

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"installed": true, "dir": dir, "bytes": size,
			"repo": card.Repo, "digest": card.Digest, "installedAt": card.Installed,
		})
	}
	ctx.Printf("Span-extraction model installed.\n")
	ctx.Printf("  %-10s %s\n", p.Dim("where"), dir)
	ctx.Printf("  %-10s %s\n", p.Dim("size"), humanBytes(size))
	if card.Repo != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("model"), card.Repo)
	}
	if card.Digest != "" {
		ctx.Printf("  %-10s %s\n", p.Dim("digest"), card.Digest[:16])
	} else {
		// Placed by hand rather than installed. It may work perfectly; it just
		// cannot be traced, and every annotation it produces will say so.
		ctx.Printf("  %-10s %s\n", p.Dim("digest"),
			p.Yellow("unrecorded — this model was not put here by `iql gliner install`"))
	}
	ctx.Printf("%s\n", p.Dim("Use it with `iql annotate create <name> --kind extract --engine gliner`."))
	return nil
}

func glinerInstall(ctx *Context, repo string, yes bool) error {
	p := ctx.Printer()
	ctx.Printf("Downloading %s into %s.\n", repo, gliner.Dir(ctx.DataDir))
	ctx.Printf("%s\n", p.Dim(
		"About 800 MB. It is fetched once and then runs entirely on this machine; "+
			"no mail is sent anywhere by this feature."))

	if !yes && !ctx.Confirm("Go ahead?") {
		return Fail(ExitError, "cancelled")
	}

	last := ""
	started := time.Now()
	err := gliner.Install(context.Background(), ctx.DataDir, repo,
		func(file string, done, total int64) {
			if ctx.JSON {
				return
			}
			// Only on whole percents, or a 800 MB download redraws the line
			// tens of thousands of times.
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
	ctx.Printf("%s\n", p.Dim("Next: iql annotate create money --kind extract --engine gliner --schema s.json"))
	return nil
}

func glinerRemove(ctx *Context, yes bool) error {
	dir := gliner.Dir(ctx.DataDir)
	if !gliner.Installed(ctx.DataDir) {
		ctx.Printf("Nothing installed.\n")
		return nil
	}
	ctx.Printf("This deletes %s.\n", dir)
	ctx.Printf("%s\n", ctx.Printer().Dim(
		"Existing annotations are kept — they are results, not the model."))
	if !yes && !ctx.Confirm("Go ahead?") {
		return Fail(ExitError, "cancelled")
	}
	if err := os.RemoveAll(dir); err != nil {
		return Fail(ExitError, "%v", err)
	}
	ctx.Printf("Removed.\n")
	return nil
}
