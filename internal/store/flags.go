package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IMAP flag names, as stored.
const (
	FlagSeen    = `\Seen`
	FlagFlagged = `\Flagged`
)

// settableFlags are the flags a user may change from InboxQL.
//
// Deliberately short. \Deleted and \Draft describe what a message *is* to the
// server and to the importer; letting a checkbox write them would make the
// mailbox disagree with itself. Read and starred are the two that are a
// person's opinion.
var settableFlags = map[string]bool{
	FlagSeen:    true,
	FlagFlagged: true,
}

// SetMessageFlag adds or removes one flag across a set of messages.
//
// # Local only, and honest about it
//
// This writes InboxQL's copy. There is no IMAP write-back: the sync path is
// one-way, so marking something read here does not mark it read on the server,
// and the next sync of that mailbox may reassert what the server thinks. Said
// plainly here because the alternative — a UI that looks like a mail client
// and silently diverges from one — is worse than a documented limit.
//
// Returns how many messages actually changed, which is what lets a caller say
// "4 marked read" rather than "done".
func SetMessageFlag(ids []string, flag string, on bool) (int, error) {
	if !settableFlags[flag] {
		return 0, fmt.Errorf("%q is not a flag you can set (try %s or %s)",
			flag, FlagSeen, FlagFlagged)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT id, COALESCE(flags, '[]') FROM messages WHERE id IN ("+
		placeholders(len(ids))+")", anySlice(ids)...)
	if err != nil {
		return 0, err
	}

	type update struct{ id, flags string }
	var updates []update
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var current []string
		json.Unmarshal([]byte(raw), &current)

		next, changed := withFlag(current, flag, on)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			rows.Close()
			return 0, err
		}
		updates = append(updates, update{id: id, flags: string(encoded)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, u := range updates {
		if _, err := tx.Exec("UPDATE messages SET flags = ? WHERE id = ?", u.flags, u.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(updates), nil
}

// withFlag returns the flag list with one flag added or removed, and whether
// anything changed.
//
// Case-insensitive on the way in because servers are inconsistent about it,
// but the canonical spelling is what gets written — otherwise `is:unread`,
// which compares exactly, would disagree with what the checkbox just did.
func withFlag(current []string, flag string, on bool) ([]string, bool) {
	out := make([]string, 0, len(current)+1)
	found := false
	for _, f := range current {
		if strings.EqualFold(f, flag) {
			found = true
			if !on {
				continue
			}
			out = append(out, flag)
			continue
		}
		out = append(out, f)
	}
	if on && !found {
		out = append(out, flag)
		return out, true
	}
	return out, found != on
}
