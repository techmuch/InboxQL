package store

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// Folder membership is pure SQL over flags, sender and mailbox name — nothing
// the compiler checks. It has already broken once silently (an edit that did
// not match, leaving every folder returning every message), so this exercises
// a real database rather than asserting on generated SQL strings.
// InitDB is guarded by a sync.Once and CloseDB does not reset it, so the
// database is opened once for the package and each test clears the tables it
// uses rather than opening its own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "iql-store-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if _, err := InitDB(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	CloseDB()
	os.Exit(code)
}

func openFolderFixture(t *testing.T) {
	t.Helper()

	for _, table := range []string{"messages", "drafts", "accounts"} {
		if _, err := db.Exec("DELETE FROM " + table); err != nil {
			t.Fatalf("clearing %s: %v", table, err)
		}
	}

	if err := SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	now := time.Now()
	msgs := []*message.Message{
		{ID: "i1", From: "alice@x.com", Mailbox: "INBOX", Flags: nil},
		{ID: "i2", From: "bob@x.com", Mailbox: "INBOX", Flags: []string{`\Seen`}},
		{ID: "star", From: "carol@x.com", Mailbox: "INBOX", Flags: []string{`\Flagged`}},
		// Attributed to a Sent folder by the v14 mailbox column.
		{ID: "sent-by-mailbox", From: "someone@x.com", Mailbox: "Sent Messages", Flags: []string{`\Seen`}},
		// No mailbox — the pre-v14 case, caught by the sender instead.
		{ID: "sent-by-sender", From: "me@example.com", Mailbox: "", Flags: []string{`\Seen`}},
		{ID: "junk", From: "spam@x.com", Mailbox: "Junk", Flags: []string{`\Junk`}},
		{ID: "gone", From: "old@x.com", Mailbox: "INBOX", Flags: []string{`\Deleted`}},
		// Deleted mail leaves every other folder, including Starred and Spam.
		{ID: "gone-starred", From: "old2@x.com", Mailbox: "INBOX", Flags: []string{`\Flagged`, `\Deleted`}},
	}
	for _, m := range msgs {
		m.AccountID = "acct"
		m.ContentHash = m.ID
		m.Date, m.InternalDate = now, now
		if err := SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}
}

func folderIDs(t *testing.T, folder string) []string {
	t.Helper()
	msgs, err := SearchMessages(SearchQuery{Folder: folder, Limit: 100})
	if err != nil {
		t.Fatalf("SearchMessages(%s): %v", folder, err)
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestFolderMembership(t *testing.T) {
	openFolderFixture(t)

	want := map[string][]string{
		FolderInbox:   {"i1", "i2", "star"},
		FolderStarred: {"star"},
		FolderSent:    {"sent-by-mailbox", "sent-by-sender"},
		FolderSpam:    {"junk"},
		FolderTrash:   {"gone", "gone-starred"},
	}

	for folder, expected := range want {
		got := folderIDs(t, folder)
		if len(got) != len(expected) {
			t.Errorf("%s = %v, want %v", folder, got, expected)
			continue
		}
		for i := range got {
			if got[i] != expected[i] {
				t.Errorf("%s = %v, want %v", folder, got, expected)
				break
			}
		}
	}
}

// The inbox is defined as the remainder precisely so that no message is filed
// in two places at once. If that ever stops holding, the sidebar counts add up
// to more than the mailbox.
func TestFoldersDoNotOverlap(t *testing.T) {
	openFolderFixture(t)

	seen := map[string]string{}
	for _, folder := range []string{FolderInbox, FolderSent, FolderSpam, FolderTrash} {
		for _, id := range folderIDs(t, folder) {
			if other, dup := seen[id]; dup {
				t.Errorf("message %s is in both %s and %s", id, other, folder)
			}
			seen[id] = folder
		}
	}

	// Every message must land somewhere; a message in no folder is invisible.
	all := folderIDs(t, FolderAll)
	if len(seen) != len(all) {
		t.Errorf("%d of %d messages are filed; the rest are unreachable", len(seen), len(all))
	}
}

// Starred is a property rather than a place, so it deliberately overlaps the
// others — but never picks up deleted mail.
func TestStarredCrossesFoldersButNotTrash(t *testing.T) {
	openFolderFixture(t)

	got := folderIDs(t, FolderStarred)
	if len(got) != 1 || got[0] != "star" {
		t.Fatalf("starred = %v, want [star]", got)
	}
}

// An unrecognised folder name must not quietly become "everything".
func TestUnknownFolderIsRejected(t *testing.T) {
	for _, name := range []string{"", FolderAll, FolderInbox, FolderDrafts} {
		if !ValidFolder(name) {
			t.Errorf("ValidFolder(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"nonsense", "INBOX", "Sent", "archive"} {
		if ValidFolder(name) {
			t.Errorf("ValidFolder(%q) = true, want false", name)
		}
	}
}

func TestFolderCountsMatchMembership(t *testing.T) {
	openFolderFixture(t)

	counts, err := FolderCounts("acct")
	if err != nil {
		t.Fatalf("FolderCounts: %v", err)
	}

	byName := map[string]FolderCount{}
	for _, c := range counts {
		byName[c.Folder] = c
	}

	for _, folder := range []string{FolderInbox, FolderStarred, FolderSent, FolderSpam, FolderTrash} {
		if got, want := byName[folder].Total, len(folderIDs(t, folder)); got != want {
			t.Errorf("%s count = %d, but the folder lists %d messages", folder, got, want)
		}
	}

	// i1 carries no flags and star carries only \Flagged, so both are unread;
	// i2 is the only inbox message marked \Seen. Starred mail being unread is
	// ordinary, not a special case.
	if byName[FolderInbox].Unread != 2 {
		t.Errorf("inbox unread = %d, want 2", byName[FolderInbox].Unread)
	}
}

// Drafts come from a different table, are never unread, and carry the flag the
// UI keys off to present them as unsent.
func TestDraftsFolder(t *testing.T) {
	openFolderFixture(t)

	for _, d := range []*Draft{
		{ID: "d1", AccountID: "acct", To: []string{"you@x.com"}, Subject: "Half written", Status: DraftStatusDraft},
		{ID: "d2", AccountID: "acct", To: []string{"you@x.com"}, Subject: "Waiting", Status: DraftStatusQueued},
		{ID: "d3", AccountID: "acct", To: []string{"you@x.com"}, Subject: "Gone out", Status: DraftStatusSent},
	} {
		if err := SaveDraft(d); err != nil {
			t.Fatalf("SaveDraft(%s): %v", d.ID, err)
		}
	}

	msgs, err := DraftsAsMessages("acct", 100, 0)
	if err != nil {
		t.Fatalf("DraftsAsMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d drafts, want 2 (a sent draft is not a draft)", len(msgs))
	}

	for _, m := range msgs {
		hasDraft, hasSeen := false, false
		for _, f := range m.Flags {
			if f == `\Draft` {
				hasDraft = true
			}
			if f == `\Seen` {
				hasSeen = true
			}
		}
		if !hasDraft {
			t.Errorf("draft %s lacks the \\Draft flag the UI keys off", m.ID)
		}
		if !hasSeen {
			t.Errorf("draft %s would show as unread", m.ID)
		}
		if m.From != "" {
			t.Errorf("draft %s has a sender %q; it was never sent by anyone", m.ID, m.From)
		}
	}

	if n, err := countDrafts("acct"); err != nil || n != 2 {
		t.Errorf("countDrafts = %d, %v; want 2, nil", n, err)
	}
}

// A message with no header used to vanish: `header` is BLOB NOT NULL, a nil
// []byte binds as NULL, and INSERT OR IGNORE turned the constraint violation
// into a successful no-op. Nothing surfaced — not an error, not a log line.
func TestSaveMessageDoesNotSilentlyDrop(t *testing.T) {
	openFolderFixture(t)

	before := len(folderIDs(t, FolderAll))
	m := &message.Message{
		ID: "headerless", AccountID: "acct", From: "a@x.com",
		ContentHash: "unique-hash", Mailbox: "INBOX",
		Date: time.Now(), InternalDate: time.Now(),
		Header: nil,
	}
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if after := len(folderIDs(t, FolderAll)); after != before+1 {
		t.Fatalf("message count went %d -> %d; the row was dropped and SaveMessage still reported success", before, after)
	}
}

// Re-importing the same message must still be a quiet no-op — that is what
// INSERT OR IGNORE is for, and the fix above must not have broken it.
func TestSaveMessageIsIdempotent(t *testing.T) {
	openFolderFixture(t)

	before := len(folderIDs(t, FolderAll))
	m := &message.Message{
		ID: "dup", AccountID: "acct", From: "a@x.com", ContentHash: "i1",
		Mailbox: "INBOX", Date: time.Now(), InternalDate: time.Now(),
		Header: []byte("From: a@x.com"),
	}
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if after := len(folderIDs(t, FolderAll)); after != before {
		t.Errorf("a duplicate content hash was stored again: %d -> %d", before, after)
	}
}
