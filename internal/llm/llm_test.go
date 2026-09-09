package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/inboxql/internal/store"
)

func TestNewProviders(t *testing.T) {
	// Not configured
	if _, err := New(store.LLMConfig{}); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}

	// Unknown provider
	if _, err := New(store.LLMConfig{Provider: "unreal", Model: "xyz"}); err == nil {
		t.Fatalf("expected error for unknown provider, got nil")
	}

	// Ollama without model
	if _, err := New(store.LLMConfig{Provider: ProviderOllama}); err == nil {
		t.Fatalf("expected error for ollama without model")
	}

	// Swama without model
	if _, err := New(store.LLMConfig{Provider: ProviderSwama}); err == nil {
		t.Fatalf("expected error for swama without model")
	}

	// Swama with model
	p, err := New(store.LLMConfig{
		Provider: ProviderSwama,
		Model:    "mlx-community/Llama-3.2-3B-Instruct-4bit",
	})
	if err != nil {
		t.Fatalf("unexpected error creating swama provider: %v", err)
	}
	if p.Name() != "swama (mlx-community/Llama-3.2-3B-Instruct-4bit)" {
		t.Fatalf("unexpected name: %s", p.Name())
	}

	// OpenAI with model
	p2, err := New(store.LLMConfig{
		Provider: ProviderOpenAI,
		Model:    "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("unexpected error creating openai provider: %v", err)
	}
	if p2.Name() != "openai (gpt-4o-mini)" {
		t.Fatalf("unexpected name: %s", p2.Name())
	}
}

func TestFetchRemoteModelsMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"id": "gpt-4o"},
					{"id": "gpt-3.5-turbo"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	models, err := FetchRemoteModels(context.Background(), ts.URL, "test-key", ProviderOpenAI)
	if err != nil {
		t.Fatalf("unexpected error fetching models: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-4o" || models[1] != "gpt-3.5-turbo" {
		t.Fatalf("unexpected models list: %v", models)
	}
}

func TestTestCompletionMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]string{
							"content": "ok",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	res, err := TestCompletion(context.Background(), store.LLMConfig{
		Provider: ProviderOpenAI,
		Model:    "mock-model",
		Endpoint: ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error in TestCompletion: %v", err)
	}
	if res["ok"] != true || res["reply"] != "ok" {
		t.Fatalf("unexpected completion response: %v", res)
	}
}

func TestDetectRuntimes(t *testing.T) {
	runtimes := DetectRuntimes(context.Background())
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(runtimes))
	}
	// Check swama runtime is present
	foundSwama := false
	for _, r := range runtimes {
		if r.Provider == ProviderSwama {
			foundSwama = true
			if len(r.LaunchModes) == 0 {
				t.Fatalf("expected launch modes for swama")
			}
		}
	}
	if !foundSwama {
		t.Fatalf("expected swama in detected runtimes")
	}
}

// A name is a convention and conventions are broken, so names only ever order
// the probing.
func TestLooksLikeEmbedding(t *testing.T) {
	embedding := []string{
		"nomic-embed-text", "mlx-community/bge-m3-mlx-fp16", "all-minilm",
		"text-embedding-3-small", "gte-large", "e5-mistral", "mxbai-embed-large",
	}
	chat := []string{
		"llama3.3:70b", "mlx-community/gemma-4-e4b-it-4bit", "gpt-4o-mini",
		"mistral", "qwen2.5-coder",
	}
	for _, name := range embedding {
		if !LooksLikeEmbedding(name) {
			t.Errorf("%q was not recognised as an embedding model", name)
		}
	}
	for _, name := range chat {
		if LooksLikeEmbedding(name) {
			t.Errorf("%q was mistaken for an embedding model", name)
		}
	}
}
