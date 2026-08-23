package store

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/user/uea/internal/message"
)

// messageColumns is the canonical column list for scanMessage.
const messageColumns = `id, account_id, uid, message_id, content_hash, normalized_body,
	from_addr, to_addrs, cc_addrs, bcc_addrs, subject, date, body, html_body,
	header, flags, size, internal_date`

// scanMessage reads one row selected with messageColumns.
func scanMessage(scan func(dest ...any) error) (*message.Message, error) {
	m := &message.Message{}
	var to, cc, bcc, flags string
	var date, internalDate int64

	if err := scan(&m.ID, &m.AccountID, &m.UID, &m.MessageID, &m.ContentHash,
		&m.NormalizedBody, &m.From, &to, &cc, &bcc, &m.Subject, &date, &m.Body,
		&m.HTMLBody, &m.Header, &flags, &m.Size, &internalDate); err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(to), &m.To)
	json.Unmarshal([]byte(cc), &m.Cc)
	json.Unmarshal([]byte(bcc), &m.Bcc)
	json.Unmarshal([]byte(flags), &m.Flags)
	m.Date = time.UnixMilli(date)
	m.InternalDate = time.UnixMilli(internalDate)
	return m, nil
}

// SearchQuery describes a message search.
//
// Matching is substring-based over subject, body and sender. This is honest
// about what exists today: there is no FTS5 index and no vector layer, so
// requirements.md 2.2's hybrid search is not what runs here. Callers that
// surface results to a user should say so rather than implying relevance
// ranking.
type SearchQuery struct {
	Text      string
	AccountID string
	From      string
	Since     string // YYYY-MM-DD, inclusive
	Until     string // YYYY-MM-DD, inclusive
	Unread    bool
	Limit     int
	Offset    int
}

// SearchMessages returns messages matching the query, newest first.
func SearchMessages(q SearchQuery) ([]*message.Message, error) {
	var clauses []string
	var args []any

	if q.Text != "" {
		// Scanned across the fields a person would expect "search" to cover.
		// Without an index this is a table scan; acceptable at the scale UEA
		// currently reaches, and the place FTS5 should slot in later.
		clauses = append(clauses, "(subject LIKE ? OR body LIKE ? OR from_addr LIKE ?)")
		like := "%" + q.Text + "%"
		args = append(args, like, like, like)
	}
	if q.AccountID != "" {
		clauses = append(clauses, "account_id = ?")
		args = append(args, q.AccountID)
	}
	if q.From != "" {
		clauses = append(clauses, "LOWER(from_addr) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.From)+"%")
	}
	if q.Since != "" {
		clauses = append(clauses, "strftime('%Y-%m-%d', date / 1000, 'unixepoch') >= ?")
		args = append(args, q.Since)
	}
	if q.Until != "" {
		clauses = append(clauses, "strftime('%Y-%m-%d', date / 1000, 'unixepoch') <= ?")
		args = append(args, q.Until)
	}
	if q.Unread {
		clauses = append(clauses, `flags NOT LIKE '%\Seen%'`)
	}

	query := "SELECT " + messageColumns + " FROM messages"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY date DESC LIMIT ? OFFSET ?"

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	args = append(args, limit, q.Offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*message.Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetThread returns every message sharing a subject line with the given
// message, oldest first, so a conversation reads top to bottom.
//
// Subject-based grouping is a deliberate approximation. Proper threading needs
// the References and In-Reply-To headers, which are captured in the raw header
// blob but not yet parsed into columns — doing that properly is a schema change
// rather than a query change.
func GetThread(id string) ([]*message.Message, error) {
	root, err := GetMessageByID(id)
	if err != nil || root == nil {
		return nil, err
	}

	normalized := NormalizeSubject(root.Subject)
	if normalized == "" {
		return []*message.Message{root}, nil
	}

	rows, err := db.Query("SELECT "+messageColumns+" FROM messages WHERE account_id = ? ORDER BY date ASC",
		root.AccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var thread []*message.Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		if NormalizeSubject(m.Subject) == normalized {
			thread = append(thread, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(thread) == 0 {
		thread = []*message.Message{root}
	}
	return thread, nil
}

// NormalizeSubject strips reply and forward prefixes so "Re: Re: Fwd: Lunch"
// and "Lunch" group together.
func NormalizeSubject(subject string) string {
	s := strings.ToLower(strings.TrimSpace(subject))
	for {
		trimmed := false
		for _, prefix := range []string{"re:", "fwd:", "fw:", "aw:", "sv:"} {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
				trimmed = true
			}
		}
		if !trimmed {
			return s
		}
	}
}
