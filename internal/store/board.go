package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// migrateBoard is schema v46: everything the board needs to be somewhere a
// person keeps their own work, not only where mail-derived tickets land.
//
// One migration for all of it, because the pieces lean on each other: a list
// is what a card's status names, a card's position is within its list, and a
// rule acts on lists, labels and checklists.
func migrateBoard(db *sql.DB) error {
	now := time.Now().UnixMilli()

	// Lists. A list's id is what tickets.status holds, so `status:todo` and
	// every saved query written against the four fixed statuses keep meaning
	// what they meant. The title is free to change; the id never does.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ticket_lists (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			kind        TEXT NOT NULL DEFAULT 'open',
			position    REAL NOT NULL,
			archived_at INTEGER,
			created_at  INTEGER NOT NULL
		)`); err != nil {
		return err
	}
	for i, l := range []struct{ id, title, kind string }{
		{TicketProposed, "Proposed", ListProposed},
		{TicketTodo, "To do", ListOpen},
		{TicketDoing, "Doing", ListOpen},
		{TicketDone, "Done", ListClosed},
	} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO ticket_lists (id, title, kind, position, created_at)
			VALUES (?, ?, ?, ?, ?)`, l.id, l.title, l.kind, float64(i+1), now); err != nil {
			return err
		}
	}
	// A status somebody already used that is not one of the four becomes a
	// list of its own, so no card falls off the board in the upgrade.
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO ticket_lists (id, title, kind, position, created_at)
		SELECT status, status, 'open', 4 + ROW_NUMBER() OVER (ORDER BY status), ?
		FROM (SELECT DISTINCT status FROM tickets
		      WHERE status <> 'rejected' AND status NOT IN (SELECT id FROM ticket_lists))`,
		now); err != nil {
		return err
	}

	for _, col := range []struct{ name, decl string }{
		{"position", "REAL"},
		{"start_at", "INTEGER"},
		{"archived_at", "INTEGER"},
		{"rule_id", "TEXT"},
	} {
		if columnExists(db, "tickets", col.name) {
			continue
		}
		if _, err := db.Exec("ALTER TABLE tickets ADD COLUMN " + col.name + " " + col.decl); err != nil {
			return err
		}
	}

	_, err := db.Exec(`
		-- Existing cards keep the order the board showed them in: soonest due,
		-- then least recently touched.
		UPDATE tickets SET position = (
			SELECT rn FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY status ORDER BY COALESCE(due_at, updated_at), created_at) AS rn
				FROM tickets) r
			WHERE r.id = tickets.id)
		WHERE position IS NULL;

		CREATE INDEX IF NOT EXISTS idx_tickets_list_position ON tickets(status, position);

		-- One card per conversation per mail rule, so a rule that runs after
		-- every sync adds mail to the card it made rather than making another.
		CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_thread_rule
			ON tickets(thread_key, rule_id)
			WHERE thread_key IS NOT NULL AND rule_id IS NOT NULL;

		CREATE TABLE IF NOT EXISTS ticket_checklist_items (
			id         TEXT PRIMARY KEY,
			ticket_id  TEXT NOT NULL,
			checklist  TEXT NOT NULL DEFAULT 'Checklist',
			text       TEXT NOT NULL,
			done       INTEGER NOT NULL DEFAULT 0,
			position   REAL NOT NULL,
			created_at INTEGER NOT NULL,
			done_at    INTEGER,
			FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_checklist_ticket ON ticket_checklist_items(ticket_id, position);

		CREATE TABLE IF NOT EXISTS ticket_comments (
			id         TEXT PRIMARY KEY,
			ticket_id  TEXT NOT NULL,
			body       TEXT NOT NULL,
			actor      TEXT NOT NULL DEFAULT 'human',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_ticket_comments_ticket ON ticket_comments(ticket_id, created_at);

		-- A card's labels share one vocabulary with the topics on mail.
		CREATE TABLE IF NOT EXISTS ticket_labels (
			ticket_id  TEXT NOT NULL,
			label      TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (ticket_id, label),
			FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_ticket_labels_label ON ticket_labels(label);

		CREATE TABLE IF NOT EXISTS topic_colors (
			topic TEXT PRIMARY KEY,
			color TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS ticket_boards (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL,
			filter     TEXT NOT NULL DEFAULT '',
			position   REAL NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS ticket_rules (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			trigger     TEXT NOT NULL,
			list        TEXT NOT NULL DEFAULT '',
			query       TEXT NOT NULL DEFAULT '',
			actions     TEXT NOT NULL DEFAULT '[]',
			enabled     INTEGER NOT NULL DEFAULT 1,
			since       INTEGER NOT NULL DEFAULT 0,
			created_at  INTEGER NOT NULL,
			last_run_at INTEGER
		);

		-- A card raised from mail starts with its conversation's topics.
		INSERT OR IGNORE INTO ticket_labels (ticket_id, label, created_at)
		SELECT DISTINCT t.id, mt.topic, t.created_at
		FROM tickets t
		JOIN messages m ON m.thread_key = t.thread_key
		JOIN message_topics mt ON mt.message_id = m.id
		WHERE t.thread_key IS NOT NULL;
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO ticket_boards (id, title, filter, position, created_at)
		VALUES ('all', 'All cards', '', 1, ?)`, now)
	return err
}

// List kinds.
//
// A kind is what a list means to everything that is not the board: whether
// a card in it is waiting to be accepted, being worked, or finished.
const (
	// ListProposed is the queue proposals land in. There is exactly one.
	ListProposed = "proposed"
	// ListOpen is work not yet finished.
	ListOpen = "open"
	// ListClosed is finished: entering one stamps the card's closed time.
	ListClosed = "closed"
)

// TicketList is one list on the board.
type TicketList struct {
	// ID is what a card's status holds, and never changes.
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Kind       string     `json:"kind"`
	Position   float64    `json:"position"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	// Cards is how many cards are in it, archived ones included.
	Cards int64 `json:"cards"`
}

// TicketLists returns the lists in board order.
func TicketLists(includeArchived bool) ([]*TicketList, error) {
	q := `SELECT l.id, l.title, l.kind, l.position, l.archived_at,
		(SELECT COUNT(*) FROM tickets t WHERE t.status = l.id)
		FROM ticket_lists l`
	if !includeArchived {
		q += " WHERE l.archived_at IS NULL"
	}
	rows, err := db.Query(q + " ORDER BY l.position, l.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TicketList{}
	for rows.Next() {
		l := &TicketList{}
		var archived sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Title, &l.Kind, &l.Position, &archived, &l.Cards); err != nil {
			return nil, err
		}
		if archived.Valid {
			a := time.UnixMilli(archived.Int64)
			l.ArchivedAt = &a
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetTicketList finds a list by id, or failing that by title.
func GetTicketList(ref string) (*TicketList, error) {
	lists, err := TicketLists(true)
	if err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	for _, l := range lists {
		if l.ID == ref {
			return l, nil
		}
	}
	for _, l := range lists {
		if strings.EqualFold(l.Title, ref) {
			return l, nil
		}
	}
	return nil, fmt.Errorf("no list %q", ref)
}

// ticketListIDs are the values status: takes, for the query compiler.
func ticketListIDs() []string {
	rows, err := db.Query("SELECT id FROM ticket_lists ORDER BY position, id")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// listKind is the kind of the list a status names; rejected and anything
// unknown are neither open nor closed.
func listKind(status string) string {
	var kind string
	if err := db.QueryRow("SELECT kind FROM ticket_lists WHERE id = ?", status).Scan(&kind); err != nil {
		return ""
	}
	return kind
}

// FirstOpenList is where an accepted proposal goes: the first open list.
func FirstOpenList() (string, error) {
	var id string
	err := db.QueryRow(`SELECT id FROM ticket_lists WHERE kind = ? AND archived_at IS NULL
		ORDER BY position, id LIMIT 1`, ListOpen).Scan(&id)
	if err == sql.ErrNoRows {
		err = db.QueryRow(`SELECT id FROM ticket_lists WHERE kind = ? ORDER BY position, id LIMIT 1`,
			ListOpen).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("there is no open list to put it in")
	}
	return id, err
}

// ProposedList is the list proposals land in.
func ProposedList() string {
	var id string
	if err := db.QueryRow("SELECT id FROM ticket_lists WHERE kind = ? LIMIT 1", ListProposed).Scan(&id); err != nil {
		return TicketProposed
	}
	return id
}

// validStatus reports whether a card may be put there: a list, or rejected.
func validStatus(status string) error {
	if status == TicketRejected {
		return nil
	}
	if listKind(status) != "" {
		return nil
	}
	return fmt.Errorf("no list %q; iql ticket lists shows them", status)
}

// CreateTicketList adds a list after another (or at the end when after is
// empty), and returns it.
func CreateTicketList(title, kind, after string) (*TicketList, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("a list needs a title")
	}
	if kind == "" {
		kind = ListOpen
	}
	if kind != ListOpen && kind != ListClosed {
		return nil, fmt.Errorf("a list is open or closed; there is only one proposed list")
	}
	base := SlugifyQueryName(title)
	if base == "" {
		base = "list"
	}
	id := base
	for i := 2; ; i++ {
		if id != TicketRejected && listKind(id) == "" {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	pos, err := listPositionAfter(after, "")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO ticket_lists (id, title, kind, position, created_at)
		VALUES (?, ?, ?, ?, ?)`, id, title, kind, pos, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	return GetTicketList(id)
}

// listPositionAfter is a position just after one list and before the next;
// after == "" is the end, after == "^" the start.
func listPositionAfter(after, moving string) (float64, error) {
	var ps []struct {
		id  string
		pos float64
	}
	rows, err := db.Query("SELECT id, position FROM ticket_lists WHERE id <> ? ORDER BY position, id", moving)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var p struct {
			id  string
			pos float64
		}
		if err := rows.Scan(&p.id, &p.pos); err != nil {
			rows.Close()
			return 0, err
		}
		ps = append(ps, p)
	}
	rows.Close()
	if len(ps) == 0 {
		return 1, nil
	}
	switch after {
	case "":
		return ps[len(ps)-1].pos + 1, nil
	case "^":
		return ps[0].pos - 1, nil
	}
	for i, p := range ps {
		if p.id != after {
			continue
		}
		if i == len(ps)-1 {
			return p.pos + 1, nil
		}
		return (p.pos + ps[i+1].pos) / 2, nil
	}
	return 0, fmt.Errorf("no list %q", after)
}

// RenameTicketList changes a list's title. Its id, and so every card in it
// and every query naming it, is unchanged.
func RenameTicketList(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("a list needs a title")
	}
	return execOne("UPDATE ticket_lists SET title = ? WHERE id = ?", "list", id, title, id)
}

// MoveTicketList puts a list after another; "^" is first, "" last.
func MoveTicketList(id, after string) error {
	if after == id {
		return nil
	}
	pos, err := listPositionAfter(after, id)
	if err != nil {
		return err
	}
	return execOne("UPDATE ticket_lists SET position = ? WHERE id = ?", "list", id, pos, id)
}

// SetTicketListKind makes a list open or closed.
//
// Closing a list stamps the cards already in it as finished now, and opening
// one clears that, so "done this week" stays answerable either way.
func SetTicketListKind(id, kind string) error {
	l, err := GetTicketList(id)
	if err != nil {
		return err
	}
	if l.Kind == kind {
		return nil
	}
	if l.Kind == ListProposed || kind == ListProposed {
		return fmt.Errorf("the proposed list is where proposals land; it stays what it is")
	}
	if kind != ListOpen && kind != ListClosed {
		return fmt.Errorf("a list is open or closed")
	}
	if err := keepsOne(l, kind, false); err != nil {
		return err
	}
	if _, err := db.Exec("UPDATE ticket_lists SET kind = ? WHERE id = ?", kind, l.ID); err != nil {
		return err
	}
	if kind == ListClosed {
		_, err = db.Exec("UPDATE tickets SET closed_at = ? WHERE status = ? AND closed_at IS NULL",
			time.Now().UnixMilli(), l.ID)
	} else {
		_, err = db.Exec("UPDATE tickets SET closed_at = NULL WHERE status = ?", l.ID)
	}
	return err
}

// keepsOne refuses a change that would leave no open or no closed list:
// accepting a proposal needs somewhere open, and finishing needs somewhere
// closed.
func keepsOne(l *TicketList, newKind string, removing bool) error {
	if l.Kind != ListOpen && l.Kind != ListClosed {
		return nil
	}
	if !removing && newKind == l.Kind {
		return nil
	}
	var others int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ticket_lists
		WHERE kind = ? AND id <> ? AND archived_at IS NULL`, l.Kind, l.ID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return fmt.Errorf("%q is the last %s list; the board needs one", l.Title, l.Kind)
	}
	return nil
}

// ArchiveTicketList takes a list off the board, or puts it back. Its cards
// stay where they are, and stay searchable.
func ArchiveTicketList(id string, archived bool) error {
	l, err := GetTicketList(id)
	if err != nil {
		return err
	}
	if archived {
		if err := keepsOne(l, "", true); err != nil {
			return err
		}
		_, err = db.Exec("UPDATE ticket_lists SET archived_at = ? WHERE id = ?", time.Now().UnixMilli(), l.ID)
		return err
	}
	_, err = db.Exec("UPDATE ticket_lists SET archived_at = NULL WHERE id = ?", l.ID)
	return err
}

// DeleteTicketList removes a list. Its cards have to go somewhere first:
// with moveTo they are moved there, and without it a list with cards is
// refused, because a card is never deleted as a side effect.
func DeleteTicketList(id, moveTo string) error {
	l, err := GetTicketList(id)
	if err != nil {
		return err
	}
	if l.Kind == ListProposed {
		return fmt.Errorf("the proposed list is where proposals land; rename or archive it instead")
	}
	if err := keepsOne(l, "", true); err != nil {
		return err
	}
	if l.Cards > 0 {
		if moveTo == "" {
			return fmt.Errorf("%q has %d card(s); say where they go (--move-to)", l.Title, l.Cards)
		}
		dest, err := GetTicketList(moveTo)
		if err != nil {
			return err
		}
		if dest.ID == l.ID {
			return fmt.Errorf("the cards have to go to another list")
		}
		var ids []string
		rows, err := db.Query("SELECT id FROM tickets WHERE status = ? ORDER BY position", l.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var tid string
			if err := rows.Scan(&tid); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, tid)
		}
		rows.Close()
		for _, tid := range ids {
			if err := MoveTicket(tid, dest.ID, ""); err != nil {
				return err
			}
		}
	}
	_, err = db.Exec("DELETE FROM ticket_lists WHERE id = ?", l.ID)
	return err
}

// execOne runs an update that must touch exactly one row of a thing.
func execOne(stmt, what, id string, args ...any) error {
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no %s %q", what, id)
	}
	return nil
}
