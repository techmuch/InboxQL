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

// The default posture: a password is required, including from this machine.
//
// The regression this pins: loopback connections were auto-authenticated as
// the administrator. A reverse proxy on the same host relays every request
// over loopback, so that published every endpoint below to anyone who could
// reach the proxy — and a proxy is what `iql start` recommends, because
// InboxQL terminates no TLS of its own.
func TestUnauthenticatedByDefault(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()

	for _, path := range protectedEndpoints {
		code, _ := s.get(t, path, nil)
		if code != http.StatusUnauthorized {
			t.Errorf("GET %s returned %d without credentials, want 401", path, code)
		}
	}

	if !strings.Contains(s.Output(), "password required") {
		t.Errorf("the startup banner did not state the auth posture:\n%s", s.Output())
	}
}

// The same request, relayed by a proxy that adds no headers at all. The peer
// address is loopback; the client is not. Without --trust-local this must be
// refused exactly as a direct external request is.
func TestProxiedRequestIsRefusedByDefault(t *testing.T) {
	e := newEnv(t)
	s := e.startServer()
	proxy := startTCPProxy(t, s.Addr)

	for _, path := range protectedEndpoints {
		code := getStatus(t, "http://"+proxy+path, nil)
		if code != http.StatusUnauthorized {
			t.Errorf("GET %s through a same-host proxy returned %d, want 401", path, code)
		}
	}
}

// --trust-local is the opt-in for a single-user desktop install.
func TestTrustLocalAllowsLocalRequests(t *testing.T) {
	e := newEnv(t)
	s := e.startServer("--trust-local")

	// The assertion is that authentication passed, not that a bare GET is a
	// valid request for every route — /api/settings answers 400 to one, which
	// it could only do after being let through.
	for _, path := range protectedEndpoints {
		code, _ := s.get(t, path, nil)
		if code == http.StatusUnauthorized {
			t.Errorf("GET %s from localhost with --trust-local returned 401; the flag did not take effect", path)
		}
	}

	if !strings.Contains(s.Output(), "--trust-local") {
		t.Errorf("the startup banner did not disclose passwordless access:\n%s", s.Output())
	}
}

// Defence in depth: even with the flag on, a request that arrived through a
// proxy is refused. Real proxies set these headers; a bare TCP forwarder does
// not, which is precisely why passwordless access is opt-in rather than
// something InboxQL tries to detect its way out of.
func TestTrustLocalStillRefusesForwardedRequests(t *testing.T) {
	e := newEnv(t)
	s := e.startServer("--trust-local")

	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Forwarded-Host"} {
		code, _ := s.get(t, "/api/messages", map[string]string{header: "203.0.113.9"})
		if code != http.StatusUnauthorized {
			t.Errorf("a loopback request carrying %s returned %d, want 401", header, code)
		}
	}
}

// Binding publicly while trusting local is a combination worth shouting about,
// since a tunnel or proxy then reaches the passwordless path.
func TestTrustLocalWarnsOnPublicBind(t *testing.T) {
	e := newEnv(t)
	port := freePort(t)
	s := e.startServerAt(":"+itoa(port), "--trust-local")

	if !strings.Contains(s.Output(), "warning") {
		t.Errorf("no warning for --trust-local with a public bind:\n%s", s.Output())
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
