package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RuntimeInfo describes a detected or queried LLM runtime environment.
type RuntimeInfo struct {
	Provider    string   `json:"provider"`
	Name        string   `json:"name"`
	Installed   bool     `json:"installed"`
	BinaryPath  string   `json:"binaryPath,omitempty"`
	Running     bool     `json:"running"`
	Endpoint    string   `json:"endpoint"`
	LaunchModes []string `json:"launchModes"`
	// Models are the bare names the runtime reports, kept for compatibility
	// with anything that just wants a list.
	Models []string `json:"models"`
	// Catalog is the same models with what each one is for, and how wide its
	// vectors are when it embeds.
	Catalog []Model `json:"catalog,omitempty"`
	// Embeddings says whether this runtime can embed at all. A runtime with a
	// chat model loaded and no embedding backend refuses every embedding
	// model it lists, which is a fact about the runtime rather than the model.
	Embeddings *bool `json:"embeddings,omitempty"`
}

// Classify fills in a runtime's catalog by probing the models worth probing.
//
// Separate from detection because it costs a model load: detection runs on
// every page render, and this runs when somebody asks what is available.
func (r *RuntimeInfo) Classify(ctx context.Context) {
	if !r.Running || len(r.Models) == 0 {
		return
	}
	r.Catalog = ClassifyModels(ctx, r.Endpoint, "", r.Provider, r.Models)

	supports := false
	for _, m := range r.Catalog {
		if m.Kind == KindEmbedding {
			supports = true
			break
		}
	}
	r.Embeddings = &supports
}

// DetectRuntimes scans the local system for supported LLM runtimes (Swama, Ollama).
func DetectRuntimes(ctx context.Context) []RuntimeInfo {
	var runtimes []RuntimeInfo

	// 1. Detect Swama
	swamaInfo := detectSwama(ctx)
	runtimes = append(runtimes, swamaInfo)

	// 2. Detect Ollama
	ollamaInfo := detectOllama(ctx)
	runtimes = append(runtimes, ollamaInfo)

	return runtimes
}

func findBinary(name string, extraPaths []string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, p := range extraPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func detectSwama(ctx context.Context) RuntimeInfo {
	home, _ := os.UserHomeDir()
	extraPaths := []string{
		"/usr/local/bin/swama",
		"/opt/homebrew/bin/swama",
		filepath.Join(home, ".swama", "bin", "swama"),
	}

	bin := findBinary("swama", extraPaths)
	endpoint := DefaultEndpoints[ProviderSwama]
	running := ProbeEndpoint(ctx, "http://localhost:28100/v1/models") ||
		ProbeEndpoint(ctx, "http://127.0.0.1:28100/v1/models")

	modes := []string{"headless"}
	if runtime.GOOS == "darwin" {
		modes = []string{"headless", "menubar"}
	}

	var models []string
	if running {
		models, _ = fetchSwamaModelsHTTP(ctx, "http://localhost:28100/v1/models")
	}
	if len(models) == 0 && bin != "" {
		models = listSwamaModelsCLI(ctx, bin)
	}

	return RuntimeInfo{
		Provider:    ProviderSwama,
		Name:        "Swama (macOS MLX)",
		Installed:   bin != "",
		BinaryPath:  bin,
		Running:     running,
		Endpoint:    endpoint,
		LaunchModes: modes,
		Models:      models,
	}
}

func detectOllama(ctx context.Context) RuntimeInfo {
	extraPaths := []string{
		"/usr/local/bin/ollama",
		"/opt/homebrew/bin/ollama",
		"/Applications/Ollama.app/Contents/Resources/ollama",
	}

	bin := findBinary("ollama", extraPaths)
	hasApp := false
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Ollama.app"); err == nil {
			hasApp = true
		}
	}

	endpoint := DefaultEndpoints[ProviderOllama]
	running := ProbeEndpoint(ctx, endpoint+"/api/tags") ||
		ProbeEndpoint(ctx, "http://127.0.0.1:11434/api/tags")

	modes := []string{"headless"}
	if hasApp {
		modes = []string{"headless", "app"}
	}

	var models []string
	if running {
		models, _ = fetchOllamaModelsHTTP(ctx, endpoint+"/api/tags")
	}
	if len(models) == 0 && bin != "" {
		models = listOllamaModelsCLI(ctx, bin)
	}

	return RuntimeInfo{
		Provider:    ProviderOllama,
		Name:        "Ollama",
		Installed:   bin != "" || hasApp,
		BinaryPath:  bin,
		Running:     running,
		Endpoint:    endpoint,
		LaunchModes: modes,
		Models:      models,
	}
}

// ProbeEndpoint returns true if a service responds at url within 1.5 seconds.
func ProbeEndpoint(ctx context.Context, url string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func fetchSwamaModelsHTTP(ctx context.Context, url string) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []string
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

func listSwamaModelsCLI(ctx context.Context, bin string) []string {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, bin, "list")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var models []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			models = append(models, fields[0])
		}
	}
	return models
}

func fetchOllamaModelsHTTP(ctx context.Context, url string) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []string
	for _, m := range result.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}
	return models, nil
}

func listOllamaModelsCLI(ctx context.Context, bin string) []string {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, bin, "list")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var models []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			models = append(models, fields[0])
		}
	}
	return models
}

// FetchRemoteModels queries an arbitrary endpoint (local or remote/cloud) for available models.
func FetchRemoteModels(ctx context.Context, endpoint, apiKey, provider string) ([]string, error) {
	if endpoint == "" {
		endpoint = DefaultEndpoints[provider]
	}
	endpoint = strings.TrimRight(endpoint, "/")

	if provider == ProviderOllama || strings.Contains(endpoint, ":11434") {
		return fetchOllamaModelsHTTP(ctx, endpoint+"/api/tags")
	}

	// Try OpenAI / v1 standard endpoints
	targets := []string{
		endpoint + "/models",
	}
	if !strings.HasSuffix(endpoint, "/v1") {
		targets = append(targets, endpoint+"/v1/models")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error

	for _, target := range targets {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}

		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		cancel()

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("%s returned %s: %s", target, resp.Status, strings.TrimSpace(string(body)))
			continue
		}

		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err == nil && len(result.Data) > 0 {
			var models []string
			for _, m := range result.Data {
				if m.ID != "" {
					models = append(models, m.ID)
				}
			}
			return models, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("could not discover models at %s", endpoint)
}
