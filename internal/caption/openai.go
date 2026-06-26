package caption

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI is an alternative Captioner (spec 031 FU-1): it calls an OpenAI-compatible
// /chat/completions endpoint with a vision-capable model. This covers OpenAI (GPT-4o),
// Azure OpenAI, vLLM, LM Studio, and other servers that expose the OpenAI Chat
// Completions API with image_url support. The image is sent as a data-URL in a
// multimodal content array (the OpenAI format — distinct from Ollama's
// base64-in-images-array). Breaker-guarded like the Ollama provider.
//
// Constitution I (Local-First): this provider enables LOCAL OpenAI-compatible servers
// (vLLM, LM Studio, llama.cpp server). A cloud endpoint (OpenAI, Azure) is the
// operator's choice; go-rag never REQUIRES cloud — the default remains the local
// Ollama provider.
type OpenAI struct {
	endpoint string // base URL, e.g. "https://api.openai.com/v1" or "http://localhost:1234/v1"
	model    string
	apiKey   string
	client   *http.Client
	br       *breaker
}

// NewOpenAI returns an OpenAI-compatible captioner at endpoint using model+apiKey.
func NewOpenAI(endpoint, model, apiKey string) *OpenAI {
	return &OpenAI{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 120 * time.Second},
		br:       newBreaker(),
	}
}

// Model returns the model identifier (provenance on the caption sidecar).
func (o *OpenAI) Model() string { return o.model }

// Caption asks the OpenAI-compatible vision model for a description, guarded by a
// circuit breaker (mirrors the Ollama provider's contract).
func (o *OpenAI) Caption(ctx context.Context, imageBytes []byte, hint string) (string, error) {
	if err := o.br.allow(); err != nil {
		return "", err
	}
	c, err := o.chat(ctx, imageBytes)
	if err != nil {
		o.br.fail()
	} else {
		o.br.ok()
	}
	return c, err
}

type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}
type oaiImageURL struct {
	URL string `json:"url"`
}
type oaiMessage struct {
	Role    string           `json:"role"`
	Content []oaiContentPart `json:"content"`
}
type oaiChatRequest struct {
	Model     string       `json:"model"`
	Messages  []oaiMessage `json:"messages"`
	MaxTokens int          `json:"max_tokens,omitempty"`
	Stream    bool         `json:"stream"`
}
type oaiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// chat performs one unguarded captioning call to the OpenAI-compatible endpoint.
func (o *OpenAI) chat(ctx context.Context, imageBytes []byte) (string, error) {
	if len(imageBytes) == 0 {
		return "", ErrNothingToCaption
	}
	mime := imageMIME(imageBytes)
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)
	body, _ := json.Marshal(oaiChatRequest{
		Model: o.model,
		Messages: []oaiMessage{{
			Role: "user",
			Content: []oaiContentPart{
				{Type: "text", Text: captionPrompt},
				{Type: "image_url", ImageURL: &oaiImageURL{URL: dataURL}},
			},
		}},
		MaxTokens: 200,
		Stream:    false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err // transient (network / server unreachable)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("openai-compatible returned %d", resp.StatusCode) // transient
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", WrapPermanent(fmt.Errorf("openai-compatible returned %d: %s", resp.StatusCode, rb))
	}
	var cr oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", WrapPermanent(fmt.Errorf("decode openai-compatible response: %w", err))
	}
	if len(cr.Choices) == 0 {
		return "", WrapPermanent(fmt.Errorf("openai-compatible: no choices in response"))
	}
	c := strings.TrimSpace(cr.Choices[0].Message.Content)
	if c == "" {
		return "", ErrNothingToCaption
	}
	return c, nil
}

// imageMIME detects JPEG/PNG from the leading bytes (for the OpenAI data-URL prefix).
func imageMIME(b []byte) string {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 {
		return "image/jpeg"
	}
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 {
		return "image/png"
	}
	return "image/jpeg"
}
