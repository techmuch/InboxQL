package store

import (
	"strings"
	"testing"

	"github.com/user/inboxql/internal/account"
)

// seedDrafts puts a couple of unsent drafts alongside the message fixture.
func seedDrafts(t *testing.T) {
	t.Helper()
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com"}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	for _, d := range []*Draft{
		{ID: "d1", AccountID: "acct", To: []string{"alice@acme.com"},
			Subject: "Invoice question", Body: "About the invoice.",
			Status: DraftStatusDraft, Origin: OriginHuman},
		{ID: "d2", AccountID: "acct", To: []string{"bob@acme.com"},
			Subject: "Lunch", Body: "Friday?",
			Status: DraftStatusQueued, Origin: OriginAgent},
	} {
		if err := SaveDraft(d); err != nil {
			t.Fatalf("SaveDraft(%s): %v", d.ID, err)
		}
	}
}

func draftSubjects(t *testing.T, q string) []string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	if res.Kind != "drafts" {
		t.Fatalf("RunQuery(%q): kind = %q, want drafts", q, res.Kind)
	}
	out := make([]string, 0, len(res.Drafts))
	for _, d := range res.Drafts {
		out = append(out, d.Subject)
	}
	return out
}

// A draft is its own entity, reachable through the same language.
func TestDraftsAreQueryable(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	if got := draftSubjects(t, "in:drafts"); len(got) != 2 {
		t.Fatalf("in:drafts = %v, want 2", got)
	}
	if got := draftSubjects(t, "in:drafts origin:agent"); len(got) != 1 || got[0] != "Lunch" {
		t.Errorf("in:drafts origin:agent = %v", got)
	}
	if got := draftSubjects(t, "in:drafts status:queued"); len(got) != 1 || got[0] != "Lunch" {
		t.Errorf("in:drafts status:queued = %v", got)
	}
	if got := draftSubjects(t, "in:drafts to:alice"); len(got) != 1 {
		t.Errorf("in:drafts to:alice = %v", got)
	}
	if got := draftSubjects(t, "in:drafts subject:invoice"); len(got) != 1 {
		t.Errorf("in:drafts subject:invoice = %v", got)
	}
}

// Negation over a draft's recipients still means "no recipient is x", and
// still partitions the drafts.
func TestDraftNegationPartitions(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	total := len(draftSubjects(t, "in:drafts"))
	for _, p := range []string{"to:alice", "origin:agent", "status:queued", "subject:lunch"} {
		pos := len(draftSubjects(t, "in:drafts "+p))
		neg := len(draftSubjects(t, "in:drafts -("+p+")"))
		if pos+neg != total {
			t.Errorf("%s: %d + %d = %d, want %d", p, pos, neg, pos+neg, total)
		}
	}
}

// Drafts must not leak into a query about mail, and mail must not leak into a
// query about drafts. They are separate tables and separate questions.
func TestDraftsAndMailDoNotLeakIntoEachOther(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	// The message fixture has a message with "invoice" in the subject, and so
	// does the draft set. Each query sees only its own.
	res, err := RunQuery("subject:invoice", 100, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Kind != "messages" {
		t.Fatalf("a query with no entity term returned %q, want messages", res.Kind)
	}
	for _, m := range res.Messages {
		if m.ID == "d1" {
			t.Error("a draft appeared in a mail query")
		}
	}

	if got := draftSubjects(t, "in:drafts subject:invoice"); len(got) != 1 {
		t.Errorf("in:drafts subject:invoice = %v, want the draft only", got)
	}
}

// `folder:drafts` is the spelling the mailbox and AGENTS.md already use, so it
// has to keep working now that drafts are an entity rather than a folder.
func TestFolderDraftsIsSugarForInDrafts(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	viaFolder := draftSubjects(t, "folder:drafts")
	viaEntity := draftSubjects(t, "in:drafts")
	if len(viaFolder) != len(viaEntity) || len(viaFolder) != 2 {
		t.Errorf("folder:drafts = %v but in:drafts = %v", viaFolder, viaEntity)
	}
}

// status: belongs to both tickets and drafts. Without an entity term it means
// tickets, so every query written before drafts existed still means what it
// did; `in:drafts` reaches the other one.
func TestStatusStaysWithTicketsUnlessDraftsAreNamed(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	res, err := RunQuery("status:todo", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(status:todo): %v", err)
	}
	if res.Kind != "tickets" {
		t.Errorf("status:todo resolved to %q, want tickets", res.Kind)
	}

	res, err = RunQuery("in:drafts status:draft", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Kind != "drafts" {
		t.Errorf("in:drafts status:draft resolved to %q, want drafts", res.Kind)
	}

	// A draft status is not a ticket status, and the error says which values
	// are legal rather than returning nothing.
	if _, err := RunQuery("status:queued", 10, 0); err == nil {
		t.Error("status:queued was accepted as a ticket status")
	}
}

// A field can be declared and still be unanswerable: a draft has no flags, no
// attachments and no thread, because it has never been mail.
func TestDraftsRejectFieldsThatOnlyMailHas(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	for _, q := range []string{
		"in:drafts is:unread",
		"in:drafts has:attachment",
		"in:drafts label:invoice",
		"in:drafts from:alice",
	} {
		err := func() error { _, e := RunQuery(q, 10, 0); return e }()
		if err == nil {
			t.Errorf("RunQuery(%q) succeeded; drafts have no such property", q)
			continue
		}
		if !strings.Contains(err.Error(), "draft") {
			t.Errorf("RunQuery(%q) failed with %q, which does not explain why", q, err)
		}
	}
}

// A field that belongs to another entity should say so rather than reporting
// itself unknown.
func TestFieldsNameTheEntityTheyBelongTo(t *testing.T) {
	openQueryFixture(t)

	// origin: is a draft field; asking for it in a mail query should point at
	// drafts rather than claiming the field does not exist.
	res, err := RunQuery("origin:agent", 10, 0)
	if err != nil {
		t.Fatalf("origin:agent should resolve to drafts: %v", err)
	}
	if res.Kind != "drafts" {
		t.Errorf("origin:agent resolved to %q, want drafts", res.Kind)
	}
}

// in: says what a query is about; it is not a predicate and cannot be negated.
func TestEntitySelectorIsNotAPredicate(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	if _, err := RunQuery("-in:drafts", 10, 0); err == nil {
		t.Error("-in:drafts was accepted; it says what the query is about")
	}
	if _, err := RunQuery("in:nonsense", 10, 0); err == nil {
		t.Error("in:nonsense was accepted")
	}
}

// Drafts aggregate by their own fields.
func TestDraftAggregates(t *testing.T) {
	openQueryFixture(t)
	seedDrafts(t)

	res, err := RunQuery("in:drafts | count by status", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	if got["draft"] != 1 || got["queued"] != 1 {
		t.Errorf("count by status = %v", got)
	}

	res, err = RunQuery("in:drafts | count", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("in:drafts | count = %d, want 2", res.Total)
	}
}
