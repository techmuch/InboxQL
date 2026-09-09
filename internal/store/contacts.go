package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/user/inboxql/internal/message"
)

// What a contact is.
//
// KindUnknown is the honest default: most addresses have never been classified,
// and guessing "person" for every one of them would make the field useless for
// the thing it exists to answer.
const (
	KindPerson       = "person"
	KindOrganization = "organization"
	KindSystem       = "system"
	KindUnknown      = "unknown"
)

// Where a claim about a contact came from, weakest first.
//
// A source may only be overwritten by one at least as strong. Without this a
// nightly rule run would quietly undo every correction a person had made.
const (
	ContactFromHeader = "header"
	ContactFromRule   = "rule"
	ContactFromModel  = "llm"
	// ContactFromHuman reuses the annotator vocabulary deliberately: a human
	// ruling means the same thing here as it does there, and two spellings of
	// "human" would eventually be compared against each other.
	ContactFromHuman = SourceHuman
)

func sourceRank(s string) int {
	switch s {
	case ContactFromHuman:
		return 3
	case ContactFromModel:
		return 2
	case ContactFromHeader, ContactFromRule:
		return 1
	}
	return 0
}

// Contact is an address someone has corresponded with.
//
// The stored half is assertions — names, a phone number, a kind. The derived
// half is counted from message_participants on every read, so it cannot drift
// away from the mailbox it describes.
type Contact struct {
	Address string `json:"address"`

	// --- asserted, stored
	HeaderName  string   `json:"headerName,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	FirstName   string   `json:"firstName,omitempty"`
	LastName    string   `json:"lastName,omitempty"`
	Phone       string   `json:"phone,omitempty"`
	Org         string   `json:"org,omitempty"`
	Title       string   `json:"title,omitempty"`
	Kind        string   `json:"kind"`
	KindSource  string   `json:"kindSource,omitempty"`
	EnrichedBy  string   `json:"enrichedBy,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// --- derived, never stored
	Messages  int64     `json:"messages"`
	Sent      int64     `json:"sent"`
	Received  int64     `json:"received"`
	FirstSeen time.Time `json:"firstSeen,omitempty"`
	LastSeen  time.Time `json:"lastSeen,omitempty"`
}

// Name is what to call this contact, strongest source first.
func (c Contact) Name() string {
	for _, candidate := range []string{
		c.DisplayName,
		strings.TrimSpace(c.FirstName + " " + c.LastName),
		c.HeaderName,
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return c.Address
}

// ensureContacts records every address a message touches.
//
// Called from the same write that records participants, which is what makes
// "every message creates a contact" true rather than aspirational. Idempotent:
// re-importing a message updates the name and touches nothing else.
//
// Only header_name is written here. A name a person typed, or a model
// extracted, is not something the next message should overwrite.
func ensureContacts(x execer, m *message.Message) error {
	now := time.Now().UnixMilli()
	seen := map[string]bool{}

	for _, p := range participantRows(m) {
		if seen[p.Addr] {
			continue
		}
		seen[p.Addr] = true

		if _, err := x.Exec(`
			INSERT INTO contacts (address, header_name, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(address) DO UPDATE SET
				header_name = COALESCE(NULLIF(excluded.header_name, ''), contacts.header_name),
				updated_at  = excluded.updated_at`,
			p.Addr, nullIfEmpty(p.Name), now, now); err != nil {
			return err
		}
	}
	return nil
}

const contactColumns = `c.address, COALESCE(c.header_name, ''), COALESCE(c.display_name, ''),
	COALESCE(c.first_name, ''), COALESCE(c.last_name, ''), COALESCE(c.phone, ''),
	COALESCE(c.org, ''), COALESCE(c.title, ''), c.kind, COALESCE(c.kind_source, ''),
	COALESCE(c.enriched_by, ''), c.confidence, COALESCE(c.notes, '')`

// contactDerived counts what a contact is, from the edges rather than a cache.
//
// Correlated subqueries rather than a join so the compiler can filter on them
// by name: `messages>10` becomes a HAVING-free predicate over a scalar.
// Aliased so ORDER BY can name them: an output alias is the only way to sort
// by a derived count without writing the subquery a second time and letting
// the two drift.
const contactDerived = `
	(SELECT COUNT(DISTINCT p.message_id) FROM message_participants p WHERE p.address = c.address) AS messages,
	(SELECT COUNT(*) FROM message_participants p WHERE p.address = c.address AND p.role = 'from') AS sent,
	(SELECT COUNT(*) FROM message_participants p WHERE p.address = c.address AND p.role != 'from') AS received,
	(SELECT MIN(m.date) FROM message_participants p JOIN messages m ON m.id = p.message_id WHERE p.address = c.address) AS first_seen,
	(SELECT MAX(m.date) FROM message_participants p JOIN messages m ON m.id = p.message_id WHERE p.address = c.address) AS last_seen`

// ContactSelectList is the column list scanContact expects.
const ContactSelectList = contactColumns + "," + contactDerived

func scanContact(scan func(...any) error) (*Contact, error) {
	c := &Contact{}
	var first, last sql.NullInt64
	if err := scan(&c.Address, &c.HeaderName, &c.DisplayName, &c.FirstName, &c.LastName,
		&c.Phone, &c.Org, &c.Title, &c.Kind, &c.KindSource, &c.EnrichedBy, &c.Confidence, &c.Notes,
		&c.Messages, &c.Sent, &c.Received, &first, &last); err != nil {
		return nil, err
	}
	if first.Valid {
		c.FirstSeen = millisToTime(first.Int64)
	}
	if last.Valid {
		c.LastSeen = millisToTime(last.Int64)
	}
	return c, nil
}

// GetContact returns one contact by address, or (nil, nil) when unknown.
func GetContact(address string) (*Contact, error) {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	c, err := scanContact(db.QueryRow(
		"SELECT "+ContactSelectList+" FROM contacts c WHERE c.address = ?", addr).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c != nil {
		tags, err := GetContactTags(c.Address)
		if err != nil {
			return nil, err
		}
		c.Tags = tags
	}
	return c, nil
}

// SaveContact writes the asserted half of a contact.
//
// Only fields the caller filled are written, and a weaker source never
// overwrites a stronger one — so re-running a rule cannot undo a correction.
func SaveContact(c *Contact, source string) error {
	addr, _ := NormaliseAddress(c.Address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(c.Address))
	}
	if addr == "" {
		return fmt.Errorf("a contact needs an address")
	}

	existing, err := GetContact(addr)
	if err != nil {
		return err
	}
	if existing != nil && sourceRank(source) < sourceRank(existing.KindSource) && c.Kind != "" {
		// The kind is the contested field: rules and models both write it, and
		// a person's ruling has to survive both.
		c.Kind = existing.Kind
		source = existing.KindSource
	}

	now := time.Now().UnixMilli()
	kind := c.Kind
	if kind == "" {
		kind = KindUnknown
		if existing != nil {
			kind = existing.Kind
		}
	}

	_, err = db.Exec(`
		INSERT INTO contacts (address, header_name, display_name, first_name, last_name,
			phone, org, title, kind, kind_source, enriched_by, confidence, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET
			header_name  = COALESCE(NULLIF(excluded.header_name, ''), contacts.header_name),
			display_name = COALESCE(NULLIF(excluded.display_name, ''), contacts.display_name),
			first_name   = COALESCE(NULLIF(excluded.first_name, ''), contacts.first_name),
			last_name    = COALESCE(NULLIF(excluded.last_name, ''), contacts.last_name),
			phone        = COALESCE(NULLIF(excluded.phone, ''), contacts.phone),
			org          = COALESCE(NULLIF(excluded.org, ''), contacts.org),
			title        = COALESCE(NULLIF(excluded.title, ''), contacts.title),
			kind         = excluded.kind,
			kind_source  = excluded.kind_source,
			enriched_by  = COALESCE(NULLIF(excluded.enriched_by, ''), contacts.enriched_by),
			confidence   = COALESCE(excluded.confidence, contacts.confidence),
			notes        = CASE WHEN excluded.notes != '' THEN excluded.notes ELSE contacts.notes END,
			updated_at   = excluded.updated_at`,
		addr, nullIfEmpty(c.HeaderName), nullIfEmpty(c.DisplayName),
		nullIfEmpty(c.FirstName), nullIfEmpty(c.LastName), nullIfEmpty(c.Phone),
		nullIfEmpty(c.Org), nullIfEmpty(c.Title), kind, nullIfEmpty(source),
		nullIfEmpty(c.EnrichedBy), c.Confidence, c.Notes, now, now)
	return err
}

// SetContactKind records what an address is, and who says so.
func SetContactKind(address, kind, source string) error {
	return SaveContact(&Contact{Address: address, Kind: kind}, source)
}

// SetContactNotes records private markdown notes for a contact.
func SetContactNotes(address, notes string) error {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	if addr == "" {
		return fmt.Errorf("a contact needs an address")
	}
	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT INTO contacts (address, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET
			notes = excluded.notes,
			updated_at = excluded.updated_at`,
		addr, notes, now, now)
	return err
}

// AddContactTag adds a custom tag to a contact.
func AddContactTag(address, tag string) error {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	if addr == "" || tag == "" {
		return fmt.Errorf("address and tag are required")
	}
	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT INTO contacts (address, created_at, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(address) DO NOTHING`,
		addr, now, now)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO contact_tags (address, tag, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(address, tag) DO NOTHING`,
		addr, tag, now)
	return err
}

// RemoveContactTag removes a custom tag from a contact.
func RemoveContactTag(address, tag string) error {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	if addr == "" || tag == "" {
		return fmt.Errorf("address and tag are required")
	}
	_, err := db.Exec(`DELETE FROM contact_tags WHERE address = ? AND tag = ?`, addr, tag)
	return err
}

// GetContactTags returns all tags assigned to a contact in alphabetical order.
func GetContactTags(address string) ([]string, error) {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	rows, err := db.Query(`SELECT tag FROM contact_tags WHERE address = ? ORDER BY tag ASC`, addr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListAllTags returns every unique contact tag in use.
func ListAllTags() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT tag FROM contact_tags ORDER BY tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// PopulateContactTags populates the Tags slice on a list of contacts.
func PopulateContactTags(contacts []*Contact) error {
	if len(contacts) == 0 {
		return nil
	}
	contactMap := make(map[string]*Contact, len(contacts))
	addrs := make([]string, 0, len(contacts))
	for _, c := range contacts {
		c.Tags = []string{}
		contactMap[c.Address] = c
		addrs = append(addrs, c.Address)
	}

	ph := make([]string, len(addrs))
	args := make([]any, len(addrs))
	for i, a := range addrs {
		ph[i] = "?"
		args[i] = a
	}

	rows, err := db.Query(`
		SELECT address, tag FROM contact_tags
		WHERE address IN (`+strings.Join(ph, ", ")+`)
		ORDER BY tag ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var a, t string
		if err := rows.Scan(&a, &t); err != nil {
			return err
		}
		if c, ok := contactMap[a]; ok {
			c.Tags = append(c.Tags, t)
		}
	}
	return rows.Err()
}
