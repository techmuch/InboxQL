package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/message"
)

// Invitations in mail become events.
//
// Everyone who has been sent an invitation has a calendar in their mailbox
// already: the text/calendar part of each message is an event, an update, a
// cancellation or an answer. This reads them into the Invitations calendar,
// with no calendar account needed, and links each event to the message — and
// so the conversation — it came from.

const invitationCursorKey = "calendar_mail_cursor"

// InvitationReport says what a pass did.
type InvitationReport struct {
	Examined  int  `json:"examined"`
	Messages  int  `json:"messages"`
	Events    int  `json:"events"`
	Cancelled int  `json:"cancelled"`
	Replies   int  `json:"replies"`
	Stale     int  `json:"stale"`
	Skipped   int  `json:"skipped"`
	Failed    int  `json:"failed"`
	Done      bool `json:"done"`
}

func (r *InvitationReport) add(o InvitationReport) {
	r.Events += o.Events
	r.Cancelled += o.Cancelled
	r.Replies += o.Replies
	r.Stale += o.Stale
	r.Skipped += o.Skipped
}

// MyAddresses are the user's own addresses: the mail accounts' and the
// calendar accounts'. An invitation's attendee with one of these is "me".
func MyAddresses() ([]string, error) {
	mine, err := selfAddresses()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT LOWER(email) FROM calendar_accounts WHERE email IS NOT NULL AND email != ''`)
	if err != nil {
		return mine, nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	for _, a := range mine {
		seen[a] = true
	}
	for rows.Next() {
		var a string
		if rows.Scan(&a) == nil && !seen[a] {
			seen[a] = true
			mine = append(mine, a)
		}
	}
	return mine, nil
}

// ProcessMailInvitations reads invitations from messages not yet looked at,
// at most limit messages per pass (0 for no limit). Messages are taken in the
// order they were stored, so a later update lands after the invitation it
// updates.
func ProcessMailInvitations(limit int) (InvitationReport, error) {
	var rep InvitationReport
	cursor := int64(0)
	if v, _ := GetSetting(invitationCursorKey); v != "" {
		cursor, _ = strconv.ParseInt(v, 10, 64)
	}
	me, err := MyAddresses()
	if err != nil {
		return rep, err
	}
	q := `SELECT m.rowid, m.id, COALESCE(m.thread_key,''), r.raw FROM messages m
		JOIN message_raw r ON r.message_id = m.id
		WHERE m.rowid > ? AND (instr(r.raw, 'text/calendar') > 0 OR instr(r.raw, 'TEXT/CALENDAR') > 0
			OR instr(r.raw, 'Text/Calendar') > 0 OR instr(r.raw, '.ics') > 0)
		ORDER BY m.rowid`
	args := []any{cursor}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return rep, err
	}
	type item struct {
		rowid      int64
		id, thread string
		raw        []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.rowid, &it.id, &it.thread, &it.raw); err != nil {
			rows.Close()
			return rep, err
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		rep.Examined++
		parts := message.CalendarParts(it.raw)
		if len(parts) > 0 {
			rep.Messages++
		}
		for _, p := range parts {
			got, err := ApplyInvitation(it.id, it.thread, p, me)
			if err != nil {
				rep.Failed++
				continue
			}
			rep.add(got)
		}
		cursor = it.rowid
	}
	if len(items) > 0 {
		if err := UpdateSetting(invitationCursorKey, strconv.FormatInt(cursor, 10)); err != nil {
			return rep, err
		}
	}
	rep.Done = limit == 0 || len(items) < limit
	if !rep.Done {
		return rep, nil
	}
	// When the scan has caught up, advance past every stored message, so the
	// next pass looks only at mail that arrives after it.
	var max sql.NullInt64
	if db.QueryRow(`SELECT MAX(rowid) FROM messages`).Scan(&max) == nil && max.Valid && max.Int64 > cursor {
		_ = UpdateSetting(invitationCursorKey, strconv.FormatInt(max.Int64, 10))
	}
	return rep, nil
}

// ResetMailInvitations makes the next pass read every message again.
func ResetMailInvitations() error { return UpdateSetting(invitationCursorKey, "0") }

// ApplyInvitation applies one iCalendar part from a message, by its method.
func ApplyInvitation(messageID, threadKey string, data []byte, me []string) (InvitationReport, error) {
	var rep InvitationReport
	cal, err := calendar.Parse(data)
	if err != nil {
		return rep, err
	}
	mine := map[string]bool{}
	for _, a := range me {
		mine[strings.ToLower(a)] = true
	}
	switch cal.Method {
	case "", "PUBLISH", "REQUEST", "ADD":
		for _, ev := range cal.Events {
			if stale(ev) {
				rep.Stale++
				continue
			}
			myStatus := ""
			if mine[ev.Organizer] {
				myStatus = "organizer"
			}
			for _, a := range ev.Attendees {
				if mine[a.Address] {
					myStatus = a.Status
				}
			}
			if _, err := UpsertEvent(MailCalendarID, ev, EventMeta{Raw: string(data), SourceMessageID: messageID,
				ThreadKey: threadKey, Origin: "mail", MyStatus: myStatus}); err != nil {
				return rep, err
			}
			rep.Events++
		}
	case "CANCEL":
		for _, ev := range cal.Events {
			if err := cancelInvitation(ev, messageID, threadKey, string(data)); err != nil {
				return rep, err
			}
			rep.Cancelled++
		}
	case "REPLY":
		for _, ev := range cal.Events {
			for _, a := range ev.Attendees {
				if _, err := db.Exec(`UPDATE event_attendees SET status = ? WHERE address = ? AND event_id IN
					(SELECT id FROM events WHERE uid = ?)`, a.Status, a.Address, ev.UID); err != nil {
					return rep, err
				}
				rep.Replies++
			}
		}
	default:
		// COUNTER, DECLINECOUNTER, REFRESH: proposals and housekeeping the
		// organiser's own calendar acts on. Recorded nowhere yet.
		rep.Skipped++
	}
	return rep, nil
}

// stale reports whether a stored copy of the same occurrence already has a
// newer sequence: an older update read after a newer one changes nothing.
func stale(ev calendar.Event) bool {
	var seq int
	err := db.QueryRow(`SELECT sequence FROM events WHERE id = ?`, EventID(MailCalendarID, ev.UID, ev.RecurrenceID)).Scan(&seq)
	return err == nil && seq > ev.Sequence
}

// cancelInvitation marks a cancelled event, or one cancelled occurrence. The
// event stays, marked, rather than vanishing: "it was cancelled" is
// information, and the message that said so is linked to it.
func cancelInvitation(ev calendar.Event, messageID, threadKey, raw string) error {
	if ev.RecurrenceID.IsZero() {
		res, err := db.Exec(`UPDATE events SET status = 'cancelled', updated_at = ? WHERE calendar_id = ? AND uid = ?`,
			time.Now().UnixMilli(), MailCalendarID, ev.UID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := rebuildSeriesTx(tx, MailCalendarID, ev.UID); err != nil {
				return err
			}
			return tx.Commit()
		}
	}
	// One occurrence, or an event never seen before: store it cancelled.
	ev.Status = "cancelled"
	_, err := UpsertEvent(MailCalendarID, ev, EventMeta{Raw: raw, SourceMessageID: messageID, ThreadKey: threadKey, Origin: "mail"})
	return err
}

// ErrNoInvitation is a message with nothing to answer.
var ErrNoInvitation = errors.New("this message carries no invitation")
