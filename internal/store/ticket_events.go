package store

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Ticket event kinds.
//
// Deliberately a small, closed set. An event log that records every field a
// form could touch becomes unreadable, and a timeline is read by a person: it
// wants the handful of moments that change what a ticket *means*, not an audit
// of every keystroke. Title and body edits are not here for that reason — they
// refine a ticket, they do not move it.
const (
	// EventCreated is the ticket being raised, with the status it started in.
	EventCreated = "created"
	// EventStatus is a move between columns, which is what dragging a card does.
	EventStatus = "status"
	// EventPriority and EventDue are the two other fields a person sets that
	// change what they are being asked to do, rather than how it is described.
	EventPriority = "priority"
	EventDue      = "due"
	// EventEvidence is another message attached as a source, which is how a
	// ticket grows as a conversation continues.
	EventEvidence = "evidence"
	// EventMerged records that another ticket was folded into this one.
	EventMerged = "merged"
)

// TicketEvent is one moment in a ticket's history.
//
// From and To are strings rather than typed fields because the log is
// heterogeneous — a status move and a due-date change are the same shape to a
// reader — and because the whole point is to preserve what a value *was*,
// including values that no longer parse under today's rules.
type TicketEvent struct {
	ID       string `json:"id"`
	TicketID string `json:"ticketId"`
	Kind     string `json:"kind"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	// Actor is who did it: a person, or the annotator that raised the ticket.
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
}

// recordTicketEvent appends to a ticket's history.
//
// Failures here are returned rather than swallowed. A silently missing event
// is the worst of both worlds: the timeline looks complete and is wrong, and
// nothing in the system would ever notice.
func recordTicketEvent(ticketID, kind, from, to, actor string, at time.Time) error {
	if actor == "" {
		actor = OriginHumanTicket
	}
	_, err := db.Exec(`
		INSERT INTO ticket_events (id, ticket_id, kind, from_value, to_value, actor, at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), ticketID, kind, nullIfEmpty(from), nullIfEmpty(to),
		actor, at.UnixMilli())
	return err
}

// TicketEvents returns a ticket's history, oldest first.
func TicketEvents(ticketID string) ([]TicketEvent, error) {
	rows, err := db.Query(`
		SELECT id, ticket_id, kind, COALESCE(from_value, ''), COALESCE(to_value, ''), actor, at
		FROM ticket_events WHERE ticket_id = ? ORDER BY at ASC, rowid ASC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TicketEvent{}
	for rows.Next() {
		var e TicketEvent
		var at int64
		if err := rows.Scan(&e.ID, &e.TicketID, &e.Kind, &e.From, &e.To, &e.Actor, &at); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ticketEventsForThreads loads the history of every ticket on the given
// conversations in one pass, keyed by ticket id.
//
// One query rather than one per ticket: a timeline over a page of threads
// would otherwise issue a query per ticket per thread, and this is the join
// that makes the whole view affordable.
func ticketEventsForThreads(keys []string) (map[string][]TicketEvent, error) {
	out := map[string][]TicketEvent{}
	if len(keys) == 0 {
		return out, nil
	}

	sql := `
		SELECT e.id, e.ticket_id, e.kind, COALESCE(e.from_value, ''), COALESCE(e.to_value, ''),
		       e.actor, e.at
		FROM ticket_events e
		JOIN tickets t ON t.id = e.ticket_id
		WHERE t.thread_key IN (` + placeholders(len(keys)) + `)
		ORDER BY e.at ASC, e.rowid ASC`

	rows, err := db.Query(sql, anySlice(keys)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var e TicketEvent
		var at int64
		if err := rows.Scan(&e.ID, &e.TicketID, &e.Kind, &e.From, &e.To, &e.Actor, &at); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at)
		out[e.TicketID] = append(out[e.TicketID], e)
	}
	return out, rows.Err()
}

// recordTicketDiff writes the events implied by a change to a ticket.
//
// # Why the actor defaults to a person
//
// The store has no request context, so it cannot be told who is calling. It
// does not have to guess, because the system already guarantees the answer: a
// re-run attaches evidence and never rewrites status, priority or due (see
// ProposeTickets). So a field that moved was moved by a person. If that
// invariant is ever relaxed, this attribution has to become an argument rather
// than an assumption — it is the one thing here that is inferred.
func recordTicketDiff(before, after *Ticket, at time.Time) error {
	if before == nil {
		if err := recordTicketEvent(after.ID, EventCreated, "", after.Status, after.Origin, at); err != nil {
			return err
		}
		// A ticket raised with a due date or a priority had them set at that
		// moment, so they are logged like any other setting of them. The
		// alternative — folding them into the "raised" line — reads correctly
		// until one of them changes later, at which point the raised line is
		// showing today's value at the original time.
		if after.Priority != "" {
			if err := recordTicketEvent(after.ID, EventPriority, "", after.Priority,
				after.Origin, at); err != nil {
				return err
			}
		}
		if due := formatDue(after.DueAt); due != "" {
			return recordTicketEvent(after.ID, EventDue, "", due, after.Origin, at)
		}
		return nil
	}

	if before.Status != after.Status {
		if err := recordTicketEvent(after.ID, EventStatus, before.Status, after.Status,
			OriginHumanTicket, at); err != nil {
			return err
		}
	}
	if before.Priority != after.Priority {
		if err := recordTicketEvent(after.ID, EventPriority, before.Priority, after.Priority,
			OriginHumanTicket, at); err != nil {
			return err
		}
	}
	if formatDue(before.DueAt) != formatDue(after.DueAt) {
		if err := recordTicketEvent(after.ID, EventDue, formatDue(before.DueAt), formatDue(after.DueAt),
			OriginHumanTicket, at); err != nil {
			return err
		}
	}
	return nil
}

// formatDue renders a due date for the log, or "" for none.
//
// Date precision, not timestamp: a due date is a day, and logging it to the
// millisecond would make "moved from no date to the 8th" read as noise.
func formatDue(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// placeholders builds "?, ?, ?" for an IN clause.
//
// n is always a slice length this package computed, never anything a user
// supplied, so the values still arrive as bound arguments.
func placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// anySlice widens a string slice for database/sql's variadic arguments.
func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
