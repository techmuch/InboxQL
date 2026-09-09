package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func TestContactsAPI(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)

	auth.SetTrustLocal(true)
	t.Cleanup(func() { auth.SetTrustLocal(false) })

	if err := auth.CreateInitialUser("admin@inboxql.local", "testpass123"); err != nil {
		t.Fatalf("CreateInitialUser: %v", err)
	}

	// Create user account
	if err := store.SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	// Create a message from Alice to Me
	m1 := &message.Message{
		ID: "m1", AccountID: "acct", MessageID: "<t1@acme.com>",
		From: "Alice <alice@acme.com>", To: []string{"me@example.com"},
		Subject: "Hello Alice", Body: "How are you?",
		Date: time.Now(), ContentHash: "ch1",
		Header: []byte("Message-ID: <t1@acme.com>\r\n"),
	}
	if err := store.SaveMessage(m1); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	h, err := Router()
	if err != nil {
		t.Fatalf("Router: %v", err)
	}

	authReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.RemoteAddr = "127.0.0.1:54321"
		return r
	}

	// 1. POST /api/contacts/tags -> add tag "vip"
	tagBody, _ := json.Marshal(map[string]string{
		"address": "alice@acme.com",
		"tag":     "vip",
		"action":  "add",
	})
	req := authReq(http.MethodPost, "/api/contacts/tags", tagBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/contacts/tags status = %d: %s", rec.Code, rec.Body.String())
	}
	var tagResp struct {
		Address string   `json:"address"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tagResp); err != nil {
		t.Fatalf("unmarshal tags response: %v", err)
	}
	if len(tagResp.Tags) != 1 || tagResp.Tags[0] != "vip" {
		t.Errorf("tags = %v, want [vip]", tagResp.Tags)
	}

	// 2. POST /api/contacts/notes -> set note
	noteBody, _ := json.Marshal(map[string]string{
		"address": "alice@acme.com",
		"notes":   "VIP client notes.",
	})
	req = authReq(http.MethodPost, "/api/contacts/notes", noteBody)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/contacts/notes status = %d: %s", rec.Code, rec.Body.String())
	}

	// 3. GET /api/contacts/responsiveness?address=alice@acme.com
	req = authReq(http.MethodGet, "/api/contacts/responsiveness?address=alice@acme.com", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/contacts/responsiveness status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp store.ContactResponsiveness
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal responsiveness response: %v", err)
	}
	if resp.Address != "alice@acme.com" {
		t.Errorf("resp.Address = %q, want alice@acme.com", resp.Address)
	}
	if resp.AwaitingMyReplyCount != 1 {
		t.Errorf("resp.AwaitingMyReplyCount = %d, want 1", resp.AwaitingMyReplyCount)
	}

	// 4. POST /api/contacts/tags -> remove tag "vip"
	untagBody, _ := json.Marshal(map[string]string{
		"address": "alice@acme.com",
		"tag":     "vip",
		"action":  "remove",
	})
	req = authReq(http.MethodPost, "/api/contacts/tags", untagBody)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/contacts/tags status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tagResp); err != nil {
		t.Fatalf("unmarshal tags response: %v", err)
	}
	if len(tagResp.Tags) != 0 {
		t.Errorf("tags after removal = %v, want empty", tagResp.Tags)
	}
}
