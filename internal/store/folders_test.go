package store

import (
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
func openFolderFixture(t *testing.T) {
	t.Helper()

	// CloseDB resets the once, so each test gets a database of its own rather
	// than sharing one opened in TestMain and clearing tables between runs.
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)

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
		// Filed away. Apple Mail's path shape, which is how it arrives from an
		// import rather than a bare "Archive".
		{ID: "filed", From: "dave@x.com", Mailbox: "9434D814-160E-4E72/Archive.mbox", Flags: []string{`\Seen`}},
		// Gmail's All Mail is deliberately *not* archive: it contains the
		// inbox too, so counting it as filed would empty the inbox of mail
		// that really is in it.
		{ID: "allmail", From: "erin@x.com", Mailbox: "8D7EBE5F/[Gmail].mbox/All Mail.mbox", Flags: nil},
		// A folder that merely contains the word is not the archive.
		{ID: "named", From: "frank@x.com", Mailbox: "Archived invoices", Flags: []string{`\Seen`}},
		// Sent, and later filed. Both claims are true and the folders have to
		// partition, so exactly one of them may have it.
		{ID: "sent-then-filed", From: "me@example.com", Mailbox: "Archive", Flags: []string{`\Seen`}},
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

// newFolderMessage builds a message in the folder fixture's account.
func newFolderMessage(id, from, mailbox string, flags []string) *message.Message {
	now := time.Now()
	return &message.Message{
		ID: id, AccountID: "acct", ContentHash: id,
		From: from, Mailbox: mailbox, Flags: flags,
		MessageID: "<" + id + "@test>",
		Date:      now, InternalDate: now,
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
		// allmail and named stay in the remainder on purpose: Gmail's All Mail
		// contains the inbox, and a folder merely named "Archived invoices" is
		// not the archive.
		FolderInbox:   {"allmail", "i1", "i2", "named", "star"},
		FolderStarred: {"star"},
		FolderSent:    {"sent-by-mailbox", "sent-by-sender", "sent-then-filed"},
		FolderArchive: {"filed"},
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
	for _, folder := range []string{FolderInbox, FolderSent, FolderArchive, FolderSpam, FolderTrash} {
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
	for _, name := range []string{"", FolderAll, FolderInbox, FolderDrafts, FolderArchive} {
		if !ValidFolder(name) {
			t.Errorf("ValidFolder(%q) = false, want true", name)
		}
	}
	// Names are lower-case identifiers, not the mailbox names they came from:
	// "Archive" is the place on disk, `archive` is the folder in this language.
	for _, name := range []string{"nonsense", "INBOX", "Sent", "Archive", "all mail"} {
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

	for _, folder := range []string{
		FolderInbox, FolderStarred, FolderSent, FolderArchive, FolderSpam, FolderTrash,
	} {
		if got, want := byName[folder].Total, len(folderIDs(t, folder)); got != want {
			t.Errorf("%s count = %d, but the folder lists %d messages", folder, got, want)
		}
	}

	// i1 carries no flags, star carries only \Flagged, and allmail carries
	// none — so three are unread; i2 and named are the inbox messages marked
	// \Seen. Starred mail being unread is ordinary, not a special case.
	if byName[FolderInbox].Unread != 3 {
		t.Errorf("inbox unread = %d, want 3", byName[FolderInbox].Unread)
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

// FolderCounts runs its folders concurrently, so the order of the result is
// not the order the goroutines finish in.
//
// The sidebar is laid out in Folders' order, so a result that came back sorted
// by whichever count returned first would shuffle the rail on every refresh.
func TestFolderCountsKeepTheirOrder(t *testing.T) {
	openQueryFixture(t)

	counts, err := FolderCounts("")
	if err != nil {
		t.Fatalf("FolderCounts: %v", err)
	}
	if len(counts) != len(Folders) {
		t.Fatalf("got %d rows, want one per folder (%d)", len(counts), len(Folders))
	}
	for i, want := range Folders {
		if counts[i].Folder != want {
			t.Errorf("row %d is %q, want %q", i, counts[i].Folder, want)
		}
	}
}

// Concurrency must not change the answers, which is the whole point of
// splitting the loop rather than rewriting the query.
func TestFolderCountsAgreeWithCountingOneAtATime(t *testing.T) {
	openQueryFixture(t)

	together, err := FolderCounts("")
	if err != nil {
		t.Fatalf("FolderCounts: %v", err)
	}
	for i, folder := range Folders {
		alone, err := countFolder(folder, "")
		if err != nil {
			t.Fatalf("countFolder(%s): %v", folder, err)
		}
		if together[i] != alone {
			t.Errorf("%s: concurrent %+v, alone %+v", folder, together[i], alone)
		}
	}
}

// The defect this folder exists for.
//
// The inbox was the remainder — not sent, not deleted, not junk — which is tidy
// until a mailbox is mostly archive. On a real one of 43,553 messages, zero
// were in anything named INBOX and the rail still reported "Inbox 39,068",
// every one of them read out of Archive.mbox. The number was right and the word
// was wrong, and there was no way to ask for Archive as Archive.
func TestArchivedMailIsNotTheInbox(t *testing.T) {
	openFolderFixture(t)

	inbox := folderMembers(t, FolderInbox)
	if inbox["filed"] {
		t.Error("archived mail is still being counted as inbox")
	}

	archive := folderMembers(t, FolderArchive)
	if !archive["filed"] {
		t.Error("the archived message is not in the archive folder")
	}

	// Gmail's All Mail holds the inbox, so it stays in the remainder. Claiming
	// it as archive would hide mail that really is in the inbox.
	if archive["allmail"] {
		t.Error("Gmail's All Mail was claimed as archive")
	}
	if !inbox["allmail"] {
		t.Error("All Mail left the inbox, which empties a Gmail account")
	}

	// Matched on the trailing segment, so a folder merely containing the word
	// is not swept in.
	if archive["named"] {
		t.Error(`"Archived invoices" was treated as the archive`)
	}
	if !inbox["named"] {
		t.Error(`"Archived invoices" fell out of the inbox too`)
	}
}

// Deleted still wins, the way it does for every other folder.
func TestDeletedArchivedMailLeavesTheArchive(t *testing.T) {
	openFolderFixture(t)

	if err := SaveMessage(&message.Message{
		ID: "filed-gone", AccountID: "acct", From: "g@x.com",
		Mailbox: "Archive", Flags: []string{`\Deleted`}, Date: time.Now(),
	}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if folderMembers(t, FolderArchive)["filed-gone"] {
		t.Error("deleted mail is still in the archive")
	}
}

// folderMembers is the set of message ids a folder contains.
func folderMembers(t *testing.T, folder string) map[string]bool {
	t.Helper()
	where := folderClause(folder)
	if where == "" {
		t.Fatalf("folder %q has no clause", folder)
	}
	rows, err := db.Query("SELECT id FROM messages WHERE " + where)
	if err != nil {
		t.Fatalf("querying %s: %v", folder, err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}
