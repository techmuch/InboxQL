package sync

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/imaptest"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// fakeAccount starts a fake server and an account pointing at it.
func fakeAccount(t *testing.T, opt imaptest.Options) (*imaptest.Server, *account.Account) {
	t.Helper()
	t.Setenv("INBOXQL_HOME", t.TempDir())
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	srv, err := imaptest.Start(opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	account.TestRoots = srv.RootCAs
	t.Cleanup(func() { account.TestRoots = nil })
	acc := &account.Account{ID: "acct", Name: "Me", Email: "me@example.com", Host: "127.0.0.1", Port: srv.Port,
		SSL: true, User: srv.User, Password: srv.Pass}
	if err := store.SaveAccount(acc); err != nil {
		t.Fatal(err)
	}
	return srv, acc
}

func runSync(t *testing.T, acc *account.Account) Report {
	t.Helper()
	r, err := NewSyncManager(1).RunToCompletion(acc, true, nil)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return r
}

// labelsByMessageID returns each stored message's labels, keyed by its
// Message-ID header.
func labelsByMessageID(t *testing.T) map[string][]string {
	t.Helper()
	res, err := store.RunQuery("", 10000, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, m := range res.Messages {
		key := strings.Trim(m.MessageID, "<>")
		if _, dup := out[key]; dup {
			t.Errorf("message %s is stored twice", key)
		}
		ls := append([]string{}, m.Labels...)
		sort.Strings(ls)
		out[key] = ls
	}
	return out
}

func messageIDOf(t *testing.T, mid string) string {
	t.Helper()
	ids, err := store.SyncTargets("acct", 0, mid)
	if err != nil || len(ids) != 1 {
		t.Fatalf("SyncTargets(%s) = %v, %v", mid, ids, err)
	}
	return ids[0]
}

func want(t *testing.T, got map[string][]string, mid string, labels ...string) {
	t.Helper()
	sort.Strings(labels)
	if labels == nil {
		labels = []string{}
	}
	g := got[mid]
	if g == nil {
		g = []string{}
	}
	if strings.Join(g, ",") != strings.Join(labels, ",") {
		t.Errorf("%s labels = %v, want %v", mid, g, labels)
	}
}

func TestGmailMirrorReadsLabels(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	srv.Deliver(imaptest.Message("a@x", "inbox mail", "a@example.com"), imaptest.Inbox, "Receipts")
	srv.Deliver(imaptest.Message("b@x", "archived", "b@example.com"), "Receipts")
	srv.Deliver(imaptest.Message("c@x", "sent", "me@example.com"), imaptest.Sent)
	srv.Deliver(imaptest.Message("d@x", "binned", "d@example.com"), imaptest.Inbox, imaptest.Trash)
	srv.Deliver(imaptest.Message("e@x", "junk", "e@example.com"), imaptest.Spam)
	srv.Deliver(imaptest.Message("f@x", "important", "f@example.com"), imaptest.Inbox, imaptest.Important, "Work/Clients")

	r := runSync(t, acc)
	if r.New != 6 {
		t.Errorf("stored %d new, want 6", r.New)
	}
	got := labelsByMessageID(t)
	want(t, got, "a@x", "inbox", "Receipts")
	want(t, got, "b@x", "Receipts")
	want(t, got, "c@x", "sent")
	// Gmail drops Inbox when a message is binned; the fake keeps it, which
	// is the harder case: the message is in Trash, so it is out of the inbox
	// view whatever its labels say.
	want(t, got, "d@x", "inbox", "trash")
	want(t, got, "e@x", "spam")
	want(t, got, "f@x", "important", "inbox", "Work/Clients")

	// Every message knows where it is, and Gmail's id for it.
	if p, _ := store.MirrorProgressFor(acc.ID); p.Stored != 6 || !p.HistoryDone || p.OnServer != 6 {
		t.Errorf("progress = %+v, want 6 stored of 6, history done", p)
	}
	if !store.AccountIsGmail(acc.ID) {
		t.Error("Gmail ids were not recorded")
	}

	// A second pass changes nothing and stores nothing twice.
	if r := runSync(t, acc); r.New != 0 {
		t.Errorf("an idle pass stored %d new", r.New)
	}
}

// History is read newest first, in batches, and resumes where it stopped.
func TestHistoryIsNewestFirstAndResumes(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	total := historyBatch*2 + 37
	for i := 0; i < total; i++ {
		srv.Deliver(imaptest.Message(fmt.Sprintf("h%04d@x", i), fmt.Sprintf("m%d", i), "a@example.com"))
	}

	sm := NewSyncManager(1)
	s, err := sm.Open(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FullPass(); err != nil {
		t.Fatal(err)
	}
	if _, more, err := s.HistoryStep(); err != nil || !more {
		t.Fatalf("first history step: more=%v err=%v", more, err)
	}
	got := labelsByMessageID(t)
	if len(got) != historyBatch {
		t.Fatalf("after one step %d stored, want %d", len(got), historyBatch)
	}
	// The newest are here, the oldest are not.
	if _, ok := got[fmt.Sprintf("h%04d@x", total-1)]; !ok {
		t.Error("the newest message was not in the first batch")
	}
	if _, ok := got["h0000@x"]; ok {
		t.Error("the oldest message came first")
	}
	s.Close()

	// New mail arriving mid-history comes in on the next pass, ahead of the
	// rest of the history.
	srv.Deliver(imaptest.Message("late@x", "late", "a@example.com"))
	s, err = sm.Open(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r, err := s.QuickPass(); err != nil || r.New != 1 {
		t.Fatalf("quick pass during history: new=%d err=%v", r.New, err)
	}
	for {
		_, more, err := s.HistoryStep()
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if got := labelsByMessageID(t); len(got) != total+1 {
		t.Errorf("after history %d stored, want %d", len(got), total+1)
	}
	if !s.HistoryDone() {
		t.Error("history is not marked done")
	}
}

// Mail already here — an Apple Mail import of the same account — is linked
// to its server copy and takes the server's labels, without being
// downloaded again.
func TestImportedMailIsLinkedNotDownloaded(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	now := time.Now()
	imported := &message.Message{ID: "imported-1", AccountID: acc.ID, MessageID: "<old@x>", ContentHash: "old",
		From: "a@example.com", Subject: "from the import", Mailbox: "ABC/Archive.mbox",
		Header: []byte("From: a@example.com"), Date: now, InternalDate: now}
	if err := store.SaveMessage(imported); err != nil {
		t.Fatal(err)
	}
	srv.Deliver(imaptest.Message("old@x", "from the import", "a@example.com"), imaptest.Inbox, "Receipts")
	srv.Deliver(imaptest.Message("new@x", "new", "a@example.com"))

	r := runSync(t, acc)
	if r.New != 1 || r.Linked != 1 {
		t.Errorf("new=%d linked=%d, want 1 and 1", r.New, r.Linked)
	}
	got := labelsByMessageID(t)
	want(t, got, "old@x", "inbox", "Receipts")
	if len(got) != 2 {
		t.Errorf("%d messages stored, want 2", len(got))
	}
	// One full download: the new message's, not the imported one's.
	bodies := 0
	for _, c := range srv.Commands() {
		if strings.HasPrefix(c, "UID FETCH") && strings.Contains(c, "BODY.PEEK[]") {
			bodies++
		}
	}
	if bodies != 1 {
		t.Errorf("%d body fetches, want 1 (the imported message was downloaded again)", bodies)
	}
}

// What changes on the server — another device archives, labels, reads,
// trashes, deletes for good — changes here.
func TestServerChangesComeHere(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	a := srv.Deliver(imaptest.Message("a@x", "a", "a@example.com"), imaptest.Inbox)
	b := srv.Deliver(imaptest.Message("b@x", "b", "b@example.com"), imaptest.Inbox)
	c := srv.Deliver(imaptest.Message("c@x", "c", "c@example.com"), imaptest.Inbox)
	runSync(t, acc)

	srv.Change(a, func(m *imaptest.Msg) {
		delete(m.Labels, imaptest.Inbox)
		m.Labels["Travel"] = true
		m.Flags[`\Seen`] = true
		m.Flags[`\Flagged`] = true
	})
	// Binning in Gmail takes Inbox off.
	srv.Change(b, func(m *imaptest.Msg) { m.Labels[imaptest.Trash] = true; delete(m.Labels, imaptest.Inbox) })
	srv.Delete(c)

	sm := NewSyncManager(1)
	s, err := sm.Open(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.FullPass(); err != nil {
		t.Fatal(err)
	}
	got := labelsByMessageID(t)
	want(t, got, "a@x", "Travel")
	want(t, got, "b@x", "trash")
	// Gone from the server entirely: kept here, in Trash.
	want(t, got, "c@x", "trash")

	m, _ := store.GetMessageByID(messageIDOf(t, "a@x"))
	if !hasFlag(m.Flags, `\Seen`) || !hasFlag(m.Flags, `\Flagged`) {
		t.Errorf("a's flags = %v, want read and starred", m.Flags)
	}
	// The binned message's copy is in Trash now.
	locs, _ := store.LocationsOf(messageIDOf(t, "b@x"))
	if len(locs) != 1 || locs[0].Mailbox != "[Gmail]/Trash" {
		t.Errorf("b is at %v, want [Gmail]/Trash", locs)
	}

	// Restored on the server: back in the inbox here.
	srv.Change(b, func(m *imaptest.Msg) { delete(m.Labels, imaptest.Trash); m.Labels[imaptest.Inbox] = true })
	if _, err := s.FullPass(); err != nil {
		t.Fatal(err)
	}
	want(t, labelsByMessageID(t), "b@x", "inbox")
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if strings.EqualFold(x, f) {
			return true
		}
	}
	return false
}

// What changes here reaches Gmail.
func TestLocalChangesReachGmail(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	a := srv.Deliver(imaptest.Message("a@x", "a", "a@example.com"), imaptest.Inbox)
	b := srv.Deliver(imaptest.Message("b@x", "b", "b@example.com"), imaptest.Inbox)
	runSync(t, acc)
	ida, idb := messageIDOf(t, "a@x"), messageIDOf(t, "b@x")

	mustChange(t, ida, store.MoveArchive)
	mustChange(t, ida, store.LabelChange{Add: []string{"Work", "Résumés"}})
	if _, err := store.SetMessageFlag([]string{ida}, store.FlagFlagged, true); err != nil {
		t.Fatal(err)
	}
	mustChange(t, idb, store.MoveTrash)

	// A pass before the push must not undo what is queued.
	sm := NewSyncManager(1)
	s, err := sm.Open(acc, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.FullPass(); err != nil {
		t.Fatal(err)
	}
	want(t, labelsByMessageID(t), "a@x", "Résumés", "Work")

	r, err := s.Push()
	if err != nil || r.Pushed != 2 || r.PushFailed != 0 {
		t.Fatalf("push: %+v %v", r, err)
	}
	if got := srv.LabelsOf(a); strings.Join(got, ",") != "Résumés,Work" {
		t.Errorf("a on Gmail = %v, want Résumés and Work, no Inbox", got)
	}
	if !hasFlag(srv.FlagsOf(a), `\Flagged`) {
		t.Error("the star did not reach Gmail")
	}
	if !hasFlag(srv.LabelsOf(b), imaptest.Trash) {
		t.Errorf("b on Gmail = %v, want it in Trash", srv.LabelsOf(b))
	}
	if p, _ := store.PendingChanges(acc.ID, 0); len(p) != 0 {
		t.Errorf("%d changes still queued", len(p))
	}

	// The next pass agrees with what was pushed, and finds b in Trash.
	if _, err := s.FullPass(); err != nil {
		t.Fatal(err)
	}
	got := labelsByMessageID(t)
	want(t, got, "a@x", "Résumés", "Work")
	want(t, got, "b@x", "trash")

	// Out of the bin: back to the inbox there too.
	mustChange(t, idb, store.MoveUntrash)
	if _, err := s.Push(); err != nil {
		t.Fatal(err)
	}
	if l := srv.LabelsOf(b); hasFlag(l, imaptest.Trash) || !hasFlag(l, imaptest.Inbox) {
		t.Errorf("b on Gmail after restoring = %v, want Inbox and not Trash", l)
	}
	for _, c := range srv.Commands() {
		if c == "EXPUNGE" || strings.HasPrefix(c, "EXPUNGE ") {
			t.Errorf("the client expunged a whole folder: %q", c)
		}
	}
}

func mustChange(t *testing.T, id string, ch store.LabelChange) {
	t.Helper()
	if _, err := store.ChangeLabels([]string{id}, ch); err != nil {
		t.Fatal(err)
	}
}

// A server with folders: each folder is a label here, and the system labels
// decide which folder a message is moved to.
func TestFolderServerMirrorAndMoves(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{NoMove: true})
	srv.Deliver(imaptest.Message("in@x", "in", "a@example.com"), "INBOX")
	srv.Deliver(imaptest.Message("arch@x", "arch", "a@example.com"), "Archive")
	srv.Deliver(imaptest.Message("sent@x", "sent", "me@example.com"), "Sent")
	srv.Deliver(imaptest.Message("bin@x", "bin", "a@example.com"), "Trash")

	runSync(t, acc)
	got := labelsByMessageID(t)
	want(t, got, "in@x", "inbox")
	want(t, got, "arch@x")
	want(t, got, "sent@x", "sent")
	want(t, got, "bin@x", "trash")

	id := messageIDOf(t, "in@x")
	mustChange(t, id, store.LabelChange{Add: []string{"Projects"}})
	mustChange(t, id, store.MoveArchive)
	if _, err := store.SetMessageFlag([]string{id}, store.FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	r := runSync(t, acc)
	if r.PushFailed != 0 {
		t.Fatalf("push failed: %+v", r)
	}
	moved := srv.Find("in@x")
	if moved == nil {
		t.Fatal("the message is gone from the server")
	}
	if got := srv.LabelsOf(moved); strings.Join(got, ",") != "Archive" {
		t.Errorf("on the server the message is in %v, want Archive", got)
	}
	if !hasFlag(srv.FlagsOf(moved), `\Seen`) {
		t.Error("read did not reach the server")
	}
	// The label of the user's own stays here; the move is read back.
	want(t, labelsByMessageID(t), "in@x", "Projects")

	// Without MOVE the client copies and expunges that one UID — never the
	// whole folder.
	sawUIDExpunge := false
	for _, c := range srv.Commands() {
		if c == "EXPUNGE" {
			t.Errorf("the client expunged a whole folder")
		}
		if strings.HasPrefix(c, "UID EXPUNGE") {
			sawUIDExpunge = true
		}
	}
	if !sawUIDExpunge {
		t.Error("expected a UID EXPUNGE for the moved message")
	}
}

// A renumbered folder is read again, and nothing is stored twice.
func TestRenumberedFolderIsReadAgain(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	srv.Deliver(imaptest.Message("a@x", "a", "a@example.com"), imaptest.Inbox)
	srv.Deliver(imaptest.Message("b@x", "b", "a@example.com"), "Receipts")
	runSync(t, acc)

	srv.Renumber("[Gmail]/All Mail")
	r := runSync(t, acc)
	if r.New != 0 || r.Linked != 2 {
		t.Errorf("after renumbering: new=%d linked=%d, want 0 and 2", r.New, r.Linked)
	}
	got := labelsByMessageID(t)
	if len(got) != 2 {
		t.Errorf("%d stored, want 2", len(got))
	}
	want(t, got, "b@x", "Receipts")
}

// Deleting a label here deletes it on Gmail, not just from every message.
func TestDeletedLabelIsDeletedOnGmail(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	m := srv.Deliver(imaptest.Message("a@x", "a", "a@example.com"), imaptest.Inbox, "Old stuff")
	runSync(t, acc)

	if _, err := store.DeleteLabel("Old stuff"); err != nil {
		t.Fatal(err)
	}
	runSync(t, acc)
	if hasFlag(srv.LabelsOf(m), "Old stuff") {
		t.Errorf("the label is still on the message on Gmail: %q", srv.Commands())
	}
	for _, c := range srv.Commands() {
		if strings.HasPrefix(c, `DELETE "Old stuff"`) {
			return
		}
	}
	t.Errorf("the label itself was not deleted on Gmail: %v", srv.Commands())
}

// Mail sent from here is filed in a plain server's Sent folder, and the copy
// kept here is linked to it rather than stored twice.
func TestSentMailIsFiledAndLinked(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{})
	runSync(t, acc)

	raw := imaptest.Message("sent-here@x", "from InboxQL", "me@example.com")
	now := time.Now()
	local := &message.Message{ID: "local-copy", AccountID: acc.ID, MessageID: "<sent-here@x>", ContentHash: "local",
		From: "me@example.com", Subject: "from InboxQL", Labels: []string{store.LabelSent},
		Header: []byte(raw), Date: now, InternalDate: now}
	if err := store.SaveMessage(local); err != nil {
		t.Fatal(err)
	}
	if err := AppendSent(acc, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	m := srv.Find("sent-here@x")
	if m == nil || !hasFlag(srv.LabelsOf(m), "Sent") || !hasFlag(srv.FlagsOf(m), `\Seen`) {
		t.Fatalf("on the server: %v", m)
	}
	r := runSync(t, acc)
	if r.New != 0 {
		t.Errorf("the sent message was stored again (%d new)", r.New)
	}
	want(t, labelsByMessageID(t), "sent-here@x", "sent")
}
