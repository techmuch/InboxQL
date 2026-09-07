//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// apiClient issues same-origin requests against a running server.
type apiClient struct {
	t   *testing.T
	srv *server
}

func (c *apiClient) do(method, path, body string, out any) int {
	c.t.Helper()
	headers := map[string]string{"Sec-Fetch-Site": "same-origin"}
	if body != "" {
		headers["Content-Type"] = "application/json"
	}
	resp := c.srv.browserRequest(c.t, method, path, body, headers)
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// The web UI can configure several models, not one — the same plural the CLI
// has.
func TestProfilesAreManageableOverTheAPI(t *testing.T) {
	e := newEnv(t)
	c := &apiClient{t: t, srv: e.startServer()}

	if code := c.do("POST", "/api/llm/profiles",
		`{"name":"local","provider":"ollama","model":"llama3","isDefault":true}`, nil); code != 200 {
		t.Fatalf("creating a profile returned %d", code)
	}
	if code := c.do("POST", "/api/llm/profiles",
		`{"name":"cloud","provider":"openai","model":"gpt-4o-mini","apiKey":"sk-test"}`, nil); code != 200 {
		t.Fatalf("creating a second profile returned %d", code)
	}

	var listed struct {
		Profiles []profileRow `json:"profiles"`
	}
	if code := c.do("GET", "/api/llm/profiles", "", &listed); code != 200 {
		t.Fatalf("listing returned %d", code)
	}
	if len(listed.Profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(listed.Profiles))
	}

	byName := map[string]profileRow{}
	for _, p := range listed.Profiles {
		byName[p.Name] = p
	}
	if byName["cloud"].Scope != "remote" || !byName["cloud"].HasAPIKey {
		t.Errorf("cloud = %+v, want remote with a stored key", byName["cloud"])
	}
	if !byName["local"].IsDefault {
		t.Error("local is not the default")
	}

	// The key is write-only: it never comes back, and saving a form that
	// could not display it must not erase it.
	raw := map[string]any{}
	c.do("GET", "/api/llm/profiles", "", &raw)
	if strings.Contains(mustJSON(t, raw), "sk-test") {
		t.Error("the API key was returned to the client")
	}
	if code := c.do("POST", "/api/llm/profiles",
		`{"name":"cloud","provider":"openai","model":"gpt-4o"}`, nil); code != 200 {
		t.Fatalf("editing returned %d", code)
	}
	c.do("GET", "/api/llm/profiles", "", &listed)
	for _, p := range listed.Profiles {
		if p.Name == "cloud" {
			if !p.HasAPIKey {
				t.Error("saving without a key erased the stored one")
			}
			if p.Model != "gpt-4o" {
				t.Errorf("the edit did not apply: model = %q", p.Model)
			}
		}
	}

	// Promoting demotes.
	if code := c.do("POST", "/api/llm/profiles/default", `{"name":"cloud"}`, nil); code != 200 {
		t.Fatal("promoting failed")
	}
	c.do("GET", "/api/llm/profiles", "", &listed)
	defaults := 0
	for _, p := range listed.Profiles {
		if p.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("%d profiles claim to be the default", defaults)
	}

	if code := c.do("DELETE", "/api/llm/profiles?name=cloud", "", nil); code != 200 {
		t.Errorf("deleting returned %d", code)
	}
}

// A refresh that fails must say so. Reporting "no models" for a scan that
// broke is the failure shape that reads as a normal empty state.
func TestRefreshReportsWhyItFoundNothing(t *testing.T) {
	e := newEnv(t)
	c := &apiClient{t: t, srv: e.startServer()}

	var out struct {
		Runtimes []struct {
			Provider  string   `json:"provider"`
			Installed bool     `json:"installed"`
			Running   bool     `json:"running"`
			Models    []string `json:"models"`
			Problem   string   `json:"problem"`
		} `json:"runtimes"`
		Models int `json:"models"`
	}
	if code := c.do("POST", "/api/llm/refresh", `{}`, &out); code != 200 {
		t.Fatalf("refresh returned %d", code)
	}
	if len(out.Runtimes) == 0 {
		t.Fatal("refresh reported no runtimes at all")
	}

	// Every runtime that yielded no models explains itself, so the UI never
	// has to render an unexplained empty list.
	for _, rt := range out.Runtimes {
		if len(rt.Models) == 0 && rt.Problem == "" {
			t.Errorf("%s produced no models and no explanation", rt.Provider)
		}
		if !rt.Installed && !strings.Contains(rt.Problem, "not installed") {
			t.Errorf("%s is not installed but says %q", rt.Provider, rt.Problem)
		}
	}

	// Naming a runtime that does not exist is an error, not an empty list.
	if code := c.do("POST", "/api/llm/refresh", `{"provider":"nonesuch"}`, nil); code != 404 {
		t.Errorf("refreshing an unknown runtime returned %d, want 404", code)
	}
}

// Annotators are the product's actual AI feature and were reachable only from
// the CLI. The web surface has to be able to define, list and remove them.
func TestAnnotatorsAreManageableOverTheAPI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	c := &apiClient{t: t, srv: e.startServer()}

	if code := c.do("POST", "/api/llm/profiles",
		`{"name":"cloud","provider":"openai","model":"gpt-4o-mini","isDefault":true}`, nil); code != 200 {
		t.Fatal("creating a profile failed")
	}

	// A rule annotator: its instruction is a query, so an invalid one is
	// rejected here rather than saved as something that matches nothing.
	if code := c.do("POST", "/api/annotators",
		`{"name":"broken","engine":"rule","instructions":"from:(unclosed"}`, nil); code != 400 {
		t.Errorf("an invalid rule was accepted with %d", code)
	}
	if code := c.do("POST", "/api/annotators",
		`{"name":"billing","engine":"rule","instructions":"from:*@acme.com"}`, nil); code != 200 {
		t.Fatal("creating a rule annotator failed")
	}

	// An LLM annotator naming a profile that does not exist is a 400, not an
	// annotator that exists and fails later.
	if code := c.do("POST", "/api/annotators",
		`{"name":"oops","engine":"llm","profile":"nope","instructions":"x"}`, nil); code != 400 {
		t.Errorf("an unknown profile was accepted with %d", code)
	}
	if code := c.do("POST", "/api/annotators",
		`{"name":"receipts","engine":"llm","kind":"extract","instructions":"Pull the amount."}`, nil); code != 200 {
		t.Fatal("creating an LLM annotator failed")
	}

	var rows []struct {
		Name           string `json:"name"`
		Engine         string `json:"engine"`
		Scope          string `json:"scope"`
		ConsentMissing bool   `json:"consentMissing"`
		Progress       struct {
			Total int64 `json:"total"`
		} `json:"progress"`
	}
	if code := c.do("GET", "/api/annotators", "", &rows); code != 200 {
		t.Fatal("listing annotators failed")
	}
	if len(rows) != 2 {
		t.Fatalf("got %d annotators, want 2", len(rows))
	}

	for _, a := range rows {
		switch a.Name {
		case "receipts":
			// The list says what running it would do, per annotator.
			if a.Scope != "remote" {
				t.Errorf("receipts is scoped %q, want remote", a.Scope)
			}
			if !a.ConsentMissing {
				t.Error("receipts has no consent but the list does not say so")
			}
		case "billing":
			if a.Scope != "" || a.ConsentMissing {
				t.Errorf("a rule annotator reports scope=%q consentMissing=%v",
					a.Scope, a.ConsentMissing)
			}
			if a.Progress.Total == 0 {
				t.Error("progress reports no messages in scope")
			}
		}
	}

	// A rule run is one statement, so it completes inline.
	var outcome struct {
		Matched int64 `json:"matched"`
		Empty   int64 `json:"empty"`
	}
	var runErr struct {
		Error string `json:"error"`
	}
	code := c.do("POST", "/api/annotators/run", `{"name":"billing"}`, &outcome)
	if code != 200 {
		c.do("POST", "/api/annotators/run", `{"name":"billing"}`, &runErr)
		t.Fatalf("running the rule annotator returned %d: %s", code, runErr.Error)
	}
	if outcome.Matched == 0 {
		t.Error("the rule matched nothing; the fixture has acme mail")
	}

	// A remote run with no consent is refused, with the sentence that
	// explains it.
	var refusal struct {
		Error string `json:"error"`
	}
	if code := c.do("POST", "/api/annotators/run", `{"name":"receipts"}`, &refusal); code != 400 {
		t.Errorf("an unconsented remote run returned %d, want 400", code)
	}
	if !strings.Contains(refusal.Error, "no consent recorded") {
		t.Errorf("the refusal does not explain itself: %q", refusal.Error)
	}

	if code := c.do("DELETE", "/api/annotators?name=billing", "", nil); code != 200 {
		t.Error("deleting an annotator failed")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// Selection existed before any action did — a count and a Clear button with
// nothing behind them. This is the first thing a selection can actually do.
func TestBulkFlagsOverTheAPI(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	c := &apiClient{t: t, srv: e.startServer()}

	var listed struct {
		Messages []struct {
			ID    string   `json:"id"`
			Flags []string `json:"flags"`
		} `json:"messages"`
	}
	if code := c.do("GET", "/api/query?q=is%3Aunread", "", &listed); code != 200 {
		t.Fatalf("listing unread returned %d", code)
	}
	if len(listed.Messages) < 2 {
		t.Fatalf("fixture has %d unread messages, want at least 2", len(listed.Messages))
	}
	ids := []string{listed.Messages[0].ID, listed.Messages[1].ID}
	before := len(listed.Messages)

	var out struct {
		Changed int `json:"changed"`
	}
	body := fmt.Sprintf(`{"ids":["%s","%s"],"flag":"\\Seen","on":true}`, ids[0], ids[1])
	if code := c.do("POST", "/api/messages/flags", body, &out); code != 200 {
		t.Fatalf("marking read returned %d", code)
	}
	if out.Changed != 2 {
		t.Errorf("changed = %d, want 2", out.Changed)
	}

	// The query language agrees, which is the point of writing the canonical
	// spelling: is:unread compares exactly.
	c.do("GET", "/api/query?q=is%3Aunread", "", &listed)
	if len(listed.Messages) != before-2 {
		t.Errorf("%d unread after marking two read, want %d", len(listed.Messages), before-2)
	}

	// A repeat reports nothing changed rather than claiming work it did not do.
	c.do("POST", "/api/messages/flags", body, &out)
	if out.Changed != 0 {
		t.Errorf("a repeat reported %d changed, want 0", out.Changed)
	}

	// Flags that describe what a message *is* are refused.
	deleted := fmt.Sprintf(`{"ids":["%s"],"flag":"\\Deleted","on":true}`, ids[0])
	if code := c.do("POST", "/api/messages/flags", deleted, nil); code != 400 {
		t.Errorf("setting \\Deleted returned %d, want 400", code)
	}
	if code := c.do("POST", "/api/messages/flags", `{"ids":[],"flag":"\\Seen","on":true}`, nil); code != 400 {
		t.Errorf("an empty id list returned %d, want 400", code)
	}
}

// A hand-picked set is not a filter; it is a list of identities, and `id:` is
// how the language names one.
func TestIdentityQueriesNameAHandPickedSet(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)
	c := &apiClient{t: t, srv: e.startServer()}

	var listed struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	c.do("GET", "/api/query?q=in%3Amessages", "", &listed)
	if len(listed.Messages) < 3 {
		t.Fatalf("fixture has %d messages", len(listed.Messages))
	}
	a, b := listed.Messages[0].ID, listed.Messages[1].ID

	// The composer builds the term, so the client never writes the parens.
	var composed struct {
		Query string `json:"query"`
	}
	path := fmt.Sprintf("/api/query/compose?q=&field=id&values=%s&values=%s", a, b)
	if code := c.do("GET", path, "", &composed); code != 200 {
		t.Fatalf("composing returned %d", code)
	}
	want := fmt.Sprintf("id:(%s OR %s)", a, b)
	if composed.Query != want {
		t.Fatalf("composed %q, want %q", composed.Query, want)
	}

	// And it runs, matching exactly those two.
	c.do("GET", "/api/query?q="+url.QueryEscape(composed.Query), "", &listed)
	if len(listed.Messages) != 2 {
		t.Errorf("id:(a OR b) matched %d messages, want 2", len(listed.Messages))
	}
}
