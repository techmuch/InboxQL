package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/query"
)

// The store's half of `iql work`: an AI harness reading conversations and
// answering the elements about them (internal/work). What is here is reading
// and bookkeeping; deciding what is pending and checking answers is there.

// RecentThreads returns conversation keys, newest first, from the mail a
// filter matches (all mail when it is empty), skipping `skip` of them.
//
// Walked by message date rather than grouped, because a GROUP BY over every
// message to find the newest few conversations reads the whole table, and
// the date index answers "the newest messages" directly.
func RecentThreads(filter string, skip, limit int) ([]string, error) {
	where, args, err := filterClause(filter)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var keys []string
	const page = 500
	for offset := 0; len(keys) < skip+limit; offset += page {
		rows, err := db.Query(`SELECT m.thread_key FROM messages m WHERE `+where+
			` AND m.thread_key IS NOT NULL ORDER BY m.date DESC LIMIT ? OFFSET ?`,
			append(append([]any{}, args...), page, offset)...)
		if err != nil {
			return nil, err
		}
		n := 0
		for rows.Next() {
			n++
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if n < page {
			break
		}
	}
	if skip >= len(keys) {
		return nil, nil
	}
	keys = keys[skip:]
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}

// ReadingMessage is one message of a conversation, as a reader gets it.
type ReadingMessage struct {
	ID      string    `json:"id"`
	Date    time.Time `json:"date"`
	From    string    `json:"from"`
	To      []string  `json:"to,omitempty"`
	Cc      []string  `json:"cc,omitempty"`
	Subject string    `json:"subject"`
	// Mine is whether one of your accounts sent it.
	Mine bool `json:"mine"`
	// Text is the message as a person reads it: the plain part, or the HTML
	// rendered without its stylesheet (message.Readable).
	Text        string        `json:"text"`
	Attachments []ReadingFile `json:"attachments,omitempty"`
	Truncated   bool          `json:"truncated,omitempty"`
	// Automated is true when standard RFC headers (List-Unsubscribe, List-Id,
	// Precedence: bulk/list, Auto-Submitted) mark the message as bulk/automated.
	Automated bool `json:"automated,omitempty"`
	// HasCalendar is true when the message or its attachments carry an
	// iCalendar (text/calendar or .ics) invitation.
	HasCalendar bool `json:"hasCalendar,omitempty"`
}

// ReadingFile is an attachment and the text extracted from it.
type ReadingFile struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Caps on what one conversation hands a reader. Generous — a receipt is two
// thousand characters, a long thread forty — but bounded, so one runaway
// newsletter cannot fill a model's context on its own.
const (
	ReadingTextCap = 20000
	ReadingFileCap = 12000
)

// ThreadReading returns a conversation's messages outside Trash and Spam,
// oldest first, with readable text and attachment text.
func ThreadReading(threadKey string) ([]*ReadingMessage, error) {
	rows, err := db.Query(`SELECT `+aliasedMessageColumns()+` FROM messages m
		WHERE m.thread_key = ? AND `+query.Unbinned("m.id")+` ORDER BY m.date ASC`, threadKey)
	if err != nil {
		return nil, err
	}
	var msgs []*message.Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			rows.Close()
			return nil, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	self := map[string]bool{}
	if mine, err := selfAddresses(); err == nil {
		for _, a := range mine {
			self[strings.ToLower(a)] = true
		}
	}

	out := make([]*ReadingMessage, 0, len(msgs))
	for _, m := range msgs {
		from, _ := NormaliseAddress(m.From)
		r := &ReadingMessage{ID: m.ID, Date: m.Date, From: m.From, To: m.To, Cc: m.Cc,
			Subject: m.Subject, Mine: self[from]}
		r.Text = message.Readable(m)
		if len(r.Text) > ReadingTextCap {
			r.Text, r.Truncated = r.Text[:ReadingTextCap], true
		}
		files, err := readingFiles(m.ID)
		if err != nil {
			return nil, err
		}
		r.Attachments = files
		r.Automated, r.HasCalendar = inspectRawSignals(m.ID, files)
		out = append(out, r)
	}
	return out, nil
}

func inspectRawSignals(messageID string, files []ReadingFile) (automated, hasCal bool) {
	for _, f := range files {
		lt := strings.ToLower(f.Type)
		ln := strings.ToLower(f.Name)
		if strings.Contains(lt, "calendar") || strings.HasSuffix(ln, ".ics") {
			hasCal = true
		}
	}
	var raw []byte
	if err := db.QueryRow(`SELECT SUBSTR(raw, 1, 8192) FROM message_raw WHERE message_id = ?`, messageID).Scan(&raw); err == nil && len(raw) > 0 {
		hdr := strings.ToLower(string(raw))
		if idx := strings.Index(hdr, "\r\n\r\n"); idx != -1 {
			hdr = hdr[:idx]
		} else if idx := strings.Index(hdr, "\n\n"); idx != -1 {
			hdr = hdr[:idx]
		}
		if strings.Contains(hdr, "list-unsubscribe:") ||
			strings.Contains(hdr, "list-id:") ||
			strings.Contains(hdr, "precedence: bulk") ||
			strings.Contains(hdr, "precedence: list") ||
			strings.Contains(hdr, "x-auto-response-suppress:") ||
			(strings.Contains(hdr, "auto-submitted:") && !strings.Contains(hdr, "auto-submitted: no")) {
			automated = true
		}
		if strings.Contains(hdr, "text/calendar") {
			hasCal = true
		}
	}
	return automated, hasCal
}

func readingFiles(messageID string) ([]ReadingFile, error) {
	rows, err := db.Query(`
		SELECT COALESCE(a.filename, ''), COALESCE(a.mime_type, ''),
		       COALESCE((SELECT group_concat(t.text, char(10)) FROM
		                   (SELECT text FROM attachment_text WHERE content_hash = a.content_hash ORDER BY page) t), '')
		FROM attachments a WHERE a.message_id = ? AND COALESCE(a.inline, 0) = 0`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReadingFile
	for rows.Next() {
		var f ReadingFile
		if err := rows.Scan(&f.Name, &f.Type, &f.Text); err != nil {
			return nil, err
		}
		if len(f.Text) > ReadingFileCap {
			f.Text = f.Text[:ReadingFileCap]
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Answered reports whether an annotator has an answer for a message at its
// current version — from anything — or a person's ruling at any version.
func Answered(a *Annotator, messageID string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM annotations
		WHERE message_id = ? AND annotator_id = ?
		  AND (source = ? OR (annotator_version = ? AND status IN (?, ?, ?)))`,
		messageID, a.ID, SourceHuman, a.Version, StatusOK, StatusEmpty, StatusFailed).Scan(&n)
	return n > 0, err
}

// KnownTopics are the topics already in use, most used first, so a reader
// picks from them before inventing a near-synonym.
func KnownTopics(limit int) ([]string, error) {
	rows, err := db.Query(`SELECT topic FROM message_topics GROUP BY topic
		ORDER BY COUNT(*) DESC, topic LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Leases.

// WorkLease is a conversation handed to a reader, and what it was asked.
type WorkLease struct {
	ThreadKey string
	Token     string
	// Asked is the work package's own record of what was asked, as JSON.
	Asked   string
	Expires time.Time
	// CreatedAt is when it was taken; BatchID the batch it was handed out in.
	CreatedAt time.Time
	BatchID   string
	// Prefill is what the template pre-filled for it, as JSON answers by
	// element, when a template was written.
	Prefill string
}

// LeaseFor returns the lease on a conversation, or nil.
func LeaseFor(threadKey string) (*WorkLease, error) {
	var l WorkLease
	var exp, at int64
	var batch, prefill sql.NullString
	err := db.QueryRow(`SELECT thread_key, token, asked, expires_at, created_at, batch_id, prefill
		FROM work_leases WHERE thread_key = ?`,
		threadKey).Scan(&l.ThreadKey, &l.Token, &l.Asked, &exp, &at, &batch, &prefill)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.Expires, l.CreatedAt = time.UnixMilli(exp), time.UnixMilli(at)
	l.BatchID, l.Prefill = batch.String, prefill.String
	return &l, nil
}

// TakeLease hands a conversation to a reader until it expires, replacing an
// expired lease. It fails if another unexpired lease holds it.
func TakeLease(threadKey, asked string, d time.Duration) (*WorkLease, error) {
	now := time.Now()
	l := &WorkLease{ThreadKey: threadKey, Token: uuid.New().String(), Asked: asked, Expires: now.Add(d), CreatedAt: now}
	res, err := db.Exec(`
		INSERT INTO work_leases (thread_key, token, asked, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(thread_key) DO UPDATE SET
			token = excluded.token, asked = excluded.asked,
			expires_at = excluded.expires_at, created_at = excluded.created_at
		WHERE work_leases.expires_at < ?`,
		threadKey, l.Token, asked, l.Expires.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("conversation %s is being read by another run", threadKey)
	}
	return l, nil
}

// DropLease ends a lease.
func DropLease(threadKey string) error {
	_, err := db.Exec(`DELETE FROM work_leases WHERE thread_key = ?`, threadKey)
	return err
}

// ActiveLeases counts unexpired leases.
func ActiveLeases() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM work_leases WHERE expires_at >= ?`,
		time.Now().UnixMilli()).Scan(&n)
	return n, err
}

// ClearAgentAnswers removes what a harness answered: its annotations, the
// topics it set and the contact kinds it decided, optionally only one
// model's, since a time, or for some annotators. Proposed tickets and
// calendar events it raised stay, as proposals a person rejects.
func ClearAgentAnswers(model string, since time.Time, annotatorIDs []string) (int64, error) {
	conds := []string{"source = ?"}
	args := []any{SourceAgent}
	if model != "" {
		conds = append(conds, "model = ?")
		args = append(args, model)
	}
	if !since.IsZero() {
		conds = append(conds, "created_at >= ?")
		args = append(args, since.UnixMilli())
	}
	if len(annotatorIDs) > 0 {
		conds = append(conds, "annotator_id IN ("+placeholders(len(annotatorIDs))+")")
		args = append(args, anySlice(annotatorIDs)...)
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM annotations WHERE "+strings.Join(conds, " AND "), args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	// Topics and kinds carry no model, so they follow the time alone.
	tconds, targs := []string{"source = ?"}, []any{SourceAgent}
	kconds, kargs := []string{"kind_source = ?"}, []any{SourceAgent}
	if !since.IsZero() {
		tconds, targs = append(tconds, "created_at >= ?"), append(targs, since.UnixMilli())
		kconds, kargs = append(kconds, "updated_at >= ?"), append(kargs, since.UnixMilli())
	}
	if model == "" && len(annotatorIDs) == 0 {
		if _, err := tx.Exec("DELETE FROM message_topics WHERE "+strings.Join(tconds, " AND "), targs...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec("UPDATE contacts SET kind = 'unknown', kind_source = NULL WHERE "+
			strings.Join(kconds, " AND "), kargs...); err != nil {
			return 0, err
		}
	}
	return n, tx.Commit()
}

// MatchingIDs returns the ids among ids that a filter matches.
func MatchingIDs(filter string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	where, args, err := filterClause(filter)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT m.id FROM messages m WHERE m.id IN (`+placeholders(len(ids))+`) AND `+where,
		append(anySlice(ids), args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// OriginAgentTicket marks a ticket an AI harness proposed through `iql work`.
const OriginAgentTicket = "agent"

// AgentTicketInThread reports whether a harness already proposed a ticket
// with this title in a conversation, so reading it again proposes nothing
// twice.
func AgentTicketInThread(threadKey, title string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tickets WHERE thread_key = ? AND origin = ? AND LOWER(title) = LOWER(?)`,
		threadKey, OriginAgentTicket, strings.TrimSpace(title)).Scan(&n)
	return n > 0, err
}

// EventFromMessage reports whether an event with this title and start was
// already made from a message.
func EventFromMessage(messageID, summary string, start time.Time) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE source_message_id = ? AND LOWER(summary) = LOWER(?) AND start_at = ?`,
		messageID, strings.TrimSpace(summary), start.UnixMilli()).Scan(&n)
	return n > 0, err
}

// AgentAnswerCount is how much one model has answered of one annotator.
type AgentAnswerCount struct {
	Annotator string    `json:"element"`
	Model     string    `json:"model"`
	Answers   int       `json:"answers"`
	Messages  int       `json:"messages"`
	Last      time.Time `json:"last"`
}

// AgentAnswerCounts summarises what harnesses have answered, by element and
// model, current versions only.
func AgentAnswerCounts() ([]AgentAnswerCount, error) {
	rows, err := db.Query(`
		SELECT a.name, COALESCE(x.model, ''), COUNT(*), COUNT(DISTINCT x.message_id), MAX(x.created_at)
		FROM annotations x JOIN annotators a ON a.id = x.annotator_id
		WHERE x.source = ? AND x.annotator_version = a.version
		GROUP BY a.name, x.model ORDER BY a.name, x.model`, SourceAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentAnswerCount
	for rows.Next() {
		var c AgentAnswerCount
		var last int64
		if err := rows.Scan(&c.Annotator, &c.Model, &c.Answers, &c.Messages, &last); err != nil {
			return nil, err
		}
		c.Last = time.UnixMilli(last)
		out = append(out, c)
	}
	return out, rows.Err()
}

// HasReceiptOrder reports whether an ok receipt annotation with the same
// non-empty order number already exists on another message, preventing
// Ordered + Shipped emails for the same order from double-counting totals.
func HasReceiptOrder(annotatorID, order, excludeMessageID string) (bool, error) {
	order = strings.TrimSpace(order)
	if order == "" {
		return false, nil
	}
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM annotations
		WHERE annotator_id = ? AND message_id != ? AND status = ?
		  AND LOWER(TRIM(COALESCE(json_extract(data_json, '$.order'), ''))) = LOWER(?)`,
		annotatorID, excludeMessageID, StatusOK, order).Scan(&n)
	return n > 0, err
}

// SenderHistory holds the most common topics, mail labels, and importance level
// previously assigned to messages from a sender address.
type SenderHistory struct {
	Topics     []string
	Labels     []string
	Importance string
}

// SenderDefaults returns the most common topics, non-system mail labels, and
// importance level from recent messages sent by address, so `work.Template`
// can pre-fill recurring senders out of the box.
func SenderDefaults(address string) (SenderHistory, error) {
	var h SenderHistory
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		return h, nil
	}
	const recentFromSender = `(SELECT message_id FROM message_participants WHERE address = ? AND role = 'from' LIMIT 50)`

	// Only what somebody concluded teaches the next template: a person, or a
	// model that changed the guess. Learning from guesses that went back
	// untouched made one wrong guess every later default for that sender.
	const consideredTopic = `(mt.source != 'agent' OR EXISTS (SELECT 1 FROM annotations a
		JOIN annotators an ON an.id = a.annotator_id AND an.name = 'topics'
		WHERE a.message_id = mt.message_id AND a.source = 'agent' AND a.prefilled = 0))`
	tRows, err := db.Query(`
		SELECT mt.topic FROM message_topics mt
		WHERE mt.message_id IN `+recentFromSender+` AND `+consideredTopic+`
		GROUP BY mt.topic
		HAVING COUNT(*) >= 2 OR (SELECT COUNT(DISTINCT mt.message_id) FROM message_topics mt WHERE mt.message_id IN `+recentFromSender+` AND `+consideredTopic+`) = 1
		ORDER BY COUNT(*) DESC, mt.topic ASC LIMIT 3`, addr, addr)
	if err == nil {
		for tRows.Next() {
			var t string
			if tRows.Scan(&t) == nil && t != "" {
				h.Topics = append(h.Topics, t)
			}
		}
		tRows.Close()
	}

	// A label an agent added counts only when a model chose it — recorded
	// as a work action — and nobody took it off since.
	const consideredLabel = `ml.label NOT IN ('inbox', 'sent', 'drafts', 'important', 'spam', 'trash', 'starred', 'unread')
		AND (ml.source != 'agent' OR EXISTS (SELECT 1 FROM work_actions w WHERE w.message_id = ml.message_id
		     AND w.kind = 'label' AND w.ref = ml.label AND w.undone_at IS NULL))`
	lRows, err := db.Query(`
		SELECT ml.label FROM message_labels ml
		WHERE ml.message_id IN `+recentFromSender+` AND `+consideredLabel+`
		GROUP BY ml.label
		HAVING COUNT(*) >= 2 OR (SELECT COUNT(DISTINCT ml.message_id) FROM message_labels ml WHERE ml.message_id IN `+recentFromSender+` AND `+consideredLabel+`) = 1
		ORDER BY COUNT(*) DESC, ml.label ASC LIMIT 3`, addr, addr)
	if err == nil {
		for lRows.Next() {
			var l string
			if lRows.Scan(&l) == nil && l != "" {
				h.Labels = append(h.Labels, l)
			}
		}
		lRows.Close()
	}

	var imp string
	var impCount int
	if err := db.QueryRow(`
		SELECT COALESCE(json_extract(a.data_json, '$.level'), ''), COUNT(*)
		FROM annotations a
		JOIN annotators an ON an.id = a.annotator_id AND an.name = 'importance'
		WHERE a.message_id IN `+recentFromSender+`
		  AND a.status = ? AND a.prefilled = 0
		  AND COALESCE(json_extract(a.data_json, '$.level'), '') != ''
		GROUP BY 1 ORDER BY COUNT(*) DESC LIMIT 1`, addr, StatusOK).Scan(&imp, &impCount); err == nil && impCount >= 2 {
		h.Importance = imp
	}

	return h, nil
}

// TuneElement updates an installed element's instructions in place (preserving
// its version unless bumpVersion is true) so users can add domain-specific
// rules without invalidating their existing answers or editing Go code.
func TuneElement(name, instructions string, bumpVersion bool) (*Annotator, error) {
	a, err := GetAnnotator(name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("element %q is not installed (run `iql elements install` first)", name)
	}
	ver := a.Version
	if bumpVersion && strings.TrimSpace(a.Instructions) != strings.TrimSpace(instructions) {
		ver++
	}
	now := time.Now()
	if _, err := db.Exec(`UPDATE annotators SET instructions = ?, version = ?, updated_at = ? WHERE id = ?`,
		instructions, ver, now.UnixMilli(), a.ID); err != nil {
		return nil, err
	}
	a.Instructions = instructions
	a.Version = ver
	a.UpdatedAt = now
	return a, nil
}
