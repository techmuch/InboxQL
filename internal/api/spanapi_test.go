package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

func score(f float64) *float64 { return &f }

// joining the segments must give back the source exactly. If it does not, the
// viewer is showing something that is not the message.
func assertLossless(t *testing.T, text string, got MarkedField) {
	t.Helper()
	var b strings.Builder
	for _, s := range got.Segments {
		b.WriteString(s.Text)
	}
	if b.String() != text {
		t.Errorf("segments rejoin to %q, want %q", b.String(), text)
	}
}

func TestSegmentMarksTheRightCharacters(t *testing.T) {
	text := "Total $80.44 at Blue Moon Pizza"
	got := segment(text, []span{
		{field: "body", start: 6, end: 12, label: "amount", score: score(0.82)},
		{field: "body", start: 16, end: 31, label: "merchant", score: score(0.99)},
	})

	assertLossless(t, text, got)
	if got.Marks != 2 {
		t.Fatalf("got %d marks, want 2", got.Marks)
	}

	var marked []string
	for _, s := range got.Segments {
		if s.Label != "" {
			marked = append(marked, s.Label+"="+s.Text)
		}
	}
	want := []string{"amount=$80.44", "merchant=Blue Moon Pizza"}
	for i := range want {
		if marked[i] != want[i] {
			t.Errorf("mark %d = %q, want %q", i, marked[i], want[i])
		}
	}
}

// The reason the server does the slicing at all. A curly apostrophe is three
// bytes and one JavaScript character; slicing by the wrong one lands short,
// and the result looks like a broken decoder rather than an encoding mistake.
func TestSegmentIsByteAccurateThroughMultibyteText(t *testing.T) {
	// "Sam’s Club" — the apostrophe is U+2019, three bytes in UTF-8.
	text := "Receipt from Sam’s Club today"
	start := strings.Index(text, "Sam")
	end := start + len("Sam’s Club")

	got := segment(text, []span{
		{field: "body", start: start, end: end, label: "merchant", score: score(0.96)},
	})

	assertLossless(t, text, got)
	for _, s := range got.Segments {
		if s.Label == "merchant" && s.Text != "Sam’s Club" {
			t.Errorf("marked %q, want %q", s.Text, "Sam’s Club")
		}
	}
}

func TestSegmentWithoutSpans(t *testing.T) {
	text := "nothing was extracted here"
	got := segment(text, nil)

	assertLossless(t, text, got)
	if got.Marks != 0 {
		t.Errorf("got %d marks, want 0", got.Marks)
	}
	if len(got.Segments) != 1 || got.Segments[0].Label != "" {
		t.Errorf("got %+v, want one unmarked run", got.Segments)
	}
}

func TestSegmentOfEmptyText(t *testing.T) {
	got := segment("", []span{{start: 0, end: 4, label: "amount"}})
	if len(got.Segments) != 0 || got.Marks != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

// Two annotators over one message can collide. The higher-scoring span wins
// rather than nesting marks the renderer would have to unpick.
func TestSegmentResolvesOverlapsByScore(t *testing.T) {
	text := "pay Coolray Heating today"
	got := segment(text, []span{
		{field: "body", start: 4, end: 19, label: "merchant", score: score(0.9)},
		{field: "body", start: 4, end: 11, label: "vendor", score: score(0.6)},
	})

	assertLossless(t, text, got)
	if got.Marks != 1 {
		t.Fatalf("got %d marks, want 1 — the loser should be dropped", got.Marks)
	}
	for _, s := range got.Segments {
		if s.Label != "" && s.Text != "Coolray Heating" {
			t.Errorf("kept %q (%s), want the higher-scoring span", s.Text, s.Label)
		}
	}
}

// An offset past the end means the body changed under a stored annotation.
// An unmarked message is a better answer than a panicking viewer.
func TestSegmentSurvivesStaleOffsets(t *testing.T) {
	text := "short"
	got := segment(text, []span{
		{field: "body", start: 2, end: 900, label: "amount", score: score(0.8)},
		{field: "body", start: 400, end: 410, label: "merchant", score: score(0.8)},
	})

	assertLossless(t, text, got)
	for _, s := range got.Segments {
		if s.Label != "" && !strings.Contains(text, s.Text) {
			t.Errorf("marked %q, which is not in the source", s.Text)
		}
	}
}

func TestSpanOfReadsASpanRecord(t *testing.T) {
	a := &store.Annotation{
		Status: store.StatusOK, Source: store.SourceLLM, Confidence: score(0.77),
		DataJSON: `{"amount":"$80.44","field":"body","start":6,"end":12}`,
	}
	got, ok := spanOf(a, "receipts")
	if !ok {
		t.Fatal("a span record was not recognised as one")
	}
	if got.label != "amount" || got.field != "body" || got.start != 6 || got.end != 12 {
		t.Errorf("got %+v", got)
	}
	if got.annotator != "receipts" {
		t.Errorf("annotator = %q", got.annotator)
	}
}

// A label, an empty result and a failure carry no offsets. They are reported
// separately rather than guessed at.
func TestSpanOfRejectsWhatIsNotASpan(t *testing.T) {
	for _, tc := range []struct {
		name string
		ann  *store.Annotation
	}{
		{"a label", &store.Annotation{Status: store.StatusOK, DataJSON: `{"matched":true}`}},
		{"no match", &store.Annotation{Status: store.StatusEmpty, DataJSON: `{}`}},
		{"a failure", &store.Annotation{Status: store.StatusFailed, DataJSON: `{}`}},
		{"no field", &store.Annotation{Status: store.StatusOK, DataJSON: `{"amount":"x","start":1,"end":2}`}},
		{"backwards", &store.Annotation{Status: store.StatusOK, DataJSON: `{"amount":"x","field":"body","start":9,"end":2}`}},
		{"not json", &store.Annotation{Status: store.StatusOK, DataJSON: `{`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := spanOf(tc.ann, "x"); ok {
				t.Error("was read as a span")
			}
		})
	}
}

// The wire shape is what the client depends on, so it is asserted rather than
// assumed: a run with no label is ordinary text.
func TestSegmentMarshalsWithoutEmptyFields(t *testing.T) {
	got := segment("a $5 b", []span{{start: 2, end: 4, label: "amount", score: score(0.5)}})
	blob, err := json.Marshal(got.Segments[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "label") || strings.Contains(string(blob), "score") {
		t.Errorf("an unmarked run serialised as %s", blob)
	}
}
