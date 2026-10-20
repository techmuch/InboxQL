package annotate

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// The pack is the elements catalog, answered by an AI harness through
// `iql work`: nothing in it runs on the decision or span engines.
func TestThePackIsTheElements(t *testing.T) {
	if len(Starters) != len(elements.Catalog) {
		t.Fatalf("%d starters for %d elements", len(Starters), len(elements.Catalog))
	}
	for _, s := range Starters {
		if s.Engine != store.EngineAgent {
			t.Errorf("%s runs on %s; the pack is answered by the harness", s.Name, s.Engine)
		}
		if _, ok := elements.ByName(s.Name); !ok {
			t.Errorf("%s is not an element", s.Name)
		}
	}
}

func TestStarterNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Starters {
		if seen[s.Name] {
			t.Errorf("%s appears twice", s.Name)
		}
		seen[s.Name] = true
	}
}

// Installing creates; it does not run.
func TestInstallCreatesInertAnnotators(t *testing.T) {
	openAnnotateFixture(t)

	made, skipped, err := InstallStarters(nil)
	if err != nil {
		t.Fatalf("InstallStarters: %v", err)
	}
	if len(made) != len(Starters) {
		t.Errorf("created %d of %d", len(made), len(Starters))
	}
	if len(skipped) != 0 {
		t.Errorf("skipped %v on an empty database", skipped)
	}
	for _, name := range made {
		a, err := store.GetAnnotator(name)
		if err != nil || a == nil {
			t.Fatalf("%s was not created: %v", name, err)
		}
		if a.Trigger != store.TriggerManual {
			t.Errorf("%s has trigger %q, want manual", name, a.Trigger)
		}
		if _, ok := elements.FromAnnotator(a); !ok {
			t.Errorf("%s does not read back as an element", name)
		}
	}
}

// A same-named annotator that never answered anything is replaced — the
// starters most mailboxes installed and never ran. One that has answered is
// left alone: its results belong to whoever made it.
func TestInstallReplacesOnlyWhatNeverAnswered(t *testing.T) {
	openAnnotateFixture(t)

	for _, a := range []*store.Annotator{
		{Name: "receipts", Kind: store.KindExtract, Engine: store.EngineGLiNER,
			Instructions: "my own wording", SchemaJSON: `{"total":"x"}`},
		{Name: "importance", Kind: store.KindLabel, Engine: store.EngineLaya,
			Instructions: "how much this matters"},
	} {
		if err := store.SaveAnnotator(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveAccount(&account.Account{ID: "a", Name: "Me", Email: "me@x.com", User: "me@x.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(&message.Message{ID: "m1", AccountID: "a", ContentHash: "m1", MessageID: "<m1@x>",
		From: "x@y.com", Subject: "s", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	used, _ := store.GetAnnotator("importance")
	if err := store.SaveAnnotations(used.ID, used.Version, "m1", []*store.Annotation{{Status: store.StatusOK, Source: store.SourceLLM}}); err != nil {
		t.Fatal(err)
	}

	made, skipped, err := InstallStarters(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(made, "receipts") {
		t.Errorf("the unused receipts annotator was not replaced: made %v", made)
	}
	if !contains(skipped, "importance") {
		t.Errorf("importance, which has answers, was not left alone: skipped %v", skipped)
	}

	r, _ := store.GetAnnotator("receipts")
	if r.Engine != store.EngineAgent || r.Version < 2 {
		t.Errorf("receipts = engine %s version %d; want the agent engine and a new version", r.Engine, r.Version)
	}
	if i, _ := store.GetAnnotator("importance"); i.Instructions != "how much this matters" {
		t.Errorf("importance was overwritten: %q", i.Instructions)
	}
}

func TestInstallOnlyWhatWasAskedFor(t *testing.T) {
	openAnnotateFixture(t)

	made, _, err := InstallStarters([]string{"travel"})
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0] != "travel" {
		t.Fatalf("created %v, want just travel", made)
	}
	if a, _ := store.GetAnnotator("receipts"); a != nil {
		t.Error("receipts was created without being asked for")
	}
}

// An explicit scope wins; otherwise the annotator's own is used. A triggered
// run has nobody to pass a flag, so the stored one has to be the default.
func TestScopeForPrefersTheExplicitOne(t *testing.T) {
	a := &store.Annotator{Scope: "label:money"}

	if got := scopeFor(a, "from:acme"); got != "from:acme" {
		t.Errorf("explicit scope = %q, want it to win", got)
	}
	if got := scopeFor(a, ""); got != "label:money" {
		t.Errorf("default scope = %q, want the annotator's own", got)
	}
	if got := scopeFor(a, "   "); got != "label:money" {
		t.Errorf("blank scope = %q, want the annotator's own", got)
	}
	if got := scopeFor(&store.Annotator{}, ""); got != "" {
		t.Errorf("unscoped annotator = %q, want the whole mailbox", got)
	}
}

// Confidence is a read-time question, not a knob baked into an instruction.
func TestStarterFloorIsStated(t *testing.T) {
	if StarterFloor <= 0 || StarterFloor >= 1 {
		t.Errorf("StarterFloor = %v, want a probability", StarterFloor)
	}
	for _, s := range Starters {
		if strings.Contains(s.Instructions, "confidence") {
			t.Errorf("%s bakes confidence into its instruction; it belongs at read time", s.Name)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
