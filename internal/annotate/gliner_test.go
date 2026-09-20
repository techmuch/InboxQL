package annotate

import (
	"testing"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/message"
)

// Each record says which part of the message it came from and where within
// that part. The model is shown the subject, the body and each readable
// attachment separately, so an offset always indexes something a reader can
// be shown — not a buffer that exists nowhere else.
func TestRecordsCarryTheirFieldAndOffsets(t *testing.T) {
	got := records([]labelled{
		{field: "subject", span: gliner.Span{Label: "amount", Text: "$80.44", Start: 12, End: 18, Score: 0.9}},
		{field: "body", span: gliner.Span{Label: "amount", Text: "$80.44", Start: 6, End: 12, Score: 0.8}},
		{field: "attachment:abc123", span: gliner.Span{Label: "due date", Text: "07/30/24", Start: 40, End: 48, Score: 0.7}},
	})
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}

	for i, want := range []struct {
		field, label, text string
		start, end         int
	}{
		{"subject", "amount", "$80.44", 12, 18},
		{"body", "amount", "$80.44", 6, 12},
		{"attachment:abc123", "due date", "07/30/24", 40, 48},
	} {
		d := got[i].data
		if d["field"] != want.field {
			t.Errorf("record %d field = %v, want %s", i, d["field"], want.field)
		}
		if d[want.label] != want.text {
			t.Errorf("record %d %s = %v, want %s", i, want.label, d[want.label], want.text)
		}
		if d["start"] != want.start || d["end"] != want.end {
			t.Errorf("record %d offsets = %v:%v, want %d:%d", i, d["start"], d["end"], want.start, want.end)
		}
	}
}

// Each record carries its own span's score, not one number copied across
// everything a single reply mentioned. That is what makes extract:x@0.8 mean
// what a reader assumes it means.
func TestRecordsKeepPerSpanConfidence(t *testing.T) {
	got := records([]labelled{
		{field: "body", span: gliner.Span{Label: "amount", Text: "$675.00", Score: 0.905, Start: 10, End: 17}},
		{field: "body", span: gliner.Span{Label: "order number", Text: "5887", Score: 0.511, Start: 40, End: 44}},
	})

	if got[0].score != 0.905 || got[1].score != 0.511 {
		t.Errorf("scores are %v and %v, want 0.905 and 0.511", got[0].score, got[1].score)
	}
	if got[0].data["amount"] != "$675.00" {
		t.Errorf("first record = %v, want the amount under its own label", got[0].data)
	}
}

// The subject and the body are read separately rather than concatenated, so
// neither needs an offset adjustment and an empty one is simply absent.
func TestDocumentsSplitsTheMessage(t *testing.T) {
	// A store has to be open: documents also asks it for the message's
	// readable attachments. These cases have none, which is the point —
	// the message's own halves are what is under test.
	openAnnotateFixture(t)

	t.Run("subject and body", func(t *testing.T) {
		parts := mustDocuments(t, &message.Message{Subject: "Receipt", Body: "Total $5"})
		if len(parts) != 2 {
			t.Fatalf("got %d parts: %+v", len(parts), parts)
		}
		if parts[0].Field != "subject" || parts[0].Text != "Receipt" {
			t.Errorf("first part = %+v", parts[0])
		}
		if parts[1].Field != "body" || parts[1].Text != "Total $5" {
			t.Errorf("second part = %+v", parts[1])
		}
	})

	t.Run("no subject", func(t *testing.T) {
		parts := mustDocuments(t, &message.Message{Body: "just a body"})
		if len(parts) != 1 || parts[0].Field != "body" {
			t.Errorf("got %+v, want the body alone", parts)
		}
	})

	t.Run("empty body falls back to the normalized one", func(t *testing.T) {
		parts := mustDocuments(t, &message.Message{
			Subject: "Hi", Body: "   ", NormalizedBody: "the real text",
		})
		if len(parts) != 2 || parts[1].Text != "the real text" {
			t.Errorf("got %+v", parts)
		}
	})

	t.Run("nothing at all", func(t *testing.T) {
		if parts := mustDocuments(t, &message.Message{}); len(parts) != 0 {
			t.Errorf("got %+v, want nothing to read", parts)
		}
	})
}

// A message longer than the model can read has to say so, or "no amount here"
// and "nobody read the part with the amount in it" look identical.
func TestReachReportsHowFarExtractionGets(t *testing.T) {
	short := "one two three"
	if got := gliner.Reach(short); got != len(short) {
		t.Errorf("Reach(short) = %d, want the whole thing (%d)", got, len(short))
	}

	long := ""
	for i := 0; i < 5000; i++ {
		long += "word "
	}
	got := gliner.Reach(long)
	if got >= len(long) {
		t.Errorf("Reach(long) = %d, want less than the whole %d", got, len(long))
	}
	if got <= 0 {
		t.Errorf("Reach(long) = %d, want a positive prefix", got)
	}
}

func mustDocuments(t *testing.T, m *message.Message) []document {
	t.Helper()
	parts, err := documents(m)
	if err != nil {
		t.Fatalf("documents: %v", err)
	}
	return parts
}
