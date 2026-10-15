package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// # The mirror's bookkeeping
//
// What the sync needs to keep a full copy of an account and to change it:
// per folder, how far it has read in each direction; per message, where it is
// on the server (a folder and a UID), which is what a change made here has to
// name to be made there.

// MailboxState is how far a folder has been read.
//
// New mail is everything above LastUID. History is everything below LowUID,
// read newest first, so LowUID falls as the history fetch goes on and
// BackfillDone says it reached the bottom. LastModSeq is the server's change
// counter at the last pass, for asking what changed since.
type MailboxState struct {
	AccountID    string
	Name         string
	UIDValidity  uint32
	LastUID      uint32
	LowUID       uint32
	BackfillDone bool
	LastModSeq   uint64
	ServerCount  int
}

func mailboxStateID(accountID, name string) string { return accountID + "-" + name }

// GetMailboxState returns a folder's state, or a fresh one.
func GetMailboxState(accountID, name string) (*MailboxState, error) {
	st := &MailboxState{AccountID: accountID, Name: name}
	var done int
	err := db.QueryRow(`
		SELECT COALESCE(uid_validity, 0), COALESCE(last_uid, 0), COALESCE(low_uid, 0),
		       COALESCE(backfill_done, 0), COALESCE(last_modseq, 0), COALESCE(server_count, 0)
		FROM mailboxes WHERE id = ?`, mailboxStateID(accountID, name)).
		Scan(&st.UIDValidity, &st.LastUID, &st.LowUID, &done, &st.LastModSeq, &st.ServerCount)
	if err == sql.ErrNoRows {
		return st, nil
	}
	st.BackfillDone = done != 0
	return st, err
}

// SaveMailboxState records a folder's state.
func SaveMailboxState(st *MailboxState) error {
	done := 0
	if st.BackfillDone {
		done = 1
	}
	_, err := db.Exec(`
		INSERT INTO mailboxes (id, account_id, name, last_uid, last_modseq, uid_validity, low_uid, backfill_done, server_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_uid = excluded.last_uid, last_modseq = excluded.last_modseq,
			uid_validity = excluded.uid_validity, low_uid = excluded.low_uid,
			backfill_done = excluded.backfill_done, server_count = excluded.server_count`,
		mailboxStateID(st.AccountID, st.Name), st.AccountID, st.Name, st.LastUID, int64(st.LastModSeq),
		st.UIDValidity, st.LowUID, done, st.ServerCount)
	return err
}

// MirrorProgress is how much of an account is here.
type MirrorProgress struct {
	OnServer     int  `json:"onServer"`
	Stored       int  `json:"stored"`
	HistoryDone  bool `json:"historyDone"`
	FoldersKnown int  `json:"folders"`
}

// MirrorProgressFor reports an account's progress.
func MirrorProgressFor(accountID string) (MirrorProgress, error) {
	var p MirrorProgress
	var pending int
	err := db.QueryRow(`
		SELECT COALESCE(SUM(server_count), 0), COUNT(*),
		       COALESCE(SUM(CASE WHEN backfill_done = 0 THEN 1 ELSE 0 END), 0)
		FROM mailboxes WHERE account_id = ? AND uid_validity != 0`, accountID).Scan(&p.OnServer, &p.FoldersKnown, &pending)
	if err != nil {
		return p, err
	}
	p.HistoryDone = p.FoldersKnown > 0 && pending == 0
	err = db.QueryRow(`SELECT COUNT(DISTINCT message_id) FROM message_locations WHERE account_id = ?`, accountID).Scan(&p.Stored)
	return p, err
}

// SyncTargets finds the stored copies of a server message: by Gmail's id
// first, then by Message-ID. Every row of the account with that Message-ID
// is returned, because an import can hold the same message twice and both
// copies should agree with the server.
func SyncTargets(accountID string, gmMsgID uint64, messageID string) ([]string, error) {
	var ids []string
	collect := func(q string, args ...any) error {
		rows, err := db.Query(q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}
	if gmMsgID != 0 {
		if err := collect(`SELECT id FROM messages WHERE account_id = ? AND gm_msgid = ?`, accountID, int64(gmMsgID)); err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	if messageID = strings.TrimSpace(messageID); messageID != "" {
		// Stored with or without angle brackets, depending on the path.
		bare := strings.Trim(messageID, "<>")
		if err := collect(`SELECT id FROM messages WHERE account_id = ? AND message_id IN (?, ?)`,
			accountID, bare, "<"+bare+">"); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// Location is where a message is on its server.
type Location struct {
	MessageID   string
	AccountID   string
	Mailbox     string
	UIDValidity uint32
	UID         uint32
	// Label is the label being in this folder implies, for servers whose
	// folders are labels; empty for Gmail's All Mail.
	Label string
}

// LinkLocation records where messages are on the server, and Gmail's ids for
// them when known.
func LinkLocation(ids []string, loc Location, gmMsgID, gmThrID uint64) error {
	for _, id := range ids {
		if _, err := db.Exec(`
			INSERT INTO message_locations (message_id, account_id, mailbox, uid_validity, uid, label)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, mailbox) DO UPDATE SET
				uid_validity = excluded.uid_validity, uid = excluded.uid, label = excluded.label`,
			id, loc.AccountID, loc.Mailbox, loc.UIDValidity, loc.UID, loc.Label); err != nil {
			return err
		}
		if gmMsgID != 0 {
			if _, err := db.Exec(`UPDATE messages SET gm_msgid = ?, gm_thrid = ? WHERE id = ?`,
				int64(gmMsgID), nullIfZero64(gmThrID), id); err != nil {
				return err
			}
		}
	}
	return nil
}

// FolderUIDs maps each UID stored for a folder to the messages at it.
func FolderUIDs(accountID, mailbox string) (map[uint32][]string, error) {
	rows, err := db.Query(`SELECT uid, message_id FROM message_locations WHERE account_id = ? AND mailbox = ?`,
		accountID, mailbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32][]string{}
	for rows.Next() {
		var uid uint32
		var id string
		if err := rows.Scan(&uid, &id); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], id)
	}
	return out, rows.Err()
}

// RemoveLocations forgets that messages are at these UIDs, and returns the
// messages affected.
func RemoveLocations(accountID, mailbox string, uids []uint32) ([]string, error) {
	var affected []string
	for _, uid := range uids {
		rows, err := db.Query(`SELECT message_id FROM message_locations WHERE account_id = ? AND mailbox = ? AND uid = ?`,
			accountID, mailbox, uid)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			rows.Scan(&id)
			affected = append(affected, id)
		}
		rows.Close()
		if _, err := db.Exec(`DELETE FROM message_locations WHERE account_id = ? AND mailbox = ? AND uid = ?`,
			accountID, mailbox, uid); err != nil {
			return nil, err
		}
	}
	return affected, nil
}

// ForgetFolder drops every location in a folder, for a folder the server has
// renumbered: its UIDs no longer mean what they did.
func ForgetFolder(accountID, mailbox string) error {
	_, err := db.Exec(`DELETE FROM message_locations WHERE account_id = ? AND mailbox = ?`, accountID, mailbox)
	return err
}

// LocationsOf returns where a message is on its server.
func LocationsOf(messageID string) ([]Location, error) {
	rows, err := db.Query(`SELECT message_id, account_id, mailbox, uid_validity, uid, label
		FROM message_locations WHERE message_id = ? ORDER BY mailbox`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Location
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.MessageID, &l.AccountID, &l.Mailbox, &l.UIDValidity, &l.UID, &l.Label); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ServerState is what the server says about a message.
type ServerState struct {
	// Labels replace the message's labels when Replace is set (Gmail, where
	// every label is the server's). Otherwise they are added.
	Labels  []string
	Replace bool
	// Flags replace the message's flags. Nil leaves them alone.
	Flags []string
}

// ApplyServerState makes messages agree with their server, except for the
// labels and flags with a change of ours still queued: those keep what this
// side said until the change has been made there.
func ApplyServerState(ids []string, st ServerState) error {
	now := time.Now().UnixMilli()
	want := map[string]bool{}
	var wantList []string
	for _, l := range st.Labels {
		if n := NormalizeLabel(l); n != "" && !want[strings.ToLower(n)] {
			want[strings.ToLower(n)] = true
			wantList = append(wantList, n)
		}
	}
	if err := ensureLabels(db, wantList...); err != nil {
		return err
	}
	for _, id := range ids {
		pendingLabels, pendingFlags, err := PendingLabels(id)
		if err != nil {
			return err
		}
		if st.Replace {
			have, err := MailLabels([]string{id})
			if err != nil {
				return err
			}
			for _, l := range have[id] {
				if !want[strings.ToLower(l)] && !pendingLabels[l] {
					if _, err := db.Exec(`DELETE FROM message_labels WHERE message_id = ? AND label = ?`, id, l); err != nil {
						return err
					}
				}
			}
		}
		for _, l := range wantList {
			if pendingLabels[l] {
				continue
			}
			if _, err := db.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at) VALUES (?, ?, 'server', ?)`,
				id, l, now); err != nil {
				return err
			}
		}
		if st.Flags != nil {
			if err := applyServerFlags(id, st.Flags, pendingFlags); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyServerFlags sets a message's flags to the server's, keeping the local
// state of any flag with a change queued.
func applyServerFlags(id string, server []string, pending map[string]bool) error {
	var raw string
	if err := db.QueryRow(`SELECT COALESCE(flags, '[]') FROM messages WHERE id = ?`, id).Scan(&raw); err != nil {
		return err
	}
	var local []string
	json.Unmarshal([]byte(raw), &local)
	has := func(list []string, f string) bool {
		for _, x := range list {
			if strings.EqualFold(x, f) {
				return true
			}
		}
		return false
	}
	var next []string
	for _, f := range server {
		if pending[f] && !has(local, f) {
			continue // we removed it; the server has not heard yet
		}
		next = append(next, f)
	}
	for f := range pending {
		if has(local, f) && !has(next, f) {
			next = append(next, f) // we added it
		}
	}
	if next == nil {
		next = []string{}
	}
	enc, _ := json.Marshal(next)
	if string(enc) == raw {
		return nil
	}
	_, err := db.Exec(`UPDATE messages SET flags = ? WHERE id = ?`, string(enc), id)
	return err
}

// RemoveFolderLabels takes off the labels of folders a message has left,
// unless another of its locations still implies the label. For servers whose
// folders are labels.
func RemoveFolderLabels(ids []string, labels []string) error {
	for _, id := range ids {
		pending, _, err := PendingLabels(id)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if l == "" || pending[l] {
				continue
			}
			if _, err := db.Exec(`
				DELETE FROM message_labels WHERE message_id = ? AND label = ?
				  AND NOT EXISTS (SELECT 1 FROM message_locations WHERE message_id = ? AND label = ?)`,
				id, l, id, l); err != nil {
				return err
			}
		}
	}
	return nil
}

// Orphan marks messages the server no longer has anywhere. They are kept —
// this is the copy that outlives the server's — and moved to Trash, which is
// where the server last had them or where something deleted from it belongs.
func Orphan(ids []string) (int, error) {
	n := 0
	now := time.Now().UnixMilli()
	for _, id := range ids {
		var left int
		if err := db.QueryRow(`SELECT COUNT(*) FROM message_locations WHERE message_id = ?`, id).Scan(&left); err != nil {
			return n, err
		}
		if left > 0 {
			continue
		}
		if _, err := db.Exec(`DELETE FROM message_labels WHERE message_id = ? AND label IN ('inbox', 'spam')`, id); err != nil {
			return n, err
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO message_labels (message_id, label, source, created_at) VALUES (?, 'trash', 'server', ?)`, id, now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// AccountIsGmail reports whether an account's messages carry Gmail ids,
// which is how a later reader knows the mirror treated it as Gmail.
func AccountIsGmail(accountID string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM messages WHERE account_id = ? AND gm_msgid IS NOT NULL LIMIT 1`, accountID).Scan(&n)
	return n > 0
}

// MessageForPush is what the push needs about a message: where it is, and
// its labels and flags as this side has them.
type MessageForPush struct {
	ID        string
	Locations []Location
	Labels    []string
	Flags     []string
}

// ForPush loads a message for pushing a change.
func ForPush(id string) (*MessageForPush, error) {
	m := &MessageForPush{ID: id}
	var raw string
	if err := db.QueryRow(`SELECT COALESCE(flags, '[]') FROM messages WHERE id = ?`, id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	json.Unmarshal([]byte(raw), &m.Flags)
	locs, err := LocationsOf(id)
	if err != nil {
		return nil, err
	}
	m.Locations = locs
	labels, err := MailLabels([]string{id})
	if err != nil {
		return nil, err
	}
	m.Labels = labels[id]
	return m, nil
}

// String names a location for logs and errors.
func (l Location) String() string { return fmt.Sprintf("%s/%d", l.Mailbox, l.UID) }
