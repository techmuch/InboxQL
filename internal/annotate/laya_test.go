package annotate

import "testing"

// # The forward header block
//
// A forwarded message opens with the quoted headers of the message inside it.
// On a mailbox of forwards that is 150–250 characters before any content, in a
// window of about 200 tokens — so it is both noise and most of the budget.
//
// It is worse than noise. Measured on one real order confirmation, feeding the
// block dropped the model from 0.74 to 0.002 on "is this a purchase": a
// confident, wrong "no" about a message whose subject line reads "Order
// Confirmation". The quoted addresses belong to someone else, which is
// evidence against the question being asked, and there is a lot of it next to
// the content.
func TestStripQuotedHeadersRemovesAForwardBlock(t *testing.T) {
	body := "---------- Forwarded message ---------\r\n" +
		"From: <no-reply@crows.org>\r\n" +
		"Date: Tue, Sep 24, 2024 at 1:49 PM\r\n" +
		"Subject: Order Confirmation (#109870)\r\n" +
		"To: <someone@example.edu>\r\n" +
		"Cc: <other@example.com>\r\n" +
		"\r\n" +
		"Order Confirmation\r\nOrder #: 109870\r\nPayment Method: Visa"

	got := stripQuotedHeaders(body)
	if want := "Order Confirmation\r\nOrder #: 109870\r\nPayment Method: Visa"; got != want {
		t.Errorf("got %q,\nwant %q", got, want)
	}
}

// The content must survive. A receipt's body is full of lines that look like
// headers — "Order #: 109870", "Payment Method: Visa" — and stripping anything
// with a colon in it would eat exactly the evidence the question is about.
func TestStripQuotedHeadersKeepsHeaderShapedContent(t *testing.T) {
	body := "Order Confirmation\nOrder #: 109870\nPayment Method: Visa\nTotal: $80.44"
	if got := stripQuotedHeaders(body); got != body {
		t.Errorf("content was stripped:\ngot  %q\nwant %q", got, body)
	}
}

// The sender's own words survive wherever the block sits, and the quoted
// headers go. Both halves matter: the preamble is the only part of a forward
// its sender wrote, and the headers are metadata whether they appear at
// character 0 or after two lines of comment.
func TestStripQuotedHeadersKeepsWhatTheSenderWrote(t *testing.T) {
	body := "Here is what they sent me, see below.\n\n" +
		"I think it is wrong.\n\n" +
		"---------- Forwarded message ---------\nFrom: <a@b.com>\nSubject: hi\n\nthe thing"
	got := stripQuotedHeaders(body)
	want := "Here is what they sent me, see below.\n\nI think it is wrong.\n\nthe thing"
	if got != want {
		t.Errorf("got %q,\nwant %q", got, want)
	}
}

// Prose about headers is not a header block. Without the forward marker
// nothing is stripped, so a message discussing a From: line keeps it.
func TestStripQuotedHeadersIgnoresProseAboutHeaders(t *testing.T) {
	body := "The From: line on that bounce was wrong.\nSubject: was fine though."
	if got := stripQuotedHeaders(body); got != body {
		t.Errorf("prose was stripped: %q", got)
	}
}

// A preamble before the forward is the sender's own words, and keeping them is
// the point: "Thanks, Government!" above a USPS order is the human half.
func TestStripQuotedHeadersKeepsAPreamble(t *testing.T) {
	body := "Thanks, Government!\n\n" +
		"---------- Forwarded message ---------\nFrom: <usps@example.com>\nSubject: Order\n\nOrder #: HA000"
	got := stripQuotedHeaders(body)
	want := "Thanks, Government!\n\nOrder #: HA000"
	if got != want {
		t.Errorf("got %q,\nwant %q", got, want)
	}
}

func TestStripQuotedHeadersLeavesAnOrdinaryBodyAlone(t *testing.T) {
	body := "Hi David,\n\nCan we meet on Tuesday?\n\nThanks"
	if got := stripQuotedHeaders(body); got != body {
		t.Errorf("an ordinary body was altered: %q", got)
	}
}
