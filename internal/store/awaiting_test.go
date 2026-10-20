package store

import (
	"sort"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// One meaning for "waiting" (query/awaiting.go): the rail's queries and the
// contact card answer from the same SQL.

func openAwaitingFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	hdr := func(id string, refs ...string) []byte {
		h := "Message-ID: <" + id + "@x>\r\n"
		if len(refs) > 0 {
			h += "In-Reply-To: <" + refs[len(refs)-1] + "@x>\r\nReferences:"
			for _, r := range refs {
				h += " <" + r + "@x>"
			}
			h += "\r\n"
		}
		return []byte(h)
	}
	msgs := []*message.Message{
		// Alice asks; nobody has answered.
		{ID: "ask", From: "Alice <alice@acme.com>", To: []string{"me@example.com"}, Subject: "Contract", Body: "Can you send it?", Header: hdr("ask")},
		// I wrote to Alice; she has not answered.
		{ID: "mine", From: "Me <me@example.com>", To: []string{"alice@acme.com"}, Subject: "Design", Body: "Thoughts?", Header: hdr("mine")},
		// Alice wrote, I answered. On who sent last alone that waits on her —
		// which is what the loops label is for.
		{ID: "q", From: "Alice <alice@acme.com>", To: []string{"me@example.com"}, Subject: "Lunch", Body: "Lunch?", Header: hdr("q")},
		{ID: "a", From: "Me <me@example.com>", To: []string{"alice@acme.com"}, Subject: "Re: Lunch", Body: "Yes", Header: hdr("a", "q")},
		// A newsletter: newest in its conversation and not mine.
		{ID: "news", From: "News <news@lists.example>", To: []string{"me@example.com"}, Subject: "Weekly", Body: "Issue 12", Header: hdr("news")},
		// Spam in someone's name.
		{ID: "junk", From: "Prize <prize@spam.example>", To: []string{"me@example.com"}, Subject: "You won", Body: "Claim", Header: hdr("junk"), Mailbox: "Junk", Flags: []string{`\Junk`}},
	}
	for i, m := range msgs {
		m.AccountID, m.ContentHash = "acct", m.ID
		m.MessageID = "<" + m.ID + "@x>"
		m.Date = base.Add(time.Duration(i) * time.Hour)
		if err := SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}
	if _, err := db.Exec("UPDATE contacts SET kind = 'system' WHERE address = 'news@lists.example'"); err != nil {
		t.Fatal(err)
	}
}

func contactAddrs(t *testing.T, q string) []string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	out := []string{}
	for _, c := range res.Contacts {
		out = append(out, c.Address)
	}
	sort.Strings(out)
	return out
}

// judgeWaiting records the waiting element's answer on a message, as a
// harness would through `iql work submit`.
func judgeWaiting(t *testing.T, messageID, level string) {
	t.Helper()
	a, err := GetAnnotator(WaitingAnnotator)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		a = &Annotator{Name: WaitingAnnotator, Kind: KindLabel, Engine: EngineAgent,
			Instructions: "who is waiting", SchemaJSON: `{"element":true,"unit":"thread","shape":"levels",` +
				`"levels":[{"label":"on-me"},{"label":"on-them"},{"label":"no-one"}]}`}
		if err := SaveAnnotator(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveAnnotations(a.ID, a.Version, messageID, []*Annotation{{Status: StatusOK, Source: SourceAgent,
		Model: "test-model", DataJSON: `{"level":"` + level + `"}`}}); err != nil {
		t.Fatal(err)
	}
}

// Nothing judged, nothing waiting: who sent last is not a judgement.
func TestNothingWaitsUntilJudged(t *testing.T) {
	openAwaitingFixture(t)
	if got := contactAddrs(t, "in:contacts awaiting:me"); len(got) != 0 {
		t.Errorf("in:contacts awaiting:me = %v before anything was judged", got)
	}
	if got := queryIDs(t, "awaiting:me"); len(got) != 0 {
		t.Errorf("awaiting:me = %v before anything was judged", got)
	}
}

func TestAwaitingAgreesWithTheCard(t *testing.T) {
	openAwaitingFixture(t)
	judgeWaiting(t, "ask", "on-me")
	judgeWaiting(t, "news", "no-one")
	// Alice asked, I answered — and promised more: on me, though I wrote last.
	judgeWaiting(t, "a", "on-me")
	judgeWaiting(t, "mine", "on-them")
	// The junk is in Spam; even judged, it is left out.
	judgeWaiting(t, "junk", "on-me")

	if got := contactAddrs(t, "in:contacts awaiting:me"); !sameSet(got, []string{"alice@acme.com"}) {
		t.Errorf("in:contacts awaiting:me = %v", got)
	}
	if got := contactAddrs(t, "in:contacts awaiting:them"); !sameSet(got, []string{"alice@acme.com"}) {
		t.Errorf("in:contacts awaiting:them = %v", got)
	}
	if got := queryIDs(t, "awaiting:me"); !sameSet(got, []string{"a", "ask"}) {
		t.Errorf("awaiting:me = %v, want [a ask]", got)
	}
	if got := queryIDs(t, "awaiting:them"); !sameSet(got, []string{"mine"}) {
		t.Errorf("awaiting:them = %v, want [mine]", got)
	}

	r, err := GetContactResponsiveness("alice@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	mine := map[string]bool{}
	for _, p := range r.AwaitingMyReplyThreads {
		mine[p.LastMessageID] = true
	}
	if len(mine) != 2 || !mine["ask"] || !mine["a"] {
		t.Errorf("card, awaiting my reply: %+v", r.AwaitingMyReplyThreads)
	}
	if len(r.AwaitingTheirReplyThreads) != 1 || r.AwaitingTheirReplyThreads[0].LastMessageID != "mine" {
		t.Errorf("card, awaiting their reply: %+v", r.AwaitingTheirReplyThreads)
	}
	if !r.LoopsJudged {
		t.Error("the card does not know the waiting element is there")
	}

	res, err := RunQuery("awaiting:me | timeline", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Threads) != 2 {
		t.Errorf("awaiting:me | timeline = %d conversations, want 2", len(res.Threads))
	}
}

// Your ruling outranks the harness's answer.
func TestARulingDecidesWhoIsWaiting(t *testing.T) {
	openAwaitingFixture(t)
	judgeWaiting(t, "ask", "on-me")
	if err := RecordRuling(WaitingAnnotator, "ask", Ruling{Matched: true, Level: "no-one", Via: RuledInflow}); err != nil {
		t.Fatal(err)
	}
	if got := contactAddrs(t, "in:contacts awaiting:me"); len(got) != 0 {
		t.Errorf("Waiting on me = %v after the user ruled nobody is waiting", got)
	}
	r, err := GetContactResponsiveness("alice@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.AwaitingMyReplyThreads) != 0 || r.ClosedByJudgement != 1 {
		t.Errorf("card: %d waiting, %d closed; want 0 and 1", len(r.AwaitingMyReplyThreads), r.ClosedByJudgement)
	}
}

// v44 updates the two rail entries whose default text changed, and only where
// they still hold the text they were installed with.
func TestRailEntriesAreUpgradedUnlessEdited(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDB(dir); err != nil {
		t.Fatal(err)
	}
	for _, q := range []*SavedQuery{
		{Name: "awaiting-me", Title: "Waiting on me", Query: "in:contacts awaiting:me"},
		{Name: "unread", Title: "Unread", Query: "is:unread -folder:spam -folder:trash account:work"},
	} {
		if err := SaveQuery(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 43"); err != nil {
		t.Fatal(err)
	}
	CloseDB()
	if _, err := InitDB(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)

	if q, _ := GetSavedQuery("awaiting-me"); q == nil || q.Query != "in:contacts awaiting:me -kind:system" {
		t.Errorf("awaiting-me = %+v", q)
	}
	if q, _ := GetSavedQuery("unread"); q == nil || q.Query != "is:unread -folder:spam -folder:trash account:work" {
		t.Errorf("an edited entry was rewritten: %+v", q)
	}
}
