package cli

import (
	"context"
	"flag"
	"strings"

	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "ocr",
		Summary: "read scanned files with a vision model",
		Usage: `iql ocr [--limit n] [--max-edge px] [--yes]

Reads the files that hold no text layer — scans, photographs of paper — by
showing each page to the configured model and storing what it reads.

Only files already known to be scans are considered. "iql maintenance text"
decides that, and records it, so this pass never re-reads a PDF that simply
had text all along. Find them with:

  iql query "in:attachments is:scanned"

flags:
  --limit n      stop after n files
  --max-edge px  longest edge to send, in pixels (default 1600)
  --yes          skip the confirmation

## What this is, and is not

A vision model transcribes and also invents. On a supermarket receipt it will
read the shop, the street and the totals correctly, misread the odd word, and
occasionally add a plausible line that is not on the paper. For search that is
usually worth it — a document that was unfindable becomes findable by most of
the words really on it — but it is not the same thing as classical OCR, and
the result is recorded as coming from OCR everywhere it is shown so that a
surprising match can be checked against the page.

## Where the pages go

Each page is sent to whatever "iql llm configure" points at. If that is a
local model, the images do not leave the machine. If it is a hosted API, every
scanned page in the mailbox is uploaded to it — which for scans of forms,
statements and correspondence is worth deciding deliberately rather than
discovering afterwards. This command says which it is and asks before
starting.`,
		Run: runOCR,
	})
}

func runOCR(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("ocr", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	limit := fs.Int("limit", 0, "stop after n files")
	maxEdge := fs.Int("max-edge", 1600, "longest edge to send, in pixels")
	yes := fs.Bool("yes", false, "skip the confirmation")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	cfg, err := store.GetLLMConfig()
	if err != nil {
		return Fail(ExitError, "reading the model configuration: %v", err)
	}
	provider, err := llm.New(cfg)
	if err != nil {
		return Fail(ExitUsage,
			"no model is configured to read images: %v\n"+
				"Configure one with `iql llm configure`.", err)
	}
	if !llm.CanSee(provider) {
		return Fail(ExitUsage,
			"%s cannot be shown images. Configure a vision-capable model.", provider.Name())
	}

	candidates, err := store.OCRCandidates()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if candidates == 0 {
		ctx.Printf("No scanned files to read.\n")
		ctx.Printf("%s\n", ctx.Printer().Dim(
			"Scans are identified by `iql maintenance text`; run that first if you have not."))
		return nil
	}
	if *limit > 0 && *limit < candidates {
		candidates = *limit
	}

	// Where the pages are about to go is the thing worth being sure of, so it
	// is stated before anything is sent rather than in the help text only.
	local := isLocalEndpoint(cfg.Endpoint, cfg.Provider)
	p := ctx.Printer()
	ctx.Printf("Reading %s with %s.\n", count(int64(candidates), "scanned file", "scanned files"), provider.Name())
	if local {
		ctx.Printf("%s\n", p.Dim("The pages stay on this machine."))
	} else {
		ctx.Printf("%s\n", p.Yellow(
			"Every page of every scan will be uploaded to "+cfg.Endpoint+"."))
	}

	if !*yes && !ctx.Confirm("Go ahead?") {
		return Fail(ExitError, "cancelled")
	}

	reader := &visionReader{provider: provider}
	out, err := store.OCRAttachments(context.Background(),
		blobstore.New(ctx.DataDir), reader, *maxEdge, *limit,
		func(done, total int, filename string) {
			if !ctx.JSON {
				ctx.Printf("  [%d/%d] %s\n", done, total, ui.Truncate(filename, 60))
			}
		})
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(out)
	}

	ctx.Printf("\nRead %s.\n", count(int64(out.Read), "file", "files"))
	if out.Illegible > 0 {
		ctx.Printf("%d had nothing legible.\n", out.Illegible)
	}
	if out.NoImages > 0 {
		ctx.Printf("%d use a page format this cannot extract.\n", out.NoImages)
	}
	if out.Failed > 0 {
		ctx.Printf("%d could not be read at all.\n", out.Failed)
	}
	if out.Read > 0 {
		ctx.Printf("%s\n", p.Dim(
			"OCR text is marked as such wherever it appears, because a model transcribing "+
				"a page can also invent. Check a surprising match against the file."))
	}
	return nil
}

// visionReader adapts a provider to what the store's OCR pass needs.
//
// The store defines the narrow interface and this satisfies it, so the store
// does not import the provider package — the same shape as the query
// compiler's injected options.
type visionReader struct{ provider llm.Provider }

func (v *visionReader) Name() string { return v.provider.Name() }

func (v *visionReader) ReadImage(ctx context.Context, mime string, data []byte) (string, error) {
	return llm.Describe(ctx, v.provider, store.OCRSystem, store.OCRPrompt,
		[]llm.Image{{MIME: mime, Data: data}})
}

// isLocalEndpoint reports whether the configured model runs on this machine.
//
// Used only to decide what to warn about. Wrong in the safe direction: an
// endpoint this does not recognise is treated as remote, so the warning
// appears when it is not needed rather than being absent when it is.
func isLocalEndpoint(endpoint, provider string) bool {
	if endpoint == "" {
		endpoint = llm.DefaultEndpoints[provider]
	}
	e := strings.ToLower(endpoint)
	return strings.Contains(e, "localhost") ||
		strings.Contains(e, "127.0.0.1") ||
		strings.Contains(e, "[::1]") ||
		strings.HasPrefix(e, "http://0.0.0.0")
}
