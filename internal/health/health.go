// Package health runs the installation's self-checks.
//
// # Why this is not just part of the doctor command
//
// The checks answer "what is wrong, and what would fix it" — a question the
// web UI needs as much as a terminal does, and one that must have exactly one
// answer. A second implementation in a React panel would be a second opinion
// about whether attachments have been extracted, and the two would drift the
// first time either changed. So the checks live here, `iql doctor` renders
// them, and the API serves them.
//
// Each check carries its own remedy. That is what lets a UI put a button on a
// warning without knowing anything about what the warning means: the check
// says what to run, and the maintenance job runner knows how to run it.
package health

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
	"github.com/user/inboxql/internal/vault"
)

// The maintenance jobs a check can ask for.
//
// Named here rather than in the package that runs them, because the checks are
// what decide a job is needed and the runner is what knows how — and a job kind
// nothing ever asks for is a job kind nobody needs.
const (
	JobAttachments = "attachments"
	JobText        = "text"
	JobOCR         = "ocr"
	JobEmbed       = "embed-attachments"
	JobReindex     = "reindex"
	JobGLiNER      = "gliner-install"
	JobAnnotators  = "annotators"
)

// Jobs lists every kind, for validating a request.
var Jobs = []string{JobAttachments, JobText, JobOCR, JobEmbed, JobReindex, JobGLiNER, JobAnnotators}

// Status is the outcome of a single check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check is one thing that was looked at.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	// Remedy is what would fix it, as a command someone could run.
	Remedy string `json:"remedy,omitempty"`
	// Job names the maintenance operation that fixes this, when one does.
	//
	// Separate from Remedy because they are for different readers: Remedy is a
	// sentence for a person at a terminal, Job is an identifier a UI can put
	// behind a button. Deriving one from the other by parsing the command
	// string would be the sort of coupling that breaks when the wording
	// changes.
	Job string `json:"job,omitempty"`
}

// Report is everything the checks found.
//
// Checks accumulate rather than short-circuit: the point is to show everything
// that is wrong in one pass, not the first thing.
type Report struct {
	Checks []Check `json:"checks"`
	// NotConfigured means the data directory was never initialised, as opposed
	// to initialised and unhealthy. The two deserve different answers: one is
	// fixed by running init, the other by investigating.
	NotConfigured bool `json:"notConfigured,omitempty"`
}

func (r *Report) add(name string, status Status, detail string, remedy ...string) {
	c := Check{Name: name, Status: status, Detail: detail}
	if len(remedy) > 0 {
		c.Remedy = remedy[0]
	}
	r.Checks = append(r.Checks, c)
}

// addJob records a check whose remedy is a maintenance job.
func (r *Report) addJob(name string, status Status, detail, remedy, job string) {
	r.Checks = append(r.Checks, Check{
		Name: name, Status: status, Detail: detail, Remedy: remedy, Job: job,
	})
}

// Failed reports whether any check failed, which is what doctor's exit code
// and the UI's overall state both key on.
func (r *Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return true
		}
	}
	return false
}

// Options configures a run.
type Options struct {
	DataDir string
	// DBPath is shown in the database check. Supplied rather than derived so
	// this package does not have to know how the caller names its file.
	DBPath string
	// SkipNetwork skips IMAP reachability, which is the slow part.
	SkipNetwork bool
	// OpenStore opens the database, when the caller has not already.
	//
	// The CLI opens it here so that a failure to open is itself a reported
	// check; the server opened it at startup and passes nil. Injected rather
	// than done here because only the caller knows which of those it is.
	OpenStore func() error
	// NotConfigured reports whether an OpenStore error means "never set up"
	// rather than "broken", which only the caller can tell.
	NotConfigured func(err error) bool
}

// Run performs every check it can, in order.
func Run(opts Options) *Report {
	rep := &Report{}

	if !checkDataDir(rep, opts) {
		return rep
	}
	if opts.OpenStore != nil {
		if err := opts.OpenStore(); err != nil {
			rep.add("database", StatusFail, err.Error(),
				fmt.Sprintf("iql init --data %s", opts.DataDir))
			if opts.NotConfigured != nil && opts.NotConfigured(err) {
				rep.NotConfigured = true
			}
			return rep
		}
	}
	rep.add("database", StatusOK, opts.DBPath)

	checkFullText(rep)
	checkOwnership(rep)
	checkAttachments(rep, opts.DataDir)
	checkGLiNER(rep, opts.DataDir)
	checkSchema(rep)
	checkVault(rep, opts.DataDir)
	accounts := checkAccounts(rep)
	checkIMAP(rep, accounts, opts.SkipNetwork)
	checkLLM(rep)

	return rep
}

func checkDataDir(rep *Report, opts Options) bool {
	info, err := os.Stat(opts.DataDir)
	if err != nil {
		rep.add("data directory", StatusFail,
			fmt.Sprintf("%s is not accessible: %v", opts.DataDir, err),
			fmt.Sprintf("iql init --data %s", opts.DataDir))
		rep.NotConfigured = true
		return false
	}
	if !info.IsDir() {
		rep.add("data directory", StatusFail, fmt.Sprintf("%s is not a directory", opts.DataDir))
		return false
	}

	probe := filepath.Join(opts.DataDir, ".iql-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		rep.add("data directory", StatusFail,
			fmt.Sprintf("%s is not writable: %v", opts.DataDir, err))
	} else {
		os.Remove(probe)
		rep.add("data directory", StatusOK, opts.DataDir)
	}
	return true
}

// checkFullText reports on the search index.
//
// Whether it exists depends on the build, not the database, so "search is
// slow" and "this binary has no index" are the same fact — and only one of
// them is visible without being told.
func checkFullText(rep *Report) {
	if !store.FullTextAvailable() {
		rep.add("full-text index", StatusWarn,
			"this binary was built without FTS5; text queries fall back to substring scans",
			"rebuild with `go build -tags sqlite_fts5`, or use a released binary")
		return
	}

	indexed, total, err := store.FullTextCoverage()
	switch {
	case err != nil:
		rep.add("full-text index", StatusWarn, err.Error())
	case indexed < total:
		// A partial index is worse than none, because it fails quietly and
		// backwards: a text search matches too little, so its negation matches
		// too much and returns exactly what it was told to exclude.
		rep.addJob("full-text index", StatusFail,
			fmt.Sprintf("only %d of %d messages are indexed; text searches and their negations will both be wrong",
				indexed, total),
			"iql maintenance reindex", JobReindex)
	default:
		rep.add("full-text index", StatusOK, fmt.Sprintf("%d message(s) indexed", indexed))
	}
}

// checkOwnership finds mail whose account does not exist.
//
// An account whose own address matches nothing in its mailbox breaks me(),
// folder:sent and the Top Senders exclusion all at once, and every one of them
// fails by returning nothing rather than by complaining.
func checkOwnership(rep *Report) {
	orphans, err := store.OrphanedMessages()
	if err != nil {
		rep.add("message ownership", StatusWarn, err.Error())
		return
	}
	if len(orphans) > 0 {
		var total int64
		names := make([]string, 0, len(orphans))
		for id, n := range orphans {
			total += n
			names = append(names, fmt.Sprintf("%s (%d)", id, n))
		}
		sort.Strings(names)
		rep.add("message ownership", StatusFail,
			fmt.Sprintf("%d message(s) belong to accounts that do not exist: %s — me(), folder:sent and the Top Senders exclusion all match nothing for them",
				total, strings.Join(names, ", ")),
			"re-create the account with that id, or re-import the mail under a real one")
	}

	coverage, err := store.AccountAddressCoverage()
	if err != nil {
		rep.add("account address", StatusWarn, err.Error())
		return
	}
	for _, c := range coverage {
		switch {
		case len(c.Addresses) == 0 && c.Messages > 0:
			rep.add("account address: "+c.AccountID, StatusWarn,
				"no address configured, so me() and Sent cannot work",
				"iql account add --email <your address>")
		case c.Messages > 0 && c.Matched == 0:
			rep.add("account address: "+c.AccountID, StatusFail,
				fmt.Sprintf("%s appears in none of this account's %d messages — me(), folder:sent and the Top Senders exclusion all match nothing",
					strings.Join(c.Addresses, ", "), c.Messages),
				"check the address with `iql account list`, or `iql query \"| top to 5\"` to see who this mail is actually addressed to")
		}
	}
}

// checkAttachments reports what has and has not been read.
//
// Three separate silences, each of which reads as "there is nothing here"
// rather than "nobody has looked": attachments never extracted, files never
// read, and readable files never embedded.
func checkAttachments(rep *Report, dataDir string) {
	if pending, err := store.UnextractedAttachments(); err != nil {
		rep.add("attachments", StatusWarn, err.Error())
	} else if pending > 0 {
		rep.addJob("attachments", StatusWarn,
			fmt.Sprintf("%d message(s) carry attachments that were never extracted, so they appear nowhere in the app", pending),
			fmt.Sprintf("iql maintenance attachments --data %s", dataDir), JobAttachments)
	} else {
		rep.add("attachments", StatusOK, "every stored message has been walked for attachments")
	}

	text, err := store.AttachmentTextProgress()
	if err != nil {
		rep.add("attachment text", StatusWarn, err.Error())
		return
	}
	switch {
	case text.Files == 0:
		rep.add("attachment text", StatusOK, "no stored files to read")
	case text.Pending > 0:
		rep.addJob("attachment text", StatusWarn,
			fmt.Sprintf("%d of %d file(s) have never been read, so their contents match nothing",
				text.Pending, text.Files),
			fmt.Sprintf("iql maintenance text --data %s", dataDir), JobText)
	default:
		detail := fmt.Sprintf("%d searchable", text.WithText)
		if text.NoText > 0 {
			detail += fmt.Sprintf(", %d scanned with no text layer", text.NoText)
		}
		if text.NoReader > 0 {
			detail += fmt.Sprintf(", %d with no reader (images and the like)", text.NoReader)
		}
		if text.Failed > 0 {
			detail += fmt.Sprintf(", %d could not be read", text.Failed)
		}
		rep.add("attachment text", StatusOK, detail)
	}

	// Scans are not a fault to fix — they are pictures of pages — so this is
	// informational, and offered rather than urged.
	if scans, err := store.OCRCandidates(); err == nil && scans > 0 {
		rep.addJob("scanned files", StatusOK,
			fmt.Sprintf("%d file(s) hold no text layer; a vision model could read them", scans),
			"iql ocr", JobOCR)
	}

	checkAttachmentEmbeddings(rep, text.WithText)
}

// checkAttachmentEmbeddings reports whether files can be compared by meaning.
//
// Offered only when there is something to embed and a model to embed with. A
// button that fails because no embedding profile exists teaches nothing; the
// check simply says so instead, which is the difference between a dead end and
// an instruction.
func checkAttachmentEmbeddings(rep *Report, readable int64) {
	if readable == 0 {
		return
	}

	coverage, err := store.AttachmentCoverage()
	if err != nil {
		return
	}
	embedded := 0
	for _, c := range coverage {
		if c.Embedded > embedded {
			embedded = c.Embedded
		}
	}
	if int64(embedded) >= readable {
		rep.add("file similarity", StatusOK,
			fmt.Sprintf("%d readable file(s) embedded; similar: works over file contents", embedded))
		return
	}

	if !hasEmbeddingProfile() {
		rep.add("file similarity", StatusOK,
			fmt.Sprintf("%d readable file(s) are not embedded, so similar: cannot compare them", readable-int64(embedded)),
			"add an embedding model: iql llm profile add <name> --purpose embedding --model <model>")
		return
	}
	rep.addJob("file similarity", StatusOK,
		fmt.Sprintf("%d readable file(s) are not embedded yet; embedding them enables similar:",
			readable-int64(embedded)),
		"iql annotate embed --attachments", JobEmbed)
}

func hasEmbeddingProfile() bool {
	profiles, err := store.ListLLMProfiles()
	if err != nil {
		return false
	}
	for _, p := range profiles {
		if p.Purpose == store.PurposeEmbedding {
			return true
		}
	}
	return false
}

func checkSchema(rep *Report) {
	version, err := store.SchemaVersionOnDisk()
	if err != nil {
		rep.add("schema version", StatusFail, err.Error())
	} else if version != store.SchemaVersion {
		// Lower means migrations did not run; higher means the database was
		// written by a newer binary and this one may not understand it.
		remedy := "run any iql command with this binary to apply migrations"
		if version > store.SchemaVersion {
			remedy = "this database was written by a newer InboxQL; upgrade the binary"
		}
		rep.add("schema version", StatusFail,
			fmt.Sprintf("database is v%d, binary expects v%d", version, store.SchemaVersion), remedy)
	} else {
		rep.add("schema version", StatusOK, fmt.Sprintf("v%d", version))
	}

	if err := store.IntegrityCheck(); err != nil {
		rep.add("database integrity", StatusFail, err.Error(),
			"restore from a backup: iql restore <file>")
	} else {
		rep.add("database integrity", StatusOK, "integrity_check passed")
	}
}

func checkVault(rep *Report, dataDir string) {
	keyPath := filepath.Join(dataDir, vault.KeyFileName)
	info, err := os.Stat(keyPath)
	switch {
	case err != nil:
		rep.add("vault key", StatusFail, fmt.Sprintf("%s is missing", keyPath),
			"account passwords cannot be decrypted without it; restore it from a backup")
	case info.Mode().Perm()&0o077 != 0:
		rep.add("vault key", StatusWarn,
			fmt.Sprintf("%s has mode %#o", keyPath, info.Mode().Perm()),
			fmt.Sprintf("chmod 600 %s", keyPath))
	default:
		rep.add("vault key", StatusOK, keyPath)
	}
}

func checkAccounts(rep *Report) []*account.Account {
	accounts, err := store.ListAccounts()
	if err != nil {
		rep.add("accounts", StatusFail, fmt.Sprintf("cannot list accounts: %v", err))
		return nil
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
		rep.add("account credentials", StatusWarn, "no accounts configured", "iql account add")
	case undecryptable > 0:
		rep.add("account credentials", StatusFail,
			fmt.Sprintf("%d of %s could not be decrypted", undecryptable, plural(len(accounts), "account password")),
			"the vault key does not match these rows; restore the original vault.key or re-enter the passwords with `iql account add`")
	default:
		rep.add("account credentials", StatusOK,
			fmt.Sprintf("%s, all decryptable", plural(len(accounts), "account")))
	}
	return accounts
}

func checkIMAP(rep *Report, accounts []*account.Account, skip bool) {
	if skip {
		rep.add("imap reachability", StatusWarn, "skipped")
		return
	}
	for _, acc := range accounts {
		name := fmt.Sprintf("imap: %s", acc.ID)
		if acc.Host == "" {
			rep.add(name, StatusWarn, "no host configured")
			continue
		}
		start := time.Now()
		c, err := sync.ConnectIMAP(acc)
		if err != nil {
			rep.add(name, StatusFail, fmt.Sprintf("%s:%d — %v", acc.Host, acc.Port, err),
				fmt.Sprintf("iql account verify %s", acc.ID))
			continue
		}
		c.Logout()
		rep.add(name, StatusOK,
			fmt.Sprintf("%s:%d — connected and authenticated in %s",
				acc.Host, acc.Port, time.Since(start).Round(time.Millisecond)))
	}
}

// checkLLM reports the configured model.
//
// Not configuring one is a legitimate choice: search, read and draft all work
// without it, so this is informational rather than a failure.
func checkLLM(rep *Report) {
	cfg, err := store.GetLLMConfig()
	switch {
	case err != nil:
		rep.add("llm provider", StatusWarn, fmt.Sprintf("cannot read settings: %v", err))
	case cfg.Provider == "":
		rep.add("llm provider", StatusWarn,
			"not configured — analyze and draft will emit context instead of prose",
			"iql llm configure --provider ollama --model llama3")
	default:
		rep.add("llm provider", StatusOK, fmt.Sprintf("%s (%s)", cfg.Provider, cfg.Model))
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
