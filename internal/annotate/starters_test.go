package annotate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/store"
)

// The pairing is the pack's whole lesson: an expensive extractor gated by a
// cheap rule. A pack where everything was a span extractor would teach the
// opposite instinct and cost an hour of CPU on a small mailbox.
func TestEveryExtractorIsGatedBySomethingCheap(t *testing.T) {
	names := map[string]Starter{}
	for _, s := range Starters {
		names[s.Name] = s
	}

	extractors := 0
	for _, s := range Starters {
		if s.Engine != store.EngineGLiNER {
			continue
		}
		extractors++
		if s.Scope == "" {
			t.Errorf("%s is a span extractor with no scope — it would read the whole mailbox", s.Name)
			continue
		}
		if s.Needs == "" {
			// Scoped by something other than a starter label, like
			// has:attachment. Fine, but it must still narrow.
			continue
		}
		gate, ok := names[s.Needs]
		if !ok {
			t.Errorf("%s is gated by %q, which is not in the pack", s.Name, s.Needs)
			continue
		}
		if gate.Engine != store.EngineRule {
			t.Errorf("%s is gated by %s, which is a %s — the gate has to be free",
				s.Name, gate.Name, gate.Engine)
		}
		if want := "label:" + s.Needs; s.Scope != want {
			t.Errorf("%s scope = %q, want %q", s.Name, s.Scope, want)
		}
	}
	if extractors == 0 {
		t.Fatal("the pack has no extractors at all")
	}
}

// A rule's instruction is a query, so an invalid one is an annotator that
// matches nothing later and reads as "found no mail" rather than as a mistake.
func TestStarterRulesAreValidQueries(t *testing.T) {
	openAnnotateFixture(t)

	for _, s := range Starters {
		if s.Engine != store.EngineRule {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			if err := store.ValidateQuery(s.Instructions); err != nil {
				t.Errorf("%s: %v", s.Name, err)
			}
		})
	}
}

// A scope is a query too, and one that does not compile makes the extractor
// it gates unrunnable.
//
// Checked after installing, because `label:money` names an annotator and the
// compiler rejects a label nobody has defined — which makes this a test that
// the pack is self-consistent, not merely well-formed.
func TestStarterScopesAreValidQueries(t *testing.T) {
	openAnnotateFixture(t)
	if _, _, err := InstallStarters(nil); err != nil {
		t.Fatal(err)
	}

	for _, s := range Starters {
		if s.Scope == "" {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			if err := store.ValidateQuery(s.Scope); err != nil {
				t.Errorf("%s scope %q: %v", s.Name, s.Scope, err)
			}
		})
	}
}

// A span extractor scores at most twelve labels at once, and its labels are
// its schema's field names — so a starter with a wider schema is one that
// cannot run.
func TestStarterSchemasFitTheEngine(t *testing.T) {
	for _, s := range Starters {
		if s.Engine != store.EngineGLiNER {
			continue
		}
		if len(s.Fields) == 0 {
			t.Errorf("%s is an extractor with no fields, so it looks for nothing", s.Name)
		}
		if len(s.Fields) > gliner.MaxLabels {
			t.Errorf("%s has %d fields; the engine scores at most %d",
				s.Name, len(s.Fields), gliner.MaxLabels)
		}
		blob, err := json.Marshal(s.Fields)
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		probe := &store.Annotator{SchemaJSON: string(blob)}
		if len(probe.Labels()) != len(s.Fields) {
			t.Errorf("%s: %d fields became %d labels", s.Name, len(s.Fields), len(probe.Labels()))
		}
	}
}

// A span extractor cannot answer yes or no about a message, so a starter that
// asked it to would be refused at creation.
func TestStartersUseAnEngineThatCanDoTheirKind(t *testing.T) {
	for _, s := range Starters {
		switch {
		case s.Kind == store.KindLabel && s.Engine != store.EngineRule:
			t.Errorf("%s is a label on the %s engine; labels here should be free", s.Name, s.Engine)
		case s.Kind == store.KindExtract && s.Engine == store.EngineRule:
			t.Errorf("%s is an extractor on the rule engine, which cannot extract", s.Name)
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

// Installing creates; it does not run. Six extractors over a mailbox is an
// hour of CPU on a small one, which is not a thing to begin by accident.
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
			t.Errorf("%s has trigger %q, want manual — a pack must not start itself",
				name, a.Trigger)
		}
		p, err := store.Progress(a, "")
		if err != nil {
			t.Fatal(err)
		}
		if p.Evaluated != 0 {
			t.Errorf("%s has already evaluated %d messages", name, p.Evaluated)
		}
	}
}

// The scope is carried, not merely suggested: an extractor that needs
// narrowing should not depend on whoever runs it remembering to say so.
func TestInstallCarriesTheScope(t *testing.T) {
	openAnnotateFixture(t)
	if _, _, err := InstallStarters(nil); err != nil {
		t.Fatal(err)
	}

	for _, s := range Starters {
		if s.Scope == "" {
			continue
		}
		a, err := store.GetAnnotator(s.Name)
		if err != nil || a == nil {
			t.Fatalf("%s missing", s.Name)
		}
		if a.Scope != s.Scope {
			t.Errorf("%s scope = %q, want %q", s.Name, a.Scope, s.Scope)
		}
	}
}

// Never overwrite: an annotator with this name may have been edited, and its
// results belong to whoever edited it.
func TestInstallLeavesExistingAnnotatorsAlone(t *testing.T) {
	openAnnotateFixture(t)

	mine := &store.Annotator{
		Name: "receipts", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Instructions: "my own wording", SchemaJSON: `{"total":"x"}`,
	}
	if err := store.SaveAnnotator(mine); err != nil {
		t.Fatal(err)
	}

	_, skipped, err := InstallStarters(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range skipped {
		if n == "receipts" {
			found = true
		}
	}
	if !found {
		t.Errorf("receipts was not reported as skipped: %v", skipped)
	}

	after, _ := store.GetAnnotator("receipts")
	if after.Instructions != "my own wording" {
		t.Errorf("an existing annotator was overwritten: %q", after.Instructions)
	}
}

func TestInstallOnlyWhatWasAskedFor(t *testing.T) {
	openAnnotateFixture(t)

	made, _, err := InstallStarters([]string{"money"})
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0] != "money" {
		t.Fatalf("created %v, want just money", made)
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

// The floor is documented once for the pack rather than rediscovered six
// times, and it is a read-time question, not a knob on the annotator.
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
