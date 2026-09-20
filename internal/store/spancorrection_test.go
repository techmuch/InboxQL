package store

import (
	"testing"
)

func spanExtractor(t *testing.T, name string) *Annotator {
	t.Helper()
	return mustAnnotator(t, &Annotator{
		Name: name, Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find the money", SchemaJSON: `{"amount":"x","merchant":"y"}`,
	})
}

// machineSpans stands in for a run, writing rows the way runGLiNER does.
func machineSpans(t *testing.T, a *Annotator, messageID string, n int) {
	t.Helper()
	anns := make([]*Annotation, 0, n)
	for i := 0; i < n; i++ {
		conf := 0.7
		anns = append(anns, &Annotation{
			Seq: i, Status: StatusOK, Source: SourceLLM, Confidence: &conf,
			DataJSON: `{"amount":"$1","field":"body","start":0,"end":2}`,
			Model:    "gliner test",
		})
	}
	if err := SaveAnnotations(a.ID, a.Version, messageID, anns); err != nil {
		t.Fatalf("SaveAnnotations: %v", err)
	}
}

func humanRows(t *testing.T, a *Annotator, messageID string) []*Annotation {
	t.Helper()
	all, err := ListAnnotations(messageID)
	if err != nil {
		t.Fatalf("ListAnnotations: %v", err)
	}
	var out []*Annotation
	for _, x := range all {
		if x.AnnotatorID == a.ID && x.Source == SourceHuman {
			out = append(out, x)
		}
	}
	return out
}

func TestCorrectionReplacesTheWholeSet(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)

	machineSpans(t, a, msg, 5)

	// A person says only two of the five are right.
	err := SetHumanSpans("money", msg, []SpanCorrection{
		{Label: "amount", Field: "body", Start: 0, End: 6, Text: "$80.44"},
		{Label: "merchant", Field: "subject", Start: 2, End: 9, Text: "Acme Co"},
	})
	if err != nil {
		t.Fatalf("SetHumanSpans: %v", err)
	}

	all, err := ListAnnotations(msg)
	if err != nil {
		t.Fatal(err)
	}
	mine := 0
	for _, x := range all {
		if x.AnnotatorID != a.ID {
			continue
		}
		mine++
		// A ruling is about the whole set, so no machine row may survive
		// beside it — a reader could not tell which set was being asserted.
		if x.Source != SourceHuman {
			t.Errorf("a %s row survived the correction: %s", x.Source, x.DataJSON)
		}
		if x.Confidence == nil || *x.Confidence != 1.0 {
			t.Errorf("human row has confidence %v, want 1.0", x.Confidence)
		}
	}
	if mine != 2 {
		t.Errorf("got %d rows for this annotator, want the 2 that were asserted", mine)
	}
}

// The property the whole design rests on: a re-run must not overwrite a
// person's ruling. SaveAnnotations deletes WHERE source != 'human'.
func TestCorrectionSurvivesAReRun(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)

	machineSpans(t, a, msg, 3)
	if err := SetHumanSpans("money", msg, []SpanCorrection{
		{Label: "amount", Field: "body", Start: 0, End: 6, Text: "$80.44"},
	}); err != nil {
		t.Fatal(err)
	}

	// The annotator runs again and finds four things.
	machineSpans(t, a, msg, 4)

	human := humanRows(t, a, msg)
	if len(human) != 1 {
		t.Fatalf("got %d human rows after a re-run, want 1", len(human))
	}
	if got := human[0].Data()["amount"]; got != "$80.44" {
		t.Errorf("the human ruling became %v after a re-run", got)
	}
}

// A false positive needs a way to say "none of these are right", and that is
// different from never having ruled.
func TestCorrectingToNothingIsAnAssertion(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)

	machineSpans(t, a, msg, 3)
	if err := SetHumanSpans("money", msg, nil); err != nil {
		t.Fatal(err)
	}

	human := humanRows(t, a, msg)
	if len(human) != 1 {
		t.Fatalf("got %d rows, want one recording the ruling", len(human))
	}
	// "Looked, nothing here" — the same three-valued distinction the machine
	// records, so -extract:x stays honest.
	if human[0].Status != StatusEmpty {
		t.Errorf("status = %q, want %q", human[0].Status, StatusEmpty)
	}
}

func TestClearingARulingIsNotTheSameAsCorrectingToNothing(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)

	if err := SetHumanSpans("money", msg, nil); err != nil {
		t.Fatal(err)
	}
	if len(humanRows(t, a, msg)) != 1 {
		t.Fatal("the ruling was not recorded")
	}

	if err := ClearHumanSpans("money", msg); err != nil {
		t.Fatal(err)
	}
	if n := len(humanRows(t, a, msg)); n != 0 {
		t.Errorf("got %d human rows after clearing, want none", n)
	}
}

func TestCorrectionRejectsWhatIsNotASpan(t *testing.T) {
	openQueryFixture(t)
	spanExtractor(t, "money")
	msg := anyMessageID(t)

	for _, tc := range []struct {
		name string
		span SpanCorrection
	}{
		{"no label", SpanCorrection{Field: "body", Start: 0, End: 2}},
		{"unknown field", SpanCorrection{Label: "amount", Field: "attachment", Start: 0, End: 2}},
		{"backwards", SpanCorrection{Label: "amount", Field: "body", Start: 9, End: 2}},
		{"empty", SpanCorrection{Label: "amount", Field: "body", Start: 4, End: 4}},
		{"negative", SpanCorrection{Label: "amount", Field: "body", Start: -1, End: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := SetHumanSpans("money", msg, []SpanCorrection{tc.span}); err == nil {
				t.Error("was accepted")
			}
		})
	}
}

func TestCorrectionNeedsAnAnnotatorThatExists(t *testing.T) {
	openQueryFixture(t)
	if err := SetHumanSpans("nosuch", anyMessageID(t), nil); err == nil {
		t.Error("a correction against an unknown annotator was accepted")
	}
	if err := ClearHumanSpans("nosuch", anyMessageID(t)); err == nil {
		t.Error("clearing an unknown annotator was accepted")
	}
}

// A rejected correction must leave the previous state alone rather than
// half-applying: the validation runs before anything is deleted.
func TestARejectedCorrectionChangesNothing(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)
	machineSpans(t, a, msg, 3)

	err := SetHumanSpans("money", msg, []SpanCorrection{
		{Label: "amount", Field: "body", Start: 0, End: 6, Text: "$80.44"},
		{Label: "", Field: "body", Start: 0, End: 2}, // invalid
	})
	if err == nil {
		t.Fatal("an invalid correction was accepted")
	}

	all, _ := ListAnnotations(msg)
	machine := 0
	for _, x := range all {
		if x.AnnotatorID == a.ID && x.Source != SourceHuman {
			machine++
		}
	}
	if machine != 3 {
		t.Errorf("got %d machine rows after a rejected correction, want the original 3", machine)
	}
}

func anyMessageID(t *testing.T) string {
	t.Helper()
	var id string
	if err := db.QueryRow("SELECT id FROM messages LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("no message in the fixture: %v", err)
	}
	return id
}

// A regression for the bug this work uncovered.
//
// SaveAnnotations deletes `WHERE source != 'human'`, which reads as though a
// correction is safe. It was not: the rows then went in with INSERT OR REPLACE
// against UNIQUE(message_id, annotator_id, annotator_version, seq), so a
// machine result at seq 0 replaced a human one at seq 0 immediately after
// being careful not to delete it. ApplyRule never had the bug — it uses
// INSERT OR IGNORE and an explicit NOT EXISTS — so this covers the other path.
func TestMachineResultsCannotOverwriteARulingAtTheSameSeq(t *testing.T) {
	openQueryFixture(t)
	a := spanExtractor(t, "money")
	msg := anyMessageID(t)

	if err := SetHumanSpans("money", msg, []SpanCorrection{
		{Label: "amount", Field: "body", Start: 0, End: 6, Text: "$80.44"},
	}); err != nil {
		t.Fatal(err)
	}

	// Exactly the collision: a machine row for the same seq the human holds.
	conf := 0.9
	if err := SaveAnnotations(a.ID, a.Version, msg, []*Annotation{{
		Seq: 0, Status: StatusOK, Source: SourceLLM, Confidence: &conf,
		DataJSON: `{"amount":"WRONG","field":"body","start":0,"end":5}`,
	}}); err != nil {
		t.Fatal(err)
	}

	all, err := ListAnnotations(msg)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if x.AnnotatorID != a.ID {
			continue
		}
		if x.Source != SourceHuman {
			t.Errorf("a %s row got in beside the ruling: %s", x.Source, x.DataJSON)
		}
		if got := x.Data()["amount"]; got != "$80.44" {
			t.Errorf("the ruling became %v", got)
		}
	}
}

// A value found inside an attached PDF has to be correctable too, and the
// file is named by content hash rather than by index: a bare index would not
// survive the file arriving again on another message, or the message being
// re-annotated in a different order.
func TestCorrectionAcceptsAnAttachmentField(t *testing.T) {
	openQueryFixture(t)
	spanExtractor(t, "money")
	msg := anyMessageID(t)

	hash := "ab12cd34ef56"
	if err := SetHumanSpans("money", msg, []SpanCorrection{
		{Label: "amount", Field: "attachment:" + hash, Start: 10, End: 16, Text: "$80.44"},
	}); err != nil {
		t.Fatalf("an attachment span was rejected: %v", err)
	}

	all, _ := ListAnnotations(msg)
	found := false
	for _, x := range all {
		if x.Data()["field"] == "attachment:"+hash {
			found = true
		}
	}
	if !found {
		t.Error("the attachment field was not stored")
	}
}

func TestSpanFieldNames(t *testing.T) {
	for _, tc := range []struct {
		field string
		ok    bool
	}{
		{"subject", true},
		{"body", true},
		{"attachment:abc123", true},
		{"attachment:", false},
		{"attachment", false},
		{"header", false},
		{"", false},
	} {
		if got := validSpanField(tc.field); got != tc.ok {
			t.Errorf("validSpanField(%q) = %v, want %v", tc.field, got, tc.ok)
		}
	}
}
