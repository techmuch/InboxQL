package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/query"
)

// Thread entry kinds.
const (
	EntryMessage    = "message"
	EntryTicket     = "ticket"
	EntryDraft      = "draft"
	EntryEvent      = "event"
	EntryAnnotation = "annotation"
)

// ThreadEntry is one moment on a conversation's timeline.
//
// Heterogeneous on purpose. A conversation is not a list of emails with things
// hanging off it — it is a sequence of events that happen to have different
// types, and interleaving them by time is the only ordering that answers "what
// happened here". Exactly one of the payload fields is set; Kind says which.
type ThreadEntry struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`

	Message    *message.Message `json:"message,omitempty"`
	Ticket     *Ticket          `json:"ticket,omitempty"`
	Draft      *Draft           `json:"draft,omitempty"`
	Event      *TicketEvent     `json:"event,omitempty"`
	Annotation *Annotation      `json:"annotation,omitempty"`

	// Actor is who or what produced this entry: an address for a message, an
	// annotator's name for a label, "human" for a move someone made.
	Actor string `json:"actor,omitempty"`
	// Summary is a one-line rendering, so a caller can list a timeline without
	// knowing the shape of all five payload types. The full object is still
	// there for anything that wants to render it properly.
	Summary string `json:"summary"`
}

// Thread is one conversation and everything anchored to it.
type Thread struct {
	Key string `json:"key"`
	// Subject is the first message's subject, which is what a person calls the
	// conversation. Empty for a thread with no mail in it, which is what a
	// hand-raised ticket is.
	Subject      string        `json:"subject,omitempty"`
	Participants []string      `json:"participants,omitempty"`
	Entries      []ThreadEntry `json:"entries"`

	// Counts, so a collapsed row can say what it holds without the caller
	// walking the entries.
	MessageCount int `json:"messageCount"`
	TicketCount  int `json:"ticketCount"`
	DraftCount   int `json:"draftCount"`

	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// scanThreads runs a PlanThreads statement and assembles each conversation.
//
// # Why the assembly is here and not in SQL
//
// The plan selects keys; this fills them in. Five queries total, regardless of
// how many threads came back — not five per thread — because the alternative
// shape (a query per entity per thread) is what turns a 50-thread page into
// 250 round trips.
//
// # Why it is here and not in the frontend
//
// Because merging typed events into one ordering is a decision about meaning,
// and the last three times a decision like that lived in TypeScript it was
// made three different ways and each of them was wrong. The client receives a
// list it renders in order.
func scanThreads(plan *query.Plan) ([]*Thread, error) {
	keys, err := threadKeys(plan)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []*Thread{}, nil
	}
	return LoadThreads(keys)
}

func threadKeys(plan *query.Plan) ([]string, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// LoadThreads assembles the named conversations, preserving the order given.
//
// The order is the caller's: the plan already sorted by recency, and re-sorting
// here would quietly discard it.
func LoadThreads(keys []string) ([]*Thread, error) {
	if len(keys) == 0 {
		return []*Thread{}, nil
	}

	threads := make(map[string]*Thread, len(keys))
	order := make([]*Thread, 0, len(keys))
	for _, k := range keys {
		if _, seen := threads[k]; seen {
			continue
		}
		t := &Thread{Key: k, Entries: []ThreadEntry{}}
		threads[k] = t
		order = append(order, t)
	}

	if err := loadThreadMessages(threads, keys); err != nil {
		return nil, err
	}
	if err := loadThreadTickets(threads, keys); err != nil {
		return nil, err
	}
	if err := loadThreadDrafts(threads, keys); err != nil {
		return nil, err
	}
	if err := loadThreadAnnotations(threads, keys); err != nil {
		return nil, err
	}

	for _, t := range order {
		finishThread(t)
	}
	return order, nil
}

// threadMatch is the WHERE clause that resolves a key against a table.
//
// It mirrors the plan's COALESCE(thread_key, id) exactly. The id branch is
// reachable only for rows whose key is NULL, so a conversation key can never
// pull in an unrelated row that happens to share its id.
func threadMatch(alias string, n int) string {
	ph := placeholders(n)
	return "(" + alias + ".thread_key IN (" + ph + ") OR (" + alias +
		".thread_key IS NULL AND " + alias + ".id IN (" + ph + ")))"
}

// twice repeats the key list, since threadMatch binds it on both branches.
func twice(keys []string) []any {
	args := anySlice(keys)
	return append(args, anySlice(keys)...)
}

func threadKeyOf(key, id string) string {
	if key != "" {
		return key
	}
	return id
}

func loadThreadMessages(threads map[string]*Thread, keys []string) error {
	rows, err := db.Query(`SELECT COALESCE(m.thread_key, ''), `+aliasedMessageColumns()+`
		FROM messages m WHERE `+threadMatch("m", len(keys))+` ORDER BY m.date ASC`,
		twice(keys)...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		m, err := scanMessage(func(dest ...any) error {
			return rows.Scan(append([]any{&key}, dest...)...)
		})
		if err != nil {
			return err
		}
		t := threads[threadKeyOf(key, m.ID)]
		if t == nil {
			continue
		}
		t.MessageCount++
		t.Entries = append(t.Entries, ThreadEntry{
			Kind: EntryMessage, At: m.Date, Message: m,
			Actor: m.From, Summary: m.Subject,
		})
	}
	return rows.Err()
}

func loadThreadTickets(threads map[string]*Thread, keys []string) error {
	rows, err := db.Query("SELECT COALESCE(t.thread_key, ''), "+ticketColumns+
		" FROM tickets t WHERE "+threadMatch("t", len(keys)), twice(keys)...)
	if err != nil {
		return err
	}
	defer rows.Close()

	ids := []string{}
	byID := map[string]*Ticket{}
	owner := map[string]*Thread{}

	for rows.Next() {
		var key string
		tk, err := scanTicket(func(dest ...any) error {
			return rows.Scan(append([]any{&key}, dest...)...)
		})
		if err != nil {
			return err
		}
		t := threads[threadKeyOf(key, tk.ID)]
		if t == nil {
			continue
		}
		t.TicketCount++
		ids = append(ids, tk.ID)
		byID[tk.ID] = tk
		owner[tk.ID] = t
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// A ticket's history is what makes the timeline a narrative rather than a
	// pile: "raised on the 2nd, started on the 6th" instead of one undated
	// card. The ticket itself is not an entry — every one of its moments is.
	events, err := ticketEventsForThreads(keys)
	if err != nil {
		return err
	}
	for _, id := range ids {
		tk, t := byID[id], owner[id]
		history := events[id]
		if len(history) == 0 {
			// A ticket written before the event log existed, or one whose
			// backfill found nothing. Showing it at its creation time is
			// better than dropping it from its own conversation.
			t.Entries = append(t.Entries, ThreadEntry{
				Kind: EntryTicket, At: tk.CreatedAt, Ticket: tk,
				Actor: tk.Origin, Summary: tk.Title,
			})
			continue
		}
		for i := range history {
			e := history[i]
			t.Entries = append(t.Entries, ThreadEntry{
				Kind: EntryEvent, At: e.At, Event: &e, Ticket: tk,
				Actor: e.Actor, Summary: describeEvent(&e, tk),
			})
		}
	}
	return nil
}

func loadThreadDrafts(threads map[string]*Thread, keys []string) error {
	rows, err := db.Query("SELECT COALESCE(d.thread_key, ''), "+draftColumns+
		" FROM drafts d WHERE "+threadMatch("d", len(keys))+" ORDER BY d.created_at ASC",
		twice(keys)...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		d, err := scanDraft(func(dest ...any) error {
			return rows.Scan(append([]any{&key}, dest...)...)
		})
		if err != nil {
			return err
		}
		t := threads[threadKeyOf(key, d.ID)]
		if t == nil {
			continue
		}
		t.DraftCount++
		// A sent draft belongs at the time it was sent, not the time someone
		// started writing it — that is where it sits in the conversation.
		at := d.CreatedAt
		if d.SentAt != nil {
			at = *d.SentAt
		}
		t.Entries = append(t.Entries, ThreadEntry{
			Kind: EntryDraft, At: at, Draft: d,
			Actor: d.Origin, Summary: d.Subject,
		})
	}
	return rows.Err()
}

// loadThreadAnnotations adds the labels and extractions on a conversation's mail.
//
// Only current-version results, and only the ones that succeeded. An
// annotation from a superseded prompt is not an answer to today's question,
// and an error belongs in the annotator's own diagnostics rather than in a
// reading view of a conversation.
func loadThreadAnnotations(threads map[string]*Thread, keys []string) error {
	rows, err := db.Query(`
		SELECT COALESCE(m.thread_key, ''), m.id, an.id, an.annotator_id, an.annotator_version,
		       an.seq, an.status, an.source, an.data_json, an.confidence,
		       COALESCE(an.model, ''), COALESCE(an.error, ''), an.created_at,
		       a.name
		FROM annotations an
		JOIN messages m   ON m.id = an.message_id
		JOIN annotators a ON a.id = an.annotator_id AND a.version = an.annotator_version
		WHERE `+threadMatch("m", len(keys))+` AND an.status = ?
		ORDER BY an.created_at ASC`, append(twice(keys), StatusOK)...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var key, messageID, name string
		var created int64
		a := &Annotation{}
		if err := rows.Scan(&key, &messageID, &a.ID, &a.AnnotatorID, &a.AnnotatorVersion,
			&a.Seq, &a.Status, &a.Source, &a.DataJSON, &a.Confidence,
			&a.Model, &a.Error, &created, &name); err != nil {
			return err
		}
		a.MessageID = messageID
		a.CreatedAt = time.UnixMilli(created)

		t := threads[threadKeyOf(key, messageID)]
		if t == nil {
			continue
		}
		t.Entries = append(t.Entries, ThreadEntry{
			Kind: EntryAnnotation, At: a.CreatedAt, Annotation: a,
			Actor: name, Summary: name,
		})
	}
	return rows.Err()
}

// describeEvent renders one ticket moment as a line of prose.
func describeEvent(e *TicketEvent, tk *Ticket) string {
	switch e.Kind {
	case EventCreated:
		return "ticket raised: " + tk.Title
	case EventStatus:
		return "ticket moved to " + e.To
	case EventPriority:
		if e.To == "" {
			return "priority cleared"
		}
		return "priority set to " + e.To
	case EventDue:
		if e.To == "" {
			return "due date cleared"
		}
		return "due " + e.To
	case EventEvidence:
		return "another message attached as evidence"
	case EventMerged:
		return "merged in: " + e.From
	}
	return e.Kind
}

// finishThread orders a conversation's entries and derives its summary fields.
func finishThread(t *Thread) {
	// Stable, so entries that share a timestamp — a message and the label
	// applied to it in the same import — keep the order they were loaded in,
	// which puts the message before the thing derived from it.
	sort.SliceStable(t.Entries, func(i, j int) bool {
		return t.Entries[i].At.Before(t.Entries[j].At)
	})

	seen := map[string]bool{}
	for i := range t.Entries {
		e := &t.Entries[i]
		if t.Start.IsZero() || e.At.Before(t.Start) {
			t.Start = e.At
		}
		if e.At.After(t.End) {
			t.End = e.At
		}
		if e.Kind == EntryMessage {
			if t.Subject == "" {
				t.Subject = e.Message.Subject
			}
			for _, raw := range messageAddresses(e.Message) {
				addr, _ := NormaliseAddress(raw)
				if addr != "" && !seen[addr] {
					seen[addr] = true
					t.Participants = append(t.Participants, addr)
				}
			}
		}
	}

	// A conversation with no mail is a hand-raised ticket. Naming it after the
	// ticket is better than leaving a blank row on the board's timeline.
	if t.Subject == "" {
		for i := range t.Entries {
			if tk := t.Entries[i].Ticket; tk != nil {
				t.Subject = tk.Title
				break
			}
		}
	}
}

func messageAddresses(m *message.Message) []string {
	out := make([]string, 0, 1+len(m.To)+len(m.Cc))
	out = append(out, m.From)
	out = append(out, m.To...)
	out = append(out, m.Cc...)
	return out
}

// MessageThreadKey returns the conversation a message belongs to.
//
// Falls back to the message's own id, matching the plan's COALESCE, so a
// message written before thread keys existed still names a conversation
// consistently everywhere.
func MessageThreadKey(messageID string) (string, error) {
	var key sql.NullString
	err := db.QueryRow("SELECT thread_key FROM messages WHERE id = ?", messageID).Scan(&key)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no message %q", messageID)
	}
	if err != nil {
		return "", err
	}
	if key.String == "" {
		return messageID, nil
	}
	return key.String, nil
}
