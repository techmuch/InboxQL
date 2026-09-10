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
}

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
	MAX(m.date) AS last_seen`

// AttachmentSelectList is the column list scanAttachmentFile expects.
const AttachmentSelectList = attachmentFileColumns

// The compiler is told the file column list once, as it is for contacts: the
// schema lives here, not there.
func init() { query.SetAttachmentSelectList(AttachmentSelectList) }

func scanAttachmentFile(scan func(...any) error) (*AttachmentFile, error) {
	f := &AttachmentFile{}
	var first, last sql.NullInt64
	if err := scan(&f.Key, &f.ContentHash, &f.Filename, &f.MimeType, &f.Size,
		&f.Inline, &f.StoragePath, &f.Skipped, &f.MessageID, &f.Subject, &f.From,
		&f.Messages, &f.Threads, &f.Names, &first, &last); err != nil {
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
