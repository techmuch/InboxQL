package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/user/uea/internal/blobstore"
	"github.com/user/uea/internal/importer"
	"github.com/user/uea/internal/importer/applemail"
	"github.com/user/uea/internal/importer/emlfiles"
	"github.com/user/uea/internal/store"
)

func init() {
	register(&Command{
		Name:    "import",
		Summary: "import mail from a desktop client, without modifying it",
		Usage: `uea import <sources|mailboxes|scan|run|eml> [flags]

  sources     which mail clients are present on this machine
  mailboxes   list a client's folders with message counts
  scan        statistics for one or more mailboxes
  run         import messages
  eml         import a folder of exported .eml files

Sources are read-only. Nothing is written, moved or deleted in the client's
store.

Common flags:
  --source <id>        apple-mail (see ` + "`uea import sources`" + `)
  --mailbox <id,...>   mailbox ids from ` + "`uea import mailboxes`" + `
  --account <id>       UEA account to import into
  --limit <n>          newest n messages, across all selected mailboxes
  --since <YYYY-MM-DD> only messages on or after this date
  --until <YYYY-MM-DD> only messages on or before this date
  --deep               scan: parse every message for attachments and contacts
  --dry-run            run: report what would happen, write nothing
  --attachments        run: also store attachment files on disk
  --max-attachment-mb  skip attachments larger than this (default 25)

On macOS, reading ~/Library/Mail needs Full Disk Access. ` + "`uea import sources`" + `
says so explicitly when that is what is missing.

Imported mail belongs to the --account you choose, and messages cascade on
account deletion — so an archive you want to keep belongs in its own account
rather than a live IMAP one.

Attachments are off by default: storing them can multiply the size of the data
directory. With --attachments they are written to <data>/attachments/, addressed
by content so the same file sent to several people is stored once. Remember that
` + "`uea backup`" + ` copies only the database — see ` + "`uea help backup`" + `.`,
		Run: runImport,
	})
}

func runImport(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "sources", "":
		return importSources(ctx, rest)
	case "mailboxes":
		return importMailboxes(ctx, rest)
	case "scan":
		return importScan(ctx, rest)
	case "run":
		return importRun(ctx, rest)
	case "eml":
		return importEML(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want sources, mailboxes, scan, run or eml)", sub)
	}
}

// sourceByID resolves a --source flag. Only sources that discover themselves
// are addressable this way; file-backed ones take an explicit path instead.
func sourceByID(id string) (importer.Source, error) {
	switch id {
	case "apple-mail", "applemail", "apple":
		return applemail.New(), nil
	case "":
		return nil, Fail(ExitUsage, "--source is required (try `uea import sources`)")
	default:
		return nil, Fail(ExitUsage, "unknown source %q (known: apple-mail)", id)
	}
}

func importSources(ctx *Context, args []string) error {
	sources := []importer.Source{applemail.New()}

	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		importer.Detection
	}
	var rows []row
	for _, s := range sources {
		rows = append(rows, row{ID: s.ID(), Name: s.Name(), Detection: s.Detect()})
	}

	if ctx.JSON {
		return ctx.EmitJSON(rows)
	}
	for _, r := range rows {
		switch {
		case r.Readable:
			ctx.Printf("[ ready ] %-12s %s\n", r.ID, r.Detail)
			ctx.Printf("                      %s\n", r.Root)
		case r.Available:
			ctx.Printf("[blocked] %-12s %s\n", r.ID, r.Detail)
			for _, line := range strings.Split(r.Remedy, "\n") {
				ctx.Printf("                      %s\n", line)
			}
		default:
			ctx.Printf("[ absent] %-12s %s\n", r.ID, r.Detail)
		}
	}
	ctx.Printf("\nNo client detected, or blocked by permissions? Export messages from your\n")
	ctx.Printf("mail client into a folder and run: uea import eml <folder> --account <id>\n")
	return nil
}

func importMailboxes(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("import mailboxes", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	sourceID := fs.String("source", "apple-mail", "source id")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	src, err := sourceByID(*sourceID)
	if err != nil {
		return err
	}
	boxes, err := src.Mailboxes(context.Background())
	if err != nil {
		return importFailure(err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"source": src.ID(), "count": len(boxes), "mailboxes": boxes})
	}
	if len(boxes) == 0 {
		ctx.Printf("No mailboxes found.\n")
		return nil
	}
	ctx.Printf("%-10s %10s  %s\n", "MESSAGES", "SIZE", "MAILBOX")
	for _, b := range boxes {
		ctx.Printf("%-10d %10s  %s\n", b.Messages, humanBytes(b.Bytes), b.Path)
		ctx.Printf("%23s  id: %s\n", "", b.ID)
	}
	return nil
}

func importScan(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("import scan", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	sourceID := fs.String("source", "apple-mail", "source id")
	mailboxes := fs.String("mailbox", "", "mailbox ids, comma separated")
	deep := fs.Bool("deep", false, "parse every message for attachments and contacts")
	account := fs.String("account", "", "account to check for duplicates against")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	ids := splitList(*mailboxes)
	if len(ids) == 0 {
		return Fail(ExitUsage, "--mailbox is required (see `uea import mailboxes`)")
	}

	src, err := sourceByID(*sourceID)
	if err != nil {
		return err
	}
	if *account != "" {
		if err := ctx.OpenStore(); err != nil {
			return err
		}
		defer store.CloseDB()
	}

	if err := verifyMailboxes(src, ids); err != nil {
		return err
	}

	depth := importer.ScanFast
	if *deep {
		depth = importer.ScanDeep
	}

	var progress importer.ProgressFunc
	if *deep {
		progress = ctx.importProgress()
	}

	var all []importer.Stats
	for _, id := range ids {
		stats, err := src.Scan(context.Background(), id, depth, progress)
		if err != nil {
			return importFailure(err)
		}
		all = append(all, stats)
	}
	ctx.clearProgress(progress)

	if ctx.JSON {
		return ctx.EmitJSON(all)
	}
	for _, s := range all {
		ctx.Printf("%s\n", s.MailboxID)
		ctx.Printf("  messages        %d\n", s.Messages)
		ctx.Printf("  size            %s\n", humanBytes(s.Bytes))
		if s.Partial > 0 {
			ctx.Printf("  not downloaded  %d (skipped on import)\n", s.Partial)
		}
		if s.Depth == importer.ScanDeep {
			ctx.Printf("  unread          %d\n", s.Unread)
			ctx.Printf("  attachments     %d (%s)\n", s.Attachments, humanBytes(s.AttachmentBytes))
			ctx.Printf("  contacts        %d distinct addresses\n", s.Contacts)
			if !s.Oldest.IsZero() {
				ctx.Printf("  date range      %s to %s\n",
					s.Oldest.Format("2006-01-02"), s.Newest.Format("2006-01-02"))
			}
			if s.Unreadable > 0 {
				ctx.Printf("  unreadable      %d\n", s.Unreadable)
			}
		} else {
			ctx.Printf("  (fast scan — add --deep for attachments, contacts and dates)\n")
		}
		ctx.Printf("\n")
	}
	return nil
}

// importFlags are the options shared by every import verb. Declared once and
// bound to a single flagset: parsing the same argv twice against two different
// flagsets is how `--account` ended up rejected by `run` and accepted by the
// code behind it.
type importFlags struct {
	source      string
	mailbox     string
	account     string
	since       string
	until       string
	limit       int
	dryRun      bool
	attachments bool
	maxAttach   int64
}

func (f *importFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.source, "source", "apple-mail", "source id")
	fs.StringVar(&f.mailbox, "mailbox", "", "mailbox ids, comma separated")
	fs.StringVar(&f.account, "account", "", "UEA account to import into")
	fs.IntVar(&f.limit, "limit", 0, "newest n messages")
	fs.StringVar(&f.since, "since", "", "on or after YYYY-MM-DD")
	fs.StringVar(&f.until, "until", "", "on or before YYYY-MM-DD")
	fs.BoolVar(&f.dryRun, "dry-run", false, "report only, write nothing")
	fs.BoolVar(&f.attachments, "attachments", false, "also store attachment files")
	fs.Int64Var(&f.maxAttach, "max-attachment-mb", 25, "skip attachments larger than this, in MB")
}

// selection converts the date and limit flags into an importer.Selection.
func (f *importFlags) selection() (importer.Selection, error) {
	sel := importer.Selection{Limit: f.limit}
	var err error
	if sel.Since, err = parseDay(f.since); err != nil {
		return sel, Fail(ExitUsage, "--since: %v", err)
	}
	if sel.Until, err = parseDay(f.until); err != nil {
		return sel, Fail(ExitUsage, "--until: %v", err)
	}
	if !sel.Until.IsZero() {
		// An inclusive end date means the whole of that day, not midnight.
		sel.Until = sel.Until.Add(24*time.Hour - time.Nanosecond)
	}
	return sel, nil
}

func importRun(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("import run", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	var f importFlags
	f.bind(fs)
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	ids := splitList(f.mailbox)
	if len(ids) == 0 {
		return Fail(ExitUsage, "--mailbox is required (see `uea import mailboxes`)")
	}
	src, err := sourceByID(f.source)
	if err != nil {
		return err
	}
	if err := verifyMailboxes(src, ids); err != nil {
		return err
	}
	return doImport(ctx, src, ids, &f)
}

func importEML(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("import eml", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	var f importFlags
	f.bind(fs)
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: uea import eml <folder> --account <id>")
	}

	src := emlfiles.New(fs.Arg(0))
	d := src.Detect()
	if !d.Readable {
		return Fail(ExitNotConfigured, "%s", withRemedy(d))
	}
	// Readable but empty is not a failure of the import so much as a wrong
	// folder, and Detect already worked out what to say about it. Reporting a
	// bland "0 imported" would hide that advice.
	if d.Remedy != "" {
		return Fail(ExitNotFound, "%s", withRemedy(d))
	}
	return doImport(ctx, src, []string{"."}, &f)
}

// doImport is the shared tail of `run` and `eml`: once a source and its
// mailboxes are chosen, selection, execution and reporting are identical.
func doImport(ctx *Context, src importer.Source, mailboxIDs []string, f *importFlags) error {
	if f.account == "" {
		return Fail(ExitUsage, "--account is required (see `uea account list`)")
	}
	sel, err := f.selection()
	if err != nil {
		return err
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	opts := importer.Options{
		AccountID:          f.account,
		Selection:          sel,
		DryRun:             f.dryRun,
		Attachments:        f.attachments,
		MaxAttachmentBytes: f.maxAttach << 20,
	}
	if f.attachments {
		opts.Blobs = blobstore.New(ctx.DataDir)
	}

	progress := ctx.importProgress()
	result, runErr := importer.Run(context.Background(), src, mailboxIDs, opts, progress)
	ctx.clearProgress(progress)

	if runErr != nil && result == nil {
		return importFailure(runErr)
	}

	if ctx.JSON {
		if err := ctx.EmitJSON(result); err != nil {
			return err
		}
	} else {
		printImportResult(ctx, result)
	}
	if runErr != nil {
		return importFailure(runErr)
	}
	if result.Failed > 0 {
		return &Error{Code: ExitError, Err: errString("some messages could not be imported")}
	}
	return nil
}

// progressWidth bounds a progress line so overwriting it with spaces actually
// erases it. Mailbox ids are long enough to wrap otherwise, and a wrapped line
// cannot be cleared with a carriage return.
const progressWidth = 72

// importProgress returns a throttled progress reporter, or nil in JSON mode
// where anything on stdout would corrupt the document.
func (c *Context) importProgress() importer.ProgressFunc {
	if c.JSON || !IsStdoutTerminal() {
		return nil
	}
	last := time.Now()
	return func(p importer.Progress) {
		if time.Since(last) < 250*time.Millisecond && p.Current != p.Total {
			return
		}
		last = time.Now()
		line := fmt.Sprintf("  %s … %d", p.Mailbox, p.Current)
		if p.Total > 0 {
			line = fmt.Sprintf("  %s … %d/%d", p.Mailbox, p.Current, p.Total)
		}
		c.Printf("\r%-*s", progressWidth, truncate(line, progressWidth))
	}
}

func (c *Context) clearProgress(p importer.ProgressFunc) {
	if p != nil {
		c.Printf("\r%*s\r", progressWidth, "")
	}
}

func printImportResult(ctx *Context, r *importer.Result) {
	if r.DryRun {
		ctx.Printf("Dry run — nothing was written.\n\n")
	}
	ctx.Printf("  scanned     %d\n", r.Scanned)
	ctx.Printf("  imported    %d\n", r.Imported)
	ctx.Printf("  duplicates  %d (already in this account)\n", r.Duplicates)
	if r.Partial > 0 {
		ctx.Printf("  incomplete  %d (never fully downloaded by the client)\n", r.Partial)
	}
	if r.Skipped > r.Partial {
		ctx.Printf("  skipped     %d (outside the selected dates)\n", r.Skipped-r.Partial)
	}
	if r.Failed > 0 {
		ctx.Printf("  failed      %d\n", r.Failed)
		for _, e := range r.Errors {
			ctx.Printf("                %s\n", e)
		}
	}
	if r.AttachmentsStored > 0 || r.AttachmentsSkipped > 0 {
		ctx.Printf("  attachments %d stored (%s)", r.AttachmentsStored, humanBytes(r.AttachmentBytes))
		if r.AttachmentsSkipped > 0 {
			ctx.Printf(", %d too large or not kept", r.AttachmentsSkipped)
		}
		ctx.Printf("\n")
	}
	ctx.Printf("  read        %s in %s\n", humanBytes(r.Bytes), r.Duration.Round(time.Millisecond))

	// Every message lands in exactly one bucket; if it does not, say so rather
	// than presenting numbers that quietly fail to add up.
	if r.Total() != r.Scanned {
		ctx.Printf("\nWARNING: %d scanned but %d accounted for — please report this.\n",
			r.Scanned, r.Total())
	}
}

// importFailure turns a permission error into advice rather than a syscall name.
func importFailure(err error) error {
	if strings.Contains(err.Error(), importer.ErrPermissionDenied.Error()) {
		return Fail(ExitNotConfigured, "%v", err)
	}
	return Fail(ExitError, "%v", err)
}

// verifyMailboxes rejects ids the source does not know about.
//
// Without this an unknown id walks a directory that is not there, finds
// nothing, and reports a successful import of zero messages — which reads
// exactly like an empty mailbox and is how someone concludes their mail failed
// to import for hours before noticing the typo.
func verifyMailboxes(src importer.Source, ids []string) error {
	known, err := src.Mailboxes(context.Background())
	if err != nil {
		return importFailure(err)
	}

	valid := make(map[string]bool, len(known))
	for _, b := range known {
		valid[b.ID] = true
	}
	for _, id := range ids {
		if !valid[id] {
			return Fail(ExitNotFound,
				"no mailbox %q in %s\n\nRun `uea import mailboxes` to list them.", id, src.Name())
		}
	}
	return nil
}

// withRemedy renders a detection as a message followed by its advice.
func withRemedy(d importer.Detection) string {
	if d.Remedy == "" {
		return d.Detail
	}
	return d.Detail + "\n\n" + d.Remedy
}

func parseDay(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD, got %q", s)
	}
	return t, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
