package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

func contactNames(t *testing.T, q string) map[string]string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	if res.Kind != "contacts" {
		t.Fatalf("RunQuery(%q): kind = %q, want contacts", q, res.Kind)
	}
	out := map[string]string{}
	for _, c := range res.Contacts {
		out[c.Address] = c.Name()
	}
	return out
}

// Every message creates a contact for every address it touches. The fixture
// writes messages through the normal path, so this is the real behaviour.
func TestEveryMessageCreatesContacts(t *testing.T) {
	openQueryFixture(t)

	got := contactNames(t, "in:contacts")
	for _, want := range []string{
		"alice@acme.com", "bob@acme.com", "me@example.com", "billing@stripe.com",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("no contact for %s: %v", want, got)
		}
	}

	// The display name from the header, which both parsers used to discard.
	if got["alice@acme.com"] != "Alice" {
		t.Errorf("alice is called %q, want the header's display name", got["alice@acme.com"])
	}
}

// Counted fields are derived from the participant edges, never cached — so
// they cannot drift away from the mailbox they describe.
func TestContactCountsAreDerived(t *testing.T) {
	openQueryFixture(t)

	res, err := RunQuery("in:contacts email:=alice@acme.com", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Contacts) != 1 {
		t.Fatalf("matched %d contacts", len(res.Contacts))
	}
	before := res.Contacts[0].Messages
	if before == 0 {
		t.Fatal("alice appears on no messages")
	}

	// Add a message and the count moves, with nothing recomputed by hand.
	m := &message.Message{
		ID: "extra", AccountID: "acct", ContentHash: "extra",
		From: "alice@acme.com", To: []string{"me@example.com"},
		Subject: "One more", Date: res.Contacts[0].LastSeen,
	}
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	res, _ = RunQuery("in:contacts email:=alice@acme.com", 10, 0)
	if res.Contacts[0].Messages != before+1 {
		t.Errorf("count is %d after adding a message, want %d",
			res.Contacts[0].Messages, before+1)
	}
}

// A human ruling outranks every rule, so a nightly classify cannot undo it.
func TestHumanRulingSurvivesReclassification(t *testing.T) {
	openQueryFixture(t)

	if err := SetContactKind("billing@stripe.com", KindOrganization, ContactFromHuman); err != nil {
		t.Fatalf("SetContactKind: %v", err)
	}
	if _, err := ClassifyContacts(false); err != nil {
		t.Fatalf("ClassifyContacts: %v", err)
	}

	c, err := GetContact("billing@stripe.com")
	if err != nil || c == nil {
		t.Fatalf("GetContact: %v, %v", c, err)
	}
	if c.Kind != KindOrganization {
		t.Errorf("a rule overwrote a human ruling: kind = %q", c.Kind)
	}
}

// The signal that misfired on a real archive, kept out on purpose.
func TestSendingWithoutReplyIsNotEvidenceOfASystem(t *testing.T) {
	openQueryFixture(t)

	// m4 is from noreply@news.example.org with no recipients at all — sends,
	// never receives. That shape is also the mailbox owner in a Sent archive,
	// so it must not be enough on its own. The local part is what catches this
	// one.
	if _, err := ClassifyContacts(false); err != nil {
		t.Fatalf("ClassifyContacts: %v", err)
	}
	c, _ := GetContact("noreply@news.example.org")
	if c == nil || c.Kind != KindSystem {
		t.Fatalf("noreply@ was not caught by the local-part rule: %+v", c)
	}
	if c.KindSource != ContactFromRule {
		t.Errorf("caught by %q, want the address rule", c.KindSource)
	}

	// dave sends and is never written to either, but says nothing automated.
	// Leaving him unknown is the honest answer.
	if d, _ := GetContact("notalice@acme.com"); d != nil && d.Kind == KindSystem {
		t.Error("an ordinary sender was classified as a system")
	}
}

// A forward carries the original's headers. Presence of List-Unsubscribe on
// one message out of many proves nothing about who forwarded it.
func TestOneForwardedNewsletterDoesNotMakeYouASystem(t *testing.T) {
	openQueryFixture(t)

	m := &message.Message{
		ID: "fwd1", AccountID: "acct", ContentHash: "fwd1",
		From: "alice@acme.com", To: []string{"me@example.com"},
		Subject: "Fwd: newsletter",
		Header:  []byte("From: alice@acme.com\r\nList-Unsubscribe: <https://x.test/u>"),
	}
	if err := SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	if _, err := ClassifyContacts(false); err != nil {
		t.Fatalf("ClassifyContacts: %v", err)
	}
	c, _ := GetContact("alice@acme.com")
	if c != nil && c.Kind == KindSystem {
		t.Error("forwarding one newsletter classified a person as a system")
	}
}

func TestContactNotesAndTags(t *testing.T) {
	openQueryFixture(t)

	// Set notes
	if err := SetContactNotes("alice@acme.com", "Met at conference; leading search team."); err != nil {
		t.Fatalf("SetContactNotes: %v", err)
	}

	c, err := GetContact("alice@acme.com")
	if err != nil || c == nil {
		t.Fatalf("GetContact: %v, %v", c, err)
	}
	if c.Notes != "Met at conference; leading search team." {
		t.Errorf("Notes = %q, want %q", c.Notes, "Met at conference; leading search team.")
	}

	// Add tags
	if err := AddContactLabel("alice@acme.com", "vip"); err != nil {
		t.Fatalf("AddContactLabel(vip): %v", err)
	}
	if err := AddContactLabel("alice@acme.com", "client"); err != nil {
		t.Fatalf("AddContactLabel(client): %v", err)
	}

	tags, err := GetContactLabels("alice@acme.com")
	if err != nil {
		t.Fatalf("GetContactLabels: %v", err)
	}
	if len(tags) != 2 || tags[0] != "client" || tags[1] != "vip" {
		t.Errorf("tags = %v, want [client vip]", tags)
	}

	// Reload contact and verify Tags populated
	c2, err := GetContact("alice@acme.com")
	if err != nil || c2 == nil {
		t.Fatalf("GetContact: %v", err)
	}
	if len(c2.Labels) != 2 || c2.Labels[0] != "client" || c2.Labels[1] != "vip" {
		t.Errorf("c2.Labels = %v, want [client vip]", c2.Labels)
	}

	// List all tags
	allTags, err := ListAllContactLabels()
	if err != nil {
		t.Fatalf("ListAllContactLabels: %v", err)
	}
	if len(allTags) != 2 {
		t.Errorf("allTags = %v, want 2 tags", allTags)
	}

	// Remove a tag
	if err := RemoveContactLabel("alice@acme.com", "vip"); err != nil {
		t.Fatalf("RemoveContactLabel: %v", err)
	}
	tagsAfter, _ := GetContactLabels("alice@acme.com")
	if len(tagsAfter) != 1 || tagsAfter[0] != "client" {
		t.Errorf("tagsAfter = %v, want [client]", tagsAfter)
	}

	// Query: in:contacts tag:client
	res, err := RunQuery("in:contacts tag:client", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts tag:client): %v", err)
	}
	if len(res.Contacts) != 1 || res.Contacts[0].Address != "alice@acme.com" {
		t.Fatalf("expected 1 contact alice@acme.com, got %v", res.Contacts)
	}
	if len(res.Contacts[0].Labels) != 1 || res.Contacts[0].Labels[0] != "client" {
		t.Errorf("expected tags populated on query result, got %v", res.Contacts[0].Labels)
	}

	// Query: in:contacts -tag:client
	resNeg, err := RunQuery("in:contacts -tag:client", 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts -tag:client): %v", err)
	}
	for _, c := range resNeg.Contacts {
		if c.Address == "alice@acme.com" {
			t.Errorf("alice should not match -tag:client")
		}
	}

	// Query: in:contacts has:notes
	resNotes, err := RunQuery("in:contacts has:notes", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts has:notes): %v", err)
	}
	if len(resNotes.Contacts) != 1 || resNotes.Contacts[0].Address != "alice@acme.com" {
		t.Fatalf("expected alice for has:notes, got %v", resNotes.Contacts)
	}

	// Query: in:contacts notes:conference
	resNotesText, err := RunQuery("in:contacts notes:conference", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts notes:conference): %v", err)
	}
	if len(resNotesText.Contacts) != 1 || resNotesText.Contacts[0].Address != "alice@acme.com" {
		t.Fatalf("expected alice for notes:conference, got %v", resNotesText.Contacts)
	}

	// Grouping: in:contacts | count by tag
	resGroup, err := RunQuery("in:contacts | count by tag", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts | count by tag): %v", err)
	}
	if len(resGroup.Groups) != 1 || resGroup.Groups[0].Label != "client" || resGroup.Groups[0].Value != 1 {
		t.Fatalf("expected 1 group 'client' with value 1, got %v", resGroup.Groups)
	}

	// Query: in:contacts awaiting:me
	resAwaiting, err := RunQuery("in:contacts awaiting:me", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts awaiting:me): %v", err)
	}
	if len(resAwaiting.Contacts) == 0 {
		t.Errorf("expected contacts awaiting me")
	}

	// Query: in:contacts has:awaiting
	resHasAwaiting, err := RunQuery("in:contacts has:awaiting", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery(in:contacts has:awaiting): %v", err)
	}
	if len(resHasAwaiting.Contacts) == 0 {
		t.Errorf("expected contacts with has:awaiting")
	}
}

// `label:` on contacts reads contact labels; without `in:` it still means a
// message label, which is what every query written before this meant by it.
func TestContactLabelsAreQueriedAsLabels(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
	if err := SaveContact(&Contact{Address: "alice@acme.com"}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := AddContactLabel("alice@acme.com", "vip"); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"in:contacts label:vip", "tag:vip", "in:contacts has:label"} {
		res, err := RunQuery(q, 50, 0)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if res.Kind != "contacts" || len(res.Contacts) != 1 {
			t.Errorf("%s: kind %q, %d contacts; want one contact", q, res.Kind, len(res.Contacts))
		}
	}

	res, err := RunQuery("label:vip", 50, 0)
	if err == nil && res.Kind == "contacts" {
		t.Error("`label:` without `in:` became a contact query; it must stay about mail")
	}
}

// Only unknown contacts that have sent something are judged: a recipient-only
// contact has no text of its own, and one already classified is not asked
// again — by a person especially.
func TestUnknownSendersAreOnlyTheOnesWithSomethingToRead(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i, m := range []*message.Message{
		{ID: "a1", From: "shop@x.com", To: []string{"me@example.com"}, Subject: "Your order has shipped"},
		{ID: "a2", From: "shop@x.com", To: []string{"me@example.com"}, Subject: "Your receipt"},
		{ID: "b1", From: "bob@x.com", To: []string{"carol@x.com"}, Subject: "Re: dinner"},
	} {
		m.AccountID, m.ContentHash, m.MessageID, m.Mailbox = "acct", m.ID, "<"+m.ID+"@x>", "INBOX"
		m.Date = base.Add(time.Duration(i) * time.Minute)
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetContactKind("bob@x.com", KindPerson, "human"); err != nil {
		t.Fatal(err)
	}

	got, err := UnknownSenders(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "shop@x.com" {
		t.Errorf("unknown senders %v; want only shop@x.com — carol never sent, bob is ruled", got)
	}

	subjects, _ := RecentSubjects("shop@x.com", 3)
	if len(subjects) != 2 || subjects[0] != "Your receipt" {
		t.Errorf("subjects %v, want newest first", subjects)
	}
}
