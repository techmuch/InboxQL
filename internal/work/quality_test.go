package work

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// fillBlanks answers whatever the template left null, plausibly, so a test
// can submit a template the way a harness would.
func fillBlanks(t *testing.T, it *Item, s *Submission) {
	t.Helper()
	none := Answer{None: "not here"}
	for _, a := range it.Ask {
		if string(s.Answers[a.Element]) != "null" {
			continue
		}
		var v any
		switch {
		case a.Unit == "thread" && (a.Element == "tasks"):
			v = none
		case a.Unit == "thread":
			v = Answer{Level: "normal", Quote: it.Subject}
		case a.Unit == "sender":
			m := map[string]Answer{}
			for _, sd := range a.Senders {
				m[sd] = Answer{Level: "person"}
			}
			v = m
		default:
			m := map[string]Answer{}
			for _, id := range a.Messages {
				m[id] = none
			}
			v = m
		}
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		s.Answers[a.Element] = raw
	}
}

func templateFor(t *testing.T, subs []Submission, thread string) Submission {
	t.Helper()
	for _, s := range subs {
		if s.Thread == thread {
			return s
		}
	}
	t.Fatalf("no template for %s", thread)
	return Submission{}
}

// An answer that went back exactly as the template guessed it is stored as
// such; a deep read asks it again, and guesses nothing.
func TestUntouchedGuessesAreMarkedAndReadAgainDeep(t *testing.T) {
	openWorkFixture(t)
	b, err := Next(Options{Limit: 10, Model: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == "" {
		t.Fatal("a batch with items has no id")
	}
	subs := Template(b, "fast")
	hd := itemFor(t, b, "hd")
	sub := templateFor(t, subs, hd.Thread)
	// "Your Electronic Receipt" looks like a receipt: not guessed none.
	if string(sub.Answers["receipts"]) != "null" {
		t.Fatalf("receipts was guessed on a receipt: %s", sub.Answers["receipts"])
	}
	sub.Answers["receipts"], _ = json.Marshal(map[string]Answer{"hd": {Records: []map[string]any{{
		"merchant": "Home Depot", "total": 287.83, "quote": "885911470902 FLEXVOLT 20-"}}}})
	fillBlanks(t, hd, &sub)
	sub.Usage = &store.WorkUsage{InputTokens: 9000, OutputTokens: 1000, CostUSD: 0.05}
	res, err := Submit(sub, nil)
	if err != nil || !res.OK() {
		t.Fatalf("submit: %v %+v", err, res)
	}
	if res.Prefilled == 0 {
		t.Fatalf("untouched guesses were not marked: %+v", res)
	}
	rows, err := store.AgentAnswersOn(hd.Thread)
	if err != nil {
		t.Fatal(err)
	}
	guessed := map[string]bool{}
	for _, r := range rows {
		if r.Prefilled {
			guessed[r.Element] = true
		}
	}
	if guessed["receipts"] {
		t.Error("receipts was answered by the model, yet stored as a guess")
	}
	if !guessed["travel"] {
		t.Errorf("travel went back as guessed but is not marked: %v", guessed)
	}

	// A normal read leaves it alone; a deep read asks exactly the guesses.
	again, err := Next(Options{Limit: 10, Query: "from:homedepot"})
	if err != nil || len(again.Items) != 0 {
		t.Fatalf("a normal read asked an answered conversation again: %v %d", err, len(again.Items))
	}
	deep, err := Next(Options{Limit: 10, Deep: true, Query: "from:homedepot"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deep.Items) != 1 {
		t.Fatalf("deep read found %d conversations, want 1", len(deep.Items))
	}
	for _, a := range deep.Items[0].Ask {
		if a.Element == "receipts" {
			t.Error("deep read asked receipts, which the model answered")
		}
	}
	for el, raw := range Template(deep, "deep")[0].Answers {
		if string(raw) != "null" {
			t.Errorf("a deep read's template guessed %s: %s", el, raw)
		}
	}

	// Efficiency counted it, with what it cost.
	m, err := Measure(time.Now().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Reading.Stored != 1 || m.Reading.Usage == nil || m.Reading.Usage.Tokens != 10000 {
		t.Errorf("reading measures: stored=%d usage=%+v", m.Reading.Stored, m.Reading.Usage)
	}
	if m.Reading.GuessedShare <= 0 || m.Reading.GuessedShare >= 1 {
		t.Errorf("guessed share = %v", m.Reading.GuessedShare)
	}
}

// A message element answered for some of its messages is refused, not
// completed with "none": that silently lost what the others held.
func TestPartAnsweredMessagesAreRefused(t *testing.T) {
	openWorkFixture(t)
	reply := &message.Message{ID: "hd2", AccountID: "acct", ContentHash: "hd2", MessageID: "<hd2@x>",
		From: "Home Depot <homedepot@order.homedepot.com>", To: []string{"me@example.com"},
		Subject: "Re: Your Electronic Receipt", Body: "Your second receipt: TOTAL 12.00",
		Header: []byte("Message-ID: <hd2@x>\r\nIn-Reply-To: <hd@x>\r\nReferences: <hd@x>\r\n"),
		Date:   time.Now().Add(-47 * time.Hour)}
	if err := store.SaveMessage(reply); err != nil {
		t.Fatal(err)
	}
	b, err := Next(Options{Limit: 10, Query: "from:homedepot"})
	if err != nil || len(b.Items) != 1 {
		t.Fatalf("Next: %v, %d items", err, len(b.Items))
	}
	it := b.Items[0]
	sub := templateFor(t, Template(b, "m"), it.Thread)
	fillBlanks(t, it, &sub)
	sub.Answers["receipts"], _ = json.Marshal(map[string]Answer{"hd": {None: "the first is not one"}})
	res, err := Submit(sub, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("receipts answered for one of two messages was accepted")
	}
	found := false
	for _, p := range res.Problems {
		if p.Element == "receipts" && p.Target == "hd2" && strings.Contains(p.Problem, "not answered") {
			found = true
		}
	}
	if !found {
		t.Errorf("the unanswered message was not named: %+v", res.Problems)
	}
	m, err := Measure(time.Now().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Reading.Refused != 1 || len(m.Reading.Problems) == 0 || !strings.Contains(m.Reading.Problems[0].Problem, "not answered") {
		t.Errorf("the refusal was not counted with its reason: refused=%d problems=%+v", m.Reading.Refused, m.Reading.Problems)
	}
}

// A label the loop added and you took off is a correction, counted.
func TestLabelYouTakeOffCountsAgainstTheLoop(t *testing.T) {
	openWorkFixture(t)
	b, err := Next(Options{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ask := itemFor(t, b, "ask")
	sub := templateFor(t, Template(b, "m"), ask.Thread)
	fillBlanks(t, ask, &sub)
	sub.Labels = []string{"contracts"}
	if res, err := Submit(sub, nil); err != nil || !res.OK() {
		t.Fatalf("submit: %v %+v", err, res)
	}
	if _, err := store.ChangeLabels([]string{"ask"}, store.LabelChange{Remove: []string{"contracts"}}); err != nil {
		t.Fatal(err)
	}
	out, err := store.WorkActionOutcomes(time.Now().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := out[store.WorkActionLabel]; got.Total != 1 || got.Undone != 1 {
		t.Errorf("label outcome = %+v, want 1 undone", got)
	}
}

// A check reads blind, agreement and disagreement are recorded, a
// disagreement flags the conversation and waits for you — and your verdict
// becomes a ruling that clears the flag.
func TestTheCheckLoop(t *testing.T) {
	openWorkFixture(t)
	b, err := Next(Options{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	subs := Template(b, "fast")
	for _, it := range b.Items {
		s := templateFor(t, subs, it.Thread)
		fillBlanks(t, it, &s)
		if s.Answers["receipts"] != nil && it.Messages[0].ID == "hd" {
			s.Answers["receipts"], _ = json.Marshal(map[string]Answer{"hd": {None: "missed it"}})
		}
		if res, err := Submit(s, nil); err != nil || !res.OK() {
			t.Fatalf("submit %s: %v %+v", it.Thread, err, res)
		}
	}

	ab, err := AuditNext(AuditOptions{Limit: 2, Model: "careful"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ab.Items) != 2 {
		t.Fatalf("audit batch has %d items, want 2", len(ab.Items))
	}
	// Taken: a second checker does not get the same conversations.
	if again, _ := AuditNext(AuditOptions{Limit: 2}); len(again.Items) != 0 {
		t.Errorf("checked conversations handed out twice: %d", len(again.Items))
	}
	raw, _ := json.Marshal(ab)
	if strings.Contains(string(raw), "missed it") {
		t.Fatal("the checker was shown the first answer")
	}
	for _, s := range Template(ab, "careful") {
		for el, a := range s.Answers {
			if string(a) != "null" {
				t.Errorf("a check's template guessed %s: %s", el, a)
			}
		}
	}

	hd := itemFor(t, ab, "hd")
	check := Submission{Thread: hd.Thread, Lease: hd.Lease, Model: "careful", Answers: map[string]json.RawMessage{}}
	fillAll := map[string]any{}
	for _, a := range hd.Ask {
		switch a.Element {
		case "receipts":
			fillAll["receipts"] = map[string]Answer{"hd": {Records: []map[string]any{{
				"merchant": "Home Depot", "total": 287.83, "quote": "TOTAL"}}}}
		case "sender":
			m := map[string]Answer{}
			for _, s := range a.Senders {
				m[s] = Answer{Level: "system"}
			}
			fillAll["sender"] = m
		}
	}
	for k, v := range fillAll {
		check.Answers[k], _ = json.Marshal(v)
	}
	// Everything else, the way the first read answered it.
	firsts, err := originalsOn(hd.Thread)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range hd.Ask {
		if _, done := check.Answers[a.Element]; done {
			continue
		}
		o := firsts[a.Element]
		if a.Unit == "thread" {
			check.Answers[a.Element], _ = json.Marshal(o[a.Messages[0]].answer)
			continue
		}
		m := map[string]Answer{}
		for k, v := range o {
			m[k] = v.answer
		}
		check.Answers[a.Element], _ = json.Marshal(m)
	}
	res, err := AuditSubmit(check)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("check refused: %+v", res.Problems)
	}
	if len(res.Disagree) == 0 || res.Disagree[0].Element != "receipts" {
		t.Fatalf("missed receipt not caught: %+v", res)
	}
	if f, _ := store.ThreadFlag(hd.Thread); f == nil || !strings.Contains(f.Reason, "receipts") {
		t.Fatalf("disagreement did not flag the conversation: %+v", f)
	}
	open, err := store.Disagreements(true, 10)
	if err != nil || len(open) == 0 {
		t.Fatalf("no open disagreement: %v", err)
	}
	var receiptCheck *store.WorkAudit
	for _, d := range open {
		if d.Element == "receipts" {
			receiptCheck = d
		}
	}
	if receiptCheck == nil || receiptCheck.Subject != "Your Electronic Receipt" {
		t.Fatalf("disagreement does not say what it is about: %+v", receiptCheck)
	}
	if Describe(receiptCheck.Original) != "nothing here" || !strings.Contains(Describe(receiptCheck.Checker), "Home Depot") {
		t.Errorf("descriptions: %q / %q", Describe(receiptCheck.Original), Describe(receiptCheck.Checker))
	}

	// You say the checker was right.
	for _, d := range open {
		choice := store.ResolvedChecker
		if d.Element != "receipts" {
			choice = store.ResolvedOriginal
		}
		if _, err := Resolve(d.ID, choice, nil); err != nil {
			t.Fatal(err)
		}
	}
	if f, _ := store.ThreadFlag(hd.Thread); f != nil {
		t.Error("the flag stayed after every disagreement was settled")
	}
	if _, err := Resolve(receiptCheck.ID, store.ResolvedOriginal, nil); err == nil {
		t.Error("a settled disagreement was settled again")
	}
	rows, _ := store.AgentAnswersOn(hd.Thread)
	for _, r := range rows {
		if r.Element == "receipts" {
			t.Error("the receipts answer is still open to checking after your ruling")
		}
	}

	m, err := Measure(time.Now().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Checks.Random.N+m.Checks.Targeted.N == 0 || m.Checks.Open != 0 {
		t.Errorf("checks measured: %+v", m.Checks)
	}
	var receipts *ElementMetrics
	for i := range m.Elements {
		if m.Elements[i].Element == "receipts" {
			receipts = &m.Elements[i]
		}
	}
	if receipts == nil || receipts.Route.Random.Wrong+receipts.Route.Targeted.Wrong != 1 {
		t.Errorf("the wrong receipt was not counted: %+v", receipts)
	}
}

// Checks that miss an element's target stop the template guessing it — except
// for a sender with a long record of being right.
func TestRoutingStopsGuessingAFailingElement(t *testing.T) {
	openWorkFixture(t)
	no, yes := false, true
	for i := 0; i < 12; i++ {
		agree := &yes
		if i%2 == 0 {
			agree = &no
		}
		if err := store.SaveAudit(&store.WorkAudit{Thread: "t", MessageID: "hd", Target: "hd", Element: "importance",
			Stratum: store.StratumRandom, Original: "{}", Checker: "{}", Agree: agree}); err != nil {
			t.Fatal(err)
		}
		if *agree == false {
			if err := store.ResolveAudit(lastAuditID(t), store.ResolvedChecker); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < TrustStreak; i++ {
		if err := store.SaveAudit(&store.WorkAudit{Thread: "t2", MessageID: "ask", Target: "ask", Element: "waiting",
			Stratum: store.StratumTargeted, Sender: "alice@acme.com", Original: "{}", Checker: "{}", Agree: &yes}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := LoadRouting()
	if err != nil {
		t.Fatal(err)
	}
	imp := r.Elements["importance"]
	if imp.Status != StatusMissing || imp.Guess || imp.Estimate == nil || imp.Estimate.Accuracy != 0.5 {
		t.Fatalf("importance route = %+v", imp)
	}
	if r.Elements["receipts"].Status != StatusUnmeasured || !r.Elements["receipts"].Guess {
		t.Errorf("unmeasured receipts route = %+v", r.Elements["receipts"])
	}
	if !r.Trusted("alice@acme.com") || r.Trusted("homedepot@order.homedepot.com") {
		t.Error("trust does not follow the check record")
	}
	if !r.MayGuess("importance", true) || r.MayGuess("importance", false) {
		t.Error("a trusted sender should earn the guess back; others should not")
	}

	b, err := Next(Options{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	subs := Template(b, "m")
	if s := templateFor(t, subs, itemFor(t, b, "hd").Thread); string(s.Answers["importance"]) != "null" {
		t.Errorf("importance guessed for an untrusted sender while missing its target: %s", s.Answers["importance"])
	}
	if s := templateFor(t, subs, itemFor(t, b, "ask").Thread); string(s.Answers["importance"]) == "null" {
		t.Error("importance not guessed for a trusted sender")
	}
	if due, _ := r.ChecksDue(); due != 0 {
		t.Errorf("checks due with nothing read: %d", due)
	}
}

func lastAuditID(t *testing.T) string {
	t.Helper()
	list, err := store.Disagreements(true, 1)
	if err != nil || len(list) == 0 {
		t.Fatalf("no disagreement: %v", err)
	}
	return list[0].ID
}

func TestCompareAnswers(t *testing.T) {
	byName := map[string]elements.Element{}
	for _, e := range elements.Catalog {
		byName[e.Name] = e
	}
	rec := func(fields map[string]any) Answer { return Answer{Records: []map[string]any{fields}} }
	for _, c := range []struct {
		name string
		el   string
		a, b Answer
		want *bool
	}{
		{"same level", "importance", Answer{Level: "low"}, Answer{Level: "Low"}, ptr(true)},
		{"other level", "importance", Answer{Level: "low"}, Answer{Level: "important"}, ptr(false)},
		{"none vs found", "receipts", Answer{None: "x"}, rec(map[string]any{"merchant": "A", "total": 1.0}), ptr(false)},
		{"both none", "receipts", Answer{None: "x"}, Answer{None: "y"}, ptr(true)},
		{"skipped", "receipts", Answer{Skip: "x"}, Answer{None: "y"}, nil},
		{"same total, other spelling", "receipts", rec(map[string]any{"merchant": "Home Depot", "total": 287.83}),
			rec(map[string]any{"merchant": "The Home Depot", "total": "287.83"}), ptr(true)},
		{"other total", "receipts", rec(map[string]any{"merchant": "A", "total": 287.83}),
			rec(map[string]any{"merchant": "A", "total": 28.78}), ptr(false)},
		{"task owner differs", "tasks", rec(map[string]any{"what": "Send it", "owner": "me"}),
			rec(map[string]any{"what": "Send the file", "owner": "them"}), ptr(false)},
		{"task due differs", "tasks", rec(map[string]any{"what": "x", "owner": "me", "due": "2026-10-24"}),
			rec(map[string]any{"what": "y", "owner": "me", "due": "2026-10-25"}), ptr(false)},
		{"tags overlap", "topics", Answer{Tags: []string{"contract", "acme"}}, Answer{Tags: []string{"Acme", "legal"}}, ptr(true)},
		{"tags apart", "topics", Answer{Tags: []string{"contract", "acme"}}, Answer{Tags: []string{"lunch"}}, ptr(false)},
	} {
		got := compareAnswers(byName[c.el], c.a, c.b)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: got %v, want %v", c.name, deref(got), deref(c.want))
		}
	}
}

func ptr(b bool) *bool { return &b }

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
