package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
	"github.com/user/inboxql/internal/vault"
)

func init() {
	register(&Command{
		Name:    "doctor",
		Summary: "diagnose the installation",
		Usage: `iql doctor [--json] [--skip-network]

Runs health checks and exits non-zero if any of them fail, so it is usable
from monitoring and CI.

Checks: data directory writable, database reachable and integral, schema
version matches the binary, vault key present with sane permissions, every
account password decryptable, and IMAP reachability per account.

Flags:
  --skip-network   skip IMAP reachability, which is the slow part

Exit codes:
  0  all checks passed (warnings may still be present)
  1  at least one check failed`,
		Run: runDoctor,
	})
}

// checkStatus is the outcome of a single check.
type checkStatus string

const (
	statusOK   checkStatus = "ok"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	Remedy string      `json:"remedy,omitempty"`
}

// checks accumulate rather than short-circuit: the point of doctor is to show
// everything that is wrong in one pass, not the first thing.
type report struct {
	Checks []check `json:"checks"`
	// notConfigured means the data directory was never initialised, as
	// opposed to initialised and unhealthy. The two deserve different exit
	// codes: one is answered by running init, the other by investigating.
	notConfigured bool
}

func (r *report) add(name string, status checkStatus, detail string, remedy ...string) {
	c := check{Name: name, Status: status, Detail: detail}
	if len(remedy) > 0 {
		c.Remedy = remedy[0]
	}
	r.Checks = append(r.Checks, c)
}

func (r *report) failed() bool {
	for _, c := range r.Checks {
		if c.Status == statusFail {
			return true
		}
	}
	return false
}

func runDoctor(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	skipNetwork := fs.Bool("skip-network", false, "skip IMAP reachability checks")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	rep := &report{}

	// --- data directory ---------------------------------------------------
	if info, err := os.Stat(ctx.DataDir); err != nil {
		rep.add("data directory", statusFail, fmt.Sprintf("%s is not accessible: %v", ctx.DataDir, err),
			fmt.Sprintf("iql init --data %s", ctx.DataDir))
		// Nothing has been set up here, which the contract in AGENTS.md
		// distinguishes from "set up and unhealthy": exit 5 tells a script to
		// run init rather than to investigate a failure.
		rep.notConfigured = true
		return finishDoctor(ctx, rep)
	} else if !info.IsDir() {
		rep.add("data directory", statusFail, fmt.Sprintf("%s is not a directory", ctx.DataDir))
		return finishDoctor(ctx, rep)
	}

	probe := filepath.Join(ctx.DataDir, ".iql-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		rep.add("data directory", statusFail, fmt.Sprintf("%s is not writable: %v", ctx.DataDir, err))
	} else {
		os.Remove(probe)
		rep.add("data directory", statusOK, ctx.DataDir)
	}

	// --- database ---------------------------------------------------------
	if err := ctx.OpenStore(); err != nil {
		rep.add("database", statusFail, err.Error(), fmt.Sprintf("iql init --data %s", ctx.DataDir))
		var cliErr *Error
		if errors.As(err, &cliErr) && cliErr.Code == ExitNotConfigured {
			rep.notConfigured = true
		}
		return finishDoctor(ctx, rep)
	}
	defer store.CloseDB()
	rep.add("database", statusOK, ctx.dbPath())

	// Whether the full-text index exists depends on the build, not the
	// database, so "search is slow" and "this binary has no index" are the same
	// fact — and only one of them is visible without being told.
	if store.FullTextAvailable() {
		indexed, total, err := store.FullTextCoverage()
		switch {
		case err != nil:
			rep.add("full-text index", statusWarn, err.Error())
		case indexed < total:
			// A partial index is worse than none, because it fails quietly and
			// backwards: a text search matches too little, so its negation
			// matches too much and returns exactly what it was told to exclude.
			rep.add("full-text index", statusFail,
				fmt.Sprintf("only %d of %d messages are indexed; text searches and their negations will both be wrong",
					indexed, total),
				"iql maintenance reindex")
		default:
			rep.add("full-text index", statusOK,
				fmt.Sprintf("%d message(s) indexed", indexed))
		}
	} else {
		rep.add("full-text index", statusWarn,
			"this binary was built without FTS5; text queries fall back to substring scans",
			"rebuild with `go build -tags sqlite_fts5`, or use a released binary")
	}

	// An account whose own address matches nothing in its mailbox breaks
	// me(), folder:sent and the Top Senders exclusion all at once, and every
	// one of them fails by returning nothing rather than by complaining.
	// Mail whose account was deleted, or never existed. Same silent emptiness
	// as a mismatched address, by a different route.
	if orphans, err := store.OrphanedMessages(); err != nil {
		rep.add("message ownership", statusWarn, err.Error())
	} else if len(orphans) > 0 {
		var total int64
		names := make([]string, 0, len(orphans))
		for id, n := range orphans {
			total += n
			names = append(names, fmt.Sprintf("%s (%d)", id, n))
		}
		sort.Strings(names)
		rep.add("message ownership", statusFail,
			fmt.Sprintf("%d message(s) belong to accounts that do not exist: %s — me(), folder:sent and the Top Senders exclusion all match nothing for them",
				total, strings.Join(names, ", ")),
			"re-create the account with that id, or re-import the mail under a real one")
	}

	if coverage, err := store.AccountAddressCoverage(); err != nil {
		rep.add("account address", statusWarn, err.Error())
	} else {
		for _, c := range coverage {
			switch {
			case len(c.Addresses) == 0 && c.Messages > 0:
				rep.add("account address: "+c.AccountID, statusWarn,
					"no address configured, so me() and Sent cannot work",
					"iql account add --email <your address>")
			case c.Messages > 0 && c.Matched == 0:
				rep.add("account address: "+c.AccountID, statusFail,
					fmt.Sprintf("%s appears in none of this account's %d messages — me(), folder:sent and the Top Senders exclusion all match nothing",
						strings.Join(c.Addresses, ", "), c.Messages),
					"check the address with `iql account list`, or `iql query \"| top to 5\"` to see who this mail is actually addressed to")
			}
		}
	}

	// Attachments that were never extracted are invisible in exactly the way
	// that reads as "this mail has no attachments" rather than "nobody looked" —
	// every attachment filter, count and preview reports nothing, and nothing
	// anywhere says why. The bytes are still in the stored raw message, so this
	// is recoverable rather than lost.
	if pending, err := store.UnextractedAttachments(); err != nil {
		rep.add("attachments", statusWarn, err.Error())
	} else if pending > 0 {
		rep.add("attachments", statusWarn,
			fmt.Sprintf("%d message(s) carry attachments that were never extracted, so they appear nowhere in the app",
				pending),
			fmt.Sprintf("iql maintenance attachments --data %s", ctx.DataDir))
	} else {
		rep.add("attachments", statusOK, "every stored message has been walked for attachments")
	}

	// Files nobody has read are invisible to search in the same way
	// unextracted attachments are invisible everywhere: the query returns
	// nothing and nothing says why.
	if text, err := store.AttachmentTextProgress(); err != nil {
		rep.add("attachment text", statusWarn, err.Error())
	} else if text.Files == 0 {
		rep.add("attachment text", statusOK, "no stored files to read")
	} else if text.Pending > 0 {
		rep.add("attachment text", statusWarn,
			fmt.Sprintf("%d of %d file(s) have never been read, so their contents match nothing",
				text.Pending, text.Files),
			fmt.Sprintf("iql maintenance text --data %s", ctx.DataDir))
	} else {
		detail := fmt.Sprintf("%d searchable", text.WithText)
		if text.NoText > 0 {
			// Not a problem to fix. A scan is a picture of a page, and saying
			// so is the difference between "nothing found it" and "there is
			// nothing to find until something reads the picture".
			detail += fmt.Sprintf(", %d scanned with no text layer", text.NoText)
		}
		if text.NoReader > 0 {
			// Images, archives, spreadsheets. Ordinary contents of a mailbox,
			// not damage — which is what one merged "unreadable" number made
			// them look like.
			detail += fmt.Sprintf(", %d with no reader (images and the like)", text.NoReader)
		}
		if text.Failed > 0 {
			detail += fmt.Sprintf(", %d could not be read", text.Failed)
		}
		rep.add("attachment text", statusOK, detail)
	}

	if version, err := store.SchemaVersionOnDisk(); err != nil {
		rep.add("schema version", statusFail, err.Error())
	} else if version != store.SchemaVersion {
		// Lower means migrations did not run; higher means the database was
		// written by a newer binary and this one may not understand it.
		status := statusFail
		remedy := "run any iql command with this binary to apply migrations"
		if version > store.SchemaVersion {
			remedy = "this database was written by a newer InboxQL; upgrade the binary"
		}
		rep.add("schema version", status,
			fmt.Sprintf("database is v%d, binary expects v%d", version, store.SchemaVersion), remedy)
	} else {
		rep.add("schema version", statusOK, fmt.Sprintf("v%d", version))
	}

	if err := store.IntegrityCheck(); err != nil {
		rep.add("database integrity", statusFail, err.Error(),
			"restore from a backup: iql restore <file>")
	} else {
		rep.add("database integrity", statusOK, "integrity_check passed")
	}

	// --- vault ------------------------------------------------------------
	keyPath := filepath.Join(ctx.DataDir, vault.KeyFileName)
	if info, err := os.Stat(keyPath); err != nil {
		rep.add("vault key", statusFail, fmt.Sprintf("%s is missing", keyPath),
			"account passwords cannot be decrypted without it; restore it from a backup")
	} else if perm := info.Mode().Perm(); perm&0o077 != 0 {
		rep.add("vault key", statusWarn, fmt.Sprintf("%s has mode %#o", keyPath, perm),
			fmt.Sprintf("chmod 600 %s", keyPath))
	} else {
		rep.add("vault key", statusOK, keyPath)
	}

	accounts, err := store.ListAccounts()
	if err != nil {
		rep.add("accounts", statusFail, fmt.Sprintf("cannot list accounts: %v", err))
		return finishDoctor(ctx, rep)
	}

	// ListAccounts blanks a password it cannot decrypt and logs a warning, so a
	// configured account with an empty password here means the vault key does
	// not match what the row was sealed with.
	undecryptable := 0
	for _, acc := range accounts {
		if acc.Host != "" && acc.Password == "" {
			undecryptable++
		}
	}
	switch {
	case len(accounts) == 0:
		rep.add("account credentials", statusWarn, "no accounts configured",
			"iql account add")
	case undecryptable > 0:
		rep.add("account credentials", statusFail,
			fmt.Sprintf("%d of %s could not be decrypted", undecryptable, count(len(accounts), "account password", "account passwords")),
			"the vault key does not match these rows; restore the original vault.key or re-enter the passwords with `iql account add`")
	default:
		rep.add("account credentials", statusOK,
			fmt.Sprintf("%s, all decryptable", count(len(accounts), "account", "accounts")))
	}

	// --- IMAP reachability ------------------------------------------------
	if *skipNetwork {
		rep.add("imap reachability", statusWarn, "skipped (--skip-network)")
	} else {
		for _, acc := range accounts {
			name := fmt.Sprintf("imap: %s", acc.ID)
			if acc.Host == "" {
				rep.add(name, statusWarn, "no host configured")
				continue
			}
			start := time.Now()
			c, err := sync.ConnectIMAP(acc)
			if err != nil {
				rep.add(name, statusFail, fmt.Sprintf("%s:%d — %v", acc.Host, acc.Port, err),
					fmt.Sprintf("iql account verify %s", acc.ID))
				continue
			}
			c.Logout()
			rep.add(name, statusOK,
				fmt.Sprintf("%s:%d — connected and authenticated in %s",
					acc.Host, acc.Port, time.Since(start).Round(time.Millisecond)))
		}
	}

	// --- LLM --------------------------------------------------------------
	// Not configuring an LLM is a legitimate choice: search, read and draft all
	// work without one, so this is informational rather than a failure.
	if cfg, err := store.GetLLMConfig(); err != nil {
		rep.add("llm provider", statusWarn, fmt.Sprintf("cannot read settings: %v", err))
	} else if cfg.Provider == "" {
		rep.add("llm provider", statusWarn, "not configured — analyze and draft will emit context instead of prose",
			"iql llm configure --provider ollama --model llama3")
	} else {
		rep.add("llm provider", statusOK, fmt.Sprintf("%s (%s)", cfg.Provider, cfg.Model))
	}

	return finishDoctor(ctx, rep)
}

func finishDoctor(ctx *Context, rep *report) error {
	if ctx.JSON {
		if err := ctx.EmitJSON(rep); err != nil {
			return err
		}
	} else {
		p := ctx.Printer()
		for _, c := range rep.Checks {
			state := ui.OK
			switch c.Status {
			case statusWarn:
				state = ui.Warn
			case statusFail:
				state = ui.Bad
			}
			p.Status(state, c.Name, c.Detail)
			if c.Remedy != "" && c.Status != statusOK {
				// The remedy is the actionable half, so it is indented under
				// the finding rather than crammed onto the same line.
				p.Printf("        %s %s\n", p.Dim("try:"), c.Remedy)
			}
		}
	}
	if rep.notConfigured {
		return Fail(ExitNotConfigured, "not initialised — run `iql init --data %s`", ctx.DataDir)
	}
	if rep.failed() {
		return &Error{Code: ExitError, Err: fmt.Errorf("one or more checks failed")}
	}
	return nil
}
