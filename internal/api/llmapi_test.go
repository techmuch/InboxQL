package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/vault"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if _, err := store.InitDB(dir); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	vault.Init(filepath.Join(dir, "vault.key"))
	SetLLMDataDir(dir)
	t.Cleanup(func() {
		store.CloseDB()
		os.RemoveAll(dir)
	})
}

func TestLLMStatusAndConfig(t *testing.T) {
	setupTestDB(t)

	mux := http.NewServeMux()
	registerLLMRoutes(mux)

	// 1. Initial status - unconfigured
	req := httptest.NewRequest("GET", "/api/llm/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var status struct {
		Config   map[string]any    `json:"config"`
		Runtimes []llm.RuntimeInfo `json:"runtimes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if status.Config["configured"] != false {
		t.Fatalf("expected unconfigured initial state")
	}
	if len(status.Runtimes) == 0 {
		t.Fatalf("expected detected runtimes")
	}

	// 2. Save config
	savePayload := map[string]any{
		"provider":      "swama",
		"model":         "mlx-community/Llama-3.2-3B-Instruct-4bit",
		"endpoint":      "http://localhost:28100/v1",
		"autoStart":     true,
		"launchMode":    "headless",
		"lifecycleMode": "managed",
	}
	body, _ := json.Marshal(savePayload)
	req = httptest.NewRequest("POST", "/api/llm/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on config save, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Verify status after save
	req = httptest.NewRequest("GET", "/api/llm/status", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var updatedStatus struct {
		Config map[string]any `json:"config"`
	}
	json.NewDecoder(rec.Body).Decode(&updatedStatus)
	if updatedStatus.Config["provider"] != "swama" {
		t.Fatalf("expected provider swama, got %v", updatedStatus.Config["provider"])
	}
	if updatedStatus.Config["autoStart"] != true {
		t.Fatalf("expected autoStart true, got %v", updatedStatus.Config["autoStart"])
	}

	// 4. Disable
	req = httptest.NewRequest("POST", "/api/llm/disable", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d", rec.Code)
	}

	cfg, _ := store.GetLLMConfig()
	if cfg.Provider != "" {
		t.Fatalf("expected empty provider after disable, got %s", cfg.Provider)
	}
}

func TestLLMDetectRoute(t *testing.T) {
	setupTestDB(t)

	mux := http.NewServeMux()
	registerLLMRoutes(mux)

	req := httptest.NewRequest("GET", "/api/llm/detect", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res struct {
		Runtimes []llm.RuntimeInfo `json:"runtimes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(res.Runtimes) < 2 {
		t.Fatalf("expected at least 2 runtimes (swama and ollama), got %d", len(res.Runtimes))
	}
}
