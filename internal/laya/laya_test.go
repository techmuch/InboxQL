package laya

import (
	"math"
	"strings"
	"testing"
)

// The two option strings are the checkpoint's own. The weights were trained
// against these exact words, so a caller supplying its own would be asking a
// different question than the one the model answers — which is why a noul
// takes no options and says so.
func TestNoulOptionsAreTheCheckpointsOwn(t *testing.T) {
	got, err := renderOptions(Question{Kind: "noul", Instructions: "x"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"false: no, the statement does not hold",
		"true: yes, the statement holds",
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, err := renderOptions(Question{
		Kind: "noul", Instructions: "x",
		Options: []Option{{Label: "yes"}, {Label: "no"}},
	}); err == nil {
		t.Error("a noul accepted options of its own")
	}
}

// The order is the answer order, and a description is worth its tokens:
// "billing: invoices, payments and refunds" scores better than "billing".
func TestChoiceOptionsCarryTheirDescriptions(t *testing.T) {
	got, err := renderOptions(Question{
		Kind: "choice", Instructions: "which team",
		Options: []Option{
			{Label: "billing", Describe: "invoices and refunds"},
			{Label: "technical"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "billing: invoices and refunds" {
		t.Errorf("got %q", got[0])
	}
	if got[1] != "technical" {
		t.Errorf("an undescribed option became %q", got[1])
	}
}

// Score options are numbered, because the model is being asked where on a
// scale rather than which of a set — and the numbering is what makes the
// expected level meaningful.
func TestScoreOptionsAreNumbered(t *testing.T) {
	got, err := renderOptions(Question{
		Kind: "score", Instructions: "how urgent",
		Options: []Option{{Label: "calm"}, {Label: "soon"}, {Label: "now"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"level 0: calm", "level 1: soon", "level 2: now"} {
		if got[i] != want {
			t.Errorf("option %d is %q, want %q", i, got[i], want)
		}
	}
}

func TestRejectsAQuestionItCannotAsk(t *testing.T) {
	for _, q := range []Question{
		{Kind: "choice", Instructions: "x", Options: []Option{{Label: "only"}}},
		{Kind: "nonsense", Instructions: "x"},
	} {
		if _, err := renderOptions(q); err == nil {
			t.Errorf("%+v was accepted", q)
		}
	}
}

// Softmax over one logit is 1.0, and over equal logits is uniform. Worth
// pinning because every probability this package reports goes through it.
func TestSoftmax(t *testing.T) {
	if got := softmax([]float64{5}); len(got) != 1 || math.Abs(got[0]-1) > 1e-9 {
		t.Errorf("softmax of one logit = %v, want [1]", got)
	}
	got := softmax([]float64{2, 2, 2})
	for _, v := range got {
		if math.Abs(v-1.0/3) > 1e-9 {
			t.Errorf("equal logits gave %v, want uniform", got)
		}
	}
	// Large values must not overflow to NaN: the max is subtracted first.
	if got := softmax([]float64{1000, 999}); math.IsNaN(got[0]) {
		t.Errorf("large logits overflowed: %v", got)
	}
}

// decide reads the answer out of the marker logits, and for a noul the labels
// are fixed rather than taken from the caller.
func TestDecideReadsTheArgmax(t *testing.T) {
	q := Question{Kind: "noul", Instructions: "x"}
	d := decide(q, []string{"false…", "true…"}, []float64{0.0, 3.0}, 0.25)
	if d.Label != "true" || d.Index != 1 {
		t.Errorf("got %+v, want true at 1", d)
	}
	if d.Probability < 0.9 {
		t.Errorf("probability %v is too low for a 3-nat gap", d.Probability)
	}
	if d.Escalate != 0.25 {
		t.Errorf("escalate = %v, want it reported unchanged", d.Escalate)
	}

	cq := Question{Kind: "choice", Instructions: "x",
		Options: []Option{{Label: "a"}, {Label: "b"}, {Label: "c"}}}
	cd := decide(cq, []string{"a", "b", "c"}, []float64{1, 5, 2}, 0)
	if cd.Label != "b" {
		t.Errorf("got %q, want b", cd.Label)
	}
}

// The expected level is the weighted mean, not the argmax: "mostly 2 with some
// 3" is a different answer from "certainly 2", and the mean is the one that
// says so.
func TestScoreLevelIsTheWeightedMean(t *testing.T) {
	q := Question{Kind: "score", Instructions: "x",
		Options: []Option{{Label: "a"}, {Label: "b"}, {Label: "c"}}}
	d := decide(q, []string{"a", "b", "c"}, []float64{0, 0, 0}, 0)
	if math.Abs(d.Level-1.0) > 1e-9 {
		t.Errorf("uniform over three levels gave %v, want 1", d.Level)
	}
}

// A question whose options overrun the head budget must lose tokens from the
// options evenly rather than lose whole options: an option that is not in the
// sequence cannot be chosen, which silently changes the answer set.
func TestEveryOptionKeepsAMarker(t *testing.T) {
	opts := make([]string, 20)
	for i := range opts {
		opts[i] = strings.Repeat("a very long option description ", 6)
	}
	m := &Model{}
	_, markers, err := m.buildSequenceWith(
		func(s string) ([]int, error) {
			// One token per word, which is enough to make the budget bite.
			return make([]int, len(strings.Fields(s))), nil
		},
		"the message", "choice", "which one", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != len(opts) {
		t.Errorf("%d options produced %d markers; one cannot be chosen", len(opts), len(markers))
	}
}
