package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// labelFixture is a two-message conversation and one lone message.
func labelFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
	if err := SaveAccount(&account.Account{ID: "acct", Name: "Me", Email: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i, m := range []*message.Message{
		{ID: "first", MessageID: "<root@x>", From: "alice@x.com", Subject: "Contract"},
		{ID: "reply", MessageID: "<reply@x>", From: "me@example.com", Subject: "Re: Contract",
			Header: []byte("References: <root@x>\r\nIn-Reply-To: <root@x>\r\n")},
		{ID: "alone", MessageID: "<alone@x>", From: "shop@x.com", Subject: "Receipt"},
	} {
		m.AccountID, m.ContentHash, m.Mailbox = "acct", m.ID, "INBOX"
		m.Date = base.Add(time.Duration(i) * time.Minute)
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []*Annotator{
		{Name: "receipt", Kind: KindLabel, Engine: EngineLaya, Instructions: "a receipt"},
		{Name: "loops", Kind: KindLabel, Engine: EngineLaya, Instructions: "expects a reply",
			SchemaJSON: `{"unit":"thread"}`},
		{Name: "importance", Kind: KindLabel, Engine: EngineLaya, Instructions: "attention",
			SchemaJSON: `{"unit":"thread","levels":[{"label":"important"},{"label":"low"}]}`},
	} {
		if err := SaveAnnotator(a); err != nil {
			t.Fatal(err)
		}
	}
}

func labelByName(t *testing.T, messageID, name string) *MessageLabel {
	t.Helper()
	got, err := MessageLabels(messageID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		if got[i].Annotator == name {
			return &got[i]
		}
	}
	return nil
}

// "Never looked at" is not "no", so it is absent rather than shown.
func TestALabelThatHasNotLookedIsAbsent(t *testing.T) {
	labelFixture(t)
	if l := labelByName(t, "alone", "receipt"); l != nil {
		t.Errorf("an unevaluated label appeared: %+v", l)
	}
}

func TestMachineAndRulingAreBothReported(t *testing.T) {
	labelFixture(t)
	a, _ := GetAnnotator("receipt")
	if err := SaveAnnotations(a.ID, a.Version, "alone", []*Annotation{{
		Status: StatusEmpty, Source: SourceLLM, Confidence: score(0.2),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := RuleOnMessage("receipt", "alone", Ruling{Matched: true, Via: RuledInflow}); err != nil {
		t.Fatal(err)
	}
	l := labelByName(t, "alone", "receipt")
	if l == nil || l.Machine == nil || l.Ruling == nil {
		t.Fatalf("want both verdicts, got %+v", l)
	}
	if l.Machine.Matched || !l.Ruling.Matched || l.Ruling.Via != RuledInflow {
		t.Errorf("machine %+v, ruling %+v", l.Machine, l.Ruling)
	}
}

// A thread label's verdict lives on the newest message of the conversation.
// Ruling while reading an older message must land there too, or it would be
// compared with nothing.
func TestAThreadRulingLandsOnTheNewestMessage(t *testing.T) {
	labelFixture(t)
	a, _ := GetAnnotator("loops")
	if err := SaveAnnotations(a.ID, a.Version, "reply", []*Annotation{{
		Status: StatusOK, Source: SourceLLM, Confidence: score(0.8),
	}}); err != nil {
		t.Fatal(err)
	}

	// Read from the older message, the verdict is the conversation's.
	if l := labelByName(t, "first", "loops"); l == nil || l.Machine == nil || !l.Machine.Matched {
		t.Fatalf("the older message did not see the conversation's verdict: %+v", l)
	}

	if err := RuleOnMessage("loops", "first", Ruling{Matched: false, Via: RuledInflow}); err != nil {
		t.Fatal(err)
	}
	rulings, _ := HumanRulings(a.ID)
	if len(rulings) != 1 || rulings[0].MessageID != "reply" {
		t.Fatalf("the ruling landed on %+v, want the newest message", rulings)
	}
}

func TestALevelMustBeOneOfTheLabels(t *testing.T) {
	labelFixture(t)
	if err := RuleOnMessage("importance", "first", Ruling{Matched: true, Level: "urgent", Via: RuledInflow}); err == nil {
		t.Error("a level the label does not have was accepted")
	}
	if err := RuleOnMessage("importance", "first", Ruling{Matched: true, Level: "low", Via: RuledInflow}); err != nil {
		t.Errorf("a real level was refused: %v", err)
	}
}

// reviewFixture is twenty messages a label has answered, scored 0.025 apart.
func reviewFixture(t *testing.T) *Annotator {
	t.Helper()
	labelFixture(t)
	a, _ := GetAnnotator("receipt")
	for i := 0; i < 20; i++ {
		id := "r" + string(rune('a'+i))
		m := &message.Message{ID: id, AccountID: "acct", ContentHash: id, MessageID: "<" + id + "@x>",
			From: "x@x.com", Subject: "s " + id, Mailbox: "INBOX", Date: time.Now()}
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
		if err := SaveAnnotations(a.ID, a.Version, id, []*Annotation{{
			Status: StatusOK, Source: SourceLLM, Confidence: score(0.5 + float64(i)*0.025),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// One from each band of scores, not the n least certain: a sample of the model
// as it is used, which is what accuracy and calibration have to be measured on.
func TestReviewSpreadsAcrossTheScores(t *testing.T) {
	reviewFixture(t)
	got, err := ReviewSample("receipt", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d items, want 4", len(got))
	}
	lo, hi := 1.0, 0.0
	for _, it := range got {
		s := *it.Machine.Score
		if s < lo {
			lo = s
		}
		if s > hi {
			hi = s
		}
	}
	// Bands of five over 0.500–0.975: the lowest band tops out at 0.600 and
	// the highest starts at 0.875.
	if lo > 0.601 || hi < 0.874 {
		t.Errorf("drawn from %.3f to %.3f; want one from each end of the range", lo, hi)
	}
}

// A message somebody has already ruled on is not asked about again.
func TestReviewSkipsWhatIsAlreadyRuled(t *testing.T) {
	reviewFixture(t)
	for i := 0; i < 19; i++ {
		id := "r" + string(rune('a'+i))
		if err := RecordRuling("receipt", id, Ruling{Matched: true, Via: RuledReview}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReviewSample("receipt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MessageID != "rt" {
		t.Errorf("want only the one unruled message, got %+v", got)
	}
}

// The case Laya actually produced on the open-loop question: it ranked the
// real requests above everything else and still answered "no" to all of them,
// because 0.5 is the wrong place to cut an uncalibrated score. The score says
// how often the answers as given were right, and where a cut would have been.
func TestScoreFindsTheCutoffTheModelMissed(t *testing.T) {
	labelFixture(t)
	a, _ := GetAnnotator("receipt")
	cases := []struct {
		id    string
		yes   float64 // the model's probability of "yes"
		truth bool
	}{
		{"r1", 0.27, true}, {"r2", 0.30, true}, {"r3", 0.25, true},
		{"r4", 0.07, false}, {"r5", 0.01, false}, {"r6", 0.10, false},
	}
	for _, c := range cases {
		m := &message.Message{ID: c.id, AccountID: "acct", ContentHash: c.id, MessageID: "<" + c.id + "@x>",
			From: "x@x.com", Subject: c.id, Mailbox: "INBOX", Date: time.Now()}
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
		// Every answer as given is "no": none of them clears 0.5.
		if err := SaveAnnotations(a.ID, a.Version, c.id, []*Annotation{{
			Status: StatusEmpty, Source: SourceLLM, Confidence: score(c.yes),
		}}); err != nil {
			t.Fatal(err)
		}
		if err := RecordRuling("receipt", c.id, Ruling{Matched: c.truth, Via: RuledReview}); err != nil {
			t.Fatal(err)
		}
	}
	// A correction made while reading is reported but not scored. The machine
	// answer goes in first: SaveAnnotations will not write over a ruling.
	if err := SaveAnnotations(a.ID, a.Version, "alone", []*Annotation{{
		Status: StatusEmpty, Source: SourceLLM, Confidence: score(0.2),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := RecordRuling("receipt", "alone", Ruling{Matched: true, Via: RuledInflow}); err != nil {
		t.Fatal(err)
	}

	s, err := ScoreAnnotator("receipt")
	if err != nil {
		t.Fatal(err)
	}
	if s.Reviewed != 6 || s.Inflow != 1 {
		t.Fatalf("reviewed %d, inflow %d; want 6 and 1", s.Reviewed, s.Inflow)
	}
	if s.Right != 3 {
		t.Errorf("right as given = %d, want 3 (only the true noes)", s.Right)
	}
	if s.Cutoff == nil || s.AtCutoff != 6 {
		t.Fatalf("cutoff %v gets %d right, want a cutoff that gets all 6", s.Cutoff, s.AtCutoff)
	}
	if *s.Cutoff <= 0.10 || *s.Cutoff > 0.25 {
		t.Errorf("cutoff %.2f, want between the noes and the yeses", *s.Cutoff)
	}
}

func TestALevelledLabelHasNoCutoff(t *testing.T) {
	labelFixture(t)
	s, err := ScoreAnnotator("importance")
	if err != nil {
		t.Fatal(err)
	}
	if s.Cutoff != nil {
		t.Error("a levelled label was given a yes/no cutoff")
	}
}

// A conversation is judged once, on its newest message — and a reply makes it
// pending again, because the reply is the new newest message.
func TestAThreadAnnotatorOnlySeesTheNewestMessage(t *testing.T) {
	labelFixture(t)
	a, _ := GetAnnotator("loops")
	pending, err := PendingMessages(a, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range pending {
		ids[m.ID] = true
	}
	if ids["first"] {
		t.Error("an older message in a conversation was offered")
	}
	if !ids["reply"] || !ids["alone"] {
		t.Errorf("want the newest of each conversation, got %v", ids)
	}

	p, err := Progress(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 2 {
		t.Errorf("coverage counts %d, want 2 conversations", p.Total)
	}
}

func TestTheDigestKnowsWhoSpokeLast(t *testing.T) {
	labelFixture(t)
	d, err := DigestThread("first")
	if err != nil {
		t.Fatal(err)
	}
	if d.Messages != 2 {
		t.Errorf("messages %d, want 2", d.Messages)
	}
	// "reply" is from me@example.com, one of the configured accounts.
	if !d.LastFromMe || !d.YouReplied {
		t.Errorf("last from me %v, replied %v; want both", d.LastFromMe, d.YouReplied)
	}

	alone, _ := DigestThread("alone")
	if alone.LastFromMe || alone.YouReplied {
		t.Errorf("a shop's lone message read as yours: %+v", alone)
	}
}

// Deleting a label an extractor is scoped on would leave the extractor's scope
// broken, and a run with a broken scope covers the whole mailbox.
func TestDeletingAGateIsRefusedUntilItsDependentsAreHandled(t *testing.T) {
	labelFixture(t)
	if err := SaveAnnotator(&Annotator{Name: "bills", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "x", Scope: "label:receipt after:90d"}); err != nil {
		t.Fatal(err)
	}
	// Mentions the name in another field: not a dependent.
	if err := SaveAnnotator(&Annotator{Name: "other", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "x", Scope: "subject:receipt"}); err != nil {
		t.Fatal(err)
	}

	deps, _ := ScopeDependents("receipt")
	if len(deps) != 1 || deps[0] != "bills" {
		t.Fatalf("dependents %v, want just bills", deps)
	}

	if _, err := DeleteAnnotatorChecked("receipt", false); err == nil {
		t.Fatal("deleted a gate without saying what it gates")
	}
	if a, _ := GetAnnotator("receipt"); a == nil {
		t.Fatal("the refused delete deleted it anyway")
	}

	disabled, err := DeleteAnnotatorChecked("receipt", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(disabled) != 1 || disabled[0] != "bills" {
		t.Errorf("switched off %v, want bills", disabled)
	}
	if b, _ := GetAnnotator("bills"); b == nil || b.Enabled {
		t.Errorf("bills should remain, switched off: %+v", b)
	}
}
