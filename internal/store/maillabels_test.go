package store

import (
	"strings"
	"testing"
)

// One `label:`, two kinds of label.
//
// A mail label is two-valued and an annotator's is three-valued. When a name
// is both, `label:x` is either saying yes and `-label:x` is neither: no mail
// label, and the annotator looked and said no. That keeps the three-way
// partition honest for the annotator while letting a person's label count.
func TestLabelAnswersForBothKinds(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "acme", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@acme.com",
	})
	// m4 and m5 stay unevaluated.
	if _, _, err := ApplyRule(a, "before:2026-03-04"); err != nil {
		t.Fatalf("ApplyRule: %v", err)
	}
	eq(t, ids(t, "label:acme"), "m1", "m2")
	eq(t, ids(t, "-label:acme"), "m3")

	// A person puts the same name on m3 (which the annotator said no to) and
	// on m4 (which it never saw).
	if _, err := ChangeLabels([]string{"m3", "m4"}, LabelChange{Add: []string{"acme"}}); err != nil {
		t.Fatal(err)
	}
	eq(t, ids(t, "label:acme"), "m1", "m2", "m3", "m4")
	eq(t, ids(t, "-label:acme")) // m3 is labelled now; nothing else is a known no
	eq(t, ids(t, "unlabeled:acme"), "m4", "m5")

	// A mail label nobody annotates is plain yes or no.
	if _, err := ChangeLabels([]string{"m1"}, LabelChange{Add: []string{"Receipts"}}); err != nil {
		t.Fatal(err)
	}
	eq(t, ids(t, "label:receipts"), "m1")
	if n := countOf(t, "-label:Receipts"); n != countOf(t, "")-1 {
		t.Errorf("-label:Receipts = %d, want every message but one", n)
	}
	eq(t, ids(t, "label:Rec*"), "m1")

	// A name that is neither is an error, not an empty answer.
	if _, err := RunQuery("label:nonesuch", 10, 0); err == nil || !strings.Contains(err.Error(), "no label or annotator") {
		t.Errorf("label:nonesuch = %v, want an error naming both kinds", err)
	}

	// has:label counts the user's own labels and annotator yeses, never the
	// system labels every message carries.
	if _, err := ChangeLabels([]string{"m5"}, LabelChange{Add: []string{LabelInbox}}); err != nil {
		t.Fatal(err)
	}
	eq(t, ids(t, "has:label"), "m1", "m2", "m3", "m4")

	// Grouping counts both kinds.
	res, err := RunQuery("| count by label", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	if got["acme"] != 4 || got["Receipts"] != 1 {
		t.Errorf("count by label = %v, want acme 4 and Receipts 1", got)
	}
}

// The moves a mail client offers are label changes, and archive is only
// taking Inbox off.
func TestMovesAreLabelChanges(t *testing.T) {
	openFolderFixture(t)

	if _, err := ChangeLabels([]string{"i1"}, MoveArchive); err != nil {
		t.Fatal(err)
	}
	if folderMembers(t, FolderInbox)["i1"] || !folderMembers(t, FolderArchive)["i1"] {
		t.Error("archive did not take i1 out of the inbox and into the archive")
	}
	if _, err := ChangeLabels([]string{"i1"}, MoveTrash); err != nil {
		t.Fatal(err)
	}
	if !folderMembers(t, FolderTrash)["i1"] || folderMembers(t, FolderAllMail)["i1"] {
		t.Error("trash did not bin i1")
	}
	if _, err := ChangeLabels([]string{"i1"}, MoveUntrash); err != nil {
		t.Fatal(err)
	}
	if !folderMembers(t, FolderInbox)["i1"] {
		t.Error("restoring from trash did not put i1 back in the inbox")
	}
	if _, err := DeleteLabel(LabelInbox); err == nil {
		t.Error("a system label was deleted")
	}
}

// A change to a message on an account with a server is queued for it; an
// account with nowhere to send it queues nothing.
func TestChangesQueueOnlyForServerAccounts(t *testing.T) {
	openFolderFixture(t) // the fixture account has no host

	if _, err := ChangeLabels([]string{"i1"}, MoveArchive); err != nil {
		t.Fatal(err)
	}
	if p, _ := PendingChanges("acct", 0); len(p) != 0 {
		t.Fatalf("an account with no server queued %d changes", len(p))
	}

	if _, err := db.Exec(`UPDATE accounts SET host = 'imap.example.com' WHERE id = 'acct'`); err != nil {
		t.Fatal(err)
	}
	var woke []string
	PendingNotify = func(a string) { woke = append(woke, a) }
	defer func() { PendingNotify = nil }()

	if _, err := ChangeLabels([]string{"i2"}, MoveArchive); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMessageFlag([]string{"i2"}, FlagFlagged, true); err != nil {
		t.Fatal(err)
	}
	p, _ := PendingChanges("acct", 0)
	if len(p) != 2 || p[0].Op != OpLabelRemove || p[0].Value != LabelInbox || p[1].Op != OpFlagAdd {
		t.Fatalf("queued %+v, want inbox removed then \\Flagged added", p)
	}
	if len(woke) != 2 {
		t.Errorf("the worker was woken %d times, want 2", len(woke))
	}
	labels, flags, _ := PendingLabels("i2")
	if !labels[LabelInbox] || !flags[FlagFlagged] {
		t.Errorf("pending for i2 = %v %v", labels, flags)
	}
}

func TestLabelForMailbox(t *testing.T) {
	cases := map[string]string{
		"INBOX":             LabelInbox,
		"[Gmail]/Sent Mail": LabelSent,
		"[Gmail]/All Mail":  "",
		"[Gmail]/Spam":      LabelSpam,
		"[Gmail]/Trash":     LabelTrash,
		"[Gmail]/Important": LabelImportant,
		"8D7EBE5F-1B91-4FAF-B206-5A54AE9C418B/[Gmail].mbox/All Mail.mbox": "",
		"9434D814-160E-4E72-B62A-F3C59DB6649A/Archive.mbox":               "",
		"9434D814-160E-4E72-B62A-F3C59DB6649A/INBOX.mbox":                 LabelInbox,
		"Work.mbox/Clients.mbox":                                          "Work/Clients",
		"[Gmail]/Receipts":                                                "Receipts",
		"Deleted Messages":                                                LabelTrash,
		"Junk E-mail":                                                     LabelSpam,
		"Sent Items":                                                      LabelSent,
		"Archived invoices":                                               "Archived invoices",
	}
	for in, want := range cases {
		if got, ok := LabelForMailbox(in, ""); !ok || got != want {
			t.Errorf("LabelForMailbox(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if got, _ := LabelForMailbox("Papierkorb", `\Trash`); got != LabelTrash {
		t.Errorf("special-use should win over the name, got %q", got)
	}
	if _, ok := LabelForMailbox("", ""); ok {
		t.Error("an empty folder name should report no folder")
	}
}

// The v43 migration labels mail that was stored before labels existed. It
// must reach the same answer saving a message does today.
func TestBackfillMatchesLabelsOnSave(t *testing.T) {
	openFolderFixture(t)

	ids := []string{"i1", "i2", "star", "sent-by-mailbox", "sent-by-sender", "junk", "gone",
		"gone-starred", "filed", "allmail", "named", "sent-then-filed"}
	before, err := MailLabels(ids)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM message_labels`); err != nil {
		t.Fatal(err)
	}
	if err := backfillLabels(db); err != nil {
		t.Fatal(err)
	}
	after, err := MailLabels(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !sameSet(before[id], after[id]) {
			t.Errorf("%s: saved with %v, backfilled to %v", id, before[id], after[id])
		}
	}
	// Running it again changes nothing.
	if err := backfillLabels(db); err != nil {
		t.Fatal(err)
	}
	again, _ := MailLabels(ids)
	for _, id := range ids {
		if !sameSet(after[id], again[id]) {
			t.Errorf("%s: a second backfill changed %v to %v", id, after[id], again[id])
		}
	}
}
