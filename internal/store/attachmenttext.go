package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/filetext"
)

// AttachmentExtraction records what happened when a file was read.
//
// # Why the record exists even when nothing was found
//
// "We looked and there was no text" and "nobody has looked yet" are different
// facts that produce the same empty result. Without the distinction, every
// extraction pass re-reads every scanned PDF in the mailbox forever, and the
// OCR pass that wants exactly those files has no way to name them.
type AttachmentExtraction struct {
	ContentHash string    `json:"contentHash"`
	Extractor   string    `json:"extractor"`
	Status      string    `json:"status"`
	Detail      string    `json:"detail,omitempty"`
	Pages       int       `json:"pages"`
	Characters  int       `json:"characters"`
	ExtractedAt time.Time `json:"extractedAt"`
}

// TextProgress is how much of the mailbox has been read.
//
// The three unsuccessful outcomes are counted apart because they mean
// different things and call for different responses. A scan holds no text and
// wants OCR; a JPEG has no reader here and never will; a failure is a bug or a
// broken file. Collapsing them into one "unreadable" number made a mailbox of
// perfectly ordinary photographs look damaged.
type TextProgress struct {
	Files     int64 `json:"files"`
	Extracted int64 `json:"extracted"`
	WithText  int64 `json:"withText"`
	NoText    int64 `json:"noText"`
	NoReader  int64 `json:"noReader"`
	Failed    int64 `json:"failed"`
	Pending   int64 `json:"pending"`
}

// SaveAttachmentText records an extraction and its pages.
//
// Replaces any previous extraction for the same file, so re-running with a
// better extractor supersedes rather than accumulates.
func SaveAttachmentText(hash string, result filetext.Result) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM attachment_text WHERE content_hash = ?", hash); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	for _, page := range result.Pages {
		if page.Text == "" {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO attachment_text (content_hash, page, text, extractor, extracted_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(content_hash, page) DO UPDATE SET
				text = excluded.text, extractor = excluded.extractor,
				extracted_at = excluded.extracted_at`,
			hash, page.Number, page.Text, result.Extractor, now); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO attachment_extractions
			(content_hash, extractor, status, detail, pages, characters, extracted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO UPDATE SET
			extractor = excluded.extractor, status = excluded.status,
			detail = excluded.detail, pages = excluded.pages,
			characters = excluded.characters, extracted_at = excluded.extracted_at`,
		hash, result.Extractor, string(result.Status), result.Detail,
		len(result.Pages), result.Characters(), now); err != nil {
		return err
	}

	return tx.Commit()
}

// GetAttachmentText returns a file's extracted pages, in order.
func GetAttachmentText(hash string) ([]filetext.Page, error) {
	rows, err := db.Query(
		"SELECT page, text FROM attachment_text WHERE content_hash = ? ORDER BY page", hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []filetext.Page
	for rows.Next() {
		var p filetext.Page
		if err := rows.Scan(&p.Number, &p.Text); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetAttachmentExtraction returns what is known about one file's extraction.
func GetAttachmentExtraction(hash string) (*AttachmentExtraction, error) {
	e := &AttachmentExtraction{}
	var at int64
	err := db.QueryRow(`
		SELECT content_hash, extractor, status, detail, pages, characters, extracted_at
		FROM attachment_extractions WHERE content_hash = ?`, hash).
		Scan(&e.ContentHash, &e.Extractor, &e.Status, &e.Detail, &e.Pages, &e.Characters, &at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ExtractedAt = time.UnixMilli(at)
	return e, nil
}

// ExtractAttachmentText reads every stored file that has not been read yet.
//
// # Why this is a command rather than something ingestion does
//
// Reading a file is orders of magnitude more expensive than storing it, and it
// is work that can be redone better later — a PDF that extracts to nothing
// today is the input to an OCR pass tomorrow. Keeping it out of the ingestion
// path means importing a mailbox stays as fast as writing it to disk, and the
// reading happens when somebody asks for it.
//
// redo re-reads files that already have a result, for when the extractor has
// improved. Without it a fix to the reader would never reach the files that
// motivated it.
func ExtractAttachmentText(blobs *blobstore.Store, redo bool, progress func(done, total int64)) (*TextProgress, error) {
	if blobs == nil {
		return nil, fmt.Errorf("reading attachments needs the blob store")
	}

	where := `a.content_hash IS NOT NULL AND a.content_hash != ''
		AND a.storage_path IS NOT NULL AND a.storage_path != ''`
	if !redo {
		where += ` AND NOT EXISTS (
			SELECT 1 FROM attachment_extractions e WHERE e.content_hash = a.content_hash)`
	}

	// One row per file, not per arrival: the same document sent to five people
	// is one extraction.
	rows, err := db.Query(`
		SELECT a.content_hash, MIN(COALESCE(a.mime_type, '')), MIN(COALESCE(a.filename, ''))
		FROM attachments a WHERE ` + where + `
		GROUP BY a.content_hash`)
	if err != nil {
		return nil, err
	}

	type pending struct{ hash, mime, name string }
	var work []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.hash, &p.mime, &p.name); err != nil {
			rows.Close()
			return nil, err
		}
		work = append(work, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	total := int64(len(work))
	for i, w := range work {
		data, err := blobs.Read(w.hash)
		if err != nil {
			// The row says the bytes are on disk and they are not. Recording
			// the failure is what stops the next pass trying again.
			if err := SaveAttachmentText(w.hash, filetext.Result{
				Status: filetext.StatusFailed, Extractor: "none",
				Detail: "the stored bytes could not be read",
			}); err != nil {
				return nil, err
			}
			continue
		}

		if err := SaveAttachmentText(w.hash, filetext.Extract(w.mime, w.name, data)); err != nil {
			return nil, err
		}
		if progress != nil {
			progress(int64(i+1), total)
		}
	}

	return AttachmentTextProgress()
}

// AttachmentTextProgress counts how much of the mailbox has been read.
func AttachmentTextProgress() (*TextProgress, error) {
	p := &TextProgress{}

	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT content_hash) FROM attachments
		WHERE content_hash IS NOT NULL AND content_hash != ''
		  AND storage_path IS NOT NULL AND storage_path != ''`).Scan(&p.Files); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT status, COUNT(*) FROM attachment_extractions GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		p.Extracted += n
		switch status {
		case string(filetext.StatusOK):
			p.WithText += n
		case string(filetext.StatusEmpty):
			p.NoText += n
		case string(filetext.StatusUnsupported):
			p.NoReader += n
		default:
			p.Failed += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	p.Pending = p.Files - p.Extracted
	if p.Pending < 0 {
		p.Pending = 0
	}
	return p, nil
}

// ScannedAttachments lists files that hold no text layer.
//
// The set an OCR pass exists for. Naming it is the whole reason `empty` is a
// status rather than an absent row.
func ScannedAttachments() ([]string, error) {
	rows, err := db.Query(`
		SELECT content_hash FROM attachment_extractions
		WHERE status = ? ORDER BY content_hash`, string(filetext.StatusEmpty))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		out = append(out, hash)
	}
	return out, rows.Err()
}
