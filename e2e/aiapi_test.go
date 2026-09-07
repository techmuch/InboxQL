//go:build e2e

package e2e

import (
	"encoding/json"
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
