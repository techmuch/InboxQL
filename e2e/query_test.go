//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedMailbox imports a handful of messages through the real `import eml` path.
//
// Written as .eml files rather than inserted directly so the parser, the
// participant index and the reference graph are all exercised the way they are
// in use — the threading bug this suite would have caught lived precisely in
// the gap between what the importer stored and what the graph expected.
func (e *env) seedMailbox(t *testing.T) {
	t.Helper()

	if r := e.run("account", "add", "--name", "Work", "--email", "me@example.com",
		"--host", "imap.example.com"); r.ExitCode != 0 {
		t.Fatalf("account add: %s%s", r.Stdout, r.Stderr)
	}

	dir := t.TempDir()
	messages := []struct{ id, from, to, subject, extra, body string }{
		{"m1", "Alice <alice@acme.com>", "me@example.com, bob@acme.com",
			"Quarterly invoice", "", "The invoice is attached."},
		{"m2", "notalice@acme.com", "me@example.com", "Lunch", "", "Friday?"},
		{"m3", "billing@stripe.com", "me@example.com", "Your receipt", "", "Payment received."},
		// A real reply, by header.
		{"m4", "bob@acme.com", "me@example.com",
			"Re: Quarterly invoice", "In-Reply-To: <m1@test>\r\nReferences: <m1@test>", "Paid."},
		// Same words, no References: must not join the thread above.
		{"m5", "dave@acme.com", "me@example.com",
			"Quarterly invoice", "", "Unrelated mention of a quarterly invoice."},
	}

	for i, m := range messages {
		raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%s@test>\r\n%sDate: Mon, 0%d Mar 2026 10:00:00 +0000\r\n\r\n%s\r\n",
			m.from, m.to, m.subject, m.id, withCRLF(m.extra), i+1, m.body)
		if err := os.WriteFile(filepath.Join(dir, m.id+".eml"), []byte(raw), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
	}

	if r := e.run("import", "eml", dir, "--account", "work"); r.ExitCode != 0 {
		t.Fatalf("import eml: %s%s", r.Stdout, r.Stderr)
	}
}

func withCRLF(s string) string {
	if s == "" {
		return ""
	}
	return s + "\r\n"
}

// count runs a query and returns how many messages matched.
func (e *env) count(t *testing.T, expr string) int64 {
	t.Helper()
	r := e.run("--json", "query", expr, "--count")
	if r.ExitCode != 0 {
		t.Fatalf("query %q exited %d: %s%s", expr, r.ExitCode, r.Stdout, r.Stderr)
	}
	var out struct {
		Count int64 `json:"count"`
	}
	r.JSON(t, &out)
	return out.Count
}

// A query expression beginning with a dash is negation, not a flag. The flag
// package would otherwise eat it, and the argument normaliser would otherwise
// rewrite it into a double negative — which returns the opposite of what was
// asked without failing.
func TestNegationSurvivesArgumentParsing(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	total := e.count(t, "")
	if total != 5 {
		t.Fatalf("seeded %d messages, want 5", total)
	}

	for _, expr := range []string{"-from:alice", "-invoice", "-(from:alice)", "NOT from:alice"} {
		r := e.run("--json", "query", expr, "--count")
		if r.ExitCode != 0 {
			t.Errorf("query %q exited %d: %s", expr, r.ExitCode, r.Stderr)
		}
	}

	pos := e.count(t, "from:alice")
	neg := e.count(t, "-from:alice")
	if pos+neg != total {
		t.Errorf("from:alice matched %d and -from:alice matched %d, want them to sum to %d",
			pos, neg, total)
	}
	if neg == total {
		t.Error("-from:alice matched everything, so the dash was not read as negation")
	}
}

func TestMatchModesThroughTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if n := e.count(t, "from:alice"); n != 2 {
		t.Errorf("from:alice = %d, want 2 (alice@ and notalice@)", n)
	}
	if n := e.count(t, "from:=alice@acme.com"); n != 1 {
		t.Errorf("from:=alice@acme.com = %d, want 1", n)
	}
	if n := e.count(t, "from:*@acme.com"); n != 4 {
		t.Errorf("from:*@acme.com = %d, want 4", n)
	}
}

// -to:x must mean "no recipient is x". The wrong reading matches nearly every
// message with more than one recipient, and looks right in casual use.
func TestRecipientNegationIsAnAntiJoin(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if n := e.count(t, "to:bob@acme.com"); n != 1 {
		t.Fatalf("to:bob@acme.com = %d, want 1", n)
	}
	// m1 is addressed to both me@ and bob@; it must not appear here.
	if n := e.count(t, "-to:bob@acme.com"); n != 4 {
		t.Errorf("-to:bob@acme.com = %d, want 4", n)
	}
}

func TestAggregatePipeline(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	r := e.run("--json", "query", "| count by domain")
	if r.ExitCode != 0 {
		t.Fatalf("count by domain exited %d: %s", r.ExitCode, r.Stderr)
	}
	var out struct {
		Kind   string `json:"kind"`
		Groups []struct {
			Label string  `json:"label"`
			Value float64 `json:"value"`
		} `json:"groups"`
	}
	r.JSON(t, &out)

	if out.Kind != "groups" {
		t.Fatalf("kind = %q, want groups", out.Kind)
	}
	got := map[string]float64{}
	for _, g := range out.Groups {
		got[g.Label] = g.Value
	}
	if got["acme.com"] != 4 || got["stripe.com"] != 1 {
		t.Errorf("count by domain = %v, want acme.com:4 stripe.com:1", got)
	}
}

// Threading follows References, so a reply joins its parent and an unrelated
// message with the same subject does not.
func TestThreadingThroughTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	r := e.run("--json", "query", "from:=alice@acme.com")
	var found struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	r.JSON(t, &found)
	if len(found.Messages) != 1 {
		t.Fatalf("expected to find alice's message, got %d", len(found.Messages))
	}

	if n := e.count(t, "thread:"+found.Messages[0].ID); n != 2 {
		t.Errorf("thread has %d messages, want 2 (the original and its reply, "+
			"not the unrelated message sharing a subject)", n)
	}
}

// A rule annotator needs no provider, so the whole label surface is testable
// end to end without a model.
func TestRuleAnnotatorAndThreeValuedLabels(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("annotate", "create", "billing", "--engine", "rule",
		"--instructions", "from:*@stripe.com"); r.ExitCode != 0 {
		t.Fatalf("annotate create: %s%s", r.Stdout, r.Stderr)
	}

	// Scoped so part of the mailbox is deliberately left unevaluated.
	if r := e.run("annotate", "run", "billing", "--scope", "from:*@acme.com OR from:*@stripe.com"); r.ExitCode != 0 {
		t.Fatalf("annotate run: %s%s", r.Stdout, r.Stderr)
	}

	total := e.count(t, "")
	yes := e.count(t, "label:billing")
	no := e.count(t, "-label:billing")
	never := e.count(t, "unlabeled:billing")

	if yes+no+never != total {
		t.Errorf("%d yes + %d no + %d unevaluated = %d, want %d",
			yes, no, never, yes+no+never, total)
	}
	if yes != 1 {
		t.Errorf("label:billing = %d, want 1", yes)
	}

	// A human ruling outranks the rule and survives a re-run.
	r := e.run("--json", "query", "from:=alice@acme.com")
	var found struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	r.JSON(t, &found)
	id := found.Messages[0].ID

	if r := e.run("annotate", "correct", "billing", id, "--yes"); r.ExitCode != 0 {
		t.Fatalf("annotate correct: %s%s", r.Stdout, r.Stderr)
	}
	if n := e.count(t, "label:billing"); n != 2 {
		t.Fatalf("after a correction label:billing = %d, want 2", n)
	}
	if r := e.run("annotate", "run", "billing"); r.ExitCode != 0 {
		t.Fatalf("annotate re-run: %s%s", r.Stdout, r.Stderr)
	}
	if n := e.count(t, "label:billing"); n != 2 {
		t.Errorf("a re-run destroyed the human correction: label:billing = %d, want 2", n)
	}
}

// An LLM annotator must refuse rather than quietly shipping a mailbox to a
// third party. There is no provider configured here, which is the first gate.
func TestLLMAnnotatorRefusesWithoutAProvider(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("annotate", "create", "triage", "--engine", "llm",
		"--instructions", "Is this urgent?"); r.ExitCode != 0 {
		t.Fatalf("annotate create: %s%s", r.Stdout, r.Stderr)
	}

	// A dry run still works and reports the scale of what would happen.
	if r := e.run("--json", "annotate", "run", "triage", "--dry-run"); r.ExitCode != 0 {
		t.Errorf("dry run exited %d: %s", r.ExitCode, r.Stderr)
	}

	if r := e.run("annotate", "run", "triage"); r.ExitCode == 0 {
		t.Error("a real run succeeded with no LLM provider configured")
	}
}

// The escape hatch is read-only, and that is enforced by the connection rather
// than by inspecting the statement.
func TestSQLIsReadOnly(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("--json", "sql", "SELECT COUNT(*) AS n FROM messages"); r.ExitCode != 0 {
		t.Fatalf("a SELECT was refused: %s%s", r.Stdout, r.Stderr)
	}

	for _, stmt := range []string{
		"DELETE FROM messages",
		"DROP TABLE messages",
		"UPDATE messages SET subject = 'x'",
		"INSERT INTO messages (id) VALUES ('x')",
	} {
		if r := e.run("sql", stmt); r.ExitCode == 0 {
			t.Errorf("sql %q was allowed", stmt)
		}
	}

	if n := e.count(t, ""); n != 5 {
		t.Errorf("the mailbox has %d messages after write attempts, want 5", n)
	}
}

// A malformed query is the caller's mistake, so it exits 2 rather than 1 and
// says where the problem is.
func TestBadQueryExitsWithUsage(t *testing.T) {
	e := newEnv(t)

	for _, expr := range []string{"nosuchfield:x", "is:purple", "(unclosed", "| frobnicate"} {
		r := e.run("query", expr)
		if r.ExitCode != 2 {
			t.Errorf("query %q exited %d, want 2 (bad arguments)", expr, r.ExitCode)
		}
	}
}

// --explain has to compile without touching the data, so it works as a syntax
// check before a long run.
func TestExplainCompilesWithoutRunning(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	r := e.run("--json", "query", "from:x -to:y after:2026-01 | count by week", "--explain")
	if r.ExitCode != 0 {
		t.Fatalf("explain exited %d: %s", r.ExitCode, r.Stderr)
	}
	var out struct {
		SQL  string `json:"sql"`
		Kind string `json:"kind"`
	}
	r.JSON(t, &out)
	if out.SQL == "" {
		t.Error("explain returned no SQL")
	}
	if out.Kind != "groups" {
		t.Errorf("kind = %q, want groups", out.Kind)
	}
}

// Desk and the dashboard share one query, and that query may carry a pipeline.
// Every analytics widget appends its own aggregate, so a shared `| timeline`
// used to produce two terminal stages and a 400 from all three widgets at once.
func TestAnalyticsChartsTheFilterNotThePipeline(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	s := e.startServer()

	get := func(t *testing.T, path string) (int, string) {
		t.Helper()
		resp := s.browserRequest(t, "GET", path, "",
			map[string]string{"Sec-Fetch-Site": "same-origin"})
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	// The same filter, with and without a pipeline, has to chart the same
	// thing — the stage says what Desk is showing, not which mail is in scope.
	code, plain := get(t, "/api/analytics?type=volume&q=folder%3Ainbox")
	if code != 200 {
		t.Fatalf("plain filter returned %d: %s", code, plain)
	}

	for _, pipeline := range []string{"| timeline", "| count by week", "| top domain 5"} {
		path := "/api/analytics?type=volume&q=" + url.QueryEscape("folder:inbox "+pipeline)
		code, body := get(t, path)
		if code != 200 {
			t.Errorf("%q returned %d: %s", pipeline, code, body)
			continue
		}
		if body != plain {
			t.Errorf("%q charted something different:\n got %s\nwant %s", pipeline, body, plain)
		}
	}

	// A query about another entity is still refused, with a sentence saying
	// why — charting tickets as if they were mail would be worse than an error.
	code, body := get(t, "/api/analytics?type=volume&q="+url.QueryEscape("status:todo"))
	if code != 400 {
		t.Errorf("a ticket query returned %d, want 400", code)
	}
	if !strings.Contains(body, "analytics charts mail") {
		t.Errorf("the refusal does not explain itself: %s", body)
	}
}

// Every message creates a contact, and what is counted about them is derived
// from the participant edges rather than cached.
func TestContactsAreCreatedFromMail(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	var contacts []struct {
		Address  string `json:"address"`
		Kind     string `json:"kind"`
		Messages int64  `json:"messages"`
	}
	r := e.run("--json", "contact", "list")
	if r.ExitCode != 0 {
		t.Fatalf("contact list: %s%s", r.Stdout, r.Stderr)
	}
	r.JSON(t, &contacts)

	byAddress := map[string]int64{}
	for _, c := range contacts {
		byAddress[c.Address] = c.Messages
	}
	for _, want := range []string{"alice@acme.com", "bob@acme.com", "me@example.com"} {
		if byAddress[want] == 0 {
			t.Errorf("no contact for %s, or no messages counted: %v", want, byAddress)
		}
	}

	// The display name from the header. Both parsers used to discard it, so a
	// contact could only ever be known by its address.
	r = e.run("--json", "contact", "show", "alice@acme.com")
	if r.ExitCode != 0 {
		t.Fatalf("contact show: %s%s", r.Stdout, r.Stderr)
	}
	var alice struct {
		HeaderName string `json:"headerName"`
		Messages   int64  `json:"messages"`
		Sent       int64  `json:"sent"`
	}
	r.JSON(t, &alice)
	if alice.HeaderName == "" {
		t.Error("alice has no name; the display name in her From header was dropped")
	}
	if alice.Sent == 0 {
		t.Error("alice sent nothing, but the fixture has her sending")
	}

	// Contact fields are query terms, like every other entity's.
	if r := e.run("query", "in:contacts messages>1"); r.ExitCode != 0 {
		t.Errorf("in:contacts messages>1: %s%s", r.Stdout, r.Stderr)
	}
}

// The one classification that matters most is the one that must not fire: a
// person who forwards an automated message is still a person.
func TestClassificationIsConservative(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("contact", "classify"); r.ExitCode != 0 {
		t.Fatalf("contact classify: %s%s", r.Stdout, r.Stderr)
	}

	var contacts []struct {
		Address string `json:"address"`
		Kind    string `json:"kind"`
	}
	r := e.run("--json", "contact", "list")
	r.JSON(t, &contacts)

	for _, c := range contacts {
		// Everyone in the fixture is a person or unknown; nobody is a system.
		// Getting this wrong on the mailbox owner is the failure this whole
		// rule set is shaped around avoiding.
		if c.Kind == "system" {
			t.Errorf("%s was classified as a system", c.Address)
		}
	}
}

// The relationship graph is a self-join over edges that already exist, so it
// needs no storage and cannot go stale.
func TestNetworkStageReportsCoOccurrence(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	r := e.run("--json", "query", "| network 10")
	if r.ExitCode != 0 {
		t.Fatalf("network: %s%s", r.Stdout, r.Stderr)
	}
	var out struct {
		Kind   string `json:"kind"`
		Groups []struct {
			Label string  `json:"label"`
			Value float64 `json:"value"`
		} `json:"groups"`
	}
	r.JSON(t, &out)
	if out.Kind != "groups" {
		t.Fatalf("kind = %q, want groups", out.Kind)
	}
	if len(out.Groups) == 0 {
		t.Fatal("no pairs found; the fixture has messages with several recipients")
	}
	for _, g := range out.Groups {
		if !strings.Contains(g.Label, " — ") {
			t.Errorf("pair label %q does not name two addresses", g.Label)
		}
		if g.Value < 1 {
			t.Errorf("pair %q has a count of %v", g.Label, g.Value)
		}
	}
}
