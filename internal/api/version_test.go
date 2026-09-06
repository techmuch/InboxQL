package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionEndpoint(t *testing.T) {
	SetVersionInfo(VersionInfo{
		Version:    "0.0.31-test",
		Revision:   "abc1234",
		Dev:        true,
		InstanceID: "inst-12345",
	})

	h, err := Router()
	if err != nil {
		t.Fatalf("Router(): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Errorf("Cache-Control header missing")
	}

	var resp VersionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshaling response: %v", err)
	}

	if resp.Version != "0.0.31-test" {
		t.Errorf("version = %q, want %q", resp.Version, "0.0.31-test")
	}
	if resp.Revision != "abc1234" {
		t.Errorf("revision = %q, want %q", resp.Revision, "abc1234")
	}
	if !resp.Dev {
		t.Errorf("dev = %v, want true", resp.Dev)
	}
	if resp.InstanceID != "inst-12345" {
		t.Errorf("instanceId = %q, want %q", resp.InstanceID, "inst-12345")
	}

	// Post method should return 405
	postReq := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/version status = %d, want 405", postRec.Code)
	}
}
