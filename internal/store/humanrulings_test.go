package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// rulingFixture is a mailbox with three messages, which annotations need
// because they carry a foreign key to one.
func rulingFixture(t *testing.T) *Annotator {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)

	if err := SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"m1", "m2", "m3", "never-seen"} {
		m := &message.Message{
			ID: id, AccountID: "acct", ContentHash: id,
			MessageID: "<" + id + "@example.test>",
			From:      "shop@example.com", Subject: "Order " + id,
			Body: "thanks for your order", Date: time.Now(), Mailbox: "INBOX",
		}
		m.InternalDate = m.Date
		if err := SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	a := &Annotator{
		Name: "purchased", Kind: KindLabel, Engine: EngineLaya,
		Instructions: "this message is a receipt",
	}
	if err := SaveAnnotator(a); err != nil {
		t.Fatal(err)
	}
	got, _ := GetAnnotator("purchased")
	return got
}

// # The join calibration depends on
//
// A human ruling does not overwrite the machine's answer; it is a second row
// that outranks it, which is what lets a re-run keep both. So "what did the
// model say about the messages a person has ruled on" is a join, and getting it
// wrong would fit a temperature against the wrong numbers — silently, because
// the result is a plausible float either way.
func TestHumanRulingsPairsAPersonsVerdictWithTheMachines(t *testing.T) {
	a := rulingFixture(t)

	conf := func(v float64) *float64 { return &v }

	// Three machine answers, two of them ruled on.
	for _, c := range []struct {
		msg    string
		status string
		score  float64
	}{
		{"m1", StatusOK, 0.97},
		{"m2", StatusEmpty, 0.12},
		{"m3", StatusOK, 0.80},
	} {
		if err := SaveAnnotations(a.ID, a.Version, c.msg, []*Annotation{{
			Status: c.status, Source: SourceLLM, Confidence: conf(c.score),
		}}); err != nil {
			t.Fatal(err)
		}
	}

	// A person agrees about m1 and overrules m2. Through SetHumanAnnotation,
	// which is the path `annotate correct` takes: it leaves the machine row
	// alone, because the comparison between the two is the whole point.
	if err := SetHumanAnnotation("purchased", "m1", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetHumanAnnotation("purchased", "m2", true, nil); err != nil {
		t.Fatal(err)
	}

	got, err := HumanRulings(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rulings, want 2 — m3 was never ruled on", len(got))
	}

	by := map[string]HumanRuling{}
	for _, r := range got {
		by[r.MessageID] = r
	}

	if r := by["m1"]; !r.Said || !r.Ruled || r.Confidence != 0.97 {
		t.Errorf("m1 = %+v, want the machine's yes at 0.97 agreed with", r)
	}
	// The one that matters: the machine said no, the person said yes, and the
	// score it was wrong at is the quantity a fit is about.
	if r := by["m2"]; r.Said || !r.Ruled || r.Confidence != 0.12 {
		t.Errorf("m2 = %+v, want the machine's no at 0.12 overruled", r)
	}
}

// A ruling on a message the annotator never evaluated has nothing to compare
// against. Including it would make a fit measure agreement with silence.
func TestHumanRulingsSkipsWhatTheMachineNeverAnswered(t *testing.T) {
	a := rulingFixture(t)

	if err := SetHumanAnnotation("purchased", "never-seen", true, nil); err != nil {
		t.Fatal(err)
	}

	_ = a
	got, err := HumanRulings(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want none — the machine never answered", got)
	}
}

// # The bug schema v33 fixes
//
// A human ruling was written at seq 0 with INSERT OR REPLACE, against a unique
// key of (message_id, annotator_id, annotator_version, seq) — so recording a
// correction deleted the machine's answer for that message.
//
// What is lost is the evidence of what was corrected. You could see that
// somebody disagreed and never what they disagreed with, which makes the
// disagreement unreviewable and a calibration fit impossible: the quantity
// being fitted is the score the model was wrong at.
//
// "Outranks" was never supposed to mean "erases". Both rows exist; the human
// one wins every read.
func TestACorrectionKeepsWhatItCorrected(t *testing.T) {
	a := rulingFixture(t)
	conf := 0.93

	if err := SaveAnnotations(a.ID, a.Version, "m1", []*Annotation{{
		Status: StatusOK, Source: SourceLLM, Confidence: &conf, Model: "laya test",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := SetHumanAnnotation("purchased", "m1", false, nil); err != nil {
		t.Fatal(err)
	}

	anns, err := ListAnnotations("m1")
	if err != nil {
		t.Fatal(err)
	}

	var machine, human *Annotation
	for _, x := range anns {
		switch x.Source {
		case SourceHuman:
			human = x
		case SourceLLM:
			machine = x
		}
	}
	if human == nil {
		t.Fatal("the ruling was not recorded")
	}
	if machine == nil {
		t.Fatal("recording a ruling erased the answer it was correcting")
	}
	if machine.Confidence == nil || *machine.Confidence != conf {
		t.Errorf("the machine's score is %v, want %v — it is what a fit is about",
			machine.Confidence, conf)
	}
	if machine.Status != StatusOK || human.Status != StatusEmpty {
		t.Errorf("machine said %q and the person said %q; want ok then empty",
			machine.Status, human.Status)
	}
}
