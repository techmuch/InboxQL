package message

import (
	"strings"
	"testing"
)

func TestCleanBodyRemovesQuotedMaterial(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{
			name: "gmail attribution",
			body: "Sounds good, let's do Friday at noon.\n\n" +
				"On Mon, 3 Mar 2026 at 10:04, Alice <alice@acme.com> wrote:\n" +
				"> Are you free this week? I was thinking we could go over the\n" +
				"> quarterly numbers together.\n",
			want: "Sounds good, let's do Friday at noon.",
		},
		{
			name: "forwarded block",
			body: "Thought you would want to see this one about the roof.\n\n" +
				"---------- Forwarded message ---------\n" +
				"From: Contractor <bob@roofing.test>\n" +
				"Subject: Your estimate\n\n" +
				"The estimate for the roof replacement is $14,000.\n",
			want: "Thought you would want to see this one about the roof.",
		},
		{
			name: "signature delimiter",
			body: "Here is the invoice you asked for, due at the end of the month.\n\n" +
				"-- \nAlice Smith\nAcme Corp\n+1 555 0100\n",
			want: "Here is the invoice you asked for, due at the end of the month.",
		},
		{
			name: "outlook header block",
			body: "Approved, please proceed with the order as described below.\n\n" +
				"From: Bob <bob@acme.com>\nSent: Monday\nTo: Alice\nSubject: Order\n\n" +
				"Can we order the replacement units this week?\n",
			want: "Approved, please proceed with the order as described below.",
		},
	}
	for _, c := range cases {
		if got := CleanBody(c.body); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// A message that is only a forward still has to be about something. Returning
// nothing would make it unembeddable and unreadable to an extractor.
func TestCleanBodyKeepsTheQuoteWhenItIsAllThereIs(t *testing.T) {
	body := "FYI\n\n---------- Forwarded message ---------\n" +
		"From: Bank <no-reply@bank.test>\n\n" +
		"Your statement for March is ready to view in online banking.\n"
	got := CleanBody(body)
	if !strings.Contains(got, "statement for March") {
		t.Errorf("a forward with no commentary lost its content: %q", got)
	}
}

// Cutting at the first delimiter would discard text written above a quoted
// message that carries its own.
func TestCleanBodyCutsAtTheLastSignature(t *testing.T) {
	body := "The contract is attached and signed, we are good to go.\n\n" +
		"-- \nAlice\n"
	if got := CleanBody(body); !strings.HasPrefix(got, "The contract is attached") {
		t.Errorf("got %q", got)
	}
}

// The hashed body is untouched, or every message changes identity and stops
// deduplicating against the copy already stored.
func TestCleanBodyDoesNotAffectHashing(t *testing.T) {
	m := &Message{Body: "Hello\n\nOn Mon someone wrote:\n> quoted\n"}
	before := HashableBody(m)
	_ = CleanBody(m.Body)
	if HashableBody(m) != before {
		t.Error("CleanBody changed what gets hashed")
	}
	if before != m.Body {
		t.Error("HashableBody no longer returns the raw body")
	}
}
