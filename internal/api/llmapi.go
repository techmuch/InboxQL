package api

import (
	"encoding/json"
	"net/http"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
	"strings"
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
	mux.HandleFunc("GET /api/llm/profiles", handleLLMProfilesList)
	mux.HandleFunc("POST /api/llm/profiles", handleLLMProfileSave)
	mux.HandleFunc("DELETE /api/llm/profiles", handleLLMProfileDelete)
	mux.HandleFunc("POST /api/llm/profiles/default", handleLLMProfileDefault)
	mux.HandleFunc("POST /api/llm/refresh", handleLLMRefresh)
	mux.HandleFunc("GET /api/llm/similarity", handleSimilarityHistogram)
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

	profiles, err := store.ListLLMProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list profiles: %v", err)
		return
	}
	redacted := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		redacted = append(redacted, p.Redacted())
	}

	resp := map[string]any{
		"config":   cfg.Redacted(),
		"profiles": redacted,
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

// --- model profiles ---------------------------------------------------------

func handleLLMProfilesList(w http.ResponseWriter, r *http.Request) {
	profiles, err := store.ListLLMProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list profiles: %v", err)
		return
	}
	out := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Redacted())
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"profiles": out})
}

// handleLLMProfileSave creates or edits one profile.
//
// The API key is write-only: it is accepted here and never returned, and an
// absent key on an edit keeps the stored one rather than clearing it. A form
// that cannot show the current key must not be able to erase it by saving.
func handleLLMProfileSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string  `json:"name"`
		Provider  string  `json:"provider"`
		Model     string  `json:"model"`
		Endpoint  string  `json:"endpoint"`
		APIKey    *string `json:"apiKey"`
		IsDefault bool    `json:"isDefault"`
		AutoStart bool    `json:"autoStart"`
		Launch    string  `json:"launchMode"`
		Purpose   string  `json:"purpose"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	existing, err := store.GetLLMProfile(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	p := existing
	if p == nil {
		p = &store.LLMProfile{Name: req.Name}
	}
	p.Provider, p.Model, p.Endpoint = req.Provider, req.Model, req.Endpoint
	p.IsDefault, p.AutoStart = req.IsDefault, req.AutoStart
	if req.Purpose != "" {
		p.Purpose = req.Purpose
	}
	// An embedding profile's width is probed rather than declared: only the
	// model knows it, and a stored vector that does not match cannot be
	// compared against anything.
	if p.Purpose == store.PurposeEmbedding {
		dims, err := llm.ProbeEmbedding(r.Context(), p.Endpoint, p.APIKey, p.Provider, p.Model)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				"%s does not embed at %s: %v", p.Model, p.Endpoint, err)
			return
		}
		p.Dimensions = dims
	}
	if req.Launch != "" {
		p.LaunchMode = req.Launch
	}
	// nil means "leave it alone"; an empty string means "remove it".
	if req.APIKey != nil {
		p.APIKey = *req.APIKey
	}

	if err := store.SaveLLMProfile(p); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p.Redacted())
}

func handleLLMProfileDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Refused rather than cascaded: an annotator whose profile vanished would
	// fail at run time, which is later and less obvious than failing here.
	users, err := annotatorsUsingProfile(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if len(users) > 0 {
		writeError(w, http.StatusConflict,
			"%s is used by %d annotator(s): %s", name, len(users), strings.Join(users, ", "))
		return
	}

	if err := store.DeleteLLMProfile(name); err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"removed": name})
}

func handleLLMProfileDefault(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if err := store.SetDefaultLLMProfile(req.Name); err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"default": req.Name})
}

// handleLLMRefresh re-scans one runtime, or all of them, and reports what
// happened rather than only what it found.
//
// # Why this is not just /detect
//
// /detect answers with runtimes and nothing else, so a scan that failed and a
// scan that found nothing are the same response. The UI's refresh button ate
// its errors on top of that, which made "the daemon is up but its model
// endpoint is broken" render as "no new models" — a fault that reads as a
// normal empty state, which is the failure shape this project keeps meeting.
//
// So: the per-runtime error comes back, and the caller can say so.
func handleLLMRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	// A bodyless POST means "everything", so refreshing does not require the
	// caller to know what is installed.
	_ = json.NewDecoder(r.Body).Decode(&req)

	runtimes := llm.DetectRuntimes(r.Context())

	type runtimeReport struct {
		llm.RuntimeInfo
		// Problem explains an installed, running runtime that returned no
		// models — the case that used to be silent.
		Problem string `json:"problem,omitempty"`
	}

	out := make([]runtimeReport, 0, len(runtimes))
	found := 0
	for _, rt := range runtimes {
		if req.Provider != "" && rt.Provider != req.Provider {
			continue
		}
		// Ask each running runtime what its models are actually for. A bare
		// list of names would offer an embedding model for completions and a
		// chat model for embeddings, and both fail at use rather than here.
		rt.Classify(r.Context())

		report := runtimeReport{RuntimeInfo: rt}
		switch {
		case !rt.Installed:
			report.Problem = rt.Name + " is not installed on this machine."
		case !rt.Running:
			report.Problem = rt.Name + " is installed but not running. Start it to list its models."
		case len(rt.Models) == 0:
			report.Problem = rt.Name + " is running but reported no models. Pull one, or check " +
				rt.Endpoint + " is reachable."
		case rt.Embeddings != nil && !*rt.Embeddings:
			// Said plainly, because otherwise the only symptom is that no
			// embedding profile can be configured and nothing explains why.
			report.Problem = rt.Name + " serves chat only — none of its models embed. " +
				"Pull an embedding model to use similarity."
		}
		found += len(rt.Models)
		out = append(out, report)
	}

	if req.Provider != "" && len(out) == 0 {
		writeError(w, http.StatusNotFound, "no runtime named %q", req.Provider)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"runtimes": out,
		"models":   found,
	})
}

// annotatorsUsingProfile names the annotators that would break if a profile
// went away.
func annotatorsUsingProfile(name string) ([]string, error) {
	annotators, err := store.ListAnnotators()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range annotators {
		if a.Profile == name {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

// handleSimilarityHistogram reports how many neighbours a message has at each
// threshold.
//
// The number beside the slider. A cosine threshold is not portable between
// embedding models and the values sit in a band whose width the model decides
// — measured on a real archive with bge-m3, everything lived between 0.26 and
// 0.65, so a plausible-sounding 0.8 returns nothing at all. Without the
// distribution the control is a dial with no markings.
func handleSimilarityHistogram(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	buckets := []float64{0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9}
	hist, err := store.SimilarityHistogram(id, buckets)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":        id,
		"buckets":   buckets,
		"counts":    hist,
		"threshold": store.DefaultSimilarityThreshold(),
	})
}
