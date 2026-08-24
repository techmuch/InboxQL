package importer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/user/uea/internal/message"
	"github.com/user/uea/internal/store"
)

// Options controls one import run.
type Options struct {
	// AccountID owns the imported messages. Required.
	//
	// Worth knowing what this means: messages.account_id is NOT NULL with
	// ON DELETE CASCADE, so removing this account later deletes everything
	// imported under it. An archive you want to outlive your mailbox config
	// belongs in its own account.
	AccountID string

	// Selection narrows which messages are taken.
	Selection Selection

	// DryRun reads and parses everything and reports what would happen,
	// without writing a single row.
	DryRun bool
}

// Result accounts for every message an import touched.
//
// Every message is in exactly one bucket. That is the point: the old import
// path could report success while INSERT OR IGNORE silently dropped rows, and
// a total that does not add up is how you discover that.
type Result struct {
	Scanned    int `json:"scanned"`
	Imported   int `json:"imported"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`

	// Partial counts messages the client never fully downloaded. They are
	// skipped rather than stored as empty.
	Partial int `json:"partial"`

	Bytes    int64         `json:"bytes"`
	Duration time.Duration `json:"-"`
	DryRun   bool          `json:"dryRun"`

	// Errors holds the first few per-message failures, for diagnosis. Capped
	// so a systematically broken mailbox cannot produce a million-line report.
	Errors []string `json:"errors,omitempty"`

	// Mailboxes breaks the totals down per mailbox.
	Mailboxes map[string]*Result `json:"mailboxes,omitempty"`
}

const maxReportedErrors = 20

func (r *Result) fail(sourceID string, err error) {
	r.Failed++
	if len(r.Errors) < maxReportedErrors {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", sourceID, err))
	}
}

func (r *Result) add(other *Result) {
	r.Scanned += other.Scanned
	r.Imported += other.Imported
	r.Duplicates += other.Duplicates
	r.Skipped += other.Skipped
	r.Failed += other.Failed
	r.Partial += other.Partial
	r.Bytes += other.Bytes
	for _, e := range other.Errors {
		if len(r.Errors) < maxReportedErrors {
			r.Errors = append(r.Errors, e)
		}
	}
}

// Total is the sum of every outcome bucket. It must equal Scanned; if it does
// not, a message went missing and the accounting is wrong.
func (r *Result) Total() int {
	return r.Imported + r.Duplicates + r.Skipped + r.Failed
}

// Run imports the given mailboxes from a source.
//
// Per-message failures never abort the run: a single unparseable file in a
// forty-thousand message archive should cost that one message, not the import.
// Only a context cancellation or a source-level error stops early.
func Run(ctx context.Context, src Source, mailboxIDs []string, opts Options, progress ProgressFunc) (*Result, error) {
	if opts.AccountID == "" {
		return nil, fmt.Errorf("an account is required to import into")
	}
	acc, err := store.GetAccount(opts.AccountID)
	if err != nil {
		return nil, fmt.Errorf("cannot load account %s: %w", opts.AccountID, err)
	}
	if acc == nil {
		return nil, fmt.Errorf("no account with id %q", opts.AccountID)
	}

	started := time.Now()
	total := &Result{DryRun: opts.DryRun, Mailboxes: map[string]*Result{}}

	// The limit spans the whole run rather than resetting per mailbox: asking
	// for 100 messages across three folders should give 100, not 300.
	remaining := opts.Selection.Limit

	for _, mailboxID := range mailboxIDs {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		if opts.Selection.Limit > 0 && remaining <= 0 {
			break
		}

		sel := opts.Selection
		sel.Limit = remaining

		got, err := runMailbox(ctx, src, mailboxID, opts, sel, progress)
		if got != nil {
			total.Mailboxes[mailboxID] = got
			total.add(got)
			if opts.Selection.Limit > 0 {
				remaining -= got.Imported + got.Duplicates
			}
		}
		if err != nil {
			total.Duration = time.Since(started)
			return total, err
		}
	}

	total.Duration = time.Since(started)
	return total, nil
}

func runMailbox(ctx context.Context, src Source, mailboxID string, opts Options, sel Selection, progress ProgressFunc) (*Result, error) {
	iter, err := src.Open(ctx, mailboxID, sel)
	if err != nil {
		return nil, fmt.Errorf("cannot open mailbox %s: %w", mailboxID, err)
	}
	defer iter.Close()

	res := &Result{DryRun: opts.DryRun}
	taken := 0

	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if sel.Limit > 0 && taken >= sel.Limit {
			break
		}

		raw := iter.Message()
		res.Scanned++
		res.Bytes += int64(len(raw.Raw))

		progress.Report(Progress{
			Mailbox: raw.Mailbox,
			Current: res.Scanned,
			Message: raw.SourceID,
		})

		// A message the client never finished downloading has no body to
		// import; storing it would create a permanent empty record that looks
		// like data loss later.
		if raw.Partial {
			res.Partial++
			res.Skipped++
			continue
		}

		msg, parseErr := message.ParseRFC822(raw.Raw)
		if msg == nil {
			res.fail(raw.SourceID, parseErr)
			continue
		}
		// A parse error still yields headers and raw bytes often enough to be
		// worth keeping; only a message with nothing usable is a failure.
		if parseErr != nil && msg.Subject == "" && msg.From == "" && msg.Body == "" {
			res.fail(raw.SourceID, parseErr)
			continue
		}

		if !sel.Wants(msg.Date) {
			res.Skipped++
			continue
		}

		msg.ID = uuid.New().String()
		msg.AccountID = opts.AccountID
		msg.Flags = raw.Flags
		msg.Size = uint32(len(raw.Raw))
		if msg.InternalDate.IsZero() {
			msg.InternalDate = msg.Date
		}
		// Imported mail has no IMAP UID. Zero rather than a synthetic value, so
		// nothing mistakes it for a real high-water mark during a later sync.
		msg.UID = 0
		msg.Rehash()

		exists, err := store.MessageExistsByContentHash(opts.AccountID, msg.ContentHash)
		if err != nil {
			res.fail(raw.SourceID, err)
			continue
		}
		if exists {
			res.Duplicates++
			taken++
			continue
		}

		if opts.DryRun {
			res.Imported++
			taken++
			continue
		}

		if err := store.SaveMessage(msg); err != nil {
			res.fail(raw.SourceID, err)
			continue
		}
		res.Imported++
		taken++
	}

	if err := iter.Err(); err != nil {
		return res, fmt.Errorf("mailbox %s: %w", mailboxID, err)
	}
	return res, nil
}

// SortMailboxes orders mailboxes by display path so listings are stable.
func SortMailboxes(boxes []Mailbox) {
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Path < boxes[j].Path })
}
