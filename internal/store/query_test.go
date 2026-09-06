package store

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

// openQueryFixture builds a mailbox with the awkward cases in it: multiple
// recipients, an address that is a substring of another, mail with no
// recipients at all, and a real reply chain.
func openQueryFixture(t *testing.T) {
	t.Helper()

	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)

	if err := SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	day := func(d int) time.Time {
		return time.Date(2026, 3, d, 12, 0, 0, 0, time.Local)
	}

	msgs := []*message.Message{
		{
			ID: "m1", MessageID: "<root@acme.com>",
			From: "Alice <alice@acme.com>", To: []string{"me@example.com", "bob@acme.com"},
			Subject: "Quarterly invoice attached", Body: "Please find the invoice.",
			Date: day(1), Size: 2000, Mailbox: "INBOX",
			Header: []byte("Message-ID: <root@acme.com>\r\nSubject: Quarterly invoice attached"),
		},
		{
			// notalice@ deliberately contains "alice": substring negation must
			// not quietly treat these as the same person.
			ID: "m2", From: "notalice@acme.com", To: []string{"me@example.com"},
			Subject: "Lunch", Body: "Free on Friday?",
			Date: day(2), Size: 500, Mailbox: "INBOX", Flags: []string{`\Seen`},
		},
		{
			ID: "m3", From: "billing@stripe.com", To: []string{"me@example.com"}, Cc: []string{"bob@acme.com"},
			Subject: "Your receipt", Body: "Payment received, 42 USD.",
			Date: day(3), Size: 9000, Mailbox: "INBOX",
		},
		{
			// No recipients at all. The anti-join has to count this as "no
			// recipient is alice" rather than dropping it from both sides.
			ID: "m4", From: "noreply@news.example.org", Subject: "Weekly digest",
			Body: "signups 120", Date: day(4), Size: 100, Mailbox: "INBOX", Flags: []string{`\Seen`},
		},
		{
			// A genuine reply: References points back at m1's Message-ID.
			ID: "m5", MessageID: "<reply@acme.com>",
			From: "bob@acme.com", To: []string{"alice@acme.com", "me@example.com"},
			Subject: "Re: Quarterly invoice attached", Body: "Paid, thanks.",
			Date: day(5), Size: 700, Mailbox: "INBOX",
			Header: []byte("Message-ID: <reply@acme.com>\r\nIn-Reply-To: <root@acme.com>\r\nReferences: <root@acme.com>"),
		},
	}

	for _, m := range msgs {
		m.AccountID = "acct"
		m.ContentHash = m.ID
		m.InternalDate = m.Date
		if m.MessageID == "" {
			// Bracketed, as every mail server sends it and as the importer
			// stores it. A bare id here would hide any mismatch between the
			// stored column and the parsed References chain.
			m.MessageID = "<" + m.ID + "@example.test>"
		}
		if err := SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}
}

func ids(t *testing.T, q string) []string {
	t.Helper()
	res, err := RunQuery(q, 100, 0)
	if err != nil {
		t.Fatalf("RunQuery(%q): %v", q, err)
	}
	if res.Kind != "messages" {
		t.Fatalf("RunQuery(%q): kind = %q, want messages", q, res.Kind)
	}
	out := make([]string, 0, len(res.Messages))
	for _, m := range res.Messages {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out
}

func countOf(t *testing.T, q string) int64 {
	t.Helper()
	n, err := CountQuery(q)
	if err != nil {
		t.Fatalf("CountQuery(%q): %v", q, err)
	}
	return n
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFilterBasics(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, ""), "m1", "m2", "m3", "m4", "m5")
	eq(t, ids(t, "from:stripe"), "m3")
	eq(t, ids(t, "from:=alice@acme.com"), "m1")
	eq(t, ids(t, "from:*@acme.com"), "m1", "m2", "m5")
	eq(t, ids(t, "is:unread"), "m1", "m3", "m5")
	eq(t, ids(t, "larger:1kb"), "m1", "m3")
	eq(t, ids(t, "after:2026-03-04"), "m4", "m5")
	eq(t, ids(t, "before:2026-03-02"), "m1")
	eq(t, ids(t, "on:2026-03-03"), "m3")
	eq(t, ids(t, "account:acct is:unread"), "m1", "m3", "m5")
}

// The distinction the language exists to make: `from:alice` is a substring
// match and so matches notalice@, while `from:=alice@acme.com` does not. If
// these ever return the same set, the match modes have collapsed.
func TestMatchModesAreDistinct(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, "from:alice"), "m1", "m2")
	eq(t, ids(t, "from:=alice@acme.com"), "m1")
	eq(t, ids(t, "-from:=alice@acme.com"), "m2", "m3", "m4", "m5")
}

// Negation over a multi-valued field must mean "no recipient is alice", not
// "some recipient is not alice".
//
// m5 has both alice@ and me@ as recipients. A join-and-compare implementation
// returns it here, because me@ is not alice@. This is the single most likely
// way for the compiler to be subtly wrong, and it looks correct in casual use.
func TestNegationOverRecipientsIsAntiJoin(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, "to:alice@acme.com"), "m5")
	eq(t, ids(t, "-to:alice@acme.com"), "m1", "m2", "m3", "m4")

	// m4 has no recipients at all and must appear in the negative set.
	got := ids(t, "-to:me@example.com")
	eq(t, got, "m4")
}

// For every predicate, the positive and negative sets must partition the
// mailbox exactly. One property test covers every anti-join bug the compiler
// could have, including ones no example test thought to check.
//
// Labels are excluded by design: they are three-valued, and their own
// invariant is asserted in TestLabelsArePartitionedThreeWays.
func TestNegationPartitionsTheMailbox(t *testing.T) {
	openQueryFixture(t)

	total := countOf(t, "")
	if total == 0 {
		t.Fatal("fixture is empty")
	}

	predicates := []string{
		"from:alice",
		"from:=alice@acme.com",
		"from:*@acme.com",
		"to:me@example.com",
		"to:alice@acme.com",
		"cc:bob@acme.com",
		"anyone:bob@acme.com",
		"subject:invoice",
		"body:payment",
		"is:unread",
		"is:starred",
		"is:reply",
		"has:attachment",
		"after:2026-03-03",
		"before:2026-03-03",
		"on:2026-03-01",
		"larger:1kb",
		"smaller:1kb",
		"account:acct",
		"folder:inbox",
		"mailbox:INBOX",
		"invoice",
		"(from:alice OR from:stripe)",
		"(from:*@acme.com is:unread)",
		"thread:m1",
	}

	for _, p := range predicates {
		t.Run(p, func(t *testing.T) {
			pos := countOf(t, p)
			neg := countOf(t, "-("+p+")")
			if pos+neg != total {
				t.Errorf("%s: %d positive + %d negative = %d, want %d",
					p, pos, neg, pos+neg, total)
			}
		})
	}
}

// De Morgan has to hold through the parser as well as the compiler.
func TestDeMorgan(t *testing.T) {
	openQueryFixture(t)

	pairs := [][2]string{
		{"-(from:alice is:unread)", "(-from:alice OR -is:unread)"},
		{"-(from:alice OR from:stripe)", "(-from:alice -from:stripe)"},
		{"--from:alice", "from:alice"},
		{"NOT from:alice", "-from:alice"},
	}
	for _, p := range pairs {
		if a, b := ids(t, p[0]), ids(t, p[1]); fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s = %v, but %s = %v", p[0], a, p[1], b)
		}
	}
}

func TestBooleanComposition(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, "from:alice OR from:stripe"), "m1", "m2", "m3")
	eq(t, ids(t, "from:*@acme.com is:unread"), "m1", "m5")
	eq(t, ids(t, "(from:alice OR from:stripe) -from:notalice"), "m1", "m3")
	eq(t, ids(t, "-(from:*@acme.com)"), "m3", "m4")
}

// Free text goes through FTS5, so a value with punctuation in it has to be
// quoted into a phrase rather than handed to the index as query syntax.
func TestFullTextSearch(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, "invoice"), "m1", "m5")
	eq(t, ids(t, "subject:lunch"), "m2")
	eq(t, ids(t, "body:payment"), "m3")
	eq(t, ids(t, `"payment received"`), "m3")

	// Punctuation that FTS5 would otherwise read as operators.
	for _, q := range []string{`subject:"Re: Quarterly"`, `"42 USD."`, "subject:*invoice*"} {
		if _, err := RunQuery(q, 10, 0); err != nil {
			t.Errorf("RunQuery(%q): %v", q, err)
		}
	}
}

func TestPipelineAggregates(t *testing.T) {
	openQueryFixture(t)

	res, err := RunQuery("| count", 0, 0)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if res.Kind != "count" || res.Total != 5 {
		t.Errorf("count = %d (kind %s), want 5", res.Total, res.Kind)
	}

	res, err = RunQuery("| count by domain", 0, 0)
	if err != nil {
		t.Fatalf("count by domain: %v", err)
	}
	if res.Kind != "groups" {
		t.Fatalf("kind = %q, want groups", res.Kind)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	if got["acme.com"] != 3 || got["stripe.com"] != 1 {
		t.Errorf("count by domain = %v, want acme.com:3 stripe.com:1", got)
	}

	// Grouping by a multi-valued key must still count each message once.
	res, err = RunQuery("| top to 10", 0, 0)
	if err != nil {
		t.Fatalf("top to: %v", err)
	}
	for _, g := range res.Groups {
		if g.Label == "me@example.com" && g.Value != 4 {
			t.Errorf("me@example.com appears in %v messages, want 4", g.Value)
		}
	}

	res, err = RunQuery("| count by month", 0, 0)
	if err != nil {
		t.Fatalf("count by month: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Label != "2026-03" || res.Groups[0].Value != 5 {
		t.Errorf("count by month = %v, want one bucket 2026-03 with 5", res.Groups)
	}
}

func TestSortAndLimit(t *testing.T) {
	openQueryFixture(t)

	res, err := RunQuery("| sort size desc | limit 2", 0, 0)
	if err != nil {
		t.Fatalf("sort: %v", err)
	}
	if len(res.Messages) != 2 || res.Messages[0].ID != "m3" || res.Messages[1].ID != "m1" {
		t.Errorf("sort size desc gave %v, want m3 then m1", msgIDs(res.Messages))
	}
}

func msgIDs(msgs []*message.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

// Threading walks References rather than grouping by subject, so a reply is in
// its parent's thread and an unrelated message with the same subject is not.
func TestThreadingUsesReferences(t *testing.T) {
	openQueryFixture(t)

	eq(t, ids(t, "thread:m1"), "m1", "m5")
	eq(t, ids(t, "thread:m5"), "m1", "m5")

	// `| thread` expands a filtered set to whole conversations: m5 alone pulls
	// in m1, which the filter itself did not match.
	eq(t, ids(t, "from:bob@acme.com | thread"), "m1", "m5")

	// A message with no references is its own thread.
	eq(t, ids(t, "thread:m3"), "m3")
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	openQueryFixture(t)

	for _, q := range []string{
		"nosuchfield:x",
		"is:purple",
		"has:wings",
		"after:notadate",
		"larger:banana",
		"| count by nonsense",
		"| frobnicate",
		"from:",
		`unterminated:"quote`,
		"-",
	} {
		if _, err := RunQuery(q, 10, 0); err == nil {
			t.Errorf("RunQuery(%q) succeeded, want an error", q)
		}
	}
}

// A value containing SQL syntax must be a value, not syntax. Everything is
// parameterised, so this is a regression guard rather than a live risk.
func TestValuesAreParameterised(t *testing.T) {
	openQueryFixture(t)

	for _, q := range []string{
		`subject:"'; DROP TABLE messages; --"`,
		`from:"' OR 1=1 --"`,
		"from:100%",
		"subject:50%_off",
	} {
		if _, err := RunQuery(q, 10, 0); err != nil {
			t.Fatalf("RunQuery(%q): %v", q, err)
		}
	}

	if n := countOf(t, ""); n != 5 {
		t.Fatalf("mailbox has %d messages after injection attempts, want 5", n)
	}
}
