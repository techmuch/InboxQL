//go:build e2e

package e2e

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// protectedEndpoints are routes that must never answer without authentication.
// /api/messages is the mail itself.
var protectedEndpoints = []string{
	"/api/accounts",
	"/api/messages",
	"/api/profile",
	"/api/errors",
	"/api/settings",
}

// The default: localhost is served without a password. InboxQL binds loopback
// unless told otherwise, so reaching the port already means being on this
// machine, and prompting the owner of the machine protects nothing.
func TestLocalhostNeedsNoPasswordByDefault(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	for _, path := range protectedEndpoints {
		code, _ := s.get(t, path, nil)
		if code == http.StatusUnauthorized {
			t.Errorf("GET %s from localhost returned 401; local access should need no password", path)
		}
	}

	if !strings.Contains(s.Output(), "passwordless on this machine") {
		t.Errorf("the startup banner did not state the auth posture:\n%s", s.Output())
	}
}

// Serving beyond localhost withdraws it. The audience is no longer the person
// at the keyboard, so the password comes back without anyone having to ask.
func TestPublicBindRequiresAPassword(t *testing.T) {
	e := newEnv(t)
	s := e.startServerAt(":" + itoa(freePort(t)))

	for _, path := range protectedEndpoints {
		code, _ := s.get(t, path, nil)
		if code != http.StatusUnauthorized {
			t.Errorf("GET %s on a public bind returned %d, want 401", path, code)
		}
	}

	if !strings.Contains(s.Output(), "password required") {
		t.Errorf("the banner did not explain why a password is required:\n%s", s.Output())
	}
}

// --require-password puts it back even on localhost.
func TestRequirePasswordOverridesLocalTrust(t *testing.T) {
	e := newEnv(t)
	s := e.startServer("--require-password")

	if code, _ := s.get(t, "/api/messages", nil); code != http.StatusUnauthorized {
		t.Errorf("GET /api/messages with --require-password returned %d, want 401", code)
	}
}

// The header check is what protects the default. A real proxy — nginx with a
// forwarding header, Caddy, Traefik — relays requests over loopback, and the
// peer address then says nothing about who sent them.
func TestForwardedRequestsNeverGetLocalTrust(t *testing.T) {
	e := newEnv(t)
	s := e.startServer() // default: passwordless locally

	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Forwarded-Host"} {
		for _, path := range protectedEndpoints {
			code, _ := s.get(t, path, map[string]string{header: "203.0.113.9"})
			if code != http.StatusUnauthorized {
				t.Errorf("GET %s carrying %s returned %d, want 401", path, header, code)
			}
		}
	}
}

// A bare TCP forwarder that sets no headers is the one case the header check
// cannot see, and it is why the listen address matters: a loopback bind is not
// reachable from off the machine in the first place, so a forwarder has to be
// running here, put there deliberately by whoever owns the machine.
//
// This test documents the boundary rather than asserting a refusal.
func TestHeaderlessProxyIsTheKnownLimit(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()
	proxy := startTCPProxy(t, s.Addr)

	code := getStatus(t, "http://"+proxy+"/api/messages", nil)
	if code != http.StatusOK {
		t.Logf("a headerless forwarder was refused (%d); the protection is stronger than documented", code)
	}

	// What must hold: the same deployment with a public listen address is
	// refused, which is the configuration someone would actually reach.
	public := e.startServerAt(":" + itoa(freePort(t)))
	if got := getStatus(t, "http://"+startTCPProxy(t, public.Addr)+"/api/messages", nil); got != http.StatusUnauthorized {
		t.Errorf("a proxied request to a public bind returned %d, want 401", got)
	}
}

// --trust-local forces passwordless access on where the listen address would
// otherwise withdraw it.
func TestTrustLocalForcesItOnAPublicBind(t *testing.T) {
	e := newEnv(t)
	s := e.startServerAt(":"+itoa(freePort(t)), "--trust-local")

	// The assertion is that authentication passed, not that a bare GET is a
	// valid request for every route — /api/settings answers 400 to one, which
	// it could only do after being let through.
	for _, path := range protectedEndpoints {
		code, _ := s.get(t, path, nil)
		if code == http.StatusUnauthorized {
			t.Errorf("GET %s from localhost with --trust-local returned 401; the flag did not take effect", path)
		}
	}

	if !strings.Contains(s.Output(), "warning") {
		t.Errorf("forcing passwordless access onto a public address did not warn:\n%s", s.Output())
	}
}

// Nothing overrides the forwarded-header check, including --trust-local.
func TestTrustLocalStillRefusesForwardedRequests(t *testing.T) {
	e := newEnv(t)
	s := e.startServerAt(":"+itoa(freePort(t)), "--trust-local")

	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Forwarded-Host"} {
		code, _ := s.get(t, "/api/messages", map[string]string{header: "203.0.113.9"})
		if code != http.StatusUnauthorized {
			t.Errorf("a loopback request carrying %s returned %d, want 401", header, code)
		}
	}
}

// A deployment can demand a password from the environment, which is how you
// set it in a service unit or a container without editing a command line.
func TestRequirePasswordFromEnvironment(t *testing.T) {
	e := newEnv(t)

	required := e.startServerEnv([]string{"INBOXQL_REQUIRE_PASSWORD=1"})
	if code, _ := required.get(t, "/api/messages", nil); code != 401 {
		t.Errorf("INBOXQL_REQUIRE_PASSWORD=1 returned %d, want 401", code)
	}

	// Anything not clearly true leaves the default alone rather than being
	// read as an instruction nobody gave.
	garbage := e.startServerEnv([]string{"INBOXQL_REQUIRE_PASSWORD=banana"})
	if code, _ := garbage.get(t, "/api/messages", nil); code == 401 {
		t.Error("an unrecognised value was read as true")
	}
}

// A real login must work, and its session must survive across requests.
func TestLoginGrantsASession(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	body := strings.NewReader(`{"username":"` + adminUser + `","password":"` + adminPassword + `"}`)
	resp, err := http.Post(s.URL("/api/login"), "application/json", body)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login returned %d, want 200", resp.StatusCode)
	}

	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "session_id" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}

	req, _ := http.NewRequest(http.MethodGet, s.URL("/api/profile"), nil)
	req.AddCookie(session)
	authed, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("authenticated request: %v", err)
	}
	defer authed.Body.Close()

	if authed.StatusCode != http.StatusOK {
		t.Errorf("an authenticated request returned %d, want 200", authed.StatusCode)
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	body := strings.NewReader(`{"username":"` + adminUser + `","password":"wrong"}`)
	resp, err := http.Post(s.URL("/api/login"), "application/json", body)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("login succeeded with the wrong password")
	}
}

// startTCPProxy stands up a byte-for-byte forwarder and returns its address.
//
// Deliberately dumb: it adds no headers, which is the worst case for anything
// trying to detect relaying. nginx, Caddy and Traefik all add more than this.
func startTCPProxy(t *testing.T, target string) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	var wg sync.WaitGroup
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			upstream, err := net.Dial("tcp", target)
			if err != nil {
				client.Close()
				continue
			}
			wg.Add(2)
			go func() { defer wg.Done(); io.Copy(upstream, client); upstream.Close() }()
			go func() { defer wg.Done(); io.Copy(client, upstream); client.Close() }()
		}
	}()

	return ln.Addr().String()
}

func getStatus(t *testing.T, url string, headers map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
