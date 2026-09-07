package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/query"
)

// Ticket statuses.
//
// StatusProposed is the queue. Extraction proposes; it does not create. A task
// list you cannot trust is worse than none — you stop opening it, and then the
// inbox it was meant to relieve is worse off too — so a machine-derived ticket
// waits to be accepted unless its confidence cleared the bar.
const (
	TicketProposed = "proposed"
	TicketTodo     = "todo"
	TicketDoing    = "doing"
	TicketDone     = "done"
	TicketRejected = "rejected"
)

// DefaultStatuses are the board's columns when nobody has said otherwise.
var DefaultStatuses = []string{TicketTodo, TicketDoing, TicketDone}

// Ticket origins.
const (
	OriginHumanTicket = "human"
	OriginAnnotator   = "annotator"
)

// Ticket is a durable entity with state that changes over time.
//
// Everything a person sets — status, priority, due, title — is theirs. A
// re-run of the annotator that proposed it may attach more evidence, but it
// never writes these fields back. That is the same rule human annotations
// already follow, generalised from a label to an entity.
type Ticket struct {
	ID        string     `json:"id"`
	ThreadKey string     `json:"threadKey,omitempty"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Status    string     `json:"status"`
	Priority  string     `json:"priority,omitempty"`
	DueAt     *time.Time `json:"dueAt,omitempty"`
	// Origin says whether a person or an annotator raised it.
	Origin      string     `json:"origin"`
	AnnotatorID string     `json:"annotatorId,omitempty"`
	Confidence  *float64   `json:"confidence,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
	// Sources are the messages this ticket came from. Provenance is the whole
	// differentiator: a ticket that cannot be traced back to the mail that
	// created it is just a task in a worse task manager.
	Sources []TicketSource `json:"sources,omitempty"`
}

// TicketSource links a ticket to its evidence.
type TicketSource struct {
	MessageID    string    `json:"messageId"`
	AnnotationID string    `json:"annotationId,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	From         string    `json:"from,omitempty"`
	Date         time.Time `json:"date,omitempty"`
}

const ticketColumns = `id, COALESCE(thread_key, ''), title, COALESCE(body, ''), status,
	COALESCE(priority, ''), due_at, origin, COALESCE(annotator_id, ''), confidence,
	created_at, updated_at, closed_at`

func scanTicket(scan func(...any) error) (*Ticket, error) {
	t := &Ticket{}
	var due, closed sql.NullInt64
	var created, updated int64
	if err := scan(&t.ID, &t.ThreadKey, &t.Title, &t.Body, &t.Status, &t.Priority,
		&due, &t.Origin, &t.AnnotatorID, &t.Confidence, &created, &updated, &closed); err != nil {
		return nil, err
	}
	t.CreatedAt = time.UnixMilli(created)
	t.UpdatedAt = time.UnixMilli(updated)
	if due.Valid {
		d := time.UnixMilli(due.Int64)
		t.DueAt = &d
	}
	if closed.Valid {
		c := time.UnixMilli(closed.Int64)
		t.ClosedAt = &c
	}
	return t, nil
}

// SaveTicket creates or updates a ticket.
func SaveTicket(t *Ticket) error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("a ticket needs a title")
	}
	now := time.Now()

	// The prior row, read before the write, is what makes the history
	// possible: SaveTicket is an upsert, so after it runs there is no way to
	// tell a status move from a no-op save.
	var before *Ticket
	if t.ID == "" {
		t.ID = uuid.New().String()
		t.CreatedAt = now
	} else {
		var err error
		if before, err = loadTicket(t.ID); err != nil {
			return err
		}
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.Status == "" {
		t.Status = TicketTodo
	}
	if t.Origin == "" {
		t.Origin = OriginHumanTicket
	}
	t.UpdatedAt = now

	// Closing stamps the time; reopening clears it, so "done this week" is
	// answerable and a reopened ticket does not still look finished.
	if t.Status == TicketDone && t.ClosedAt == nil {
		t.ClosedAt = &now
	} else if t.Status != TicketDone {
		t.ClosedAt = nil
	}

	var due, closed any
	if t.DueAt != nil {
		due = t.DueAt.UnixMilli()
	}
	if t.ClosedAt != nil {
		closed = t.ClosedAt.UnixMilli()
	}

	_, err := db.Exec(`
		INSERT INTO tickets (id, thread_key, title, body, status, priority, due_at,
			origin, annotator_id, confidence, created_at, updated_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			thread_key = excluded.thread_key, title = excluded.title, body = excluded.body,
			status = excluded.status, priority = excluded.priority, due_at = excluded.due_at,
			updated_at = excluded.updated_at, closed_at = excluded.closed_at`,
		t.ID, nullIfEmpty(t.ThreadKey), t.Title, nullIfEmpty(t.Body), t.Status,
		nullIfEmpty(t.Priority), due, t.Origin, nullIfEmpty(t.AnnotatorID), t.Confidence,
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(), closed)
	if err != nil {
		return err
	}

	return recordTicketDiff(before, t, now)
}

// loadTicket reads a ticket without its evidence.
//
// Separate from GetTicket because the callers here want the row to compare
// against, and loading sources for that would be a query per save.
func loadTicket(id string) (*Ticket, error) {
	t, err := scanTicket(db.QueryRow("SELECT "+ticketColumns+" FROM tickets WHERE id = ?", id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// GetTicket returns one ticket with its evidence.
func GetTicket(id string) (*Ticket, error) {
	t, err := scanTicket(db.QueryRow("SELECT "+ticketColumns+" FROM tickets WHERE id = ?", id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Sources, err = ticketSources(t.ID)
	return t, err
}

func ticketSources(ticketID string) ([]TicketSource, error) {
	rows, err := db.Query(`
		SELECT ts.message_id, COALESCE(ts.annotation_id, ''),
		       COALESCE(m.subject, ''), COALESCE(m.from_addr, ''), COALESCE(m.date, 0)
		FROM ticket_sources ts
		LEFT JOIN messages m ON m.id = ts.message_id
		WHERE ts.ticket_id = ?
		ORDER BY m.date ASC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TicketSource{}
	for rows.Next() {
		var s TicketSource
		var date int64
		if err := rows.Scan(&s.MessageID, &s.AnnotationID, &s.Subject, &s.From, &date); err != nil {
			return nil, err
		}
		s.Date = time.UnixMilli(date)
		out = append(out, s)
	}
	return out, rows.Err()
}

// AttachSource records a message as evidence for a ticket.
//
// The event is written only when a row was actually inserted. Re-running an
// annotator calls this for every message it already knows about, and logging
// those would bury the real history under a re-run's worth of noise.
func AttachSource(ticketID, messageID, annotationID string) error {
	now := time.Now()
	res, err := db.Exec(`
		INSERT OR IGNORE INTO ticket_sources (ticket_id, message_id, annotation_id, created_at)
		VALUES (?, ?, ?, ?)`,
		ticketID, messageID, nullIfEmpty(annotationID), now.UnixMilli())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	return recordTicketEvent(ticketID, EventEvidence, "", messageID, OriginAnnotator, now)
}

// SetTicketStatus moves a ticket, which is what dragging a card does.
func SetTicketStatus(id, status string) error {
	t, err := GetTicket(id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("no ticket %q", id)
	}
	t.Status = status
	return SaveTicket(t)
}

// DeleteTicket removes a ticket and its evidence links.
//
// The messages and annotations behind it are untouched: they are facts about
// the mailbox, and deleting a ticket is a statement about the ticket.
func DeleteTicket(id string) error {
	res, err := db.Exec("DELETE FROM tickets WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no ticket %q", id)
	}
	return nil
}

// TicketProposal is what an extraction run would raise.
type TicketProposal struct {
	Proposed int64 `json:"proposed"`
	Accepted int64 `json:"autoAccepted"`
	Existing int64 `json:"alreadyRaised"`
	DryRun   bool  `json:"dryRun"`
}

// ProposeTickets raises tickets from an extractor's results.
//
// # Identity
//
// One ticket per conversation per annotator, keyed on thread_key. That makes a
// re-run idempotent — the second pass finds what the first proposed instead of
// duplicating it — and it is right far more often than trying to resolve
// entities across threads, which is wrong often enough to destroy trust.
// Threads that should be one ticket, or tickets that should be several, are a
// merge or a split: explicit, and the user's call.
//
// # Confidence
//
// Above autoAccept a ticket lands in the working set; below it, it waits in the
// proposed queue. Both are recorded either way, so nothing the extractor found
// is silently dropped.
func ProposeTickets(annotatorName string, autoAccept float64, dryRun bool) (*TicketProposal, error) {
	a, err := GetAnnotator(annotatorName)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", annotatorName)
	}
	if a.Kind != KindExtract {
		return nil, fmt.Errorf("%q is a %s, and a ticket needs fields to be extracted; use --kind extract",
			a.Name, a.Kind)
	}

	rows, err := db.Query(`
		SELECT an.id, an.message_id, an.data_json, an.confidence,
		       COALESCE(m.thread_key, m.id), COALESCE(m.subject, '')
		FROM annotations an
		JOIN messages m ON m.id = an.message_id
		WHERE an.annotator_id = ? AND an.annotator_version = ? AND an.status = ?
		ORDER BY m.date ASC`, a.ID, a.Version, StatusOK)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		annotationID, messageID, data, threadKey, subject string
		confidence                                        *float64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.annotationID, &c.messageID, &c.data, &c.confidence,
			&c.threadKey, &c.subject); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := &TicketProposal{DryRun: dryRun}

	for _, c := range candidates {
		existing, err := ticketForThread(c.threadKey, a.ID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			out.Existing++
			if !dryRun {
				// More evidence for a ticket already raised. The ticket's own
				// state is left exactly as the user left it.
				if err := AttachSource(existing.ID, c.messageID, c.annotationID); err != nil {
					return nil, err
				}
			}
			continue
		}

		status := TicketProposed
		if c.confidence != nil && *c.confidence >= autoAccept {
			status = TicketTodo
			out.Accepted++
		}
		out.Proposed++
		if dryRun {
			continue
		}

		t := &Ticket{
			ThreadKey:   c.threadKey,
			Title:       ticketTitle(c.data, c.subject),
			Body:        ticketField(c.data, "body", "summary", "notes"),
			Status:      status,
			Priority:    ticketField(c.data, "priority", "urgency"),
			DueAt:       ticketDue(c.data),
			Origin:      OriginAnnotator,
			AnnotatorID: a.ID,
			Confidence:  c.confidence,
		}
		if err := SaveTicket(t); err != nil {
			return nil, err
		}
		if err := AttachSource(t.ID, c.messageID, c.annotationID); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func ticketForThread(threadKey, annotatorID string) (*Ticket, error) {
	if threadKey == "" {
		return nil, nil
	}
	t, err := scanTicket(db.QueryRow(
		"SELECT "+ticketColumns+" FROM tickets WHERE thread_key = ? AND annotator_id = ?",
		threadKey, annotatorID).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// ticketTitle picks the extracted title, falling back to the subject line.
func ticketTitle(data, subject string) string {
	if v := ticketField(data, "title", "task", "action", "summary"); v != "" {
		return v
	}
	if subject != "" {
		return subject
	}
	return "(untitled)"
}

// ticketField reads the first of several plausible keys out of extracted JSON.
//
// Extractors are defined by users writing prompts, so the key a model returns
// is "title" or "task" or "action" depending on how the instruction was
// phrased. Accepting several beats making every extractor schema exact.
func ticketField(data string, keys ...string) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := fields[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func ticketDue(data string) *time.Time {
	raw := ticketField(data, "due", "dueAt", "due_date", "deadline")
	if raw == "" {
		return nil
	}
	// An extracted deadline is in the future, and a model asked for "due" will
	// happily answer "3d".
	start, _, err := query.ParseFutureDate(raw)
	if err != nil {
		return nil
	}
	t := time.UnixMilli(start)
	return &t
}

// AcceptTicket moves a proposal into the working set.
func AcceptTicket(id string) error { return SetTicketStatus(id, TicketTodo) }

// RejectTicket records that a proposal was wrong.
//
// Kept rather than deleted: a rejection is the evidence that the extractor got
// this one wrong, which is what makes it possible to tell whether a prompt
// change was an improvement.
func RejectTicket(id string) error { return SetTicketStatus(id, TicketRejected) }

// MergeTickets folds one ticket's evidence into another and removes it.
func MergeTickets(intoID, fromID string) error {
	into, err := GetTicket(intoID)
	if err != nil {
		return err
	}
	from, err := GetTicket(fromID)
	if err != nil {
		return err
	}
	if into == nil || from == nil {
		return fmt.Errorf("both tickets must exist")
	}
	for _, s := range from.Sources {
		if err := AttachSource(intoID, s.MessageID, s.AnnotationID); err != nil {
			return err
		}
	}
	// Recorded on the surviving ticket, since the merged one's own history is
	// about to be deleted along with it. The title is kept rather than the id
	// for the same reason: the id will name nothing once this returns.
	if err := recordTicketEvent(intoID, EventMerged, from.Title, into.Title,
		OriginHumanTicket, time.Now()); err != nil {
		return err
	}
	return DeleteTicket(fromID)
}

// TicketStatuses lists the statuses actually in use, for a board's columns.
func TicketStatuses() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT status FROM tickets ORDER BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		seen[s] = true
		out = append(out, s)
	}
	// The defaults always appear, so a board is not empty before the first
	// ticket reaches a column.
	for _, s := range DefaultStatuses {
		if !seen[s] {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// ParseTicketDue resolves a due-date expression using the query language's
// date grammar, so `--due 7d` means what `due:7d` means.
func ParseTicketDue(v string) (*time.Time, error) {
	start, _, err := query.ParseFutureDate(v)
	if err != nil {
		return nil, err
	}
	t := time.UnixMilli(start)
	return &t, nil
}

// BoardColumn is one column of a board.
type BoardColumn struct {
	Status  string    `json:"status"`
	Total   int64     `json:"total"`
	Tickets []*Ticket `json:"tickets"`
}

// BoardColumns builds a board from a filter and the status field.
//
// The two are deliberately separate. The filter says which tickets appear;
// the columns are the values of one designated field. That is what gives
// dragging a card an unambiguous meaning — it writes that field, which is the
// inverse of the query that defined the column. If columns were arbitrary
// queries there would be no answer to what dragging from `due:7d` into
// `from:acme` should do.
func BoardColumns(filter string, perColumn int) ([]*BoardColumn, error) {
	if perColumn <= 0 {
		perColumn = 10
	}
	statuses, err := TicketStatuses()
	if err != nil {
		return nil, err
	}

	out := []*BoardColumn{}
	for _, status := range statuses {
		// Rejected tickets are kept as evidence that the extractor was wrong,
		// but a board is a working surface and they are not work.
		if status == TicketRejected {
			continue
		}

		scoped := strings.TrimSpace(filter + " status:" + status)
		res, err := RunQuery(scoped, perColumn, 0)
		if err != nil {
			return nil, err
		}
		total, err := CountQuery(scoped)
		if err != nil {
			return nil, err
		}
		out = append(out, &BoardColumn{Status: status, Total: total, Tickets: res.Tickets})
	}
	return out, nil
}
