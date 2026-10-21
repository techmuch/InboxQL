package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/query"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// --- where a card sits ---------------------------------------------------

// bottomOf is a position below every card in a list.
func bottomOf(list string) (float64, error) {
	var max sql.NullFloat64
	if err := db.QueryRow("SELECT MAX(position) FROM tickets WHERE status = ?", list).Scan(&max); err != nil {
		return 0, err
	}
	if !max.Valid {
		return 1, nil
	}
	return max.Float64 + 1, nil
}

// cardPositionAfter is a position just after one card in a list and before
// the next; after == "" is the bottom, "^" the top. The moving card is left
// out, so moving a card next to itself is a no-op rather than a puzzle.
//
// Neighbours are found in the whole list, not the filtered board, so "after
// this card" means the same thing whatever the board is showing.
func cardPositionAfter(list, after, moving string) (float64, error) {
	for attempt := 0; attempt < 2; attempt++ {
		rows, err := db.Query(`SELECT id, COALESCE(position, 0) FROM tickets
			WHERE status = ? AND id <> ? ORDER BY position, created_at`, list, moving)
		if err != nil {
			return 0, err
		}
		var ids []string
		var ps []float64
		for rows.Next() {
			var id string
			var p float64
			if err := rows.Scan(&id, &p); err != nil {
				rows.Close()
				return 0, err
			}
			ids = append(ids, id)
			ps = append(ps, p)
		}
		rows.Close()
		if len(ids) == 0 {
			return 1, nil
		}

		var lo, hi float64
		switch after {
		case "":
			return ps[len(ps)-1] + 1, nil
		case "^":
			return ps[0] - 1, nil
		default:
			i := indexOf(ids, after)
			if i < 0 {
				return 0, fmt.Errorf("card %q is not in that list", after)
			}
			if i == len(ids)-1 {
				return ps[i] + 1, nil
			}
			lo, hi = ps[i], ps[i+1]
		}
		// Halving forever runs out of float precision after about fifty moves
		// into the same gap. When the gap is that small the list is
		// renumbered once and the position worked out again.
		if hi-lo > 1e-9 {
			return (lo + hi) / 2, nil
		}
		if err := renumberList(list); err != nil {
			return 0, err
		}
	}
	return 0, fmt.Errorf("could not find a place in %q", list)
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// renumberList spreads a list's cards back out to 1, 2, 3, keeping order.
//
// The order is read first and written after. A single UPDATE with a ranking
// subquery re-ranks against rows it has already rewritten, and can leave two
// cards on the same number — the very gap it was meant to open.
func renumberList(list string) error {
	rows, err := db.Query("SELECT id FROM tickets WHERE status = ? ORDER BY position, created_at", list)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE tickets SET position = ? WHERE id = ?", float64(i+1), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MoveTicket puts a card in a list after another card: after == "" is the
// bottom, "^" the top. What dragging a card does, between lists or within one.
func MoveTicket(id, list, after string) error { return moveTicket(id, list, after, OriginHumanTicket) }

func moveTicket(id, list, after, actor string) error {
	t, err := loadTicket(id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("no ticket %q", id)
	}
	if list == "" {
		list = t.Status
	}
	if list != t.Status {
		t.Status = list
		if err := saveTicket(t, actor); err != nil {
			return err
		}
		if after == "" {
			return nil // saveTicket already put it at the bottom
		}
	}
	pos, err := cardPositionAfter(list, after, id)
	if err != nil {
		return err
	}
	_, err = db.Exec("UPDATE tickets SET position = ? WHERE id = ?", pos, id)
	return err
}

// FindTicket resolves a full id or a unique prefix of one. The CLI prints
// eight characters, so those eight have to be enough to act on.
func FindTicket(ref string) (*Ticket, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("which ticket?")
	}
	if t, err := GetTicket(ref); err != nil || t != nil {
		return t, err
	}
	rows, err := db.Query(`SELECT id FROM tickets WHERE id LIKE ? ESCAPE '\' LIMIT 2`,
		escapeLike(ref)+"%")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	switch len(ids) {
	case 0:
		return nil, fmt.Errorf("no ticket matching %q", ref)
	case 1:
		return GetTicket(ids[0])
	}
	return nil, fmt.Errorf("%q matches more than one ticket; use more of the id", ref)
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ArchiveTicket takes a card off the board, or puts it back. Archived is not
// deleted: the card stays searchable with `archived:yes`.
func ArchiveTicket(id string, archived bool) error {
	return archiveTicket(id, archived, OriginHumanTicket)
}

func archiveTicket(id string, archived bool, actor string) error {
	t, err := loadTicket(id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("no ticket %q", id)
	}
	if (t.ArchivedAt != nil) == archived {
		return nil
	}
	now := time.Now()
	var at any
	kind := EventRestored
	if archived {
		at = now.UnixMilli()
		kind = EventArchived
	}
	if _, err := db.Exec("UPDATE tickets SET archived_at = ?, updated_at = ? WHERE id = ?",
		at, now.UnixMilli(), id); err != nil {
		return err
	}
	return recordTicketEvent(id, kind, "", "", actor, now)
}

// --- labels ----------------------------------------------------------------

// NormalizeCardLabel is how a card label is spelled, the same as a topic on mail.
func NormalizeCardLabel(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// AddTicketLabels puts labels on a card; ones it already has are left alone.
func AddTicketLabels(id string, labels []string) error {
	return addTicketLabels(id, labels, OriginHumanTicket)
}

func addTicketLabels(id string, labels []string, actor string) error {
	if t, err := loadTicket(id); err != nil || t == nil {
		if err == nil {
			err = fmt.Errorf("no ticket %q", id)
		}
		return err
	}
	now := time.Now()
	for _, l := range labels {
		l = NormalizeCardLabel(l)
		if l == "" {
			continue
		}
		res, err := db.Exec(`INSERT OR IGNORE INTO ticket_labels (ticket_id, label, created_at)
			VALUES (?, ?, ?)`, id, l, now.UnixMilli())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if err := recordTicketEvent(id, EventLabel, "", l, actor, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveTicketLabels takes labels off a card.
func RemoveTicketLabels(id string, labels []string) error {
	return removeTicketLabels(id, labels, OriginHumanTicket)
}

func removeTicketLabels(id string, labels []string, actor string) error {
	now := time.Now()
	for _, l := range labels {
		l = NormalizeCardLabel(l)
		res, err := db.Exec("DELETE FROM ticket_labels WHERE ticket_id = ? AND label = ?", id, l)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if err := recordTicketEvent(id, EventLabel, l, "", actor, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetTicketLabels makes a card's labels exactly these.
func SetTicketLabels(id string, labels []string) error {
	current, err := ticketLabels(id)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, l := range labels {
		if l = NormalizeCardLabel(l); l != "" {
			want[l] = true
		}
	}
	var drop []string
	for _, l := range current {
		if !want[l] {
			drop = append(drop, l)
		}
		delete(want, l)
	}
	if err := RemoveTicketLabels(id, drop); err != nil {
		return err
	}
	add := make([]string, 0, len(want))
	for l := range want {
		add = append(add, l)
	}
	sort.Strings(add)
	return AddTicketLabels(id, add)
}

func ticketLabels(id string) ([]string, error) {
	rows, err := db.Query("SELECT label FROM ticket_labels WHERE ticket_id = ? ORDER BY label", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SeedTicketLabels gives a card just made the topics its conversation has
// now, without history, because it is where the card started. Only for new
// cards: on an old one it would put back labels somebody took off.
func SeedTicketLabels(id string) error {
	t, err := loadTicket(id)
	if err != nil || t == nil || t.ThreadKey == "" {
		return err
	}
	return seedLabelsFromThread(id, t.ThreadKey)
}

// seedLabelsFromThread gives a new card its conversation's topics.
func seedLabelsFromThread(id, threadKey string) error {
	_, err := db.Exec(`INSERT OR IGNORE INTO ticket_labels (ticket_id, label, created_at)
		SELECT DISTINCT ?, mt.topic, ?
		FROM messages m JOIN message_topics mt ON mt.message_id = m.id
		WHERE m.thread_key = ?`, id, time.Now().UnixMilli(), threadKey)
	return err
}

// LabelColors are the colours a label can be, as Trello has them.
var LabelColors = []string{"green", "yellow", "orange", "red", "purple", "blue", "sky", "lime", "pink", "black"}

// LabelInfo is one label: its colour, and how much carries it.
type LabelInfo struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// Chosen says the colour was picked rather than derived from the name.
	Chosen   bool  `json:"chosen,omitempty"`
	Cards    int64 `json:"cards"`
	Messages int64 `json:"messages"`
}

// defaultLabelColor is a colour that stays the same for a name, so a label
// nobody coloured looks the same on every card and every day.
func defaultLabelColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return LabelColors[h.Sum32()%uint32(len(LabelColors))]
}

// BoardLabels lists the labels on cards and the topics on mail, one
// vocabulary, most used first.
func BoardLabels() ([]LabelInfo, error) {
	rows, err := db.Query(`
		SELECT name, SUM(cards), SUM(messages) FROM (
			SELECT label AS name, COUNT(*) AS cards, 0 AS messages FROM ticket_labels GROUP BY label
			UNION ALL
			SELECT topic, 0, COUNT(*) FROM message_topics GROUP BY topic
			UNION ALL
			SELECT topic, 0, 0 FROM topic_colors)
		GROUP BY name ORDER BY SUM(cards) DESC, SUM(messages) DESC, name`)
	if err != nil {
		return nil, err
	}
	var out []LabelInfo
	for rows.Next() {
		var l LabelInfo
		if err := rows.Scan(&l.Name, &l.Cards, &l.Messages); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	chosen, err := chosenColors()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if c, ok := chosen[out[i].Name]; ok {
			out[i].Color, out[i].Chosen = c, true
		} else {
			out[i].Color = defaultLabelColor(out[i].Name)
		}
	}
	return out, nil
}

func chosenColors() (map[string]string, error) {
	rows, err := db.Query("SELECT topic, color FROM topic_colors")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out[t] = c
	}
	return out, rows.Err()
}

// SetLabelColor picks a label's colour; "" goes back to the derived one.
func SetLabelColor(label, color string) error {
	label = NormalizeCardLabel(label)
	if label == "" {
		return fmt.Errorf("which label?")
	}
	if color == "" {
		_, err := db.Exec("DELETE FROM topic_colors WHERE topic = ?", label)
		return err
	}
	color = strings.ToLower(strings.TrimSpace(color))
	if indexOf(LabelColors, color) < 0 {
		return fmt.Errorf("%q is not a label colour (%s)", color, strings.Join(LabelColors, ", "))
	}
	_, err := db.Exec(`INSERT INTO topic_colors (topic, color) VALUES (?, ?)
		ON CONFLICT(topic) DO UPDATE SET color = excluded.color`, label, color)
	return err
}

// --- checklists ------------------------------------------------------------

// ChecklistItem is one line of a card's checklist.
type ChecklistItem struct {
	ID        string     `json:"id"`
	TicketID  string     `json:"ticketId"`
	Checklist string     `json:"checklist"`
	Text      string     `json:"text"`
	Done      bool       `json:"done"`
	Position  float64    `json:"position"`
	CreatedAt time.Time  `json:"createdAt"`
	DoneAt    *time.Time `json:"doneAt,omitempty"`
}

// DefaultChecklist is the name a checklist gets when nobody gave one.
const DefaultChecklist = "Checklist"

// AddChecklistItem adds a line to the bottom of a checklist on a card.
func AddChecklistItem(ticketID, checklist, text string) (*ChecklistItem, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("a checklist item needs text")
	}
	checklist = strings.TrimSpace(checklist)
	if checklist == "" {
		checklist = DefaultChecklist
	}
	if t, err := loadTicket(ticketID); err != nil || t == nil {
		if err == nil {
			err = fmt.Errorf("no ticket %q", ticketID)
		}
		return nil, err
	}
	var max sql.NullFloat64
	if err := db.QueryRow("SELECT MAX(position) FROM ticket_checklist_items WHERE ticket_id = ?",
		ticketID).Scan(&max); err != nil {
		return nil, err
	}
	it := &ChecklistItem{ID: uuid.New().String(), TicketID: ticketID, Checklist: checklist,
		Text: text, Position: max.Float64 + 1, CreatedAt: time.Now()}
	_, err := db.Exec(`INSERT INTO ticket_checklist_items
		(id, ticket_id, checklist, text, done, position, created_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		it.ID, it.TicketID, it.Checklist, it.Text, it.Position, it.CreatedAt.UnixMilli())
	return it, err
}

// ChecklistItems returns a card's checklist lines, checklist by checklist.
func ChecklistItems(ticketID string) ([]ChecklistItem, error) {
	rows, err := db.Query(`SELECT id, ticket_id, checklist, text, done, position, created_at, done_at
		FROM ticket_checklist_items WHERE ticket_id = ?
		ORDER BY (SELECT MIN(i2.position) FROM ticket_checklist_items i2
		          WHERE i2.ticket_id = ticket_checklist_items.ticket_id
		            AND i2.checklist = ticket_checklist_items.checklist), position`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var it ChecklistItem
		var created int64
		var done sql.NullInt64
		if err := rows.Scan(&it.ID, &it.TicketID, &it.Checklist, &it.Text, &it.Done, &it.Position,
			&created, &done); err != nil {
			return nil, err
		}
		it.CreatedAt = time.UnixMilli(created)
		it.DoneAt = optionalTime(done)
		out = append(out, it)
	}
	return out, rows.Err()
}

func checklistItem(id string) (*ChecklistItem, error) {
	var it ChecklistItem
	var created int64
	var done sql.NullInt64
	err := db.QueryRow(`SELECT id, ticket_id, checklist, text, done, position, created_at, done_at
		FROM ticket_checklist_items WHERE id = ?`, id).Scan(&it.ID, &it.TicketID, &it.Checklist,
		&it.Text, &it.Done, &it.Position, &created, &done)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no checklist item %q", id)
	}
	it.CreatedAt = time.UnixMilli(created)
	it.DoneAt = optionalTime(done)
	return &it, err
}

// FindChecklistItem resolves an item by id, id prefix, or its text on a card.
func FindChecklistItem(ticketID, ref string) (*ChecklistItem, error) {
	items, err := ChecklistItems(ticketID)
	if err != nil {
		return nil, err
	}
	var hits []ChecklistItem
	for _, it := range items {
		if it.ID == ref {
			return &it, nil
		}
		if strings.HasPrefix(it.ID, ref) || strings.EqualFold(it.Text, ref) {
			hits = append(hits, it)
		}
	}
	if len(hits) == 1 {
		return &hits[0], nil
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no checklist item %q on that card", ref)
	}
	return nil, fmt.Errorf("%q matches more than one checklist item", ref)
}

// SetChecklistItemDone ticks a line or unticks it.
func SetChecklistItemDone(id string, done bool) error {
	return setChecklistItemDone(id, done, OriginHumanTicket)
}

func setChecklistItemDone(id string, done bool, actor string) error {
	it, err := checklistItem(id)
	if err != nil {
		return err
	}
	if it.Done == done {
		return nil
	}
	now := time.Now()
	var at any
	if done {
		at = now.UnixMilli()
	}
	if _, err := db.Exec("UPDATE ticket_checklist_items SET done = ?, done_at = ? WHERE id = ?",
		done, at, id); err != nil {
		return err
	}
	if done {
		return recordTicketEvent(it.TicketID, EventChecked, "", it.Text, actor, now)
	}
	return nil
}

// EditChecklistItem changes a line's text.
func EditChecklistItem(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("a checklist item needs text")
	}
	return execOne("UPDATE ticket_checklist_items SET text = ? WHERE id = ?", "checklist item", id, text, id)
}

// DeleteChecklistItem removes a line.
func DeleteChecklistItem(id string) error {
	return execOne("DELETE FROM ticket_checklist_items WHERE id = ?", "checklist item", id, id)
}

// RenameChecklist renames one of a card's checklists.
func RenameChecklist(ticketID, from, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("a checklist needs a name")
	}
	return execOne("UPDATE ticket_checklist_items SET checklist = ? WHERE ticket_id = ? AND checklist = ?",
		"checklist", from, to, ticketID, from)
}

// DeleteChecklist removes a whole checklist from a card.
func DeleteChecklist(ticketID, checklist string) error {
	return execOne("DELETE FROM ticket_checklist_items WHERE ticket_id = ? AND checklist = ?",
		"checklist", checklist, ticketID, checklist)
}

// checkAll ticks every open line on a card, which is what a rule does when a
// card reaches Done.
func checkAll(ticketID, actor string) error {
	items, err := ChecklistItems(ticketID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if !it.Done {
			if err := setChecklistItemDone(it.ID, true, actor); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- comments --------------------------------------------------------------

// TicketComment is a note on a card, kept in its history.
type TicketComment struct {
	ID        string    `json:"id"`
	TicketID  string    `json:"ticketId"`
	Body      string    `json:"body"`
	BodyHTML  string    `json:"bodyHtml,omitempty"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AddTicketComment writes a comment on a card. actor is "human", or "agent"
// for a harness writing through the CLI.
func AddTicketComment(ticketID, body, actor string) (*TicketComment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("a comment needs text")
	}
	if actor == "" {
		actor = OriginHumanTicket
	}
	if t, err := loadTicket(ticketID); err != nil || t == nil {
		if err == nil {
			err = fmt.Errorf("no ticket %q", ticketID)
		}
		return nil, err
	}
	now := time.Now()
	c := &TicketComment{ID: uuid.New().String(), TicketID: ticketID, Body: body, Actor: actor,
		CreatedAt: now, UpdatedAt: now}
	_, err := db.Exec(`INSERT INTO ticket_comments (id, ticket_id, body, actor, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, c.ID, c.TicketID, c.Body, c.Actor, now.UnixMilli(), now.UnixMilli())
	return c, err
}

// EditTicketComment changes a comment's text.
func EditTicketComment(id, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("a comment needs text")
	}
	return execOne("UPDATE ticket_comments SET body = ?, updated_at = ? WHERE id = ?", "comment", id,
		body, time.Now().UnixMilli(), id)
}

// DeleteTicketComment removes a comment.
func DeleteTicketComment(id string) error {
	return execOne("DELETE FROM ticket_comments WHERE id = ?", "comment", id, id)
}

// TicketComments returns a card's comments, oldest first.
func TicketComments(ticketID string) ([]TicketComment, error) {
	rows, err := db.Query(`SELECT id, ticket_id, body, actor, created_at, updated_at
		FROM ticket_comments WHERE ticket_id = ? ORDER BY created_at, rowid`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketComment{}
	for rows.Next() {
		var c TicketComment
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.TicketID, &c.Body, &c.Actor, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
		c.BodyHTML = RenderMarkdown(c.Body)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- a card, opened ----------------------------------------------------------

// TicketDetail is a card as its panel shows it.
type TicketDetail struct {
	*Ticket
	// BodyHTML is the description rendered from Markdown. Raw HTML in the
	// description is escaped, not passed through.
	BodyHTML  string          `json:"bodyHtml,omitempty"`
	ListTitle string          `json:"listTitle,omitempty"`
	Checklist []ChecklistItem `json:"checklist"`
	Notes     []TicketComment `json:"commentList"`
	History   []TicketEvent   `json:"history"`
}

// GetTicketDetail returns a card with everything its panel shows.
func GetTicketDetail(id string) (*TicketDetail, error) {
	t, err := GetTicket(id)
	if err != nil || t == nil {
		return nil, err
	}
	d := &TicketDetail{Ticket: t, BodyHTML: RenderMarkdown(t.Body)}
	if l, err := GetTicketList(t.Status); err == nil {
		d.ListTitle = l.Title
	}
	if d.Checklist, err = ChecklistItems(t.ID); err != nil {
		return nil, err
	}
	if d.Notes, err = TicketComments(t.ID); err != nil {
		return nil, err
	}
	if d.History, err = TicketEvents(t.ID); err != nil {
		return nil, err
	}
	return d, nil
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// RenderMarkdown renders a card's text. goldmark escapes raw HTML unless told
// otherwise, so what a description says cannot become markup on the page.
func RenderMarkdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return buf.String()
}

// decorateTickets fills in what the front of a card shows beyond its row:
// labels, checklist progress and how many comments. Three queries for any
// number of cards, not three per card.
func decorateTickets(ts []*Ticket) error {
	if len(ts) == 0 {
		return nil
	}
	byID := map[string]*Ticket{}
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	in := placeholders(len(ids))

	rows, err := db.Query("SELECT ticket_id, label FROM ticket_labels WHERE ticket_id IN ("+in+
		") ORDER BY label", anySlice(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, l string
		if err := rows.Scan(&id, &l); err != nil {
			rows.Close()
			return err
		}
		byID[id].Labels = append(byID[id].Labels, l)
	}
	rows.Close()

	rows, err = db.Query("SELECT ticket_id, SUM(done), COUNT(*) FROM ticket_checklist_items WHERE ticket_id IN ("+
		in+") GROUP BY ticket_id", anySlice(ids)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var done, total int
		if err := rows.Scan(&id, &done, &total); err != nil {
			rows.Close()
			return err
		}
		byID[id].ChecklistDone, byID[id].ChecklistTotal = done, total
	}
	rows.Close()

	rows, err = db.Query("SELECT ticket_id, COUNT(*) FROM ticket_comments WHERE ticket_id IN ("+
		in+") GROUP BY ticket_id", anySlice(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		byID[id].Comments = n
	}
	return rows.Err()
}

// --- boards ----------------------------------------------------------------

// TicketBoard is a named filter over the lists: "Home" might be `label:home`.
type TicketBoard struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Filter   string  `json:"filter"`
	Position float64 `json:"position"`
	// Labels are the labels a card added on this board is given: the ones
	// its filter asks for.
	Labels []string `json:"labels,omitempty"`
}

// TicketBoards returns the boards in order.
func TicketBoards() ([]*TicketBoard, error) {
	rows, err := db.Query("SELECT id, title, filter, position FROM ticket_boards ORDER BY position, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TicketBoard{}
	for rows.Next() {
		b := &TicketBoard{}
		if err := rows.Scan(&b.ID, &b.Title, &b.Filter, &b.Position); err != nil {
			return nil, err
		}
		b.Labels = FilterLabels(b.Filter)
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetTicketBoard finds a board by id or title.
func GetTicketBoard(ref string) (*TicketBoard, error) {
	boards, err := TicketBoards()
	if err != nil {
		return nil, err
	}
	for _, b := range boards {
		if b.ID == ref {
			return b, nil
		}
	}
	for _, b := range boards {
		if strings.EqualFold(b.Title, ref) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("no board %q", ref)
}

// checkBoardFilter refuses a filter that is not a query over cards, where it
// is written rather than when the board is opened.
func checkBoardFilter(filter string) error {
	if strings.Contains(filter, "|") {
		return fmt.Errorf("a board filter chooses cards; it takes no | stages")
	}
	_, err := CountQuery(strings.TrimSpace("in:tickets " + filter))
	return err
}

// CreateTicketBoard adds a board at the end.
func CreateTicketBoard(title, filter string) (*TicketBoard, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("a board needs a title")
	}
	filter = strings.TrimSpace(filter)
	if err := checkBoardFilter(filter); err != nil {
		return nil, err
	}
	base := SlugifyQueryName(title)
	if base == "" {
		base = "board"
	}
	id := base
	for i := 2; ; i++ {
		if _, err := GetTicketBoard(id); err != nil {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	var max sql.NullFloat64
	if err := db.QueryRow("SELECT MAX(position) FROM ticket_boards").Scan(&max); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO ticket_boards (id, title, filter, position, created_at)
		VALUES (?, ?, ?, ?, ?)`, id, title, filter, max.Float64+1, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	return GetTicketBoard(id)
}

// UpdateTicketBoard changes a board's title and filter.
func UpdateTicketBoard(id, title, filter string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("a board needs a title")
	}
	filter = strings.TrimSpace(filter)
	if err := checkBoardFilter(filter); err != nil {
		return err
	}
	return execOne("UPDATE ticket_boards SET title = ?, filter = ? WHERE id = ?", "board", id, title, filter, id)
}

// MoveTicketBoard puts a board after another; "^" first, "" last.
func MoveTicketBoard(id, after string) error {
	boards, err := TicketBoards()
	if err != nil {
		return err
	}
	var rest []*TicketBoard
	for _, b := range boards {
		if b.ID != id {
			rest = append(rest, b)
		}
	}
	if len(rest) == len(boards) {
		return fmt.Errorf("no board %q", id)
	}
	pos := 1.0
	switch {
	case len(rest) == 0:
	case after == "":
		pos = rest[len(rest)-1].Position + 1
	case after == "^":
		pos = rest[0].Position - 1
	default:
		i := -1
		for j, b := range rest {
			if b.ID == after {
				i = j
			}
		}
		if i < 0 {
			return fmt.Errorf("no board %q", after)
		}
		pos = rest[i].Position + 1
		if i < len(rest)-1 {
			pos = (rest[i].Position + rest[i+1].Position) / 2
		}
	}
	_, err = db.Exec("UPDATE ticket_boards SET position = ? WHERE id = ?", pos, id)
	return err
}

// DeleteTicketBoard removes a board. Cards are not on a board, they are
// chosen by it, so nothing else changes.
func DeleteTicketBoard(id string) error {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM ticket_boards").Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return fmt.Errorf("that is the only board")
	}
	return execOne("DELETE FROM ticket_boards WHERE id = ?", "board", id, id)
}

// FilterLabels are the labels a filter asks for by name, positively and at
// the top level: `label:home due:7d` asks for home. A card added on a board
// with that filter is given them, so it does not vanish from the board it
// was added on.
func FilterLabels(filter string) []string {
	if strings.TrimSpace(filter) == "" {
		return nil
	}
	q, err := query.Parse(filter)
	if err != nil || q == nil {
		return nil
	}
	var out []string
	var walk func(n query.Node)
	walk = func(n query.Node) {
		switch t := n.(type) {
		case *query.And:
			for _, c := range t.Nodes {
				walk(c)
			}
		case *query.Term:
			f := strings.ToLower(t.Field)
			if (f == "label" || f == "topic") && (t.Op == query.OpMatch || t.Op == query.OpExact) {
				if l := NormalizeCardLabel(t.Value); l != "" {
					out = append(out, l)
				}
			}
		}
	}
	walk(q.Filter)
	return out
}

// --- the calendar ----------------------------------------------------------

// TicketsDue returns the cards due in [from, to): not archived, not rejected.
func TicketsDue(from, to time.Time) ([]*Ticket, error) {
	rows, err := db.Query("SELECT "+ticketColumns+` FROM tickets
		WHERE due_at IS NOT NULL AND due_at >= ? AND due_at < ?
		  AND archived_at IS NULL AND status <> ?
		ORDER BY due_at, position`, from.UnixMilli(), to.UnixMilli(), TicketRejected)
	if err != nil {
		return nil, err
	}
	out := []*Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows.Scan)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, decorateTickets(out)
}
