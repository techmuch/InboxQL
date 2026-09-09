package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// What a model is for.
//
// A profile pointed at the wrong kind fails at run time with a provider error
// nobody can act on, so the kind is recorded when the model is discovered
// rather than discovered when the model is used.
const (
	KindChat      = "chat"
	KindEmbedding = "embedding"
	KindUnknown   = "unknown"
)

// Model is one model a runtime offers.
type Model struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Dimensions is the width of the vectors this model returns, known only
	// after a probe. Vectors of different widths cannot be compared, so this
	// is what a stored embedding has to be stamped with.
	Dimensions int `json:"dimensions,omitempty"`
}

// embeddingNames are the families whose names say what they are.
//
// Only ever used to *order* probing, never to conclude. A name is a convention
// and conventions are broken; the probe below is the actual answer.
var embeddingNames = regexp.MustCompile(
	`(?i)(embed|bge-|gte-|e5-|minilm|nomic-embed|mxbai|text-embedding|all-mpnet)`)

// LooksLikeEmbedding reports whether a model's name suggests it embeds.
func LooksLikeEmbedding(name string) bool { return embeddingNames.MatchString(name) }

// ClassifyModels labels a runtime's models, probing the ones worth probing.
//
// # Why probe rather than trust the name
//
// The list a runtime returns is bare strings. Offering an embedding model as a
// chat model — or the reverse — produces a 500 the first time it is used, which
// is both later and less informative than finding out at configuration time.
//
// Probing is definitive and it returns the dimension, which nothing else
// reports reliably and which the storage layout depends on.
//
// # Why not probe everything
//
// A probe loads the model. Doing that for every model on a machine with a
// dozen of them would take minutes and evict whatever was resident. So the
// name orders the work: candidates get probed, everything else is chat unless
// somebody says otherwise.
func ClassifyModels(ctx context.Context, endpoint, apiKey, provider string, names []string) []Model {
	out := make([]Model, 0, len(names))
	for _, name := range names {
		m := Model{Name: name, Kind: KindChat}
		if LooksLikeEmbedding(name) {
			if dims, err := ProbeEmbedding(ctx, endpoint, apiKey, provider, name); err == nil && dims > 0 {
				m.Kind, m.Dimensions = KindEmbedding, dims
			} else {
				// It reads like an embedding model and did not embed. Saying
				// "unknown" is honest; calling it chat would offer it for
				// completions it will also refuse.
				m.Kind = KindUnknown
			}
		}
		out = append(out, m)
	}
	return out
}

// ProbeEmbedding asks a model to embed one word and reports the width it
// returned.
//
// A zero-length vector or any error means "this is not an embedding model
// here", which is a fact about the pairing rather than about the model: a
// runtime that has not loaded an embedding backend refuses every one of them.
func ProbeEmbedding(ctx context.Context, endpoint, apiKey, provider, model string) (int, error) {
	vecs, err := embedVia(ctx, endpoint, apiKey, provider, model, []string{"probe"})
	if err != nil {
		return 0, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return 0, fmt.Errorf("%s returned no vector", model)
	}
	return len(vecs[0]), nil
}

// embedVia calls whichever embeddings protocol the provider speaks.
//
// Ollama has its own shape; everything else speaks OpenAI's, which is what
// llama.cpp, LM Studio, vLLM and the hosted proxies all implement.
func embedVia(ctx context.Context, endpoint, apiKey, provider, model string, input []string) ([][]float32, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(DefaultEndpoints[provider], "/")
	}
	client := &http.Client{Timeout: 60 * time.Second}

	if provider == ProviderOllama {
		return ollamaEmbed(ctx, client, endpoint, model, input)
	}
	return openAIEmbed(ctx, client, endpoint, apiKey, model, input)
}

func openAIEmbed(ctx context.Context, client *http.Client, endpoint, apiKey, model string, input []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": model, "input": input})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return nil, fmt.Errorf("embeddings returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, 0, len(out.Data))
	for _, d := range out.Data {
		vecs = append(vecs, d.Embedding)
	}
	return vecs, nil
}

func ollamaEmbed(ctx context.Context, client *http.Client, endpoint, model string, input []string) ([][]float32, error) {
	// /api/embed takes a batch; older builds only have /api/embeddings, which
	// takes one string. Try the batch endpoint and fall back.
	body, _ := json.Marshal(map[string]any{"model": model, "input": input})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var out struct {
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err == nil && len(out.Embeddings) > 0 {
			return out.Embeddings, nil
		}
	}

	var vecs [][]float32
	for _, text := range input {
		one, _ := json.Marshal(map[string]any{"model": model, "prompt": text})
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/embeddings", bytes.NewReader(one))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		res, err := client.Do(r)
		if err != nil {
			return nil, err
		}
		var out struct {
			Embedding []float32 `json:"embedding"`
		}
		err = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(out.Embedding) == 0 {
			return nil, fmt.Errorf("%s returned no vector", model)
		}
		vecs = append(vecs, out.Embedding)
	}
	return vecs, nil
}

// Embed returns a vector per input text.
//
// Exported alongside Complete rather than folded into the Provider interface:
// a chat provider and an embedding provider are different capabilities, and
// most profiles have exactly one of them. Making Provider carry both would
// oblige every implementation to return "not supported" for half its methods.
func Embed(ctx context.Context, endpoint, apiKey, provider, model string, input []string) ([][]float32, error) {
	if len(input) == 0 {
		return nil, nil
	}
	return embedVia(ctx, endpoint, apiKey, provider, model, input)
}
