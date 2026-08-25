package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/inboxql/internal/store"
)

func TestIsLoopback(t *testing.T) {
	loopbackAddrs := []string{
		"127.0.0.1:8080",
		"127.0.0.1:12345",
		"127.0.0.42:9000",
		"[::1]:8080",
		"::1",
		"localhost:8080",
		"localhost",
	}
	for _, addr := range loopbackAddrs {
		if !IsLoopback(addr) {
			t.Errorf("expected %q to be loopback", addr)
		}
	}

	externalAddrs := []string{
		"192.168.1.50:8080",
		"10.0.0.1:8080",
		"172.16.0.1:8080",
		"8.8.8.8:53",
		"example.com:80",
	}
	for _, addr := range externalAddrs {
		if IsLoopback(addr) {
			t.Errorf("expected %q NOT to be loopback", addr)
		}
	}
}

func TestMiddlewareLoopbackAutoAuth(t *testing.T) {
	// Passwordless loopback access is opt-in now, so the behaviour this test
	// describes only exists once it is switched on.
	SetTrustLocal(true)
	t.Cleanup(func() { SetTrustLocal(false) })

	newTestStore(t)

	called := false
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		u := r.Context().Value(UserContextKey).(*store.User)
		if u == nil || u.Username != "admin@inboxql.local" {
			t.Errorf("unexpected user in context: %+v", u)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Request from localhost should succeed without cookies
	reqLocal := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	reqLocal.RemoteAddr = "127.0.0.1:54321"
	recLocal := httptest.NewRecorder()
	handler.ServeHTTP(recLocal, reqLocal)

	if recLocal.Code != http.StatusOK {
		t.Errorf("expected 200 OK for localhost, got %d", recLocal.Code)
	}
	if !called {
		t.Errorf("expected handler to be called")
	}

	// 2. Request from external address without cookie should be 401 Unauthorized
	called = false
	reqExternal := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	reqExternal.RemoteAddr = "192.168.1.100:54321"
	recExternal := httptest.NewRecorder()
	handler.ServeHTTP(recExternal, reqExternal)

	if recExternal.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for external IP, got %d", recExternal.Code)
	}
	if called {
		t.Errorf("expected handler NOT to be called for unauthorized external request")
	}
}

// newTestStore prepares an isolated database with one administrator.
func newTestStore(t *testing.T) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)
	if err := CreateInitialUser("admin@inboxql.local", "testpass123"); err != nil {
		t.Fatalf("CreateInitialUser: %v", err)
	}
}

func okHandler() http.Handler {
	return Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func status(h http.Handler, remoteAddr string, headers map[string]string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// The default must require a password. Auto-authenticating loopback out of the
// box published every protected endpoint to anyone who could reach a reverse
// proxy on the same host — and a proxy is what `iql start` recommends, because
// InboxQL terminates no TLS of its own.
func TestPasswordRequiredByDefault(t *testing.T) {
	newTestStore(t)
	SetTrustLocal(false)

	if code := status(okHandler(), "127.0.0.1:54321", nil); code != http.StatusUnauthorized {
		t.Errorf("loopback request got %d without --trust-local, want 401", code)
	}
}

// The regression that started this. A reverse proxy on the same host relays
// every request over loopback, so the peer address describes the proxy and
// says nothing about who is on the other end of it.
func TestProxiedRequestIsNotTrusted(t *testing.T) {
	newTestStore(t)
	SetTrustLocal(true)
	t.Cleanup(func() { SetTrustLocal(false) })

	h := okHandler()

	// Sanity check: a genuine local client is allowed through.
	if code := status(h, "127.0.0.1:54321", nil); code != http.StatusOK {
		t.Fatalf("a direct loopback request got %d, want 200 — the fixture is wrong", code)
	}

	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Forwarded-Host"} {
		code := status(h, "127.0.0.1:54321", map[string]string{header: "203.0.113.9"})
		if code != http.StatusUnauthorized {
			t.Errorf("a loopback request carrying %s got %d, want 401 — it was relayed, "+
				"so the peer address is the proxy's, not the client's", header, code)
		}
	}
}

// An external client is refused whether or not the flag is set; the flag
// widens what loopback may do, never what the network may do.
func TestExternalNeverTrusted(t *testing.T) {
	newTestStore(t)

	for _, trust := range []bool{false, true} {
		SetTrustLocal(trust)
		if code := status(okHandler(), "203.0.113.9:44444", nil); code != http.StatusUnauthorized {
			t.Errorf("external request got %d with trustLocal=%v, want 401", code, trust)
		}
	}
	SetTrustLocal(false)
}

// A valid session must work from anywhere — the flag governs the passwordless
// path only, and must not become a second gate on legitimate sessions.
func TestSessionAuthenticatesRegardlessOfFlag(t *testing.T) {
	newTestStore(t)
	SetTrustLocal(false)

	session, err := Authenticate("admin@inboxql.local", "testpass123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	req.RemoteAddr = "203.0.113.9:44444"
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session.ID})
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	okHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("an authenticated external request got %d, want 200", rec.Code)
	}
}

func TestViaProxyDetection(t *testing.T) {
	cases := map[string]bool{
		"X-Forwarded-For":  true,
		"X-Real-Ip":        true,
		"Forwarded":        true,
		"X-Forwarded-Host": true,
		"User-Agent":       false,
		"Accept":           false,
	}
	for header, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(header, "value")
		if got := viaProxy(req); got != want {
			t.Errorf("viaProxy with %s = %v, want %v", header, got, want)
		}
	}
	if viaProxy(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Error("a request with no headers was treated as proxied")
	}
}
