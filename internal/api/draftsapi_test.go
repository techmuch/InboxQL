package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

func TestDraftFromTheInterface(t *testing.T) {
	setupTestDB(t)
	store.SaveAccount(&account.Account{ID: "a", Name: "Me", Email: "me@example.com", User: "me@example.com"})
	mux := http.NewServeMux()
	registerDraftRoutes(mux)

	rec, out := call(t, mux, "POST", "/api/drafts", map[string]any{"accountId": "nope", "to": []string{"x@y.com"}})
	if rec.Code != 400 {
		t.Errorf("unknown account = %d %v", rec.Code, out)
	}
	rec, out = call(t, mux, "POST", "/api/drafts", map[string]any{"accountId": "a", "to": []string{"Ada@Example.com, bob@example.com"},
		"subject": "Hello", "body": "Hi", "queue": true})
	if rec.Code != 201 || !strings.Contains(out["note"].(string), "iql outbox approve") {
		t.Fatalf("queue = %d %v", rec.Code, out)
	}
	d := out["draft"].(map[string]any)
	if d["status"] != "queued" || d["origin"] != "human" || len(d["to"].([]any)) != 2 || d["to"].([]any)[0] != "ada@example.com" {
		t.Errorf("draft %v", d)
	}
	if rec, _ := call(t, mux, "POST", "/api/drafts", map[string]any{"accountId": "a", "queue": true, "subject": "x"}); rec.Code != 400 {
		t.Errorf("queue with no recipient = %d", rec.Code)
	}
	_, out = call(t, mux, "GET", "/api/drafts?status=queued", nil)
	if len(out["drafts"].([]any)) != 1 {
		t.Errorf("queued list %v", out)
	}
}
