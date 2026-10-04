package store

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// # What v36 is for
//
// `header` never held headers. It held the entire raw RFC822 message — 86.7%
// of a real mailbox's messages table — and SQLite keeps that in the b-tree a
// scan walks, so every query an index could not answer dragged 106 KB a row to
// read a 40-byte subject.
//
// Measured on 100,000 generated messages before and after moving it out:
// `subject LIKE` 6.15 s → 0.022 s, `GROUP BY sender` 6.18 s → 0.033 s, the
// table 1.9 GB → 17 MB.
//
// These pin the behaviour that makes that safe: the bytes survive, and the
// paths that need them still get them.

func openRawFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@x.com"}); err != nil {
		t.Fatal(err)
	}
}

func rawMessage(id, raw string) *message.Message {
	m := &message.Message{
		ID: id, AccountID: "acct", ContentHash: id,
		MessageID: "<" + id + "@x>", From: "a@x.com", Subject: "Subject " + id,
		Body: "body", Date: time.Now(), Mailbox: "INBOX",
		Header: []byte(raw),
	}
	m.InternalDate = m.Date
	return m
}

// The bytes go in and come back. Everything else here is about who reads them.
func TestTheRawSurvivesTheSplit(t *testing.T) {
	openRawFixture(t)

	raw := "From: a@x.com\r\nSubject: Subject m1\r\n\r\nthe whole message"
	if err := SaveMessage(rawMessage("m1", raw)); err != nil {
		t.Fatal(err)
	}

	got, err := LoadRaw("m1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Errorf("raw came back as %q", got)
	}
}

// A list must not carry it. This is the entire speedup: a query over messages
// does not touch the raw table, so the scan reads a couple of hundred bytes a
// row instead of a hundred kilobytes.
func TestAListDoesNotCarryTheRaw(t *testing.T) {
	openRawFixture(t)
	if err := SaveMessage(rawMessage("m1", strings.Repeat("x", 50_000))); err != nil {
		t.Fatal(err)
	}

	msgs, err := SearchMessages(SearchQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if len(msgs[0].Header) != 0 {
		t.Errorf("a listed message carries %d bytes of raw; the scan is still reading it",
			len(msgs[0].Header))
	}
}

// The detail path does carry it: a caller asking for one message by id is the
// one that re-parses it, and silently handing back a message with no raw is
// how an export comes to contain synthesised headers that look fine.
func TestTheDetailPathCarriesTheRaw(t *testing.T) {
	openRawFixture(t)
	raw := "From: a@x.com\r\nX-Special: yes\r\n\r\nbody"
	if err := SaveMessage(rawMessage("m1", raw)); err != nil {
		t.Fatal(err)
	}

	got, err := GetMessageByID("m1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Header) != raw {
		t.Errorf("the detail path returned %q", got.Header)
	}
}

// WithRaw fills in what a list left out, and is the way an export gets the
// bytes it needs.
func TestWithRawFillsInWhatAListLeftOut(t *testing.T) {
	openRawFixture(t)
	raw := "From: a@x.com\r\n\r\nbody"
	if err := SaveMessage(rawMessage("m1", raw)); err != nil {
		t.Fatal(err)
	}

	msgs, _ := SearchMessages(SearchQuery{Limit: 1})
	if _, err := WithRaw(msgs[0]); err != nil {
		t.Fatal(err)
	}
	if string(msgs[0].Header) != raw {
		t.Errorf("WithRaw gave %q", msgs[0].Header)
	}
}

// A message with no stored raw is not an error. Mail imported before the raw
// was kept simply has none, and the callers that want it already cope with a
// message they cannot re-parse.
func TestAMessageWithNoRawIsNotAnError(t *testing.T) {
	openRawFixture(t)
	m := rawMessage("m1", "")
	m.Header = nil
	if err := SaveMessage(m); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadRaw("m1")
	if err != nil {
		t.Errorf("a message with no raw errored: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("got %d bytes for a message with no raw", len(raw))
	}
	if _, err := GetMessageByID("m1"); err != nil {
		t.Errorf("the detail path failed on a message with no raw: %v", err)
	}
}

// Deleting a message takes its raw with it. Without the cascade the bytes are
// orphaned, and at a hundred kilobytes each that is the whole mailbox leaking.
func TestDeletingAMessageTakesItsRaw(t *testing.T) {
	openRawFixture(t)
	if err := SaveMessage(rawMessage("m1", "From: a@x.com\r\n\r\nbody")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM messages WHERE id = ?", "m1"); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM message_raw WHERE message_id = ?", "m1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the raw outlived its message; the cascade is not firing")
	}
}

// Re-importing the same message must not write a second copy of bytes already
// stored against the row that won.
func TestReimportDoesNotDuplicateTheRaw(t *testing.T) {
	openRawFixture(t)
	raw := "From: a@x.com\r\n\r\nbody"
	if err := SaveMessage(rawMessage("m1", raw)); err != nil {
		t.Fatal(err)
	}
	// Same content hash, different id: what a re-import looks like.
	second := rawMessage("m2", raw)
	second.ContentHash = "m1"
	if err := SaveMessage(second); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM message_raw").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d raw rows for one deduplicated message", n)
	}
}
