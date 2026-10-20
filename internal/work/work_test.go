package work

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func openWorkFixture(t *testing.T) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)
	if err := store.SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-48 * time.Hour)
	for i, m := range []*message.Message{
		{ID: "hd", From: "Home Depot <homedepot@order.homedepot.com>", To: []string{"me@example.com"},
			Subject: "Your Electronic Receipt",
			HTMLBody: `<html><head><style>@charset "UTF-8"; body{margin:0}</style></head><body><table>` +
				`<tr><td>885911470902 FLEXVOLT 20-</td><td>&lt;A&gt;</td><td>269.00</td></tr>` +
				`<tr><td>TOTAL</td><td>287.83</td></tr></table></body></html>`},
		{ID: "ask", From: "Alice <alice@acme.com>", To: []string{"me@example.com"},
			Subject: "Contract", Body: "Can you send the signed contract back by Friday? Also, lunch on Oct 30 at noon at Pino's?"},
	} {
		m.AccountID, m.ContentHash, m.MessageID = "acct", m.ID, "<"+m.ID+"@x>"
		m.Header = []byte("Message-ID: <" + m.ID + "@x>\r\n")
		m.Date = base.Add(time.Duration(i) * time.Hour)
		if err := store.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}
	if _, _, _, err := elements.Install(nil, false); err != nil {
		t.Fatal(err)
	}
}

func itemFor(t *testing.T, b *Batch, newest string) *Item {
	t.Helper()
	for _, it := range b.Items {
		if it.Messages[len(it.Messages)-1].ID == newest {
			return it
		}
	}
	t.Fatalf("no item for %s", newest)
	return nil
}

func answers(t *testing.T, it *Item, a map[string]any) Submission {
	t.Helper()
	raw := map[string]json.RawMessage{}
	for k, v := range a {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		raw[k] = b
	}
	return Submission{Thread: it.Thread, Lease: it.Lease, Model: "test-model", Answers: raw}
}

// Everything the Home Depot receipt and Alice's note hold, answered once,
// becomes something a query finds.
func TestTheLoop(t *testing.T) {
	openWorkFixture(t)

	b, err := Next(Options{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(b.Items))
	}
	if len(b.Elements) != len(elements.Catalog) {
		t.Errorf("%d element definitions, want %d", len(b.Elements), len(elements.Catalog))
	}

	hd := itemFor(t, b, "hd")
	text := hd.Messages[0].Text
	if strings.Contains(text, "@charset") || !strings.Contains(text, "885911470902 FLEXVOLT 20- <A> 269.00") {
		t.Errorf("the receipt reads badly:\n%s", text)
	}
	// Taken: a second call does not hand the same conversations out.
	if again, _ := Next(Options{Limit: 10}); len(again.Items) != 0 {
		t.Errorf("leased conversations were handed out again: %d", len(again.Items))
	}

	none := map[string]string{"none": "not here"}
	res, err := Submit(answers(t, hd, map[string]any{
		"receipts": map[string]any{"hd": map[string]any{"records": []any{map[string]any{
			"merchant": "Home Depot", "total": "$287.83", "quote": "885911470902 FLEXVOLT 20-",
			"items": []any{map[string]any{"name": "FLEXVOLT 20V/60V battery", "brand": "DeWalt",
				"sku": "885911470902", "price": 269}}}}}},
		"travel":     map[string]any{"hd": none},
		"events":     map[string]any{"hd": none},
		"tasks":      none,
		"waiting":    map[string]any{"level": "no-one", "quote": "TOTAL"},
		"importance": map[string]any{"level": "low", "quote": "Your Electronic Receipt"},
		"sender":     map[string]any{"homedepot@order.homedepot.com": map[string]any{"level": "system"}},
		"topics":     map[string]any{"tags": []string{"Home Depot", "tools"}},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("refused: %+v", res.Problems)
	}

	ask := itemFor(t, b, "ask")
	res, err = Submit(answers(t, ask, map[string]any{
		"receipts": map[string]any{"ask": none},
		"travel":   map[string]any{"ask": none},
		"events": map[string]any{"ask": map[string]any{"records": []any{map[string]any{
			"title": "Lunch with Alice", "start": "2026-10-30T12:00", "location": "Pino's",
			"quote": "lunch on Oct 30 at noon at Pino's"}}}},
		"tasks": map[string]any{"records": []any{map[string]any{
			"what": "Send Alice the signed contract", "owner": "me", "due": "2026-10-24",
			"quote": "Can you send the signed contract back by Friday?"}}},
		"waiting":    map[string]any{"level": "on-me", "confidence": 0.9, "quote": "Can you send the signed contract back"},
		"importance": map[string]any{"level": "important", "quote": "by Friday"},
		"sender":     map[string]any{"alice@acme.com": map[string]any{"level": "person"}},
		"topics":     map[string]any{"tags": []string{"contract"}},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || res.Tickets != 1 || res.Events != 1 || res.Contacts != 1 {
		t.Fatalf("result %+v", res)
	}

	for q, want := range map[string]int{
		"extract:receipts.items.brand=DeWalt": 1,
		"extract:receipts.items.brand=Ryobi":  0,
		"extract:receipts.total>200":          1,
		"awaiting:me":                         1,
		"label:importance":                    2,
		"topic:tools":                         1,
		"extract:importance.level=important":  1,
	} {
		r, err := store.RunQuery(q, 10, 0)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if len(r.Messages) != want {
			t.Errorf("%s matched %d, want %d", q, len(r.Messages), want)
		}
	}
	if c, _ := store.GetContact("alice@acme.com"); c == nil || c.Kind != store.KindPerson || c.KindSource != store.ContactFromAgent {
		t.Errorf("alice = %+v", c)
	}
	r, _ := store.RunQuery("in:contacts awaiting:me", 10, 0)
	if len(r.Contacts) != 1 || r.Contacts[0].Address != "alice@acme.com" {
		t.Errorf("in:contacts awaiting:me = %+v", r.Contacts)
	}
	tickets, _ := store.RunQuery("status:proposed", 10, 0)
	if len(tickets.Tickets) != 1 || tickets.Tickets[0].Title != "Send Alice the signed contract" {
		t.Errorf("proposed tickets: %+v", tickets.Tickets)
	}

	// Answered: nothing left to read. A clear makes it pending again.
	if again, _ := Next(Options{Limit: 10}); len(again.Items) != 0 {
		t.Errorf("answered conversations offered again: %d", len(again.Items))
	}
	if _, err := store.ClearAgentAnswers("test-model", time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := Next(Options{Limit: 10}); len(again.Items) != 2 {
		t.Errorf("after clearing, %d conversations pending, want 2", len(again.Items))
	}
}

// Every way an answer can be wrong is refused, nothing is stored, and the
// problems say what to fix.
func TestRefusals(t *testing.T) {
	openWorkFixture(t)
	b, err := Next(Options{Limit: 10, Only: []string{"waiting", "receipts"}})
	if err != nil {
		t.Fatal(err)
	}
	ask := itemFor(t, b, "ask")

	cases := []struct {
		name    string
		answers map[string]any
		want    string
	}{
		{"made-up quote", map[string]any{
			"waiting":  map[string]any{"level": "on-me", "quote": "please reply urgently"},
			"receipts": map[string]any{"ask": map[string]string{"none": "no"}},
		}, "is not in the conversation"},
		{"invented level", map[string]any{
			"waiting":  map[string]any{"level": "maybe", "quote": "by Friday"},
			"receipts": map[string]any{"ask": map[string]string{"none": "no"}},
		}, "is not a level"},
		{"missing element", map[string]any{
			"waiting": map[string]any{"level": "on-me", "quote": "by Friday"},
		}, "not answered"},
		{"unknown field", map[string]any{
			"waiting": map[string]any{"level": "on-me", "quote": "by Friday"},
			"receipts": map[string]any{"ask": map[string]any{"records": []any{map[string]any{
				"merchant": "Acme", "colour": "red", "quote": "signed contract"}}}},
		}, `"colour" is not a field`},
		{"wrong type", map[string]any{
			"waiting": map[string]any{"level": "on-me", "quote": "by Friday"},
			"receipts": map[string]any{"ask": map[string]any{"records": []any{map[string]any{
				"merchant": "Acme", "total": "a lot", "quote": "signed contract"}}}},
		}, "is a number"},
		{"not asked", map[string]any{
			"waiting":    map[string]any{"level": "on-me", "quote": "by Friday"},
			"receipts":   map[string]any{"ask": map[string]string{"none": "no"}},
			"importance": map[string]any{"level": "low", "quote": "by Friday"},
		}, "not asked"},
		{"two answers in one", map[string]any{
			"waiting":  map[string]any{"level": "on-me", "none": "x", "quote": "by Friday"},
			"receipts": map[string]any{"ask": map[string]string{"none": "no"}},
		}, "exactly one of"},
	}
	for _, c := range cases {
		res, err := Submit(answers(t, ask, c.answers), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.OK() {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		found := false
		for _, p := range res.Problems {
			found = found || strings.Contains(p.Problem, c.want)
		}
		if !found {
			t.Errorf("%s: problems %+v do not mention %q", c.name, res.Problems, c.want)
		}
	}
	if r, _ := store.RunQuery("awaiting:me", 10, 0); len(r.Messages) != 0 {
		t.Error("a refused submission stored something")
	}

	// A wrong lease is refused; the right one, still held, is accepted.
	bad := answers(t, ask, map[string]any{
		"waiting":  map[string]any{"level": "on-me", "quote": "by Friday"},
		"receipts": map[string]any{"ask": map[string]string{"none": "no"}},
	})
	good := bad
	bad.Lease = "someone-else"
	if res, _ := Submit(bad, nil); res.OK() {
		t.Error("a wrong lease was accepted")
	}
	if res, _ := Submit(good, nil); !res.OK() {
		t.Errorf("a sound answer was refused: %+v", res.Problems)
	}
	// Submitted once; the lease is gone.
	if res, _ := Submit(good, nil); res.OK() {
		t.Error("the same answer was accepted twice")
	}
}

// Instructions that change after a conversation was taken refuse the answer:
// it answered a different question.
func TestChangedInstructionsRefuseAnswers(t *testing.T) {
	openWorkFixture(t)
	b, err := Next(Options{Limit: 10, Only: []string{"waiting"}})
	if err != nil {
		t.Fatal(err)
	}
	ask := itemFor(t, b, "ask")
	a, _ := store.GetAnnotator("waiting")
	a.Instructions += " Be strict."
	if err := store.SaveAnnotator(a); err != nil {
		t.Fatal(err)
	}
	res, err := Submit(answers(t, ask, map[string]any{"waiting": map[string]any{"level": "on-me", "quote": "by Friday"}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || !strings.Contains(res.Problems[0].Problem, "instructions changed") {
		t.Errorf("result %+v", res)
	}
}
