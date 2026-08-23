package store

import "testing"

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
