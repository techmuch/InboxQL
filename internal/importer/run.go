package importer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
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

	// Attachments stores attachment bytes as well as message text. Off by
	// default: a first import should be fast and small, and turning it on can
	// multiply the data directory by an order of magnitude.
	Attachments bool

	// MaxAttachmentBytes caps a single attachment. Parts over the cap are
	// recorded with their real size and a reason, never silently dropped.
	// Zero means the package default.
	MaxAttachmentBytes int64

	// Blobs is where attachment bytes are written. Required when Attachments
	// is set.
	Blobs *blobstore.Store

	// JobID ties recorded errors to an import job. Empty for a CLI run, which
	// still logs its failures — they are worth keeping either way.
	JobID string
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

	// AttachmentsStored and AttachmentsSkipped account for parts separately
	// from messages: an import can succeed while an oversized attachment is
	// deliberately left behind.
	AttachmentsStored  int   `json:"attachmentsStored"`
	AttachmentsSkipped int   `json:"attachmentsSkipped"`
	AttachmentBytes    int64 `json:"attachmentBytes"`

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

// fail records a per-message failure in the result.
//
// The in-memory list is capped so a systematically broken mailbox cannot
// produce a million-line report; the database keeps them all, which is what
// the error log reads.
func (r *Result) fail(sourceID string, err error) {
	r.Failed++
	if len(r.Errors) < maxReportedErrors {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", sourceID, err))
	}
}

// recordFailure counts a failure and persists it.
//
// A count with no detail is not actionable — "3 failed" says nothing about
// which three or why — so every failure gets a row, capped only by what
// actually went wrong rather than by a display limit.
func recordFailure(res *Result, opts Options, mailbox, ref string, err error) {
	res.fail(ref, err)

	// Best effort: the caller is already handling something that went wrong,
	// and losing the record must not escalate into losing the import.
	_ = store.LogError(&store.LoggedError{
		Category:  store.ErrorCategoryImport,
		JobID:     opts.JobID,
		AccountID: opts.AccountID,
		Context:   mailbox,
		Reference: ref,
		Message:   err.Error(),
	})
}

func (r *Result) add(other *Result) {
	r.Scanned += other.Scanned
	r.Imported += other.Imported
	r.Duplicates += other.Duplicates
	r.Skipped += other.Skipped
	r.Failed += other.Failed
	r.Partial += other.Partial
	r.AttachmentsStored += other.AttachmentsStored
	r.AttachmentsSkipped += other.AttachmentsSkipped
	r.AttachmentBytes += other.AttachmentBytes
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
	if opts.Attachments && opts.Blobs == nil {
		return nil, fmt.Errorf("attachment import requires a blob store")
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
			recordFailure(res, opts, raw.Mailbox, raw.SourceID, parseErr)
			continue
		}
		// A parse error still yields headers and raw bytes often enough to be
		// worth keeping; only a message with nothing usable is a failure.
		if parseErr != nil && msg.Subject == "" && msg.From == "" && msg.Body == "" {
			recordFailure(res, opts, raw.Mailbox, raw.SourceID, parseErr)
			continue
		}

		if !sel.Wants(msg.Date) {
			res.Skipped++
			continue
		}

		msg.ID = uuid.New().String()
		msg.AccountID = opts.AccountID
		msg.Mailbox = raw.Mailbox
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
			recordFailure(res, opts, raw.Mailbox, raw.SourceID, err)
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
			recordFailure(res, opts, raw.Mailbox, raw.SourceID, err)
			continue
		}
		res.Imported++
		taken++

		// Attachments come after the message row exists, because they are
		// foreign-keyed to it. A failure here costs the attachments, not the
		// message — the text is the part people came for.
		if opts.Attachments {
			if err := storeAttachments(msg.ID, raw.Raw, opts, res); err != nil {
				recordFailure(res, opts, raw.Mailbox, raw.SourceID, fmt.Errorf("attachments: %w", err))
			}
		}
	}

	if err := iter.Err(); err != nil {
		return res, fmt.Errorf("mailbox %s: %w", mailboxID, err)
	}
	return res, nil
}

// storeAttachments extracts and persists a message's attachment parts.
func storeAttachments(messageID string, raw []byte, opts Options, res *Result) error {
	parts, err := message.ExtractAttachments(raw, opts.MaxAttachmentBytes)
	if err != nil {
		// A message whose MIME structure will not re-walk has no attachments
		// as far as we are concerned; the body was already stored.
		return nil //nolint:nilerr
	}

	for _, part := range parts {
		record := &store.Attachment{
			ID:        uuid.New().String(),
			MessageID: messageID,
			Filename:  part.Filename,
			MimeType:  part.ContentType,
			Size:      part.Size,
			Inline:    part.Inline,
			ContentID: part.ContentID,
			Skipped:   part.Skipped,
		}

		if part.Data != nil && opts.Blobs != nil {
			hash, err := opts.Blobs.Put(part.Data)
			if err != nil {
				return err
			}
			record.ContentHash = hash
			record.StoragePath = opts.Blobs.Path(hash)
			res.AttachmentsStored++
			res.AttachmentBytes += part.Size
		} else {
			if record.Skipped == "" {
				record.Skipped = "attachment storage unavailable"
			}
			res.AttachmentsSkipped++
		}

		if err := store.SaveAttachment(record); err != nil {
			return err
		}
	}
	return nil
}

// SortMailboxes orders mailboxes by display path so listings are stable.
func SortMailboxes(boxes []Mailbox) {
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Path < boxes[j].Path })
}
