package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	tempDir := t.TempDir()
	origDB := filepath.Join(tempDir, "inboxql.db")
	_ = os.Remove(origDB)

	_, err := store.InitDB(tempDir)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer store.CloseDB()

	// Create initial admin user
	if err := CreateInitialUser("admin@inboxql.local", "testpass123"); err != nil {
		t.Fatalf("CreateInitialUser failed: %v", err)
	}

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
