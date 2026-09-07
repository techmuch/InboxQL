package store

import "testing"

// A reply belongs to the conversation it answers. Until drafts carried a
// thread key, they were the one entity a timeline could not place.
func TestDraftJoinsTheConversationItAnswers(t *testing.T) {
	openQueryFixture(t)

	// m1's Message-ID is bracketed, as every server sends it, and the draft
	// stores the header verbatim. Both sides have to be normalised or the
	// join silently finds nothing.
	d := &Draft{
		ID: "d1", AccountID: "acct", InReplyTo: "<root@acme.com>",
		To: []string{"alice@acme.com"}, Subject: "Re: Quarterly invoice attached",
		Body: "On its way.",
	}
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	if d.ThreadKey != "root@acme.com" {
		t.Fatalf("draft landed in thread %q, want root@acme.com", d.ThreadKey)
	}

	// And it survives a round trip through the database.
	got, err := GetDraft("d1")
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if got.ThreadKey != "root@acme.com" {
		t.Errorf("reloaded draft is in thread %q, want root@acme.com", got.ThreadKey)
	}

	// The messages being replied to are in that same conversation, which is
	// the whole point: one key spans both entities.
	rows, err := db.Query("SELECT id, thread_key FROM messages WHERE thread_key = ?", got.ThreadKey)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		t.Fatalf("the draft's thread holds %v, want the root and its reply", ids)
	}
}

// A draft composed from scratch answers nothing, and inventing a conversation
// for it would put it in someone else's thread.
func TestDraftWithNoReplyHasNoThread(t *testing.T) {
	openQueryFixture(t)

	d := &Draft{ID: "d2", AccountID: "acct", To: []string{"alice@acme.com"}, Subject: "Hello"}
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if d.ThreadKey != "" {
		t.Errorf("a fresh draft was filed under thread %q", d.ThreadKey)
	}
}

// Replying to mail this instance never imported is normal — the draft is
// still a draft, it simply has no conversation to sit in.
func TestDraftReplyingToUnknownMailIsStillSaved(t *testing.T) {
	openQueryFixture(t)

	d := &Draft{ID: "d3", AccountID: "acct", InReplyTo: "<never-imported@elsewhere.test>",
		To: []string{"someone@elsewhere.test"}, Subject: "Re: something"}
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if d.ThreadKey != "" {
		t.Errorf("draft was filed under thread %q, want none", d.ThreadKey)
	}
}

// A draft can be retargeted while it is being composed. A key resolved only on
// insert would file the reply under the conversation it used to answer.
func TestRetargetingADraftMovesItsThread(t *testing.T) {
	openQueryFixture(t)

	d := &Draft{ID: "d4", AccountID: "acct", InReplyTo: "<root@acme.com>", Subject: "Re: invoice"}
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if d.ThreadKey != "root@acme.com" {
		t.Fatalf("first save put it in %q", d.ThreadKey)
	}

	d.InReplyTo = ""
	if err := SaveDraft(d); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if d.ThreadKey != "" {
		t.Errorf("retargeted draft still claims thread %q", d.ThreadKey)
	}
	got, _ := GetDraft("d4")
	if got.ThreadKey != "" {
		t.Errorf("stored draft still claims thread %q", got.ThreadKey)
	}
}
