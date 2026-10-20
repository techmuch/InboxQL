package store

import (
	"reflect"
	"testing"
)

// Trash and Spam stay out of a query unless it asks (query/binned.go). The
// folder fixture has one message in Spam (junk) and two in Trash (gone,
// gone-starred).

func TestQueriesLeaveOutTrashAndSpam(t *testing.T) {
	openFolderFixture(t)

	cases := []struct {
		q    string
		want []string
	}{
		{"from:old@x.com", []string{}},
		{"from:spam@x.com", []string{}},
		{"from:old@x.com folder:trash", []string{"gone"}},
		{"from:spam@x.com is:junk", []string{"junk"}},
		// Mentioning Trash brings back Trash, not Spam.
		{"is:deleted OR from:spam@x.com", []string{"gone", "gone-starred"}},
		{"folder:all from:old@x.com OR folder:all from:spam@x.com", []string{"gone", "junk"}},
		{"id:gone", []string{"gone"}},
	}
	for _, c := range cases {
		if got := queryIDs(t, c.q); !sameSet(got, c.want) {
			t.Errorf("%q = %v, want %v", c.q, got, c.want)
		}
	}

	// Everything, said with no words, is All Mail.
	if all, allMail := queryIDs(t, ""), queryIDs(t, "folder:allmail"); !sameSet(all, allMail) {
		t.Errorf("the empty query = %v, but folder:allmail = %v", all, allMail)
	}
}

// A saved query that mentions Trash brings it back where it is used.
func TestSavedQueryMentionsCount(t *testing.T) {
	openFolderFixture(t)
	if err := SaveQuery(&SavedQuery{Name: "binned", Title: "Binned", Query: "folder:trash"}); err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	if got := queryIDs(t, "saved:binned from:old@x.com"); !sameSet(got, []string{"gone"}) {
		t.Errorf("saved:binned from:old@x.com = %v, want [gone]", got)
	}
}

// A list says how much it left out, so the Desk can offer it.
func TestHiddenCountsWhatWasLeftOut(t *testing.T) {
	openFolderFixture(t)

	res, err := RunQuery("from:x.com", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hidden != 3 {
		t.Errorf("from:x.com hid %d, want 3 (junk, gone, gone-starred)", res.Hidden)
	}
	if !reflect.DeepEqual(res.LeftOut, []string{"trash", "spam"}) {
		t.Errorf("leftOut = %v", res.LeftOut)
	}

	res, err = RunQuery("folder:trash", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hidden != 0 {
		t.Errorf("folder:trash hid %d, want 0: nothing in Trash is also only in Spam", res.Hidden)
	}

	res, err = RunQuery("folder:all", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hidden != 0 || len(res.LeftOut) != 0 {
		t.Errorf("folder:all hid %d and left out %v", res.Hidden, res.LeftOut)
	}
}

// A conversation found by a query shows its messages outside Trash, and says
// how many it holds back.
func TestThreadsLeaveOutTrashedReplies(t *testing.T) {
	openFolderFixture(t)
	// i1 and gone become one conversation.
	if _, err := db.Exec("UPDATE messages SET thread_key = 'conv' WHERE id IN ('i1', 'gone')"); err != nil {
		t.Fatal(err)
	}

	res, err := RunQuery("from:alice@x.com | timeline", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(res.Threads))
	}
	th := res.Threads[0]
	if th.MessageCount != 1 || th.InTrash != 1 {
		t.Errorf("thread has %d messages and %d in Trash, want 1 and 1", th.MessageCount, th.InTrash)
	}
	for _, e := range th.Entries {
		if e.Message != nil && e.Message.ID == "gone" {
			t.Error("the trashed reply is listed in the thread")
		}
	}

	// Asked for, it is there.
	res, err = RunQuery("thread:conv folder:all | timeline", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Threads) != 1 || res.Threads[0].MessageCount != 2 {
		t.Errorf("folder:all should show the whole conversation: %+v", res.Threads)
	}

	// And `| thread` does not bring it back in through the conversation.
	if got := queryIDs(t, "from:alice@x.com | thread"); !sameSet(got, []string{"i1"}) {
		t.Errorf("from:alice@x.com | thread = %v, want [i1]", got)
	}
}

// A contact's counts agree with what a query about their mail would find.
func TestContactCountsLeaveOutTrashAndSpam(t *testing.T) {
	openFolderFixture(t)

	c, err := GetContact("old@x.com")
	if err != nil || c == nil {
		t.Fatalf("GetContact: %v %v", c, err)
	}
	if c.Messages != 0 || c.Sent != 0 || !c.LastSeen.IsZero() {
		t.Errorf("old@x.com, whose only mail is in Trash, counts %d messages, %d sent, last seen %v",
			c.Messages, c.Sent, c.LastSeen)
	}

	c, err = GetContact("alice@x.com")
	if err != nil || c == nil || c.Messages != 1 {
		t.Errorf("alice@x.com: %+v %v", c, err)
	}

	res, err := RunQuery("in:contacts messages>0 email:=old@x.com", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contacts) != 0 {
		t.Errorf("messages>0 matched a contact with nothing outside Trash")
	}

	r, err := GetContactResponsiveness("spam@x.com")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range r.HourlyDistribution {
		total += n
	}
	if total != 0 {
		t.Errorf("a sender whose only mail is Spam has %d messages in their active hours", total)
	}
}

// Files run through the same rule, over a join with its own `id` columns.
func TestFileQueriesCompileWithTheBins(t *testing.T) {
	openFolderFixture(t)
	for _, q := range []string{"in:attachments", "in:attachments | count", "in:attachments folder:trash"} {
		if _, err := RunQuery(q, 10, 0); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
}
