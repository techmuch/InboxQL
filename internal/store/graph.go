package store

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"log"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/user/inboxql/internal/message"
)

// Participant roles. These are the `role` column's only legal values, and the
// query language's `to:`/`cc:`/`bcc:`/`from:` terms map onto them directly.
const (
	RoleFrom = "from"
	RoleTo   = "to"
	RoleCc   = "cc"
	RoleBcc  = "bcc"
)

// NormaliseAddress reduces a header address to a comparable form.
//
// "Alice Smith <Alice@Example.COM>" and "alice@example.com" are the same
// mailbox, and a query language that treated them as different would be
// unusable: nobody types the display name. The name is kept separately rather
// than discarded so `| participants` can still show something human.
//
// Unparseable input is lowercased and returned as-is. Mail servers emit plenty
// of addresses net/mail rejects, and dropping those rows would silently make
// negation wrong — a message with one malformed recipient would look like a
// message with no recipients.
func NormaliseAddress(raw string) (addr, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if parsed, err := mail.ParseAddress(raw); err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address)), strings.TrimSpace(parsed.Name)
	}
	// Fall back to the angle-bracket form without full RFC 5322 validation.
	if i := strings.LastIndex(raw, "<"); i >= 0 {
		if j := strings.Index(raw[i:], ">"); j > 0 {
			inner := strings.TrimSpace(raw[i+1 : i+j])
			display := strings.Trim(strings.TrimSpace(raw[:i]), `"`)
			if inner != "" {
				return strings.ToLower(inner), display
			}
		}
	}
	return strings.ToLower(raw), ""
}

// participantRows flattens a message into the edges it contributes.
func participantRows(m *message.Message) []struct{ Role, Addr, Name string } {
	var out []struct{ Role, Addr, Name string }
	add := func(role, raw string) {
		addr, name := NormaliseAddress(raw)
		if addr == "" {
			return
		}
		// The parsed header is the better source: NormaliseAddress can only
		// recover a name that survived into the address string, and From is
		// deliberately stored bare because it feeds the content hash.
		if fromHeader := m.Names[addr]; fromHeader != "" {
			name = fromHeader
		}
		out = append(out, struct{ Role, Addr, Name string }{role, addr, name})
	}
	add(RoleFrom, m.From)
	for _, a := range m.To {
		add(RoleTo, a)
	}
	for _, a := range m.Cc {
		add(RoleCc, a)
	}
	for _, a := range m.Bcc {
		add(RoleBcc, a)
	}
	return out
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// writeParticipants replaces the edge rows for one message.
//
// Delete-then-insert rather than upsert: a message that is re-saved with a
// recipient removed must lose that edge, or a stale row keeps answering
// `to:` queries for a recipient who is no longer there.
func writeParticipants(x execer, m *message.Message) error {
	if _, err := x.Exec("DELETE FROM message_participants WHERE message_id = ?", m.ID); err != nil {
		return err
	}
	for _, p := range participantRows(m) {
		if _, err := x.Exec(
			`INSERT OR REPLACE INTO message_participants (message_id, role, address, name)
			 VALUES (?, ?, ?, ?)`,
			m.ID, p.Role, p.Addr, p.Name); err != nil {
			return err
		}
	}
	return nil
}

// HeaderRefs pulls the threading headers out of a raw header blob.
//
// Returns the In-Reply-To message id and the full References chain, both with
// angle brackets stripped so they compare directly against messages.message_id.
func HeaderRefs(header []byte) (inReplyTo string, refs []string) {
	if len(header) == 0 {
		return "", nil
	}
	// textproto needs a blank line to know the header block ended.
	buf := bytes.NewReader(append(bytes.TrimRight(header, "\r\n"), '\r', '\n', '\r', '\n'))
	h, err := textproto.NewReader(bufio.NewReader(buf)).ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return "", nil
	}

	inReplyTo = firstMessageID(h.Get("In-Reply-To"))

	seen := map[string]bool{}
	for _, id := range messageIDs(h.Get("References")) {
		if !seen[id] {
			seen[id] = true
			refs = append(refs, id)
		}
	}
	// In-Reply-To is the nearest parent and is not always repeated in
	// References. Appending it keeps the walk connected for the mail clients
	// that omit it.
	if inReplyTo != "" && !seen[inReplyTo] {
		refs = append(refs, inReplyTo)
	}
	return inReplyTo, refs
}

// NormaliseMessageID strips the angle brackets a Message-ID header carries.
//
// The messages.message_id column keeps whatever the server sent, brackets and
// all, while References and In-Reply-To are parsed bare. Comparing the two
// without normalising is how threading silently found nothing: every root kept
// a key of "<id@host>" and every reply a key of "id@host", so no reply ever
// joined its parent and each message became its own conversation.
func NormaliseMessageID(id string) string {
	return strings.Trim(strings.TrimSpace(id), "<>")
}

// messageIDs splits a header value into bare message ids.
func messageIDs(v string) []string {
	var out []string
	for _, field := range strings.Fields(v) {
		if id := strings.Trim(strings.TrimSpace(field), "<>,"); id != "" && strings.Contains(id, "@") {
			out = append(out, id)
		}
	}
	return out
}

func firstMessageID(v string) string {
	if ids := messageIDs(v); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// writeRefs records a message's position in the reference graph.
func writeRefs(x execer, m *message.Message) error {
	inReplyTo, refs := HeaderRefs(m.Header)

	// The thread root is the first entry of References, which RFC 5322 defines
	// as the ancestry in order. A message with no references is its own root.
	//
	// Known limit: a client that sends In-Reply-To but omits References starts
	// a new key at every reply, so a thread from such a client splits into
	// pairs. That is still strictly better than grouping by subject line,
	// which merged unrelated conversations that happened to share a subject.
	threadKey := NormaliseMessageID(m.MessageID)
	if len(refs) > 0 {
		threadKey = NormaliseMessageID(refs[0])
	}
	if threadKey == "" {
		threadKey = m.ID
	}

	if _, err := x.Exec("UPDATE messages SET in_reply_to = ?, thread_key = ? WHERE id = ?",
		nullIfEmpty(inReplyTo), threadKey, m.ID); err != nil {
		return err
	}
	if _, err := x.Exec("DELETE FROM message_refs WHERE message_id = ?", m.ID); err != nil {
		return err
	}
	for i, ref := range refs {
		if _, err := x.Exec(
			"INSERT OR REPLACE INTO message_refs (message_id, ref, ordinal) VALUES (?, ?, ?)",
			m.ID, ref, i); err != nil {
			return err
		}
	}
	return nil
}

// backfillParticipants populates the edge table from the JSON address columns.
//
// Runs once, inside the v15 migration. Reads every row, so it is the slow part
// of upgrading a large mailbox; it is also the only chance to do it, since the
// JSON columns stay as the source of truth for display.
func backfillParticipants(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, from_addr, to_addrs, cc_addrs, bcc_addrs FROM messages`)
	if err != nil {
		return err
	}

	type entry struct {
		id                string
		from, to, cc, bcc string
	}
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.from, &e.to, &e.cc, &e.bcc); err != nil {
			rows.Close()
			return err
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// Closed before the writes: an open read cursor can hold the lock the
	// transaction below needs.
	rows.Close()

	if len(all) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range all {
		m := &message.Message{ID: e.id, From: e.from}
		json.Unmarshal([]byte(e.to), &m.To)
		json.Unmarshal([]byte(e.cc), &m.Cc)
		json.Unmarshal([]byte(e.bcc), &m.Bcc)
		if err := writeParticipants(tx, m); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("Indexed participants for %d message(s).", len(all))
	return nil
}

// backfillRefs parses threading headers for every stored message.
func backfillRefs(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, message_id, header FROM messages`)
	if err != nil {
		return err
	}

	type entry struct {
		id        string
		messageID string
		header    []byte
	}
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.messageID, &e.header); err != nil {
			rows.Close()
			return err
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(all) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	linked := 0
	for _, e := range all {
		m := &message.Message{ID: e.id, MessageID: e.messageID, Header: e.header}
		if err := writeRefs(tx, m); err != nil {
			return err
		}
		if inReplyTo, _ := HeaderRefs(e.header); inReplyTo != "" {
			linked++
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("Parsed reference headers for %d message(s); %d have a parent.", len(all), linked)
	return nil
}

// ReindexGraph rebuilds the participant and reference tables from the stored
// messages.
//
// Exposed through `iql maintenance` because these tables are derived: a
// database whose keys were computed by an earlier build, or one restored from
// a backup, can be brought back into agreement without re-importing anything.
func ReindexGraph() error {
	if err := backfillParticipants(db); err != nil {
		return err
	}
	if err := backfillRefs(db); err != nil {
		return err
	}
	// Names first, then contacts: a contact created before its name has been
	// recovered would be listed by address forever, since ensureContacts only
	// fills a name it has not got.
	if err := backfillParticipantNames(db); err != nil {
		return err
	}
	return backfillContacts(db)
}

// backfillContacts creates a contact for every address already on record.
//
// Idempotent, and it never overwrites an assertion: only the header name is
// touched, and only when the contact has none.
func backfillContacts(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO contacts (address, header_name, created_at, updated_at)
		SELECT p.address,
		       (SELECT p2.name FROM message_participants p2
		         WHERE p2.address = p.address AND p2.name IS NOT NULL AND p2.name != ''
		         GROUP BY p2.name ORDER BY COUNT(*) DESC, p2.name LIMIT 1),
		       CAST(strftime('%s','now') AS INTEGER) * 1000,
		       CAST(strftime('%s','now') AS INTEGER) * 1000
		FROM message_participants p
		GROUP BY p.address
		ON CONFLICT(address) DO UPDATE SET
			header_name = COALESCE(contacts.header_name, excluded.header_name)`)
	return err
}

// HeaderNames maps each address in a raw header block to the display name it
// was given there.
//
// The recovery path for mail already imported. Every message keeps its raw
// header, so the names discarded at parse time are still on disk — this is what
// lets the backfill reach them without re-importing anything.
func HeaderNames(header []byte) map[string]string {
	if len(header) == 0 {
		return nil
	}
	buf := bytes.NewReader(append(bytes.TrimRight(header, "\r\n"), '\r', '\n', '\r', '\n'))
	h, err := textproto.NewReader(bufio.NewReader(buf)).ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return nil
	}

	out := map[string]string{}
	for _, field := range []string{"From", "To", "Cc", "Bcc"} {
		for _, raw := range h.Values(field) {
			list, err := mail.ParseAddressList(raw)
			if err != nil {
				// One unparseable recipient list must not cost the names in
				// the others; headers in a real archive are frequently malformed.
				continue
			}
			for _, a := range list {
				addr := strings.ToLower(strings.TrimSpace(a.Address))
				name := strings.TrimSpace(a.Name)
				if addr == "" || name == "" {
					continue
				}
				if _, seen := out[addr]; !seen {
					out[addr] = name
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// backfillParticipantNames recovers display names from stored headers.
//
// Runs once, inside the v24 migration. Only fills empty names — a name already
// recorded came from a live parse and is at least as good.
func backfillParticipantNames(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT m.id, m.header FROM messages m
		WHERE m.header IS NOT NULL AND m.header != ''
		  AND EXISTS (SELECT 1 FROM message_participants p
		              WHERE p.message_id = m.id AND (p.name IS NULL OR p.name = ''))`)
	if err != nil {
		return err
	}

	type named struct {
		id    string
		names map[string]string
	}
	var pending []named
	for rows.Next() {
		var id string
		var header []byte
		if err := rows.Scan(&id, &header); err != nil {
			rows.Close()
			return err
		}
		if names := HeaderNames(header); len(names) > 0 {
			pending = append(pending, named{id: id, names: names})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range pending {
		for addr, name := range p.names {
			if _, err := tx.Exec(`
				UPDATE message_participants SET name = ?
				WHERE message_id = ? AND address = ? AND (name IS NULL OR name = '')`,
				name, p.id, addr); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
