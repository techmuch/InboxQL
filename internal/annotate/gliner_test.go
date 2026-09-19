package annotate

import (
	"testing"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/message"
)

// The model is shown the subject and the body joined, so its offsets index a
// string nothing else holds. Every record has to say which field it landed in
// and where within that field, or the offsets point into a buffer that does
// not exist anywhere a reader can open.
func TestRecordsResolveOffsetsToAField(t *testing.T) {
	m := &message.Message{
		Subject: "Receipt for $80.44",
		Body:    "Total $80.44\nCheck #319",
	}
	text, subjectLen := spanText(m)

	// Two spans, one in each half, located the way the model would.
	inSubject := gliner.Span{
		Label: "amount", Text: "$80.44",
		Start: 12, End: 18, Score: 0.9,
	}
	bodyAt := subjectLen + 6 // "Total " is 6 bytes into the body
	inBody := gliner.Span{
		Label: "amount", Text: "$80.44",
		Start: bodyAt, End: bodyAt + 6, Score: 0.8,
	}
	if text[inSubject.Start:inSubject.End] != "$80.44" {
		t.Fatalf("test fixture is wrong: subject span is %q", text[inSubject.Start:inSubject.End])
	}
	if text[inBody.Start:inBody.End] != "$80.44" {
		t.Fatalf("test fixture is wrong: body span is %q", text[inBody.Start:inBody.End])
	}

	got := records([]gliner.Span{inSubject, inBody}, subjectLen)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}

	first := got[0].data
	if first["field"] != "subject" {
		t.Errorf("first record is in %v, want subject", first["field"])
	}
	if s, e := first["start"].(int), first["end"].(int); m.Subject[s:e] != "$80.44" {
		t.Errorf("subject[%d:%d] = %q, want $80.44", s, e, m.Subject[s:e])
	}

	second := got[1].data
	if second["field"] != "body" {
		t.Errorf("second record is in %v, want body", second["field"])
	}
	if s, e := second["start"].(int), second["end"].(int); m.Body[s:e] != "$80.44" {
		t.Errorf("body[%d:%d] = %q, want $80.44", s, e, m.Body[s:e])
	}
}

// Each record carries its own span's score, not one number copied across
// everything a single reply mentioned. That is what makes extract:x@0.8 mean
// what a reader assumes it means.
func TestRecordsKeepPerSpanConfidence(t *testing.T) {
	spans := []gliner.Span{
		{Label: "amount", Text: "$675.00", Start: 10, End: 17, Score: 0.905},
		{Label: "account number", Text: "5887", Start: 40, End: 44, Score: 0.511},
	}
	got := records(spans, 0)

	if got[0].score != 0.905 || got[1].score != 0.511 {
		t.Errorf("scores are %v and %v, want 0.905 and 0.511", got[0].score, got[1].score)
	}
	if got[0].data["amount"] != "$675.00" {
		t.Errorf("first record = %v, want the amount under its own label", got[0].data)
	}
	if got[1].data["account number"] != "5887" {
		t.Errorf("second record = %v, want the account number under its own label", got[1].data)
	}
}

func TestSpanTextFallsBackAndReportsTheSplit(t *testing.T) {
	t.Run("no subject", func(t *testing.T) {
		text, n := spanText(&message.Message{Body: "just a body"})
		if text != "just a body" || n != 0 {
			t.Errorf("got %q, %d", text, n)
		}
	})

	t.Run("empty body uses the normalized one", func(t *testing.T) {
		text, n := spanText(&message.Message{
			Subject: "Hi", Body: "   ", NormalizedBody: "the real text",
		})
		if text[n:] != "the real text" {
			t.Errorf("body half is %q, want the normalized body", text[n:])
		}
	})

	t.Run("split lands on the body", func(t *testing.T) {
		m := &message.Message{Subject: "Subject line", Body: "Body text"}
		text, n := spanText(m)
		if text[n:] != m.Body {
			t.Errorf("text[%d:] = %q, want %q", n, text[n:], m.Body)
		}
		if text[:len(m.Subject)] != m.Subject {
			t.Errorf("the subject is not at the front of %q", text)
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
