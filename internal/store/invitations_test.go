package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// inviteMail is an Outlook-shaped message: the invitation as an alternative
// body, not an attachment.
func inviteMail(id, method string, seq int, start string, extra string) []byte {
	ics := strings.Join([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "METHOD:" + method, "BEGIN:VEVENT",
		"UID:engine-review", "SEQUENCE:" + fmt.Sprint(seq), "SUMMARY:Engine review",
		"DTSTART:" + start, "DTEND:" + strings.Replace(start, "T15", "T16", 1),
		"ORGANIZER:mailto:ada@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@example.com", extra,
		"END:VEVENT", "END:VCALENDAR"}, "\r\n")
	ics = strings.ReplaceAll(ics, "\r\n\r\n", "\r\n")
	return []byte("From: Ada <ada@example.com>\r\nTo: me@example.com\r\nSubject: Invitation\r\nMessage-ID: <" + id +
		"@x>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=b\r\n\r\n--b\r\n" +
		"Content-Type: text/plain\r\n\r\nYou are invited.\r\n--b\r\nContent-Type: text/calendar; method=" + method +
		"\r\n\r\n" + ics + "\r\n--b--\r\n")
}

func saveRaw(t *testing.T, id string, raw []byte) {
	t.Helper()
	m, _ := message.ParseRFC822(raw)
	m.ID, m.AccountID, m.Header = id, "acct", raw
	m.ContentHash = id
	if err := SaveMessage(m); err != nil {
		t.Fatal(err)
	}
}

func TestInvitationsFromMail(t *testing.T) {
	setupTestDB(t)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Add(72 * time.Hour).UTC().Format("20060102")
	later := time.Now().Add(96 * time.Hour).UTC().Format("20060102")
	saveRaw(t, "m1", inviteMail("m1", "REQUEST", 0, day+"T150000Z", ""))
	saveRaw(t, "m2", inviteMail("m2", "REQUEST", 2, later+"T150000Z", "")) // moved a day
	saveRaw(t, "m3", inviteMail("m3", "REQUEST", 1, day+"T150000Z", ""))   // an older update, read late

	rep, err := ProcessMailInvitations(0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Messages != 3 || rep.Events != 2 || rep.Stale != 1 || !rep.Done {
		t.Fatalf("report %+v", rep)
	}
	evs, _ := EventsByUID(MailCalendarID, "engine-review")
	if len(evs) != 1 || evs[0].Sequence != 2 || evs[0].Start.Format("20060102") != later || evs[0].MyStatus != "needs-action" {
		t.Fatalf("event %+v", evs)
	}
	if evs[0].SourceMessageID != "m2" {
		t.Errorf("linked to %q", evs[0].SourceMessageID)
	}

	// Nothing new: a second pass reads nothing.
	if rep, _ := ProcessMailInvitations(0); rep.Examined != 0 {
		t.Errorf("second pass examined %d", rep.Examined)
	}

	// A reply from a colleague updates their status; a cancellation marks it.
	saveRaw(t, "m4", []byte(strings.Replace(string(inviteMail("m4", "REPLY", 2, later+"T150000Z",
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:bob@example.com")), "ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:me@example.com\r\n", "", 1)))
	UpsertEvent(MailCalendarID, evs[0].Model(), EventMeta{}) // bob is an attendee on our copy
	db.Exec(`INSERT INTO event_attendees (event_id, address, status) VALUES (?, 'bob@example.com', 'needs-action')`, evs[0].ID)
	saveRaw(t, "m5", inviteMail("m5", "CANCEL", 3, later+"T150000Z", ""))
	rep, _ = ProcessMailInvitations(0)
	if rep.Replies != 1 || rep.Cancelled != 1 {
		t.Fatalf("report %+v", rep)
	}
	ev, _ := GetEvent(evs[0].ID)
	if ev.Status != "cancelled" {
		t.Errorf("status %q", ev.Status)
	}
	var bob string
	db.QueryRow(`SELECT status FROM event_attendees WHERE event_id = ? AND address = 'bob@example.com'`, ev.ID).Scan(&bob)
	if bob != "accepted" {
		t.Errorf("bob %q", bob)
	}
	if occ, _ := ListOccurrences(time.Now(), time.Now().Add(10*24*time.Hour), nil); len(occ) != 0 {
		t.Errorf("a cancelled event is still shown: %+v", occ)
	}
}
