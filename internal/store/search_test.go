package store

import (
	"encoding/json"
	"testing"
)

// Thread grouping is subject-based, so subject normalisation is what decides
// whether a reply lands in the right conversation.
func TestNormalizeSubject(t *testing.T) {
	cases := map[string]string{
		"Q3 budget review":              "q3 budget review",
		"Re: Q3 budget review":          "q3 budget review",
		"RE: Q3 budget review":          "q3 budget review",
		"Fwd: Q3 budget review":         "q3 budget review",
		"Re: Re: Fwd: Q3 budget review": "q3 budget review",
		"  Re:   Q3 budget review  ":    "q3 budget review",
		// Non-English clients prefix differently; AW is German, SV Scandinavian.
		"AW: Q3 budget review": "q3 budget review",
		"SV: Q3 budget review": "q3 budget review",
		"":                     "",
	}

	for in, want := range cases {
		if got := NormalizeSubject(in); got != want {
			t.Errorf("NormalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

// "Reply" as a word must not be mistaken for the "Re:" prefix.
func TestNormalizeSubjectDoesNotOverTrim(t *testing.T) {
	if got := NormalizeSubject("Reply guidelines"); got != "reply guidelines" {
		t.Errorf("got %q, want %q", got, "reply guidelines")
	}
	if got := NormalizeSubject("Research notes"); got != "research notes" {
		t.Errorf("got %q, want %q", got, "research notes")
	}
}

// A timestamp stored in the wrong unit decodes to a year outside what JSON
// can represent. encoding/json marshals the whole value before writing a byte,
// so one such row made an entire query return 200 with an empty body — an
// inbox that looks empty rather than an error anyone could act on.
func TestOneUnrepresentableTimestampDoesNotBlankTheResult(t *testing.T) {
	openQueryFixture(t)

	// A zero time.Time written as microseconds, which is how this reached a
	// real database.
	const zeroTimeAsMicros = -62135596800000000
	if _, err := db.Exec("UPDATE messages SET internal_date = ? WHERE id = 'm1'",
		int64(zeroTimeAsMicros)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := RunQuery("in:messages", 100, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Messages) != 5 {
		t.Fatalf("got %d messages, want all 5", len(res.Messages))
	}

	// The whole result has to survive encoding, not just the good rows.
	if _, err := json.Marshal(res); err != nil {
		t.Fatalf("one bad timestamp made the whole result unencodable: %v", err)
	}

	// The bad row is still there, with a date that says "unknown" rather than
	// a fabricated one.
	for _, m := range res.Messages {
		if m.ID != "m1" {
			continue
		}
		if !m.InternalDate.IsZero() {
			t.Errorf("m1's unreadable timestamp became %v, want the zero time", m.InternalDate)
		}
		if m.Date.IsZero() {
			t.Error("m1's good Date was clamped too")
		}
	}
}
