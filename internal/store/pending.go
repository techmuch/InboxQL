package store

import (
	"time"
)

// # Changes waiting for the server
//
// A change made here — archive, a label, read, starred — is applied to the
// local copy at once and queued for the account's server. The sync worker
// drains the queue; until it has, the queued labels are what this side
// believes, and a sync that reads the server leaves them alone (the server
// wins every other disagreement).
//
// Only accounts with a server queue anything. An imported archive has nowhere
// to send a change, and a disconnected account has no login to send it with.

// Queued operations.
const (
	OpLabelAdd    = "label+"
	OpLabelRemove = "label-"
	OpFlagAdd     = "flag+"
	OpFlagRemove  = "flag-"
)

// MaxPendingAttempts is how many times a change is tried before it is left
// for a person to look at.
const MaxPendingAttempts = 8

// PendingNotify, when set, is told which account has new work. The sync
// workers set it so a change reaches the server in seconds, not at the next
// pass.
var PendingNotify func(accountID string)

func enqueue(x execer, accountID, messageID, op, value string, now int64) error {
	_, err := x.Exec(`
		INSERT INTO pending_changes (account_id, message_id, op, value, created_at)
		SELECT ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM accounts WHERE id = ? AND COALESCE(host, '') != '' AND disconnected = 0)`,
		accountID, messageID, op, value, now, accountID)
	return err
}

func notifyPending(accounts map[string]bool) {
	if PendingNotify == nil {
		return
	}
	for a := range accounts {
		PendingNotify(a)
	}
}

// PendingChange is one queued change.
type PendingChange struct {
	ID        int64     `json:"id"`
	AccountID string    `json:"accountId"`
	MessageID string    `json:"messageId"`
	Op        string    `json:"op"`
	Value     string    `json:"value"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// PendingChanges returns an account's queued changes, oldest first, leaving
// out the ones that have used up their attempts.
func PendingChanges(accountID string, limit int) ([]PendingChange, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.Query(`
		SELECT id, account_id, message_id, op, value, attempts, COALESCE(last_error, ''), created_at
		FROM pending_changes
		WHERE account_id = ? AND attempts < ?
		ORDER BY id LIMIT ?`, accountID, MaxPendingAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingChange
	for rows.Next() {
		var p PendingChange
		var created int64
		if err := rows.Scan(&p.ID, &p.AccountID, &p.MessageID, &p.Op, &p.Value, &p.Attempts, &p.LastError, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = time.UnixMilli(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// CompletePendingChange removes a change the server has applied.
func CompletePendingChange(id int64) error {
	_, err := db.Exec(`DELETE FROM pending_changes WHERE id = ?`, id)
	return err
}

// FailPendingChange records a failed attempt.
func FailPendingChange(id int64, reason string) error {
	_, err := db.Exec(`UPDATE pending_changes SET attempts = attempts + 1, last_error = ? WHERE id = ?`, reason, id)
	return err
}

// RetryPendingChanges gives changes that used up their attempts another go.
func RetryPendingChanges(accountID string) (int, error) {
	res, err := db.Exec(`UPDATE pending_changes SET attempts = 0 WHERE account_id = ? AND attempts >= ?`,
		accountID, MaxPendingAttempts)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		notifyPending(map[string]bool{accountID: true})
	}
	return int(n), nil
}

// PendingSummary is what an account's card says about its queue.
type PendingSummary struct {
	Waiting   int    `json:"waiting"`
	Failed    int    `json:"failed"`
	LastError string `json:"lastError,omitempty"`
}

// PendingSummaryFor counts an account's queue.
func PendingSummaryFor(accountID string) (PendingSummary, error) {
	var s PendingSummary
	err := db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN attempts < ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN attempts >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE((SELECT last_error FROM pending_changes
		                 WHERE account_id = ? AND last_error IS NOT NULL ORDER BY id DESC LIMIT 1), '')
		FROM pending_changes WHERE account_id = ?`,
		MaxPendingAttempts, MaxPendingAttempts, accountID, accountID).Scan(&s.Waiting, &s.Failed, &s.LastError)
	return s, err
}

// PendingLabels returns, for one message, the labels and flags with a change
// still queued. A sync reading the server keeps the local value of these.
func PendingLabels(messageID string) (labels, flags map[string]bool, err error) {
	labels, flags = map[string]bool{}, map[string]bool{}
	rows, err := db.Query(`SELECT op, value FROM pending_changes WHERE message_id = ? AND attempts < ?`,
		messageID, MaxPendingAttempts)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var op, v string
		if err := rows.Scan(&op, &v); err != nil {
			return nil, nil, err
		}
		switch op {
		case OpLabelAdd, OpLabelRemove:
			labels[v] = true
		case OpFlagAdd, OpFlagRemove:
			flags[v] = true
		}
	}
	return labels, flags, rows.Err()
}
