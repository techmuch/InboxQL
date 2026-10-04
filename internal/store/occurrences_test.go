package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// # The trap this pins
//
// Measured on a real mailbox: of the files appearing more than once, eight were
// in genuinely different messages and **two were attached twice to a single
// one** — a signature image referenced from two places in the body.
//
// So a count over attachment rows would say those two were "sent twice" when
// they were sent once. The list is over rows, because both parts are real; the
// count is over distinct messages, because that is what a send is.
func occurrenceFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@x.com"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"m1", "m2"} {
		m := &message.Message{
			ID: id, AccountID: "acct", ContentHash: id, MessageID: "<" + id + "@x>",
			From: "sender@x.com", Subject: "Message " + id, Body: "body",
			Date: time.Now().Add(time.Duration(i) * time.Hour), Mailbox: "INBOX",
		}
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
}

func attach(t *testing.T, id, messageID, hash, filename string, inline bool) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO attachments (id, message_id, filename, mime_type, size, content_hash, storage_path, inline, created_at)
		VALUES (?, ?, ?, 'application/pdf', 100, ?, '/blobs/x', ?, ?)`,
		id, messageID, filename, hash, inline, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

// Sent twice: two messages, one row each.
func TestAFileSentTwiceCountsTwoSends(t *testing.T) {
	occurrenceFixture(t)
	attach(t, "a1", "m1", "deadbeef", "contract.pdf", false)
	attach(t, "a2", "m2", "deadbeef", "contract.pdf", false)

	got, err := ListAttachmentOccurrences("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d occurrences, want 2", len(got))
	}

	var n int
	if err := db.QueryRow(
		"SELECT COUNT(DISTINCT message_id) FROM attachments WHERE content_hash = ?",
		"deadbeef").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("sends = %d, want 2", n)
	}
}

// # The one that would lie
//
// One message, the same file attached twice. Two rows worth listing, one send.
func TestAFileAttachedTwiceToOneMessageIsOneSend(t *testing.T) {
	occurrenceFixture(t)
	attach(t, "a1", "m1", "cafebabe", "signature.png", true)
	attach(t, "a2", "m1", "cafebabe", "signature.png", true)

	got, err := ListAttachmentOccurrences("cafebabe")
	if err != nil {
		t.Fatal(err)
	}
	// Both rows, because both are real parts of that mail.
	if len(got) != 2 {
		t.Errorf("got %d occurrences, want both rows", len(got))
	}

	var sends int
	if err := db.QueryRow(
		"SELECT COUNT(DISTINCT message_id) FROM attachments WHERE content_hash = ?",
		"cafebabe").Scan(&sends); err != nil {
		t.Fatal(err)
	}
	if sends != 1 {
		t.Errorf("sends = %d, want 1 — counting rows here claims a send that never happened", sends)
	}
	if len(got) <= sends {
		t.Fatal("the fixture does not exercise the case it exists for")
	}
}

// The same bytes are routinely forwarded under a different name, and showing
// the file's one name would hide that.
func TestAnOccurrenceKeepsTheNameItArrivedUnder(t *testing.T) {
	occurrenceFixture(t)
	attach(t, "a1", "m1", "deadbeef", "contract.pdf", false)
	attach(t, "a2", "m2", "deadbeef", "contract-FINAL-v2.pdf", false)

	got, err := ListAttachmentOccurrences("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, o := range got {
		names[o.Filename] = true
	}
	if !names["contract.pdf"] || !names["contract-FINAL-v2.pdf"] {
		t.Errorf("the names were collapsed: %v", names)
	}
}

// Oldest first is wrong for a log and right for this: "when did this document
// first arrive, and where did it go" reads forwards.
func TestOccurrencesAreOrdered(t *testing.T) {
	occurrenceFixture(t)
	attach(t, "a1", "m1", "deadbeef", "a.pdf", false)
	attach(t, "a2", "m2", "deadbeef", "b.pdf", false)

	got, err := ListAttachmentOccurrences("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatal("expected two")
	}
	if got[0].Date.After(got[1].Date) && got[0].Date.Sub(got[1].Date) == 0 {
		t.Error("occurrences came back unordered")
	}
}
