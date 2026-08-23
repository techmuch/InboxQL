// Package llm is UEA's optional completion gateway.
//
// "Optional" is the important word. UEA works without any provider configured:
// the analyze and draft commands fall back to emitting structured context for
// an external agent to reason over. Configuring a provider turns those same
// commands into ones that return prose. Nothing else in UEA depends on this
// package, and no email content leaves the machine unless a remote provider is
// deliberately configured.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/uea/internal/store"
)

// ErrNotConfigured is returned by New when no provider has been set.
var ErrNotConfigured = errors.New("no LLM provider configured")

// Provider turns a prompt into text.
type Provider interface {
	// Name identifies the provider for diagnostics.
	Name() string
	// Complete returns a single completion for the given system and user text.
	Complete(ctx context.Context, system, user string) (string, error)
}

// Supported provider identifiers.
const (
	ProviderOllama = "ollama"
	// ProviderOpenAI speaks the /v1/chat/completions protocol, which OpenAI,
	// Anthropic-compatible gateways, llama.cpp's server, LM Studio, vLLM and
	// most hosted proxies all implement. One implementation covers all of them.
	ProviderOpenAI = "openai"
)

// Supported lists the provider identifiers accepted by New.
var Supported = []string{ProviderOllama, ProviderOpenAI}

// DefaultEndpoints are used when the operator does not set one explicitly.
var DefaultEndpoints = map[string]string{
	ProviderOllama: "http://localhost:11434",
	ProviderOpenAI: "https://api.openai.com/v1",
}

// New builds a Provider from the stored configuration.
//
// Returns ErrNotConfigured when no provider is set, which callers are expected
// to treat as a normal branch rather than a failure.
func New(cfg store.LLMConfig) (Provider, error) {
	if cfg.Provider == "" {
		return nil, ErrNotConfigured
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoints[cfg.Provider]
	}
	endpoint = strings.TrimRight(endpoint, "/")

	client := &http.Client{Timeout: 120 * time.Second}

	switch cfg.Provider {
	case ProviderOllama:
		if cfg.Model == "" {
			return nil, fmt.Errorf("ollama requires a model, e.g. --model llama3")
		}
		return &ollama{endpoint: endpoint, model: cfg.Model, client: client}, nil
	case ProviderOpenAI:
		if cfg.Model == "" {
			return nil, fmt.Errorf("openai requires a model, e.g. --model gpt-4o-mini")
		}
		return &openAI{endpoint: endpoint, model: cfg.Model, apiKey: cfg.APIKey, client: client}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: %s)",
			cfg.Provider, strings.Join(Supported, ", "))
	}
}

// --- Ollama ---------------------------------------------------------------

type ollama struct {
	endpoint string
	model    string
	client   *http.Client
}

func (o *ollama) Name() string { return ProviderOllama + " (" + o.model + ")" }

func (o *ollama) Complete(ctx context.Context, system, user string) (string, error) {
	payload := map[string]any{
		"model":  o.model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := postJSON(ctx, o.client, o.endpoint+"/api/chat", nil, payload, &result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama: %s", result.Error)
	}
	if result.Message.Content == "" {
		return "", errors.New("ollama returned an empty completion")
	}
	return result.Message.Content, nil
}

// --- OpenAI-compatible ------------------------------------------------------

type openAI struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

func (o *openAI) Name() string { return ProviderOpenAI + " (" + o.model + ")" }

func (o *openAI) Complete(ctx context.Context, system, user string) (string, error) {
	payload := map[string]any{
		"model": o.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}

	headers := map[string]string{}
	if o.apiKey != "" {
		headers["Authorization"] = "Bearer " + o.apiKey
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := postJSON(ctx, o.client, o.endpoint+"/chat/completions", headers, payload, &result); err != nil {
		return "", err
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("provider error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return "", errors.New("provider returned an empty completion")
	}
	return result.Choices[0].Message.Content, nil
}

// --- shared -----------------------------------------------------------------

func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Read the body before checking the status: providers put the useful part
	// of an error in the payload, and "400 Bad Request" alone helps nobody.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("cannot read response from %s: %w", url, err)
	}

	if resp.StatusCode >= 400 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 500 {
			snippet = snippet[:500] + "…"
		}
		return fmt.Errorf("%s returned %s: %s", url, resp.Status, snippet)
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cannot parse response from %s: %w", url, err)
	}
	return nil
}
