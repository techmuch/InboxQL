package llm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Viewer is a provider that can be shown an image.
//
// # Why an optional interface rather than a method on Provider
//
// Most models cannot see, and most of what InboxQL asks a model to do is
// text. Widening Provider would force every implementation to carry a method
// it answers with an error, and would let a caller ask a text-only model to
// read a page and discover the fact at runtime. A separate interface makes the
// capability something a caller tests for and reports, which is what
// [Describe] is for.
type Viewer interface {
	// Describe answers a prompt about one or more images.
	Describe(ctx context.Context, system, prompt string, images []Image) (string, error)
}

// Image is one picture to show a model.
type Image struct {
	MIME string
	Data []byte
}

// ErrNoVision says a provider cannot be shown an image.
var ErrNoVision = errors.New("this model cannot read images")

// Describe shows images to a provider that can see them.
//
// The indirection exists so callers get one honest error — naming the provider
// that cannot do it — rather than a failed type assertion at three call sites.
func Describe(ctx context.Context, p Provider, system, prompt string, images []Image) (string, error) {
	viewer, ok := p.(Viewer)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoVision, p.Name())
	}
	return viewer.Describe(ctx, system, prompt, images)
}

// CanSee reports whether a provider accepts images at all.
//
// Whether the configured *model* can see is a different question, and one only
// the model can answer — a text-only model behind an OpenAI-compatible
// endpoint accepts the request and returns something unhelpful. That is
// reported when it happens rather than predicted here, because a list of which
// models have vision is a list that is wrong within a month.
func CanSee(p Provider) bool {
	_, ok := p.(Viewer)
	return ok
}

// visionTimeout is generous because reading a page is slow.
//
// A local model on consumer hardware takes tens of seconds per page, and the
// 120-second default that suits a sentence of prose turns a working setup into
// an intermittent one.
const visionTimeout = 10 * time.Minute

// Describe sends images alongside the prompt in the OpenAI content-parts form,
// which Swama, llama.cpp, LM Studio, vLLM and OpenAI itself all accept.
func (o *openAI) Describe(ctx context.Context, system, prompt string, images []Image) (string, error) {
	content := []map[string]any{{"type": "text", "text": prompt}}
	for _, img := range images {
		mime := img.MIME
		if mime == "" {
			mime = "image/jpeg"
		}
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}

	messages := []map[string]any{}
	if system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	messages = append(messages, map[string]any{"role": "user", "content": content})

	payload := map[string]any{
		"model":    o.model,
		"messages": messages,
		"stream":   false,
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

	headers := map[string]string{}
	if o.apiKey != "" {
		headers["Authorization"] = "Bearer " + o.apiKey
	}

	client := &http.Client{Timeout: visionTimeout}
	if err := postJSON(ctx, client, o.endpoint+"/chat/completions", headers, payload, &result); err != nil {
		return "", err
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("%s: %s", o.provider, result.Error.Message)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("%s returned nothing for the image", o.provider)
	}
	return result.Choices[0].Message.Content, nil
}

// Describe sends images in Ollama's own form, which puts base64 images in a
// field beside the text rather than inside the content.
func (o *ollama) Describe(ctx context.Context, system, prompt string, images []Image) (string, error) {
	encoded := make([]string, 0, len(images))
	for _, img := range images {
		encoded = append(encoded, base64.StdEncoding.EncodeToString(img.Data))
	}

	messages := []map[string]any{}
	if system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	messages = append(messages, map[string]any{
		"role": "user", "content": prompt, "images": encoded,
	})

	payload := map[string]any{
		"model":    o.model,
		"stream":   false,
		"messages": messages,
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}

	client := &http.Client{Timeout: visionTimeout}
	if err := postJSON(ctx, client, o.endpoint+"/api/chat", nil, payload, &result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama: %s", result.Error)
	}
	if result.Message.Content == "" {
		return "", errors.New("ollama returned nothing for the image")
	}
	return result.Message.Content, nil
}
