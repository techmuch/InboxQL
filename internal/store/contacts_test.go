package store

import (
	"testing"

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
	if err := AddContactTag("alice@acme.com", "vip"); err != nil {
		t.Fatalf("AddContactTag(vip): %v", err)
	}
	if err := AddContactTag("alice@acme.com", "client"); err != nil {
		t.Fatalf("AddContactTag(client): %v", err)
	}

	tags, err := GetContactTags("alice@acme.com")
	if err != nil {
		t.Fatalf("GetContactTags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "client" || tags[1] != "vip" {
		t.Errorf("tags = %v, want [client vip]", tags)
	}

	// Reload contact and verify Tags populated
	c2, err := GetContact("alice@acme.com")
	if err != nil || c2 == nil {
		t.Fatalf("GetContact: %v", err)
	}
	if len(c2.Tags) != 2 || c2.Tags[0] != "client" || c2.Tags[1] != "vip" {
		t.Errorf("c2.Tags = %v, want [client vip]", c2.Tags)
	}

	// List all tags
	allTags, err := ListAllTags()
	if err != nil {
		t.Fatalf("ListAllTags: %v", err)
	}
	if len(allTags) != 2 {
		t.Errorf("allTags = %v, want 2 tags", allTags)
	}

	// Remove a tag
	if err := RemoveContactTag("alice@acme.com", "vip"); err != nil {
		t.Fatalf("RemoveContactTag: %v", err)
	}
	tagsAfter, _ := GetContactTags("alice@acme.com")
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
	if len(res.Contacts[0].Tags) != 1 || res.Contacts[0].Tags[0] != "client" {
		t.Errorf("expected tags populated on query result, got %v", res.Contacts[0].Tags)
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

