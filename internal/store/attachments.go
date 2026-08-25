package store

import (
	"database/sql"
	"time"
)

// Attachment is one stored attachment row.
//
// StoragePath is empty when the bytes were not kept. Size and Filename are
// still populated in that case, so the record says what the message carried
// even when InboxQL chose not to hold it.
type Attachment struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"messageId"`
	Filename    string    `json:"filename"`
	MimeType    string    `json:"mimeType"`
	Size        int64     `json:"size"`
	ContentHash string    `json:"contentHash,omitempty"`
	StoragePath string    `json:"storagePath,omitempty"`
	Inline      bool      `json:"inline"`
	ContentID   string    `json:"contentId,omitempty"`
	Skipped     string    `json:"skipped,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Stored reports whether the attachment's bytes are actually on disk.
func (a *Attachment) Stored() bool { return a.StoragePath != "" }

// SaveAttachment inserts an attachment record.
func SaveAttachment(a *Attachment) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	_, err := db.Exec(`
		INSERT INTO attachments (id, message_id, filename, mime_type, size,
		                         content_hash, storage_path, inline, content_id,
		                         skipped, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING;
	`, a.ID, a.MessageID, a.Filename, a.MimeType, a.Size,
		a.ContentHash, nullIfEmpty(a.StoragePath), a.Inline,
		nullIfEmpty(a.ContentID), nullIfEmpty(a.Skipped), a.CreatedAt.UnixMilli())
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const attachmentColumns = `id, message_id, filename, mime_type, size,
	content_hash, storage_path, inline, content_id, skipped, created_at`

func scanAttachment(scan func(...any) error) (*Attachment, error) {
	a := &Attachment{}
	var storagePath, contentID, skipped sql.NullString
	var created int64
	if err := scan(&a.ID, &a.MessageID, &a.Filename, &a.MimeType, &a.Size,
		&a.ContentHash, &storagePath, &a.Inline, &contentID, &skipped, &created); err != nil {
		return nil, err
	}
	a.StoragePath = storagePath.String
	a.ContentID = contentID.String
	a.Skipped = skipped.String
	a.CreatedAt = time.UnixMilli(created)
	return a, nil
}

// ListAttachments returns a message's attachments.
func ListAttachments(messageID string) ([]*Attachment, error) {
	rows, err := db.Query("SELECT "+attachmentColumns+" FROM attachments WHERE message_id = ? ORDER BY filename", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Attachment
	for rows.Next() {
		a, err := scanAttachment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachmentStats totals what is recorded, for doctor and the settings UI.
func AttachmentStats() (count int, stored int, bytes int64, err error) {
	err = db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN storage_path IS NOT NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(size), 0)
		FROM attachments`).Scan(&count, &stored, &bytes)
	return count, stored, bytes, err
}

// ReferencedBlobs returns every content hash still pointed at by a row.
//
// The input to a sweep for unreferenced blobs: content addressing means a blob
// may back any number of messages, so deletion is never a local decision.
func ReferencedBlobs() (map[string]struct{}, error) {
	rows, err := db.Query("SELECT DISTINCT content_hash FROM attachments WHERE storage_path IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]struct{}{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out[h] = struct{}{}
	}
	return out, rows.Err()
}
