package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/query"
)

// SavedQuery is a named query.
//
// Name is the handle the language uses (`saved:invoices`), so it is slugged and
// unique; Title is what a person reads. Keeping them separate means renaming
// the display text does not break every query that referenced it.
type SavedQuery struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Query       string `json:"query"`
	Description string `json:"description,omitempty"`
	Pinned      bool   `json:"pinned"`
	// Position is where it sits in the rail, lowest first.
	//
	// Supersedes Pinned as the sort. Pinned keeps working because it is in the
	// CLI and in people's habits, but ordering by it meant the only way to move
	// something was to rename it.
	Position int `json:"position"`
	// Icon is a short name from [RailIcons], not markup. A rail of identical
	// bookmarks is worse than half as many distinct ones, and a query is not a
	// thing to accept SVG from.
	Icon      string    `json:"icon,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// SlugifyQueryName reduces a title to the handle `saved:` will accept.
func SlugifyQueryName(s string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

const savedColumns = `id, name, title, query, COALESCE(description, ''), pinned,
	COALESCE(position, 0), COALESCE(icon, ''), created_at, updated_at`

// RailIcons are the icons a saved query may carry.
//
// A fixed vocabulary rather than free text: these resolve to components in the
// frontend, and a name with no component renders as nothing — a rail row with
// no icon and no error. Generic words, so they describe what a row is for
// rather than naming a brand.
var RailIcons = []string{
	"bookmark", "inbox", "star", "send", "file", "tag", "folder",
	"clock", "users", "paperclip", "ticket", "bot", "alert", "search",
}

// ValidIcon reports whether an icon name is one the rail can draw.
func ValidIcon(name string) bool {
	if name == "" {
		return true // no icon is legal; the rail falls back to a bookmark
	}
	for _, i := range RailIcons {
		if i == name {
			return true
		}
	}
	return false
}

func scanSavedQuery(scan func(...any) error) (*SavedQuery, error) {
	q := &SavedQuery{}
	var pinned int
	var created, updated int64
	if err := scan(&q.ID, &q.Name, &q.Title, &q.Query, &q.Description, &pinned,
		&q.Position, &q.Icon, &created, &updated); err != nil {
		return nil, err
	}
	q.Pinned = pinned != 0
	q.CreatedAt = time.UnixMilli(created)
	q.UpdatedAt = time.UnixMilli(updated)
	return q, nil
}

// SaveQuery creates or updates a saved query.
//
// The query text is validated before it is stored. A saved query that does not
// parse is worse than no saved query: it fails later, from wherever it was
// referenced, with an error about a name the user did not type.
func SaveQuery(q *SavedQuery) error {
	if strings.TrimSpace(q.Title) == "" {
		return fmt.Errorf("a saved query needs a name")
	}
	if q.Name == "" {
		q.Name = SlugifyQueryName(q.Title)
	}
	if q.Name == "" {
		return fmt.Errorf("%q does not reduce to a usable name; use letters and digits", q.Title)
	}
	if strings.TrimSpace(q.Query) == "" {
		return fmt.Errorf("a saved query needs a query")
	}
	if err := ValidateQuery(q.Query); err != nil {
		return fmt.Errorf("this query does not compile: %w", err)
	}
	// Saved queries do not nest. Rejecting it here means the error names the
	// query being defined, rather than surfacing later from whatever
	// referenced it.
	if referencesSaved(q.Query) {
		return fmt.Errorf("a saved query cannot reference another saved query; inline what `saved:` points at")
	}

	now := time.Now()
	existing, err := GetSavedQuery(q.Name)
	if err != nil {
		return err
	}
	if !ValidIcon(q.Icon) {
		return fmt.Errorf("%q is not an icon this rail can draw (%s)",
			q.Icon, strings.Join(RailIcons, ", "))
	}

	if existing != nil {
		q.ID, q.CreatedAt = existing.ID, existing.CreatedAt
		// Position is not written here. Editing a query's text should not move
		// it in the rail — that is what `move` is for, and an edit that
		// silently reordered things would be the kind of surprise this whole
		// feature exists to remove.
		q.Position = existing.Position
	} else {
		if q.ID == "" {
			q.ID = uuid.New().String()
		}
		q.CreatedAt = now
		// New ones go at the end, where somebody looking for what they just
		// made will find it.
		q.Position = nextRailPosition()
	}
	q.UpdatedAt = now

	_, err = db.Exec(`
		INSERT INTO saved_queries (id, name, title, query, description, pinned, position, icon, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, title = excluded.title, query = excluded.query,
			description = excluded.description, pinned = excluded.pinned,
			position = excluded.position, icon = excluded.icon,
			updated_at = excluded.updated_at`,
		q.ID, q.Name, q.Title, q.Query, nullIfEmpty(q.Description),
		boolToInt(q.Pinned), q.Position, q.Icon,
		q.CreatedAt.UnixMilli(), q.UpdatedAt.UnixMilli())
	return err
}

// nextRailPosition is one past the last.
func nextRailPosition() int {
	var n sql.NullInt64
	if err := db.QueryRow("SELECT MAX(position) FROM saved_queries").Scan(&n); err != nil || !n.Valid {
		return 0
	}
	return int(n.Int64) + 1
}

// referencesSaved reports whether a query text uses the saved: field.
func referencesSaved(src string) bool {
	parsed, err := query.Parse(src)
	if err != nil {
		return false
	}
	return usesField(parsed.Filter, "saved")
}

func usesField(n query.Node, field string) bool {
	switch t := n.(type) {
	case *query.And:
		for _, c := range t.Nodes {
			if usesField(c, field) {
				return true
			}
		}
	case *query.Or:
		for _, c := range t.Nodes {
			if usesField(c, field) {
				return true
			}
		}
	case *query.Not:
		return usesField(t.Node, field)
	case *query.Term:
		return t.Field == field
	}
	return false
}

// GetSavedQuery looks one up by name. Returns nil when absent.
func GetSavedQuery(name string) (*SavedQuery, error) {
	q, err := scanSavedQuery(db.QueryRow(
		"SELECT "+savedColumns+" FROM saved_queries WHERE name = ?", name).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return q, err
}

// ListSavedQueries returns them pinned first, then alphabetically.
func ListSavedQueries() ([]*SavedQuery, error) {
	rows, err := db.Query("SELECT " + savedColumns + " FROM saved_queries ORDER BY position ASC, name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*SavedQuery{}
	for rows.Next() {
		q, err := scanSavedQuery(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// DeleteSavedQuery removes one.
//
// Queries that reference it by name start failing, which is the right
// behaviour: a dangling reference is reported at the point of use rather than
// silently matching nothing.
func DeleteSavedQuery(name string) error {
	res, err := db.Exec("DELETE FROM saved_queries WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no saved query named %q", name)
	}
	return nil
}

// savedQueryText resolves a name for the compiler.
func savedQueryText(name string) (string, bool, error) {
	q, err := GetSavedQuery(name)
	if err != nil || q == nil {
		return "", false, err
	}
	return q.Query, true, nil
}

// selfAddresses returns every address belonging to a configured account.
//
// This is what me() means. Both the account's own email and its IMAP login are
// included, because providers differ over which one appears in a From header —
// the same reasoning that already governs the Top Senders exclusion.
func selfAddresses() ([]string, error) {
	rows, err := db.Query(`
		SELECT LOWER(email) FROM accounts WHERE email IS NOT NULL AND email != ''
		UNION
		SELECT LOWER(user) FROM accounts WHERE user IS NOT NULL AND user != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		// An IMAP login is not always an address; only the ones that are can
		// match a participant row.
		if strings.Contains(addr, "@") {
			out = append(out, addr)
		}
	}
	return out, rows.Err()
}

// MoveSavedQuery puts one at a position, shifting the rest around it.
//
// Positions are renumbered on every move rather than left sparse. A sparse list
// works until two things land on the same number, and then the order depends on
// the tiebreak rather than on what anybody asked for — which is the bug this
// replaced.
func MoveSavedQuery(name string, to int) error {
	all, err := ListSavedQueries()
	if err != nil {
		return err
	}

	from := -1
	for i, q := range all {
		if q.Name == name {
			from = i
			break
		}
	}
	if from < 0 {
		return fmt.Errorf("no saved query named %q", name)
	}

	// Clamped rather than refused: "move it to the top" from the top is not an
	// error, it is a thing that is already true.
	if to < 0 {
		to = 0
	}
	if to >= len(all) {
		to = len(all) - 1
	}
	if to == from {
		return nil
	}

	moved := all[from]
	all = append(all[:from], all[from+1:]...)
	rest := make([]*SavedQuery, 0, len(all)+1)
	rest = append(rest, all[:to]...)
	rest = append(rest, moved)
	rest = append(rest, all[to:]...)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, q := range rest {
		if _, err := tx.Exec(
			"UPDATE saved_queries SET position = ? WHERE id = ?", i, q.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetSavedQueryIcon changes which icon a rail entry draws.
//
// Its own function rather than a field on save, for the same reason position
// is: changing how a row looks should not touch its version of anything else,
// and a caller that built a SavedQuery without an icon would otherwise clear it.
func SetSavedQueryIcon(name, icon string) error {
	if !ValidIcon(icon) {
		return fmt.Errorf("%q is not an icon this rail can draw (%s)",
			icon, strings.Join(RailIcons, ", "))
	}
	res, err := db.Exec(
		"UPDATE saved_queries SET icon = ?, updated_at = ? WHERE name = ?",
		icon, time.Now().UnixMilli(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no saved query named %q", name)
	}
	return nil
}
