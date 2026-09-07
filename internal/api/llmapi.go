package api

import (
	"encoding/json"
	"net/http"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
)

var llmDataDir string

// SetLLMDataDir sets the data directory used for LLM runtime logs.
func SetLLMDataDir(dir string) {
	llmDataDir = dir
}

func registerLLMRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/llm/status", handleLLMStatus)
	mux.HandleFunc("GET /api/llm/detect", handleLLMDetect)
	mux.HandleFunc("POST /api/llm/start", handleLLMStart)
	mux.HandleFunc("POST /api/llm/stop", handleLLMStop)
	mux.HandleFunc("POST /api/llm/models", handleLLMModels)
	mux.HandleFunc("POST /api/llm/test", handleLLMTest)
	mux.HandleFunc("POST /api/llm/config", handleLLMConfigSave)
	mux.HandleFunc("POST /api/llm/disable", handleLLMDisable)
}

func handleLLMStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := store.GetLLMConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get LLM config: %v", err)
		return
	}

	runtimes := llm.DetectRuntimes(r.Context())

	var activeRunning bool
	var activeEndpoint string
	if cfg.Provider != "" {
		activeEndpoint = cfg.Endpoint
		if activeEndpoint == "" {
			activeEndpoint = llm.DefaultEndpoints[cfg.Provider]
		}
		// Determine if active provider is running
		for _, rt := range runtimes {
			if rt.Provider == cfg.Provider {
				activeRunning = rt.Running
				break
			}
		}
		if !activeRunning && activeEndpoint != "" {
			activeRunning = llm.ProbeEndpoint(r.Context(), activeEndpoint)
		}
	}

	resp := map[string]any{
		"config":   cfg.Redacted(),
		"runtimes": runtimes,
		"activeStatus": map[string]any{
			"running":  activeRunning,
			"endpoint": activeEndpoint,
			"provider": cfg.Provider,
			"model":    cfg.Model,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleLLMDetect(w http.ResponseWriter, r *http.Request) {
	runtimes := llm.DetectRuntimes(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"runtimes": runtimes,
	})
}

func handleLLMStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider   string `json:"provider"`
		LaunchMode string `json:"launchMode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	targetDir := llmDataDir
	if targetDir == "" {
		targetDir = importDataDir
	}

	proc, err := llm.GetProcessManager().StartServer(r.Context(), req.Provider, req.LaunchMode, targetDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start %s: %v", req.Provider, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"provider":   proc.Provider,
		"pid":        proc.PID,
		"launchMode": proc.LaunchMode,
		"logPath":    proc.LogPath,
	})
}

func handleLLMStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	if err := llm.GetProcessManager().StopServer(req.Provider); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stop %s: %v", req.Provider, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"provider": req.Provider,
	})
}

func handleLLMModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
		Provider string `json:"provider"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	apiKey := req.APIKey
	if apiKey == "" {
		stored, err := store.GetLLMConfig()
		if err == nil && (req.Provider == "" || stored.Provider == req.Provider) {
			apiKey = stored.APIKey
		}
	}

	models, err := llm.FetchRemoteModels(r.Context(), req.Endpoint, apiKey, req.Provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to fetch models: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"models": models,
	})
}

func handleLLMTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	var testCfg store.LLMConfig
	if req.Provider == "" {
		var err error
		testCfg, err = store.GetLLMConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot load config: %v", err)
			return
		}
	} else {
		apiKey := req.APIKey
		if apiKey == "" {
			stored, err := store.GetLLMConfig()
			if err == nil && stored.Provider == req.Provider {
				apiKey = stored.APIKey
			}
		}
		testCfg = store.LLMConfig{
			Provider: req.Provider,
			Model:    req.Model,
			Endpoint: req.Endpoint,
			APIKey:   apiKey,
		}
	}

	res, err := llm.TestCompletion(r.Context(), testCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "LLM test failed: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func handleLLMConfigSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		Endpoint      string `json:"endpoint"`
		APIKey        string `json:"apiKey"`
		AutoStart     bool   `json:"autoStart"`
		LaunchMode    string `json:"launchMode"`
		LifecycleMode string `json:"lifecycleMode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	if req.Provider == "" {
		if err := store.SaveLLMConfig(store.LLMConfig{}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear LLM config: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "disabled",
			"config": store.LLMConfig{}.Redacted(),
		})
		return
	}

	apiKey := req.APIKey
	if apiKey == "" {
		existing, err := store.GetLLMConfig()
		if err == nil && existing.Provider == req.Provider {
			apiKey = existing.APIKey
		}
	}

	cfg := store.LLMConfig{
		Provider:      req.Provider,
		Model:         req.Model,
		Endpoint:      req.Endpoint,
		APIKey:        apiKey,
		AutoStart:     req.AutoStart,
		LaunchMode:    req.LaunchMode,
		LifecycleMode: req.LifecycleMode,
	}

	if err := store.SaveLLMConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save LLM config: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "saved",
		"config": cfg.Redacted(),
	})
}

func handleLLMDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := store.SaveLLMConfig(store.LLMConfig{}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear config: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "disabled",
		"config": store.LLMConfig{}.Redacted(),
	})
}
