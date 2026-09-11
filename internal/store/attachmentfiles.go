package store

import (
	"database/sql"
	"time"

	"github.com/user/inboxql/internal/query"
)

// AttachmentFile is one distinct file, however many messages carried it.
//
// # Why this is not just Attachment
//
// An `attachments` row is an arrival: one part on one message. A document sent
// to five people is five rows, and listing those reads as five different files
// with the same name. What a person means by "a file" is the bytes, and the
// bytes are hashed on the way in — so the entity is the hash, and the arrivals
// become a count on it.
//
// The counts are derived on read rather than stored. A cached "appears on 3
// messages" is a second answer to a question the attachments table already
// answers, and it is wrong the first time a message is deleted.
type AttachmentFile struct {
	// Key identifies the file. The content hash where there is one; the row's
	// own id where the bytes were never captured, which keeps such a part a
	// file in its own right rather than collapsing all of them into one.
	Key         string `json:"key"`
	ContentHash string `json:"contentHash,omitempty"`
	// Filename, MimeType and MessageID come from the most recent arrival —
	// the one someone means when they remember seeing this last week.
	Filename    string `json:"filename"`
	MimeType    string `json:"mimeType"`
	Size        int64  `json:"size"`
	Inline      bool   `json:"inline"`
	StoragePath string `json:"storagePath,omitempty"`
	Skipped     string `json:"skipped,omitempty"`
	MessageID   string `json:"messageId"`
	Subject     string `json:"subject,omitempty"`
	From        string `json:"from,omitempty"`
	// Messages and Threads are how far this file reached. Distinguished
	// because they answer different questions: the same document quoted down
	// one long thread is not the same as one sent to three separate people.
	Messages int `json:"messages"`
	Threads  int `json:"threads"`
	// Names is how many different filenames this file arrived under, so a UI
	// can say "also known as" without a second query to find out there is
	// nothing to say.
	Names     int       `json:"names"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// TextStatus is what reading the file found: ok, empty, unsupported,
	// failed, or "" for a file nothing has read yet.
	//
	// Carried on the row because the distinction matters to whoever is looking
	// at it: a scanned PDF that says nothing about itself looks like a file
	// whose contents simply do not match, when in fact nothing has ever been
	// able to read them.
	TextStatus string `json:"textStatus,omitempty"`
	TextPages  int    `json:"textPages,omitempty"`
}

// Searchable reports whether the file's contents can be searched.
func (f *AttachmentFile) Searchable() bool { return f.TextStatus == "ok" }

// Scanned reports whether the file was read and found to be images.
func (f *AttachmentFile) Scanned() bool { return f.TextStatus == "empty" }

// Stored reports whether the file's bytes are on disk and openable.
func (f *AttachmentFile) Stored() bool { return f.StoragePath != "" }

// Shared reports whether the same bytes arrived on more than one message.
func (f *AttachmentFile) Shared() bool { return f.Messages > 1 }

// attachmentFileColumns is the grouped select the planner emits.
//
// The bare columns beside MAX(m.date) are deliberate and load-bearing: SQLite
// guarantees that in a query with exactly one MIN or MAX aggregate, bare
// columns are taken from the row that produced it. That is what makes the name
// and message here belong to the newest arrival rather than to an arbitrary
// one. Adding a second MIN/MAX over a different column would silently forfeit
// that guarantee — MIN(m.date) below is inside a subquery for exactly that
// reason, not as a matter of taste.
const attachmentFileColumns = `
	COALESCE(NULLIF(a.content_hash, ''), a.id) AS file_key,
	COALESCE(a.content_hash, ''),
	COALESCE(a.filename, ''),
	COALESCE(a.mime_type, ''),
	a.size,
	a.inline,
	COALESCE(a.storage_path, ''),
	COALESCE(a.skipped, ''),
	a.message_id,
	COALESCE(m.subject, ''),
	COALESCE((SELECT p.address FROM message_participants p
	          WHERE p.message_id = a.message_id AND p.role = 'from' LIMIT 1), ''),
	COUNT(DISTINCT a.message_id) AS messages,
	COUNT(DISTINCT COALESCE(m.thread_key, m.id)) AS threads,
	COUNT(DISTINCT a.filename) AS names,
	(SELECT MIN(m2.date) FROM attachments a2 JOIN messages m2 ON m2.id = a2.message_id
	 WHERE COALESCE(NULLIF(a2.content_hash, ''), a2.id) = COALESCE(NULLIF(a.content_hash, ''), a.id)) AS first_seen,
	COALESCE((SELECT e.status FROM attachment_extractions e
	          WHERE e.content_hash = a.content_hash), '') AS text_status,
	COALESCE((SELECT e.pages FROM attachment_extractions e
	          WHERE e.content_hash = a.content_hash), 0) AS text_pages,
	MAX(m.date) AS last_seen`

// AttachmentSelectList is the column list scanAttachmentFile expects.
const AttachmentSelectList = attachmentFileColumns

// The compiler is told the file column list once, as it is for contacts: the
// schema lives here, not there.
func init() { query.SetAttachmentSelectList(AttachmentSelectList) }

// GetAttachmentFile returns one file by its key, or nil when nothing has it.
//
// The key is a content hash for anything stored, so this is also how a request
// for bytes resolves what it is allowed to serve: a caller holding a hash from
// a URL learns the filename and type this mailbox recorded for it, and never
// takes either from the request.
func GetAttachmentFile(key string) (*AttachmentFile, error) {
	f, err := scanAttachmentFile(db.QueryRow(
		"SELECT "+AttachmentSelectList+
			" FROM attachments a JOIN messages m ON m.id = a.message_id"+
			" WHERE COALESCE(NULLIF(a.content_hash, ''), a.id) = ?"+
			" GROUP BY COALESCE(NULLIF(a.content_hash, ''), a.id)", key).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// AttachmentOccurrence is one arrival of a file: the message it came on, and
// the name it came under there.
type AttachmentOccurrence struct {
	AttachmentID string    `json:"attachmentId"`
	MessageID    string    `json:"messageId"`
	ThreadKey    string    `json:"threadKey"`
	Filename     string    `json:"filename"`
	Subject      string    `json:"subject"`
	From         string    `json:"from"`
	Date         time.Time `json:"date"`
	Mailbox      string    `json:"mailbox,omitempty"`
	Inline       bool      `json:"inline"`
}

// ListAttachmentOccurrences returns every message a file arrived on, newest
// first.
//
// # Why this is a list and not a count
//
// "This file is on 3 messages" is the summary; the question it immediately
// provokes is "which ones". A count that cannot be opened is a dead end, and
// the join that produces it is the same join that produces the rows.
func ListAttachmentOccurrences(key string) ([]*AttachmentOccurrence, error) {
	rows, err := db.Query(`
		SELECT a.id, a.message_id, COALESCE(m.thread_key, m.id),
		       COALESCE(a.filename, ''), COALESCE(m.subject, ''),
		       COALESCE((SELECT p.address FROM message_participants p
		                 WHERE p.message_id = a.message_id AND p.role = 'from' LIMIT 1), ''),
		       m.date, COALESCE(m.mailbox, ''), a.inline
		FROM attachments a JOIN messages m ON m.id = a.message_id
		WHERE COALESCE(NULLIF(a.content_hash, ''), a.id) = ?
		ORDER BY m.date DESC`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*AttachmentOccurrence{}
	for rows.Next() {
		o := &AttachmentOccurrence{}
		var date sql.NullInt64
		if err := rows.Scan(&o.AttachmentID, &o.MessageID, &o.ThreadKey, &o.Filename,
			&o.Subject, &o.From, &date, &o.Mailbox, &o.Inline); err != nil {
			return nil, err
		}
		if date.Valid {
			o.Date = time.UnixMilli(date.Int64)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func scanAttachmentFile(scan func(...any) error) (*AttachmentFile, error) {
	f := &AttachmentFile{}
	var first, last sql.NullInt64
	if err := scan(&f.Key, &f.ContentHash, &f.Filename, &f.MimeType, &f.Size,
		&f.Inline, &f.StoragePath, &f.Skipped, &f.MessageID, &f.Subject, &f.From,
		&f.Messages, &f.Threads, &f.Names, &first, &f.TextStatus, &f.TextPages,
		&last); err != nil {
		return nil, err
	}
	if first.Valid {
		f.FirstSeen = time.UnixMilli(first.Int64)
	}
	if last.Valid {
		f.LastSeen = time.UnixMilli(last.Int64)
	}
	return f, nil
}
