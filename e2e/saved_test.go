//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// A saved query is a term in the language, so the CLI has to be able to define
// one and an agent has to be able to run one the user saved. Without CLI
// parity, saved queries would be a UI-only concept the contract cannot reach.
func TestSavedQueriesComposeFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("saved", "save", "Acme mail", "--query", "from:*@acme.com"); r.ExitCode != 0 {
		t.Fatalf("saved save: %s%s", r.Stdout, r.Stderr)
	}

	if n := e.count(t, "saved:acme-mail"); n != 4 {
		t.Errorf("saved:acme-mail matched %d, want 4", n)
	}
	// The point of a saved query: it composes rather than merely recalling.
	if n := e.count(t, "saved:acme-mail subject:invoice"); n != 3 {
		t.Errorf("saved:acme-mail subject:invoice matched %d, want 3", n)
	}
	if n := e.count(t, "-saved:acme-mail"); n != 1 {
		t.Errorf("-saved:acme-mail matched %d, want 1", n)
	}

	r := e.run("--json", "saved", "list")
	var listed []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
		Query string `json:"query"`
	}
	r.JSON(t, &listed)
	if len(listed) != 1 || listed[0].Name != "acme-mail" || listed[0].Title != "Acme mail" {
		t.Errorf("saved list returned %+v", listed)
	}

	if r := e.run("saved", "delete", "acme-mail"); r.ExitCode != 0 {
		t.Fatalf("saved delete: %s%s", r.Stdout, r.Stderr)
	}
	// A dangling reference fails loudly rather than matching nothing.
	if r := e.run("query", "saved:acme-mail"); r.ExitCode != 2 {
		t.Errorf("a deleted reference exited %d, want 2", r.ExitCode)
	}
}

// A saved query that does not compile is rejected where it is written, not
// from wherever it is later referenced.
func TestSavedQueriesAreValidatedOnSave(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	for _, bad := range []string{"nosuchfield:x", "(unclosed", "is:purple"} {
		if r := e.run("saved", "save", "Bad", "--query", bad); r.ExitCode == 0 {
			t.Errorf("saved save accepted %q", bad)
		}
	}

	if r := e.run("saved", "save", "Acme", "--query", "from:*@acme.com"); r.ExitCode != 0 {
		t.Fatalf("saved save: %s%s", r.Stdout, r.Stderr)
	}
	r := e.run("saved", "save", "Nested", "--query", "saved:acme is:unread")
	if r.ExitCode == 0 {
		t.Error("a nested saved query was accepted")
	}
	if !strings.Contains(r.Stderr, "cannot reference another") {
		t.Errorf("the refusal was %q, which does not explain the rule", r.Stderr)
	}
}

// me() resolves from the configured accounts, so "mail to me" needs no address.
func TestSelfFunctionFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if n := e.count(t, "to:me()"); n != 5 {
		t.Errorf("to:me() matched %d, want 5", n)
	}
	// Still a term like any other: it negates and it partitions.
	if pos, neg := e.count(t, "to:me()"), e.count(t, "-to:me()"); pos+neg != e.count(t, "") {
		t.Errorf("to:me() %d and -to:me() %d do not partition the mailbox", pos, neg)
	}
}

// A leading dash is negation, and a field-scoped group says once what would
// otherwise be repeated — both have to survive argument parsing.
func TestShorthandsSurviveTheShell(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	grouped := e.count(t, "from:(alice OR stripe)")
	spelledOut := e.count(t, "from:alice OR from:stripe")
	if grouped != spelledOut {
		t.Errorf("from:(alice OR stripe) matched %d but the long form matched %d", grouped, spelledOut)
	}

	if n := e.count(t, "-from:(alice OR stripe)"); n+grouped != e.count(t, "") {
		t.Errorf("negating a field-scoped group does not partition the mailbox")
	}
}

// Completion has to answer on text that is mid-edit, which is exactly the text
// the parser rejects.
func TestCompletionFromTheCLI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	r := e.run("--json", "query", "from:al", "--complete", "7")
	if r.ExitCode != 0 {
		t.Fatalf("completion exited %d: %s", r.ExitCode, r.Stderr)
	}
	var c struct {
		Context    string `json:"context"`
		Field      string `json:"field"`
		Prefix     string `json:"prefix"`
		Candidates []struct {
			Value  string `json:"value"`
			Detail string `json:"detail"`
		} `json:"candidates"`
	}
	r.JSON(t, &c)

	if c.Context != "value" || c.Field != "from" || c.Prefix != "al" {
		t.Fatalf("completion reported context=%q field=%q prefix=%q", c.Context, c.Field, c.Prefix)
	}
	var found bool
	for _, cand := range c.Candidates {
		if cand.Value == "alice@acme.com" && cand.Detail != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("completing from:al did not offer alice@acme.com with a count: %+v", c.Candidates)
	}

	// Incomplete text of every shape returns an answer rather than an error.
	for _, partial := range []string{"", "-", "(", "from:(", "| ", "| count by ", `subject:"open`} {
		if r := e.run("--json", "query", partial, "--complete", "0"); r.ExitCode != 0 {
			t.Errorf("completion on %q exited %d: %s", partial, r.ExitCode, r.Stderr)
		}
	}
}

// A misspelled field gets a suggestion, because a typo is the common case.
func TestUnknownFieldSuggestsTheObviousFix(t *testing.T) {
	e := newEnv(t)

	r := e.run("query", "form:alice")
	if r.ExitCode != 2 {
		t.Errorf("exit = %d, want 2", r.ExitCode)
	}
	if !strings.Contains(r.Stderr, "did you mean from") {
		t.Errorf("the error was %q, which does not suggest the fix", r.Stderr)
	}

	// Something that is not a typo of anything lists what there is instead.
	r = e.run("query", "zzzzzz:x")
	if !strings.Contains(r.Stderr, "fields:") {
		t.Errorf("the error was %q, which neither suggests nor enumerates", r.Stderr)
	}
}

// Composition is served rather than reimplemented in the frontend, because
// three TypeScript versions of it existed and all three split on whitespace —
// so a quoted value containing a space stopped being one term.
func TestQueryCompositionIsServerSide(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	s := e.startServer()

	compose := func(t *testing.T, params string) string {
		t.Helper()
		resp := s.browserRequest(t, "GET", "/api/query/compose?"+params, "",
			map[string]string{"Sec-Fetch-Site": "same-origin"})
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("compose?%s returned %d", params, resp.StatusCode)
		}
		var out struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		return out.Query
	}

	cases := []struct{ params, want, why string }{
		{"q=&add=from%3Aalice", "from:alice", "adding to an empty query"},
		{"q=folder%3Ainbox+from%3Aalice&add=from%3Abob", "folder:inbox from:bob",
			"a second sender replaces the first rather than asking for both"},
		{`q=subject%3A%22a+b%22&add=subject%3A%22a+b%22`, `subject:"a b"`,
			"a quoted term clicked twice must not duplicate"},
		{"q=folder%3Ainbox+from%3Aalice&field=folder&term=folder%3Asent", "from:alice folder:sent",
			"selecting a folder keeps the filter"},
		{"q=is%3Aunread+%7C+count+by+week&add=from%3Aalice", "is:unread from:alice | count by week",
			"the pipeline stays last"},
	}
	for _, c := range cases {
		if got := compose(t, c.params); got != c.want {
			t.Errorf("%s: got %q, want %q", c.why, got, c.want)
		}
	}

	// Whatever it produces has to run.
	final := compose(t, "q=folder%3Ainbox&add=from%3Aalice")
	if r := e.run("query", final, "--count"); r.ExitCode != 0 {
		t.Errorf("composed query %q does not run: %s", final, r.Stderr)
	}
}

// Analytics charts mail. One shared query means a ticket query can arrive
// there, and silently charting something else is worse than saying so.
func TestAnalyticsRefusesNonMailQueries(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	s := e.startServer()

	resp := s.browserRequest(t, "GET", "/api/analytics?type=senders&q=in%3Atickets", "",
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("a ticket query to analytics returned %d, want 400", resp.StatusCode)
	}

	ok := s.browserRequest(t, "GET", "/api/analytics?type=senders&q=is%3Aunread", "",
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	defer ok.Body.Close()
	if ok.StatusCode != 200 {
		t.Errorf("a mail query to analytics returned %d, want 200", ok.StatusCode)
	}
}
