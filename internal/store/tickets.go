package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
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
	// Position orders a card within its list; lower is higher up.
	Position   float64    `json:"position"`
	StartAt    *time.Time `json:"startAt,omitempty"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	// RuleID names the automation rule that made it, if one did.
	RuleID string `json:"ruleId,omitempty"`

	// Labels share one vocabulary with the topics on mail.
	Labels []string `json:"labels,omitempty"`
	// Checklist and Comments are counts, for the front of a card.
	ChecklistDone  int `json:"checklistDone,omitempty"`
	ChecklistTotal int `json:"checklistTotal,omitempty"`
	Comments       int `json:"comments,omitempty"`
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
	// Attachments is how many distinct files this evidence carried. Derived on
	// read from the same edge table everything else reads, so it cannot fall
	// out of step with the files themselves — and a count rather than the rows
	// because a board shows many tickets at once and needs only to know
	// whether there is anything to open.
	Attachments int `json:"attachments,omitempty"`
}

const ticketColumns = `id, COALESCE(thread_key, ''), title, COALESCE(body, ''), status,
	COALESCE(priority, ''), due_at, origin, COALESCE(annotator_id, ''), confidence,
	created_at, updated_at, closed_at, COALESCE(position, 0), start_at, archived_at,
	COALESCE(rule_id, '')`

func scanTicket(scan func(...any) error) (*Ticket, error) {
	t := &Ticket{}
	var due, closed, start, archived sql.NullInt64
	var created, updated int64
	if err := scan(&t.ID, &t.ThreadKey, &t.Title, &t.Body, &t.Status, &t.Priority,
		&due, &t.Origin, &t.AnnotatorID, &t.Confidence, &created, &updated, &closed,
		&t.Position, &start, &archived, &t.RuleID); err != nil {
		return nil, err
	}
	t.StartAt = optionalTime(start)
	t.ArchivedAt = optionalTime(archived)
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

// optionalTime reads a nullable epoch-millisecond column.
func optionalTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}

// SaveTicket creates or updates a ticket, as a person.
//
// Position, archive and labels are not written here: each has its own
// operation (MoveTicket, ArchiveTicket, SetTicketLabels), so saving a card
// someone had open while it was moved does not move it back.
func SaveTicket(t *Ticket) error { return saveTicket(t, OriginHumanTicket) }

// saveTicket is SaveTicket with the actor named, for automation.
func saveTicket(t *Ticket, actor string) error {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
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
		first, err := FirstOpenList()
		if err != nil {
			return err
		}
		t.Status = first
	}
	if err := validStatus(t.Status); err != nil {
		return err
	}
	if t.Origin == "" {
		t.Origin = OriginHumanTicket
	}
	t.UpdatedAt = now

	// A new card, or one that changed list, goes to the bottom of its list,
	// as in Trello. Otherwise it keeps its place: the position a caller sent
	// is ignored, because only MoveTicket says where a card goes.
	switch {
	case before == nil || before.Status != t.Status:
		pos, err := bottomOf(t.Status)
		if err != nil {
			return err
		}
		t.Position = pos
	default:
		t.Position = before.Position
	}
	if before != nil {
		t.ArchivedAt = before.ArchivedAt
		t.RuleID = before.RuleID
	}

	// Closing stamps the time; reopening clears it, so "done this week" is
	// answerable and a reopened ticket does not still look finished. Closed
	// is the list's kind, so a list of your own can finish cards too.
	closing := listKind(t.Status) == ListClosed
	if closing && t.ClosedAt == nil {
		t.ClosedAt = &now
	} else if !closing {
		t.ClosedAt = nil
	}

	var due, closed, start, archived any
	if t.DueAt != nil {
		due = t.DueAt.UnixMilli()
	}
	if t.ClosedAt != nil {
		closed = t.ClosedAt.UnixMilli()
	}
	if t.StartAt != nil {
		start = t.StartAt.UnixMilli()
	}
	if t.ArchivedAt != nil {
		archived = t.ArchivedAt.UnixMilli()
	}

	_, err := db.Exec(`
		INSERT INTO tickets (id, thread_key, title, body, status, priority, due_at,
			origin, annotator_id, confidence, created_at, updated_at, closed_at,
			position, start_at, archived_at, rule_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			thread_key = excluded.thread_key, title = excluded.title, body = excluded.body,
			status = excluded.status, priority = excluded.priority, due_at = excluded.due_at,
			updated_at = excluded.updated_at, closed_at = excluded.closed_at,
			position = excluded.position, start_at = excluded.start_at`,
		t.ID, nullIfEmpty(t.ThreadKey), t.Title, nullIfEmpty(t.Body), t.Status,
		nullIfEmpty(t.Priority), due, t.Origin, nullIfEmpty(t.AnnotatorID), t.Confidence,
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(), closed,
		t.Position, start, archived, nullIfEmpty(t.RuleID))
	if err != nil {
		return err
	}

	if err := recordTicketDiff(before, t, now, actor); err != nil {
		return err
	}
	if before == nil && t.ThreadKey != "" {
		// A card raised from mail starts with its conversation's topics;
		// from then on they are the card's own.
		if err := seedLabelsFromThread(t.ID, t.ThreadKey); err != nil {
			return err
		}
	}
	if before == nil || before.Status != t.Status {
		return runEntryRules(t.ID, t.Status)
	}
	return nil
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
	if t.Sources, err = ticketSources(t.ID); err != nil {
		return nil, err
	}
	return t, decorateTickets([]*Ticket{t})
}

func ticketSources(ticketID string) ([]TicketSource, error) {
	rows, err := db.Query(`
		SELECT ts.message_id, COALESCE(ts.annotation_id, ''),
		       COALESCE(m.subject, ''), COALESCE(m.from_addr, ''), COALESCE(m.date, 0),
		       (SELECT COUNT(DISTINCT a.content_hash)
		        FROM attachments a WHERE a.message_id = ts.message_id)
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
		if err := rows.Scan(&s.MessageID, &s.AnnotationID, &s.Subject, &s.From, &date,
			&s.Attachments); err != nil {
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

// SetTicketStatus moves a ticket to the bottom of a list.
func SetTicketStatus(id, status string) error { return MoveTicket(id, status, "") }

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

// SettingTicketAutoAccept is the confidence at or above which a proposed
// ticket skips the queue and lands in the working set.
//
// A setting rather than only a flag because it is a judgement about how much
// you trust your extractors, which is a standing preference, not a per-run
// decision. The CLI flag still overrides it for one run.
const SettingTicketAutoAccept = "ticket_auto_accept"

// DefaultTicketAutoAccept returns the stored threshold.
//
// Above 1 means "never auto-accept", which is the safe default: extraction
// proposes, and a task list you cannot trust is worse than no task list.
func DefaultTicketAutoAccept() float64 {
	raw, err := GetSetting(SettingTicketAutoAccept)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 1.1
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v < 0 {
		return 1.1
	}
	return v
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
		       m.thread_key, COALESCE(m.subject, '')
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

		status := ProposedList()
		if c.confidence != nil && *c.confidence >= autoAccept {
			if status, err = FirstOpenList(); err != nil {
				return nil, err
			}
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

// AcceptTicket moves a proposal into the working set: the first open list.
func AcceptTicket(id string) error {
	list, err := FirstOpenList()
	if err != nil {
		return err
	}
	return SetTicketStatus(id, list)
}

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

// BoardColumn is one list on a board, with the cards the filter lets through.
type BoardColumn struct {
	// Status is the list's id, which is what a card in it holds.
	Status   string    `json:"status"`
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`
	Position float64   `json:"position"`
	Total    int64     `json:"total"`
	Tickets  []*Ticket `json:"tickets"`
}

// BoardColumns builds a board from a filter and the lists.
//
// The two are deliberately separate. The filter says which tickets appear;
// the columns are the lists, which are the values of the status field. That
// is what gives dragging a card an unambiguous meaning — it writes that
// field, which is the inverse of the query that defined the column. If
// columns were arbitrary queries there would be no answer to what dragging
// from `due:7d` into `from:acme` should do.
//
// Rejected tickets are kept as evidence that the extractor was wrong, but a
// board is a working surface and they are not work, so rejected is not a list.
func BoardColumns(filter string, perColumn int) ([]*BoardColumn, error) {
	if perColumn <= 0 {
		perColumn = 10
	}
	lists, err := TicketLists(false)
	if err != nil {
		return nil, err
	}
	if strings.Contains(filter, "|") {
		return nil, fmt.Errorf("a board filter chooses cards; it takes no | stages")
	}

	out := []*BoardColumn{}
	for _, l := range lists {
		scoped := strings.TrimSpace("in:tickets " + filter + " status:" + l.ID)
		res, err := RunQuery(scoped+" | sort position", perColumn, 0)
		if err != nil {
			return nil, err
		}
		total, err := CountQuery(scoped)
		if err != nil {
			return nil, err
		}
		out = append(out, &BoardColumn{Status: l.ID, Title: l.Title, Kind: l.Kind,
			Position: l.Position, Total: total, Tickets: res.Tickets})
	}
	return out, nil
}
