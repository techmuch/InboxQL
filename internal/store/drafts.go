package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Draft status values. A draft moves draft -> queued -> sent, or lands in
// failed when delivery is attempted and rejected.
const (
	// DraftStatusDraft is being composed; not eligible for delivery.
	DraftStatusDraft = "draft"
	// DraftStatusQueued is in the outbox: waiting for approval, or — with a
	// SendAt — sent by a person and inside its Undo window.
	DraftStatusQueued = "queued"
	// DraftStatusSending is being handed to the SMTP server right now.
	DraftStatusSending = "sending"
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
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	InReplyTo string `json:"inReplyTo,omitempty"`
	// ThreadKey is the conversation this draft answers, resolved from
	// InReplyTo on save. Stored rather than joined so that messages, tickets
	// and drafts all name a conversation the same way.
	ThreadKey string   `json:"threadKey,omitempty"`
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
	// SendAt is when a draft someone pressed Send on goes out: the end of
	// its Undo window. Nil for a draft waiting in the outbox for approval.
	SendAt    *time.Time `json:"sendAt,omitempty"`
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

	// Resolved on every save rather than only on insert: a draft can be
	// retargeted while it is being composed, and a stale key would file the
	// reply under the conversation it used to answer.
	if key, err := draftThreadKey(d.InReplyTo); err != nil {
		return err
	} else {
		d.ThreadKey = key
	}

	_, err := db.Exec(`
		INSERT INTO drafts (id, account_id, in_reply_to, thread_key, to_addrs, cc_addrs, bcc_addrs,
		                    subject, body, status, origin, created_at, updated_at,
		                    queued_at, sent_at, last_error, send_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id  = EXCLUDED.account_id,
			in_reply_to = EXCLUDED.in_reply_to,
			thread_key  = EXCLUDED.thread_key,
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
			last_error  = EXCLUDED.last_error,
			send_at     = EXCLUDED.send_at;
	`, d.ID, d.AccountID, d.InReplyTo, nullIfEmpty(d.ThreadKey), string(to), string(cc), string(bcc),
		d.Subject, d.Body, d.Status, d.Origin,
		d.CreatedAt.UnixMilli(), d.UpdatedAt.UnixMilli(),
		millisOrNil(d.QueuedAt), millisOrNil(d.SentAt), d.LastError, millisOrNil(d.SendAt))
	return err
}

func millisOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

const draftColumns = `id, account_id, in_reply_to, thread_key, to_addrs, cc_addrs, bcc_addrs,
	subject, body, status, origin, created_at, updated_at, queued_at, sent_at, last_error, send_at`

func scanDraft(scan func(dest ...any) error) (*Draft, error) {
	d := &Draft{}
	var to, cc, bcc string
	var inReplyTo, threadKey, lastError sql.NullString
	var created, updated int64
	var queued, sent, sendAt sql.NullInt64

	if err := scan(&d.ID, &d.AccountID, &inReplyTo, &threadKey, &to, &cc, &bcc,
		&d.Subject, &d.Body, &d.Status, &d.Origin,
		&created, &updated, &queued, &sent, &lastError, &sendAt); err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(to), &d.To)
	json.Unmarshal([]byte(cc), &d.Cc)
	json.Unmarshal([]byte(bcc), &d.Bcc)
	d.InReplyTo = inReplyTo.String
	d.ThreadKey = threadKey.String
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
	if sendAt.Valid {
		t := time.UnixMilli(sendAt.Int64)
		d.SendAt = &t
	}
	return d, nil
}

// ScheduleSend puts a draft in the outbox to go at a time: the end of its
// Undo window. Pressing Send is the approval.
func ScheduleSend(id string, at time.Time) (*Draft, error) {
	d, err := GetDraft(id)
	if err != nil || d == nil {
		return d, err
	}
	switch d.Status {
	case DraftStatusSent, DraftStatusSending:
		return d, fmt.Errorf("this message has already been sent")
	}
	now := time.Now()
	d.Status = DraftStatusQueued
	if d.QueuedAt == nil {
		d.QueuedAt = &now
	}
	d.SendAt = &at
	d.LastError = ""
	return d, SaveDraft(d)
}

// CancelSend takes back a draft still inside its Undo window. False means it
// is too late — it has gone, or is going.
func CancelSend(id string) (bool, error) {
	res, err := db.Exec(`UPDATE drafts SET status = ?, send_at = NULL, queued_at = NULL, updated_at = ?
		WHERE id = ? AND status = ? AND send_at IS NOT NULL`,
		DraftStatusDraft, time.Now().UnixMilli(), id, DraftStatusQueued)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DueDrafts are the drafts whose Undo window has passed.
func DueDrafts(now time.Time) ([]*Draft, error) {
	rows, err := db.Query("SELECT "+draftColumns+" FROM drafts WHERE status = ? AND send_at IS NOT NULL AND send_at <= ?",
		DraftStatusQueued, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Draft
	for rows.Next() {
		d, err := scanDraft(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimDraft marks a queued draft as going out, so that two senders — the
// server's loop and a person at a terminal — cannot both send it. False
// means somebody else has it, or it was taken back.
func ClaimDraft(id string) (bool, error) {
	res, err := db.Exec(`UPDATE drafts SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		DraftStatusSending, time.Now().UnixMilli(), id, DraftStatusQueued)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
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

// draftThreadKey resolves the conversation a draft answers.
//
// InReplyTo holds a Message-ID header, which arrives with angle brackets on one
// side of this comparison and without them on the other depending on who wrote
// it — so both sides are trimmed. That mismatch is the exact bug that once left
// every reply in a conversation of its own.
//
// A draft composed from scratch answers nothing and correctly has no key.
func draftThreadKey(inReplyTo string) (string, error) {
	bare := NormaliseMessageID(inReplyTo)
	if bare == "" {
		return "", nil
	}
	var key sql.NullString
	err := db.QueryRow(
		"SELECT thread_key FROM messages WHERE trim(message_id, '<>') = ? LIMIT 1", bare).Scan(&key)
	if err == sql.ErrNoRows {
		// Replying to mail this instance never imported. The draft is still a
		// draft; it simply has no conversation to sit in.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return key.String, nil
}

// DraftCalendar is the iCalendar part a draft carries, if any: an iMIP REPLY
// answering an invitation.
type DraftCalendar struct {
	Method string `json:"method"`
	ICS    string `json:"ics"`
}

// SetDraftCalendar attaches an iCalendar part to a draft.
func SetDraftCalendar(draftID, method, ics string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO draft_calendar (draft_id, method, ics) VALUES (?, ?, ?)`, draftID, method, ics)
	return err
}

// GetDraftCalendar returns a draft's iCalendar part, or nil.
func GetDraftCalendar(draftID string) (*DraftCalendar, error) {
	var c DraftCalendar
	err := db.QueryRow(`SELECT method, ics FROM draft_calendar WHERE draft_id = ?`, draftID).Scan(&c.Method, &c.ICS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
