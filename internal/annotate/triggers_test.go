package annotate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func mustSave(t *testing.T, a *store.Annotator) *store.Annotator {
	t.Helper()
	if err := store.SaveAnnotator(a); err != nil {
		t.Fatalf("SaveAnnotator(%s): %v", a.Name, err)
	}
	got, err := store.GetAnnotator(a.Name)
	if err != nil || got == nil {
		t.Fatalf("GetAnnotator(%s): %v", a.Name, err)
	}
	return got
}

// A trigger is a "when". Only the annotators that asked for this one are due,
// and an annotator asking for a different trigger is not swept along with it.
func TestDueSelectsByTrigger(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "onsync", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})
	mustSave(t, &store.Annotator{
		Name: "nightly", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerDaily,
	})
	mustSave(t, &store.Annotator{
		Name: "byhand", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerManual,
	})

	due, err := Due(store.TriggerAfterSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Name != "onsync" {
		names := []string{}
		for _, a := range due {
			names = append(names, a.Name)
		}
		t.Fatalf("after-sync is due %v, want just [onsync]", names)
	}
}

// An annotator with nothing pending is skipped rather than started. A job that
// evaluates nothing is noise in every log and progress bar it touches.
func TestDueSkipsWhatHasNothingWaiting(t *testing.T) {
	openTriggerFixture(t)

	a := mustSave(t, &store.Annotator{
		Name: "done", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})

	due, err := Due(store.TriggerAfterSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("a fresh annotator was not due: %v", due)
	}

	// Evaluate everything it can see.
	if _, _, err := store.ApplyRule(a, ""); err != nil {
		t.Fatal(err)
	}

	due, err = Due(store.TriggerAfterSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("still due after evaluating everything: %v", due[0].Name)
	}
}

// The annotator's own scope is what a triggered run covers: nobody is there to
// pass a flag.
func TestDueHonoursTheStoredScope(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "gate", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerManual,
	})
	wide := mustSave(t, &store.Annotator{
		Name: "wide", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})
	narrow := mustSave(t, &store.Annotator{
		Name: "narrow", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
		Scope: "label:gate",
	})

	all, err := store.Progress(wide, scopeFor(wide, ""))
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := store.Progress(narrow, scopeFor(narrow, ""))
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total >= all.Total {
		t.Errorf("scoped annotator sees %d of %d — the scope did not narrow it",
			scoped.Total, all.Total)
	}
}

func TestDueRejectsAnUnknownTrigger(t *testing.T) {
	openTriggerFixture(t)
	if _, err := Due("whenever"); err == nil {
		t.Error("an unknown trigger was accepted")
	}
}

// A broken scope — a label it referenced was deleted — must not stop the
// others in the sweep.
func TestDueSkipsAnAnnotatorWithABrokenScope(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "broken", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
		Scope: "label:nosuchannotator",
	})
	mustSave(t, &store.Annotator{
		Name: "fine", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})

	due, err := Due(store.TriggerAfterSync)
	if err != nil {
		t.Fatalf("one broken scope failed the whole sweep: %v", err)
	}
	for _, a := range due {
		if a.Name == "broken" {
			t.Error("an annotator with an uncompilable scope was reported due")
		}
	}
	found := false
	for _, a := range due {
		if a.Name == "fine" {
			found = true
		}
	}
	if !found {
		t.Error("the working annotator was lost with the broken one")
	}
}

func TestSweepRunsWhatIsDue(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "swept", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})

	out, err := Sweep(context.Background(), store.TriggerAfterSync, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Ran) != 1 || out.Ran[0].Annotator != "swept" {
		t.Fatalf("ran %+v, want just swept", out.Ran)
	}
	if out.Remaining != 0 {
		t.Errorf("%d left pending after a rule pass over everything", out.Remaining)
	}

	// Idempotent: the queue is drained, so a second pass has nothing to do.
	again, err := Sweep(context.Background(), store.TriggerAfterSync, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Ran) != 0 {
		t.Errorf("a second sweep ran %+v, want nothing", again.Ran)
	}
}

// Cancellation leaves what is done done and what is not pending — the same
// state an interrupted manual run leaves.
func TestSweepStopsWhenCancelled(t *testing.T) {
	openTriggerFixture(t)

	mustSave(t, &store.Annotator{
		Name: "swept", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := Sweep(ctx, store.TriggerAfterSync, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Ran) != 0 {
		t.Errorf("a cancelled sweep ran %+v", out.Ran)
	}
}

// One annotator failing must not stop the rest. A missing span model should
// not block the rule labels behind it in the queue.
func TestSweepRecordsAFailureAndCarriesOn(t *testing.T) {
	openTriggerFixture(t)

	// No model installed in a temp directory, so this one cannot run.
	mustSave(t, &store.Annotator{
		Name: "needsmodel", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x"}`,
		Trigger: store.TriggerAfterSync,
	})
	mustSave(t, &store.Annotator{
		Name: "cheap", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: "is:unread", Trigger: store.TriggerAfterSync,
	})

	out, err := Sweep(context.Background(), store.TriggerAfterSync, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("a failing annotator failed the sweep: %v", err)
	}
	if len(out.Failed) == 0 {
		t.Error("the failure was not recorded")
	}
	ran := false
	for _, o := range out.Ran {
		if o.Annotator == "cheap" {
			ran = true
		}
	}
	if !ran {
		t.Error("the working annotator did not run after the failing one")
	}
}

// A pass is capped so that a trigger finding forty thousand pending messages
// is not indistinguishable from a hang.
func TestTriggeredRunsAreCapped(t *testing.T) {
	if TriggeredLimit <= 0 {
		t.Errorf("TriggeredLimit = %d, want a bound", TriggeredLimit)
	}
}

func TestTriggerNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{store.TriggerManual, true},
		{store.TriggerAfterSync, true},
		{store.TriggerDaily, true},
		{"on-open", false},
		{"", false},
	} {
		if got := store.ValidTrigger(tc.name); got != tc.ok {
			t.Errorf("ValidTrigger(%q) = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

// openTriggerFixture is a store with a little mail in it.
//
// The annotate package's usual fixture is deliberately empty — its other tests
// are about which gateway a run would use, decided before a message is read.
// A trigger is about what is pending, so it needs something to be pending.
func openTriggerFixture(t *testing.T) {
	t.Helper()
	openAnnotateFixture(t)

	// Messages reference an account, so one has to exist first.
	if err := store.SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	for i, subject := range []string{
		"Fwd: Order & Pay Receipt for $80.44",
		"Fwd: Your statement is ready",
		"Re: lunch on Thursday",
	} {
		m := &message.Message{
			ID:        fmt.Sprintf("m%d", i),
			AccountID: "acct",
			MessageID: fmt.Sprintf("<m%d@test>", i),
			Subject:   subject,
			From:      "someone@example.com",
			Body:      "the body of " + subject,
			Date:      time.Now(),
			Flags:     []string{},
		}
		if err := store.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}
}
