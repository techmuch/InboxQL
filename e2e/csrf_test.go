//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// These tests replay a real exploit chain against the default deployment.
//
// InboxQL listens on loopback and authenticates without a password there, and
// that path accepted a request carrying no cookie at all — so SameSite did
// nothing and any page the user happened to visit could drive the API. The
// chain was:
//
//  1. POST /api/accounts, cross-origin, Content-Type: text/plain (a simple
//     request, so no preflight), naming an existing account and changing only
//     its IMAP host. The stored password was preserved by the
//     blank-password-means-keep rule.
//  2. POST /api/accounts/sync — no body needed — which made InboxQL decrypt
//     that password and present it to the attacker's server.
//
// Everything below asserts on the headers a browser actually sends, because
// that is what distinguishes the attack from the local tooling the
// passwordless path exists to serve.

// browserRequest issues a request carrying the headers a browser would set for
// a page on another origin.
func (s *server) browserRequest(t *testing.T, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, s.URL(path), strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// A cross-origin page must not be able to change anything, whatever it sends.
func TestCrossOriginWritesAreRefused(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	cases := []struct {
		name    string
		headers map[string]string
		// want isolates which layer refuses. 403 is the origin gate; 415 is
		// the Content-Type check. Both are independently sufficient, and
		// asserting the specific code keeps one from silently covering for the
		// other if it regresses.
		want int
	}{
		// Sent as JSON, so only the origin gate can refuse this. A real
		// attacker could not send it without a preflight, which would fail —
		// this is here to test the gate in isolation.
		{"cross-site, valid JSON", map[string]string{
			"Sec-Fetch-Site": "cross-site",
			"Origin":         "https://evil.example",
			"Content-Type":   "application/json",
		}, http.StatusForbidden},
		{"sec-fetch-site cross-site", map[string]string{
			"Sec-Fetch-Site": "cross-site",
			"Origin":         "https://evil.example",
			"Content-Type":   "text/plain",
		}, http.StatusForbidden},
		// localhost:5173 and localhost:8080 are the same site, so a Lax cookie
		// would be sent. A dev server must not be able to drive the API either.
		{"sec-fetch-site same-site", map[string]string{
			"Sec-Fetch-Site": "same-site",
			"Content-Type":   "application/json",
		}, http.StatusForbidden},
		// The fallback path, for browsers older than Sec-Fetch-Site.
		{"foreign origin, no sec-fetch", map[string]string{
			"Origin":       "https://evil.example",
			"Content-Type": "application/json",
		}, http.StatusForbidden},
		{"opaque origin", map[string]string{
			"Origin":       "null",
			"Content-Type": "application/json",
		}, http.StatusForbidden},
		// A simple request cannot set application/json, so text/plain is what
		// an attacker would actually send. The Content-Type check refuses it
		// before the body is read at all.
		{"simple request with a JSON body", map[string]string{
			"Content-Type": "text/plain",
		}, http.StatusUnsupportedMediaType},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := s.browserRequest(t, http.MethodPost, "/api/settings",
				`{"key":"csrf_probe","value":"pwned"}`, c.headers)
			resp.Body.Close()

			if resp.StatusCode != c.want {
				t.Fatalf("a cross-origin write returned %d, want %d", resp.StatusCode, c.want)
			}
		})
	}

	// Nothing was written by any of them.
	r := e.run("--json", "sql", "SELECT COUNT(*) AS n FROM app_settings WHERE key = 'csrf_probe'")
	if r.ExitCode != 0 {
		t.Fatalf("sql: %s%s", r.Stdout, r.Stderr)
	}
	if strings.Contains(r.Stdout, "pwned") || !strings.Contains(r.Stdout, "0") {
		t.Errorf("a cross-origin request wrote a setting:\n%s", r.Stdout)
	}
}

// The whole chain, end to end: retarget an account, then sync it.
func TestCrossOriginCannotRetargetAnAccount(t *testing.T) {
	e := newEnv(t)

	if r := e.run("account", "add", "--name", "My Gmail", "--email", "victim@gmail.com",
		"--host", "imap.gmail.com", "--port", "993"); r.ExitCode != 0 {
		t.Fatalf("account add: %s%s", r.Stdout, r.Stderr)
	}

	s := e.startServer()

	attack := map[string]string{
		"Sec-Fetch-Site": "cross-site",
		"Origin":         "https://evil.example",
		"Content-Type":   "text/plain",
	}
	resp := s.browserRequest(t, http.MethodPost, "/api/accounts",
		`{"name":"My Gmail","host":"imap.evil.example","port":993,"user":"victim@gmail.com"}`, attack)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("the retarget returned %d, want a refusal", resp.StatusCode)
	}

	resp = s.browserRequest(t, http.MethodPost, "/api/accounts/sync?id=my-gmail", "", attack)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("the sync trigger returned %d, want a refusal", resp.StatusCode)
	}

	r := e.run("--json", "sql", "SELECT host FROM accounts WHERE id = 'my-gmail'")
	if strings.Contains(r.Stdout, "evil") {
		t.Errorf("the account was retargeted:\n%s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "imap.gmail.com") {
		t.Errorf("the account host is not what it was:\n%s", r.Stdout)
	}
}

// The same-origin UI must keep working, or the fix is worse than the hole.
func TestSameOriginRequestsStillWork(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	resp := s.browserRequest(t, http.MethodPost, "/api/settings",
		`{"key":"ui_probe","value":"ok"}`, map[string]string{
			"Sec-Fetch-Site": "same-origin",
			"Origin":         "http://" + s.Addr,
			"Content-Type":   "application/json",
		})
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("a same-origin write returned %d, want it to succeed", resp.StatusCode)
	}

	r := e.run("--json", "sql", "SELECT value FROM app_settings WHERE key = 'ui_probe'")
	if !strings.Contains(r.Stdout, "ok") {
		t.Errorf("the same-origin write did not land:\n%s", r.Stdout)
	}
}

// A local script sends neither header, and passwordless access exists for it.
func TestLocalToolingStillWorksWithoutHeaders(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	resp := s.browserRequest(t, http.MethodPost, "/api/settings",
		`{"key":"cli_probe","value":"ok"}`, map[string]string{
			"Content-Type": "application/json",
		})
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("a headerless local write returned %d; the CLI path is broken", resp.StatusCode)
	}

	resp = s.browserRequest(t, http.MethodGet, "/api/accounts", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("a headerless local read returned %d, want 200", resp.StatusCode)
	}
}

// Handlers that decode whatever arrives give away the preflight restriction
// for nothing: text/plain is a simple request, application/json is not.
func TestJSONEndpointsRequireJSONContentType(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	resp := s.browserRequest(t, http.MethodPost, "/api/settings",
		`{"key":"ct_probe","value":"x"}`, map[string]string{"Content-Type": "text/plain"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a text/plain body returned %d, want 415", resp.StatusCode)
	}
}

// Changing where an account points must not carry the old credential across.
// This holds independently of the origin checks: two unrelated controls, each
// sufficient on its own to break the exfiltration path.
func TestChangingHostRequiresThePasswordAgain(t *testing.T) {
	e := newEnv(t)

	if r := e.run("account", "add", "--name", "Work", "--email", "me@example.com",
		"--host", "imap.example.com", "--port", "993"); r.ExitCode != 0 {
		t.Fatalf("account add: %s%s", r.Stdout, r.Stderr)
	}

	s := e.startServer()

	local := map[string]string{"Content-Type": "application/json"}

	// A different host with no password: refused.
	resp := s.browserRequest(t, http.MethodPost, "/api/accounts",
		`{"id":"work","name":"Work","host":"imap.elsewhere.example","port":993,"user":"me@example.com"}`, local)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("retargeting without a password returned %d, want 400", resp.StatusCode)
	}

	// The same host, no password: the stored one is kept, as before.
	resp = s.browserRequest(t, http.MethodPost, "/api/accounts",
		`{"id":"work","name":"Work Mail","host":"imap.example.com","port":993,"user":"me@example.com"}`, local)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Errorf("an ordinary edit returned %d, want it to succeed", resp.StatusCode)
	}

	r := e.run("--json", "sql", "SELECT host, name FROM accounts WHERE id = 'work'")
	if !strings.Contains(r.Stdout, "imap.example.com") || !strings.Contains(r.Stdout, "Work Mail") {
		t.Errorf("the ordinary edit did not land:\n%s", r.Stdout)
	}
}

// A create whose slugged id collides with an existing account used to overwrite
// it silently.
func TestCreateDoesNotSilentlyOverwriteByName(t *testing.T) {
	e := newEnv(t)

	if r := e.run("account", "add", "--name", "Work", "--email", "me@example.com",
		"--host", "imap.example.com", "--port", "993"); r.ExitCode != 0 {
		t.Fatalf("account add: %s%s", r.Stdout, r.Stderr)
	}

	s := e.startServer()

	resp := s.browserRequest(t, http.MethodPost, "/api/accounts",
		`{"name":"Work","host":"imap.other.example","port":993}`,
		map[string]string{"Content-Type": "application/json"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("a colliding create returned %d, want 409", resp.StatusCode)
	}

	r := e.run("--json", "sql", "SELECT host FROM accounts WHERE id = 'work'")
	if strings.Contains(r.Stdout, "other") {
		t.Errorf("the existing account was overwritten:\n%s", r.Stdout)
	}
}
