package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/inboxql/internal/avatar"
)

func TestAvatarRoundTrip(t *testing.T) {
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+avatar.Hash("alice@example.com") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
			return
		}
		http.NotFound(w, r)
	}))
	defer gr.Close()
	t.Setenv("INBOXQL_GRAVATAR_URL", gr.URL+"/")
	importDataDir = t.TempDir()
	mux := http.NewServeMux()
	registerAvatarRoutes(mux)

	if rec, _ := call(t, mux, "GET", "/api/avatars/image?address=alice@example.com", nil); rec.Code != 404 {
		t.Errorf("before fetching = %d, want 404", rec.Code)
	}
	if rec, out := call(t, mux, "POST", "/api/avatars/gravatar", map[string]any{"address": "alice@example.com"}); rec.Code != 200 || out["status"] != "stored" {
		t.Fatalf("fetch = %d %v", rec.Code, out)
	}
	rec, _ := call(t, mux, "GET", "/api/avatars/image?address=Alice@Example.com", nil)
	if rec.Code != 200 || rec.Body.String() != "png" || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("image = %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if _, out := call(t, mux, "GET", "/api/avatars/stored", nil); out["stored"].(map[string]any)["alice@example.com"] == nil {
		t.Errorf("stored = %v", out)
	}
	if _, out := call(t, mux, "POST", "/api/avatars/gravatar", map[string]any{"address": "bob@example.com"}); out["status"] != "none" {
		t.Errorf("no gravatar = %v", out)
	}
	if rec, _ := call(t, mux, "POST", "/api/avatars/gravatar", map[string]any{"address": "http://evil/"}); rec.Code != 400 {
		t.Errorf("non-address = %d", rec.Code)
	}
	if _, out := call(t, mux, "DELETE", "/api/avatars?address=alice@example.com", nil); out["status"] != "absent" {
		t.Errorf("after delete = %v", out)
	}
}
