package store

import (
	"testing"
)

func mustAnnotator(t *testing.T, a *Annotator) *Annotator {
	t.Helper()
	if err := SaveAnnotator(a); err != nil {
		t.Fatalf("SaveAnnotator(%s): %v", a.Name, err)
	}
	got, err := GetAnnotator(a.Name)
	if err != nil || got == nil {
		t.Fatalf("GetAnnotator(%s): %v", a.Name, err)
	}
	return got
}

func TestRuleAnnotatorAppliesToTheWholeMailbox(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "acme", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@acme.com",
	})

	matched, empty, err := ApplyRule(a, "")
	if err != nil {
		t.Fatalf("ApplyRule: %v", err)
	}
	if matched != 3 || empty != 2 {
		t.Fatalf("ApplyRule matched %d empty %d, want 3 and 2", matched, empty)
	}

	eq(t, ids(t, "label:acme"), "m1", "m2", "m5")
	eq(t, ids(t, "-label:acme"), "m3", "m4")
	eq(t, ids(t, "unlabeled:acme") /* none: the rule covered everything */)
}

// The invariant that makes negation over labels honest.
//
// A label is three-valued, so the positive and negative sets do NOT partition
// the mailbox on their own — the unevaluated remainder is the third part. If
// this ever becomes a two-way split, `-label:x` has started quietly claiming
// that every message nobody looked at is a negative result.
func TestLabelsArePartitionedThreeWays(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "acme", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@acme.com",
	})

	// Deliberately partial: only messages before the 4th are evaluated, so m4
	// and m5 stay unevaluated.
	if _, _, err := ApplyRule(a, "before:2026-03-04"); err != nil {
		t.Fatalf("ApplyRule: %v", err)
	}

	total := countOf(t, "")
	yes := countOf(t, "label:acme")
	no := countOf(t, "-label:acme")
	never := countOf(t, "unlabeled:acme")

	if yes+no+never != total {
		t.Errorf("%d matched + %d not-matched + %d unevaluated = %d, want %d",
			yes, no, never, yes+no+never, total)
	}
	if never == 0 {
		t.Fatal("fixture did not leave anything unevaluated; the test proves nothing")
	}

	// The specific failure this guards: -label must not sweep in the
	// unevaluated remainder.
	eq(t, ids(t, "label:acme"), "m1", "m2")
	eq(t, ids(t, "-label:acme"), "m3")
	eq(t, ids(t, "unlabeled:acme"), "m4", "m5")

	// And the loose reading is still sayable.
	eq(t, ids(t, "-label:acme OR unlabeled:acme"), "m3", "m4", "m5")
}

// Editing the instruction bumps the version, which must make earlier results
// stop answering queries — otherwise a query silently mixes two different
// definitions of the same label.
func TestVersionBumpInvalidatesEarlierResults(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "target", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@acme.com",
	})
	if _, _, err := ApplyRule(a, ""); err != nil {
		t.Fatalf("ApplyRule: %v", err)
	}
	eq(t, ids(t, "label:target"), "m1", "m2", "m5")

	// Same name, different rule.
	updated := mustAnnotator(t, &Annotator{
		Name: "target", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@stripe.com",
	})
	if updated.Version != 2 {
		t.Fatalf("version = %d after editing instructions, want 2", updated.Version)
	}

	// Nothing has been evaluated at v2 yet, so the label matches nothing and
	// everything is unevaluated — rather than still returning the v1 answers.
	eq(t, ids(t, "label:target"))
	if n := countOf(t, "unlabeled:target"); n != 5 {
		t.Errorf("unlabeled:target = %d, want 5 after a version bump", n)
	}

	if _, _, err := ApplyRule(updated, ""); err != nil {
		t.Fatalf("ApplyRule v2: %v", err)
	}
	eq(t, ids(t, "label:target"), "m3")
}

// A person's ruling is the one thing a re-run must not destroy: it is both the
// user's work and the only evidence for whether a prompt change helped.
func TestHumanCorrectionsSurviveReruns(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "billing", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@stripe.com",
	})
	if _, _, err := ApplyRule(a, ""); err != nil {
		t.Fatalf("ApplyRule: %v", err)
	}
	eq(t, ids(t, "label:billing"), "m3")

	// The rule missed m1, which is an invoice from a different sender.
	if err := SetHumanAnnotation("billing", "m1", true, nil); err != nil {
		t.Fatalf("SetHumanAnnotation: %v", err)
	}
	eq(t, ids(t, "label:billing"), "m1", "m3")

	// Re-running the same rule must not undo it.
	if _, _, err := ApplyRule(a, ""); err != nil {
		t.Fatalf("ApplyRule again: %v", err)
	}
	eq(t, ids(t, "label:billing"), "m1", "m3")

	// Nor must bumping the version.
	updated := mustAnnotator(t, &Annotator{
		Name: "billing", Kind: KindLabel, Engine: EngineRule,
		Instructions: "from:*@stripe.com OR subject:receipt",
	})
	if _, _, err := ApplyRule(updated, ""); err != nil {
		t.Fatalf("ApplyRule v2: %v", err)
	}
	eq(t, ids(t, "label:billing"), "m1", "m3")

	progress, err := Progress(updated, "")
	if err != nil {
		t.Fatalf("Progress: %v", err)
	}
	if progress.Human != 1 {
		t.Errorf("Progress.Human = %d, want 1", progress.Human)
	}
}

// A message a human has ruled on is not pending: re-asking a model about it
// spends tokens to produce an answer that will be ignored.
func TestPendingSkipsHumanRuledMessages(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "triage", Kind: KindLabel, Engine: EngineLLM,
		Instructions: "Is this urgent?",
	})

	pending, err := PendingMessages(a, "", 100)
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(pending) != 5 {
		t.Fatalf("pending = %d, want 5", len(pending))
	}

	if err := SetHumanAnnotation("triage", "m1", true, nil); err != nil {
		t.Fatalf("SetHumanAnnotation: %v", err)
	}
	pending, err = PendingMessages(a, "", 100)
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(pending) != 4 {
		t.Errorf("pending = %d after a correction, want 4", len(pending))
	}
	for _, m := range pending {
		if m.ID == "m1" {
			t.Error("m1 is still pending despite a human ruling")
		}
	}
}

func TestConfidenceFilter(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "guess", Kind: KindLabel, Engine: EngineLLM, Instructions: "maybe",
	})

	high, low := 0.95, 0.4
	if err := SaveAnnotations(a.ID, a.Version, "m1", []*Annotation{
		{Status: StatusOK, Source: SourceLLM, Confidence: &high},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}
	if err := SaveAnnotations(a.ID, a.Version, "m2", []*Annotation{
		{Status: StatusOK, Source: SourceLLM, Confidence: &low},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}

	eq(t, ids(t, "label:guess"), "m1", "m2")
	eq(t, ids(t, "label:guess@0.9"), "m1")
	eq(t, ids(t, "conf>0.9"), "m1")
}

// One message can yield several records — a digest with a bar per day — and
// the series has to keep them all.
func TestExtractionSeriesUsesItsOwnTimeField(t *testing.T) {
	openQueryFixture(t)

	a := mustAnnotator(t, &Annotator{
		Name: "metrics", Kind: KindExtract, Engine: EngineLLM,
		Instructions: "Extract the signup counts.",
		// The digest arrived on the 4th but reports the preceding days, so the
		// series must be keyed on the extracted date, not the message date.
		SchemaJSON: `{"timeField":"day"}`,
	})

	if err := SaveAnnotations(a.ID, a.Version, "m4", []*Annotation{
		{Seq: 0, Status: StatusOK, Source: SourceLLM, DataJSON: `{"day":"2026-01-05","signups":100}`},
		{Seq: 1, Status: StatusOK, Source: SourceLLM, DataJSON: `{"day":"2026-01-06","signups":150}`},
		{Seq: 2, Status: StatusOK, Source: SourceLLM, DataJSON: `{"day":"2026-02-02","signups":50}`},
	}); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}

	res, err := RunQuery("| extract metrics | sum signups by month", 0, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	// Keyed on the extracted day: January holds 250, February 50. Keyed on the
	// message date every record would land in 2026-03 instead.
	if got["2026-01"] != 250 || got["2026-02"] != 50 {
		t.Errorf("sum signups by month = %v, want 2026-01:250 2026-02:50", got)
	}
	if _, shifted := got["2026-03"]; shifted {
		t.Error("records were bucketed by the message date rather than their own")
	}

	eq(t, ids(t, "extract:metrics"), "m4")
	eq(t, ids(t, "extract:metrics.signups>120"), "m4")
	eq(t, ids(t, "extract:metrics.signups>500"))
}

func TestExtractorsCannotBeRules(t *testing.T) {
	openQueryFixture(t)

	a := &Annotator{
		Name: "bad", Kind: KindExtract, Engine: EngineRule,
		Instructions: "from:x", SchemaJSON: `{"not":`,
	}
	if err := SaveAnnotator(a); err == nil {
		t.Error("SaveAnnotator accepted an invalid schema")
	}
}

// A misspelled annotator name is indistinguishable from a real negative
// result: `label:invoces` would return nothing, which reads as "no invoices"
// rather than "no such annotator". Every other unknown name in the language is
// an error, and these are user-defined so they are the easiest to get wrong.
func TestUnknownAnnotatorNamesAreRejected(t *testing.T) {
	openQueryFixture(t)

	mustAnnotator(t, &Annotator{
		Name: "billing", Kind: KindLabel, Engine: EngineRule, Instructions: "from:x",
	})

	for _, q := range []string{
		"label:billling",
		"unlabeled:nosuch",
		"extract:nosuch",
		"extract:nosuch.field>1",
		"| extract nosuch | sum amount by month",
	} {
		if _, err := RunQuery(q, 10, 0); err == nil {
			t.Errorf("RunQuery(%q) succeeded, want an error naming the annotator", q)
		}
	}

	// The real one still works.
	if _, err := RunQuery("label:billing", 10, 0); err != nil {
		t.Errorf("RunQuery on an existing annotator failed: %v", err)
	}
}
