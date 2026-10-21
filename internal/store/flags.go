package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
// # Here first, then the server
//
// This writes InboxQL's copy at once and queues the change for the message's
// server (see pending.go), which the account's sync worker applies within
// seconds. An account with no server — an imported archive — keeps the change
// here only.
//
// Returns how many messages actually changed, which is what lets a caller say
// "4 marked read" rather than "done".
func SetMessageFlag(ids []string, flag string, on bool) (int, error) {
	changed, err := setMessageFlag(ids, flag, on)
	return len(changed), err
}

// setMessageFlag is SetMessageFlag reporting which messages it changed, so a
// bulk change can be undone on exactly those.
func setMessageFlag(ids []string, flag string, on bool) ([]string, error) {
	if !settableFlags[flag] {
		return nil, fmt.Errorf("%q is not a flag you can set (try %s or %s)",
			flag, FlagSeen, FlagFlagged)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT id, account_id, COALESCE(flags, '[]') FROM messages WHERE id IN ("+
		placeholders(len(ids))+")", anySlice(ids)...)
	if err != nil {
		return nil, err
	}

	type update struct{ id, acct, flags string }
	var updates []update
	for rows.Next() {
		var id, acct, raw string
		if err := rows.Scan(&id, &acct, &raw); err != nil {
			rows.Close()
			return nil, err
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
			return nil, err
		}
		updates = append(updates, update{id: id, acct: acct, flags: string(encoded)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	op := OpFlagAdd
	if !on {
		op = OpFlagRemove
	}
	now := time.Now().UnixMilli()
	accounts := map[string]bool{}
	for _, u := range updates {
		if _, err := tx.Exec("UPDATE messages SET flags = ? WHERE id = ?", u.flags, u.id); err != nil {
			return nil, err
		}
		if err := enqueue(tx, u.acct, u.id, op, flag, now); err != nil {
			return nil, err
		}
		accounts[u.acct] = true
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	notifyPending(accounts)
	out := make([]string, len(updates))
	for i, u := range updates {
		out[i] = u.id
	}
	return out, nil
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
