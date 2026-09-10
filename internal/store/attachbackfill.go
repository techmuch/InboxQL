package store

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
)

// AttachmentCounts reports what one message's parts cost.
type AttachmentCounts struct {
	Stored  int
	Skipped int
	Bytes   int64
}

// StoreAttachments extracts a raw message's parts and records them.
//
// # Why both ingestion paths call this one function
//
// They did not, and that is how attachments came to exist on neither. The
// importer had its own extraction and the IMAP sync had none at all, so a
// synced mailbox reported no attachments however many it carried — and nothing
// anywhere said so. Two implementations of the same derivation is the shape
// this project has been bitten by before; one function called from both is the
// fix that stays fixed.
//
// Idempotent by message: a message that already has rows is left alone, so a
// re-save cannot duplicate them.
func StoreAttachments(messageID string, raw []byte, blobs *blobstore.Store, maxBytes int64) (AttachmentCounts, error) {
	var out AttachmentCounts
	if len(raw) == 0 || !message.MayHaveAttachments(raw) {
		return out, nil
	}
	if maxBytes <= 0 {
		maxBytes = 25 << 20
	}

	var existing int
	if err := db.QueryRow("SELECT COUNT(*) FROM attachments WHERE message_id = ?", messageID).
		Scan(&existing); err != nil {
		return out, err
	}
	if existing > 0 {
		return out, nil
	}

	parts, err := message.ExtractAttachments(raw, maxBytes)
	if err != nil || len(parts) == 0 {
		// A message whose MIME will not re-walk has no attachments as far as
		// this is concerned; the body was stored either way.
		return out, nil //nolint:nilerr
	}

	for _, part := range parts {
		record := &Attachment{
			ID:        uuid.New().String(),
			MessageID: messageID,
			Filename:  part.Filename,
			MimeType:  part.ContentType,
			Size:      part.Size,
			Inline:    part.Inline,
			ContentID: part.ContentID,
			Skipped:   part.Skipped,
		}

		if part.Data != nil && blobs != nil {
			hash, err := blobs.Put(part.Data)
			if err != nil {
				return out, fmt.Errorf("storing %s from %s: %w", part.Filename, messageID, err)
			}
			record.ContentHash = hash
			record.StoragePath = blobs.Path(hash)
			out.Stored++
			out.Bytes += part.Size
		} else {
			if record.Skipped == "" {
				record.Skipped = "attachment storage unavailable"
			}
			out.Skipped++
		}

		if err := SaveAttachment(record); err != nil {
			return out, err
		}
	}
	return out, nil
}

// AttachmentBackfill reports what a recovery pass found.
type AttachmentBackfill struct {
	Examined  int64 `json:"examined"`
	Messages  int64 `json:"messages"`
	Recovered int64 `json:"recovered"`
	Stored    int64 `json:"stored"`
	Skipped   int64 `json:"skipped"`
	Bytes     int64 `json:"bytes"`
	DryRun    bool  `json:"dryRun"`
}

// RecoverAttachments re-extracts attachment parts from mail already imported.
//
// # Why this can work at all
//
// The `header` column does not hold headers. It holds the whole raw message —
// a hundred kilobytes on average, which is most of the database. That is a
// wasteful thing to have done and it is also the only reason this function can
// exist: the MIME parts are still on disk, so attachments that were never
// extracted can be recovered without going back to the original mailbox.
//
// # Why it is a command and not a migration
//
// It re-parses every message and writes every attachment to the blob store.
// On a personal archive that is seconds; on a large mailbox it is minutes and
// gigabytes, and a migration that does either without being asked is a bad
// surprise on the first start after an upgrade.
//
// Idempotent: a message that already has attachment rows is left alone, so
// running it twice costs a scan and changes nothing.
func RecoverAttachments(blobs *blobstore.Store, maxBytes int64, dryRun bool) (*AttachmentBackfill, error) {
	if blobs == nil {
		return nil, fmt.Errorf("attachment recovery needs the blob store")
	}
	if maxBytes <= 0 {
		maxBytes = 25 << 20
	}

	// Ids first, bytes one at a time. The obvious loop — select id and header
	// together and process as the cursor walks — would either hold a read
	// cursor open across every write, or, if buffered, pull the raw text of a
	// whole mailbox into memory at once. The header column holds entire
	// messages, so on a large archive that is gigabytes to recover a few.
	where, args := unextractedCandidates()
	rows, err := db.Query(`SELECT m.id FROM messages m WHERE `+where, args...)
	if err != nil {
		return nil, err
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := &AttachmentBackfill{Examined: int64(len(ids)), DryRun: dryRun}

	for _, id := range ids {
		var raw []byte
		if err := db.QueryRow("SELECT header FROM messages WHERE id = ?", id).Scan(&raw); err != nil {
			return out, err
		}

		if dryRun {
			// Counting only, so nothing is decoded: the cap still reports a
			// part's true size, it just does not pull the bytes into memory.
			parts, err := message.ExtractAttachments(raw, 1)
			if err != nil || len(parts) == 0 {
				continue
			}
			out.Messages++
			for _, part := range parts {
				out.Recovered++
				out.Bytes += part.Size
			}
			continue
		}

		counts, err := StoreAttachments(id, raw, blobs, maxBytes)
		if err != nil {
			return out, err
		}
		if counts.Stored+counts.Skipped == 0 {
			// A message whose MIME will not re-walk has no attachments as far
			// as this is concerned. The body was stored at import either way.
			continue
		}
		out.Messages++
		out.Recovered += int64(counts.Stored + counts.Skipped)
		out.Stored += int64(counts.Stored)
		out.Skipped += int64(counts.Skipped)
		out.Bytes += counts.Bytes
	}

	return out, nil
}

// unextractedCandidates builds the predicate for "carries attachment markers
// and has no attachment rows".
//
// The marker test runs in SQL rather than in Go. Both do the same substring
// scan, but doing it here means a mailbox's worth of raw messages — the header
// column holds entire messages, so this is most of the database — is never
// copied across the driver just to be discarded. The markers come from
// [message.AttachmentMarkers] rather than being written out again here: two
// copies that drifted would disagree about which mail is worth walking, and
// the disagreement would surface as attachments that exist and are never seen.
func unextractedCandidates() (where string, args []any) {
	tests := make([]string, 0, len(message.AttachmentMarkers))
	for _, marker := range message.AttachmentMarkers {
		tests = append(tests, "instr(m.header, ?) > 0")
		args = append(args, marker)
	}
	return `m.header IS NOT NULL AND LENGTH(m.header) > 0
		  AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.message_id = m.id)
		  AND (` + strings.Join(tests, " OR ") + `)`, args
}

// UnextractedAttachments counts messages carrying parts nothing ever recorded.
//
// A doctor check rather than a silent gap: with nothing extracted the whole
// attachment surface reports nothing at all, which reads as "this mail has no
// attachments" rather than "nobody looked".
//
// Candidates are narrowed in SQL and then parsed with a one-byte cap: this
// counts parts, it never decodes them.
func UnextractedAttachments() (messages int64, err error) {
	where, args := unextractedCandidates()
	rows, err := db.Query(`SELECT m.header FROM messages m WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return 0, err
		}
		if parts, err := message.ExtractAttachments(raw, 1); err == nil && len(parts) > 0 {
			messages++
		}
	}
	return messages, rows.Err()
}
