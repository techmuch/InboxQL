package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Draft status values. A draft moves draft -> queued -> sent, or lands in
// failed when delivery is attempted and rejected.
const (
	// DraftStatusDraft is being composed; not eligible for delivery.
	DraftStatusDraft = "draft"
	// DraftStatusQueued is in the outbox awaiting human approval.
	DraftStatusQueued = "queued"
	// DraftStatusSent has been handed to an SMTP server.
	DraftStatusSent = "sent"
	// DraftStatusFailed was approved but delivery failed.
	DraftStatusFailed = "failed"
)

// Draft origin values. Recorded so a human approving the outbox can see what
// composed a message; an agent-written draft deserves a closer read.
const (
	OriginHuman = "human"
	OriginAgent = "agent"
	OriginLLM   = "llm"
)

// Draft is an outgoing message in some stage of composition or delivery.
type Draft struct {
	ID        string   `json:"id"`
	AccountID string   `json:"accountId"`
	InReplyTo string   `json:"inReplyTo,omitempty"`
	To        []string `json:"to"`
	Cc        []string `json:"cc,omitempty"`
	Bcc       []string `json:"bcc,omitempty"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Status    string   `json:"status"`
	Origin    string   `json:"origin"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	QueuedAt  *time.Time `json:"queuedAt,omitempty"`
	SentAt    *time.Time `json:"sentAt,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

// SaveDraft inserts or updates a draft.
func SaveDraft(d *Draft) error {
	to, _ := json.Marshal(d.To)
	cc, _ := json.Marshal(d.Cc)
	bcc, _ := json.Marshal(d.Bcc)

	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	if d.Status == "" {
		d.Status = DraftStatusDraft
	}
	if d.Origin == "" {
		d.Origin = OriginHuman
	}

	_, err := db.Exec(`
		INSERT INTO drafts (id, account_id, in_reply_to, to_addrs, cc_addrs, bcc_addrs,
		                    subject, body, status, origin, created_at, updated_at,
		                    queued_at, sent_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id  = EXCLUDED.account_id,
			in_reply_to = EXCLUDED.in_reply_to,
			to_addrs    = EXCLUDED.to_addrs,
			cc_addrs    = EXCLUDED.cc_addrs,
			bcc_addrs   = EXCLUDED.bcc_addrs,
			subject     = EXCLUDED.subject,
			body        = EXCLUDED.body,
			status      = EXCLUDED.status,
			origin      = EXCLUDED.origin,
			updated_at  = EXCLUDED.updated_at,
			queued_at   = EXCLUDED.queued_at,
			sent_at     = EXCLUDED.sent_at,
			last_error  = EXCLUDED.last_error;
	`, d.ID, d.AccountID, d.InReplyTo, string(to), string(cc), string(bcc),
		d.Subject, d.Body, d.Status, d.Origin,
		d.CreatedAt.UnixMilli(), d.UpdatedAt.UnixMilli(),
		millisOrNil(d.QueuedAt), millisOrNil(d.SentAt), d.LastError)
	return err
}

func millisOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

const draftColumns = `id, account_id, in_reply_to, to_addrs, cc_addrs, bcc_addrs,
	subject, body, status, origin, created_at, updated_at, queued_at, sent_at, last_error`

func scanDraft(scan func(dest ...any) error) (*Draft, error) {
	d := &Draft{}
	var to, cc, bcc string
	var inReplyTo, lastError sql.NullString
	var created, updated int64
	var queued, sent sql.NullInt64

	if err := scan(&d.ID, &d.AccountID, &inReplyTo, &to, &cc, &bcc,
		&d.Subject, &d.Body, &d.Status, &d.Origin,
		&created, &updated, &queued, &sent, &lastError); err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(to), &d.To)
	json.Unmarshal([]byte(cc), &d.Cc)
	json.Unmarshal([]byte(bcc), &d.Bcc)
	d.InReplyTo = inReplyTo.String
	d.LastError = lastError.String
	d.CreatedAt = time.UnixMilli(created)
	d.UpdatedAt = time.UnixMilli(updated)
	if queued.Valid {
		t := time.UnixMilli(queued.Int64)
		d.QueuedAt = &t
	}
	if sent.Valid {
		t := time.UnixMilli(sent.Int64)
		d.SentAt = &t
	}
	return d, nil
}

// GetDraft returns a draft by id, or (nil, nil) when it does not exist.
func GetDraft(id string) (*Draft, error) {
	row := db.QueryRow("SELECT "+draftColumns+" FROM drafts WHERE id = ?", id)
	d, err := scanDraft(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// ListDrafts returns drafts, newest first, optionally filtered by status.
func ListDrafts(status string) ([]*Draft, error) {
	query := "SELECT " + draftColumns + " FROM drafts"
	var args []any
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []*Draft
	for rows.Next() {
		d, err := scanDraft(rows.Scan)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	return drafts, rows.Err()
}

// DeleteDraft removes a draft.
//
// Sent drafts are kept deliberately: they are the only record that InboxQL
// delivered a message, so deleting one destroys the audit trail.
func DeleteDraft(id string) error {
	res, err := db.Exec("DELETE FROM drafts WHERE id = ? AND status != ?", id, DraftStatusSent)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		existing, err := GetDraft(id)
		if err != nil {
			return err
		}
		if existing != nil && existing.Status == DraftStatusSent {
			return fmt.Errorf("draft %s was already sent and is kept as a record", id)
		}
	}
	return nil
}
