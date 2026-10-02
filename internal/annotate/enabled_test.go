package annotate

import (
	"context"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

// # What "off" has to mean
//
// Deleting an annotator cascades to every annotation it ever wrote, including
// the human corrections somebody made by hand — which no amount of re-running
// recovers. So "I do not want this one" needs an answer that costs one UPDATE
// in each direction.
//
// These tests pin both halves of that answer: what stops, and what does not.

// The landmine. A bool's zero value is false, so if SaveAnnotator wrote
// Enabled then every caller that built an Annotator without thinking about the
// field would silently switch it off — a stop with nothing on screen to
// explain it. Saving must not be able to do that.
func TestSavingDoesNotSwitchAnAnnotatorOff(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "subject:invoice",
	})
	if !a.Enabled {
		t.Fatal("a new annotator is off; it should arrive on")
	}

	// A save that says nothing about Enabled — the shape every existing caller
	// has, and the shape of an edit from the web form.
	again := mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "subject:invoice OR subject:receipt",
	})
	if !again.Enabled {
		t.Error("editing the instruction switched it off")
	}

	// And it cannot switch one back on either, which is the same rule read the
	// other way: the flag changes only when something asks for it to.
	if err := store.SetAnnotatorEnabled("money", false); err != nil {
		t.Fatal(err)
	}
	back := mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "subject:invoice OR subject:receipt OR subject:bill",
	})
	if back.Enabled {
		t.Error("editing a switched-off annotator switched it on")
	}
}

// A trigger must not fire a switched-off annotator. Without this check, "off"
// buys nothing: a daily annotator somebody switched off carries on overnight,
// which is the one time nobody is watching.
func TestDueSkipsASwitchedOffAnnotator(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "nightly", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerDaily,
	})
	mustSave(t, &store.Annotator{
		Name: "alsonightly", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerDaily,
	})

	if err := store.SetAnnotatorEnabled("nightly", false); err != nil {
		t.Fatal(err)
	}

	due, err := Due(store.TriggerDaily)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Name != "alsonightly" {
		names := []string{}
		for _, a := range due {
			names = append(names, a.Name)
		}
		t.Fatalf("daily is due %v, want just [alsonightly]", names)
	}
}

// Named explicitly, so this is not a trigger to skip quietly — it is a request
// to answer. The error carries the remedy, because the state is invisible from
// wherever the command was typed.
func TestRunRefusesASwitchedOffAnnotator(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	})
	if err := store.SetAnnotatorEnabled("money", false); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), "money", Options{})
	if err == nil {
		t.Fatal("a switched-off annotator ran")
	}
	if !strings.Contains(err.Error(), "annotate enable money") {
		t.Errorf("the error does not say how to switch it on: %v", err)
	}
}

// # The decision the whole design rests on
//
// Off means it will not run. Everything it has already said still counts.
//
// So `label:money` keeps matching while `money` is off, because those labels
// are facts about messages that really were observed — the same reason a
// version bump keeps the old answers rather than deleting them.
//
// The alternative, where off also hides results, makes unticking destructive
// again but invisibly: query answers change with no row deleted. That is worse
// than the delete it was meant to replace, because there is nothing to undo.
func TestSwitchingOffKeepsEverythingItAlreadySaid(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread",
	})
	if _, _, err := store.ApplyRule(a, ""); err != nil {
		t.Fatal(err)
	}

	before, err := store.CountQuery("label:money")
	if err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("the fixture labelled nothing; the test cannot say anything")
	}

	if err := store.SetAnnotatorEnabled("money", false); err != nil {
		t.Fatal(err)
	}

	after, err := store.CountQuery("label:money")
	if err != nil {
		t.Fatalf("querying a switched-off annotator's label failed: %v", err)
	}
	if after != before {
		t.Errorf("label:money matched %d before and %d after switching off; "+
			"off must not change what is already known", before, after)
	}

	// And the results themselves are still there to read, which is what makes
	// switching back on free.
	p, err := store.Progress(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Matched == 0 {
		t.Error("its results are gone")
	}
}

// Switching back on restores participation without re-running anything. This
// is the requirement in one test: neither direction creates work.
func TestSwitchingBackOnCostsNothing(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerDaily,
	})
	if _, _, err := store.ApplyRule(a, ""); err != nil {
		t.Fatal(err)
	}
	covered, err := store.Progress(a, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, on := range []bool{false, true} {
		if err := store.SetAnnotatorEnabled("money", on); err != nil {
			t.Fatal(err)
		}
	}

	again, err := store.GetAnnotator("money")
	if err != nil || again == nil {
		t.Fatal(err)
	}
	if !again.Enabled {
		t.Fatal("it did not come back on")
	}
	now, err := store.Progress(again, "")
	if err != nil {
		t.Fatal(err)
	}
	if now.Evaluated != covered.Evaluated || now.Matched != covered.Matched {
		t.Errorf("coverage changed across off/on: %d/%d then %d/%d",
			covered.Matched, covered.Evaluated, now.Matched, now.Evaluated)
	}
	// Nothing pending, so its trigger has nothing to do — the round trip
	// created no work.
	due, err := Due(store.TriggerDaily)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("switching back on queued a re-run: %d due", len(due))
	}
}

// Naming something that is not there is a not-found, not a silent success.
func TestSetEnabledOnAnUnknownAnnotator(t *testing.T) {
	openTriggerFixture(t)
	if err := store.SetAnnotatorEnabled("nosuchthing", false); err == nil {
		t.Error("switching off an annotator that does not exist succeeded")
	}
}

// # The number that makes the claim checkable
//
// "Switching off keeps everything" is only believable as a number, and the
// number has to describe what a delete would actually take: annotation rows.
//
// [store.Progress] counts DISTINCT messages, which is right for a coverage bar
// and wrong here, because one message can hold many records — a receipt with
// an amount, a date and three references is five. On the real mailbox the two
// differ by about nine to one: `receipts` covers 32 messages and holds 269
// records, 19 of them set by hand. Reporting coverage as "what was kept" would
// have understated the hand-set values nineteen-fold, and those are the half
// no re-run recovers.
//
// This test exists because that mistake was made once already, in the first
// draft of the switch.
func TestVolumeCountsRowsNotMessages(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "receipts", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x","ref":"y"}`,
	})

	ids, err := store.PendingMessages(a, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("the fixture has no messages; the test cannot say anything")
	}

	// Three records on one message, which is the ordinary case for an
	// extractor and the case where rows and messages diverge.
	anns := []*store.Annotation{}
	for i := 0; i < 3; i++ {
		anns = append(anns, &store.Annotation{
			MessageID: ids[0].ID, AnnotatorID: a.ID, AnnotatorVersion: a.Version,
			Status: store.StatusOK, Source: store.SourceRule,
			DataJSON: `{"label":"amount","value":"$5"}`, Seq: i,
		})
	}
	if err := store.SaveAnnotations(a.ID, a.Version, ids[0].ID, anns); err != nil {
		t.Fatal(err)
	}

	vol, err := store.AnnotationVolumeOf(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if vol.Records != 3 {
		t.Errorf("records = %d, want 3 — it is counting messages", vol.Records)
	}
	if vol.Messages != 1 {
		t.Errorf("messages = %d, want 1", vol.Messages)
	}

	p, err := store.Progress(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Matched != 1 {
		t.Errorf("Progress.Matched = %d, want 1 — it counts messages, and that "+
			"is the whole reason volume is a separate call", p.Matched)
	}
}

// A human ruling is counted as rows too, for the same reason — and it is the
// number that matters most, because re-running recovers everything else.
func TestVolumeCountsHandSetValuesAsRows(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "receipts", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x"}`,
	})
	ids, err := store.PendingMessages(a, "", 1)
	if err != nil || len(ids) == 0 {
		t.Fatalf("fixture: %v", err)
	}

	if err := store.SetHumanSpans(a.Name, ids[0].ID, []store.SpanCorrection{
		{Field: "subject", Start: 0, End: 1, Label: "amount"},
		{Field: "subject", Start: 2, End: 3, Label: "amount"},
	}); err != nil {
		t.Fatal(err)
	}

	vol, err := store.AnnotationVolumeOf(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if vol.Human != 2 {
		t.Errorf("human = %d, want 2 rows across one message", vol.Human)
	}
	if vol.Messages != 1 {
		t.Errorf("messages = %d, want 1", vol.Messages)
	}
}
