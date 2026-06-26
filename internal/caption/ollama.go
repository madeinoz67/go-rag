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

// Ollama is the v1 Captioner (spec 031 US4): it calls a local Ollama MULTIMODAL
// endpoint (/api/chat with an images field) to caption an image. It is a distinct
// client from the enrich text-only client (/api/generate) — vision requires the
// chat endpoint with base64 image payloads. Safe for concurrent use (the
// pipeline's background workers call it).
//
// TODO(provider-abstraction, spec 031 follow-up): the provider (Ollama) + its API
// endpoint are hardcoded as the sole captioning backend, and the same gap exists
// for enrich.Enricher + embed.Embedder (each has a single Ollama impl). To support
// alternative providers (a cloud vision API, an OpenAI-compatible endpoint, another
// local inference server), introduce a provider-selection config (provider +
// endpoint + model per capability) + non-Ollama impls behind these interfaces.
// Captured as a tracked follow-up in specs/031-pdf-structured-ingestion/tasks.md.
//
// NOTE (research.md T002 / US4 SC-004 validation, 2026-06-26): the /api/chat
// images-field shape is the documented Ollama native format (raw base64 strings in
// messages[].images, NOT OpenAI-style data-URLs) — VERIFIED end-to-end: a real
// chart JPEG → minicpm-v caption → caption chunk → retrieval. minicpm-v is the
// tested-recommended model (llava miscounted the bars). A wrong shape surfaces as
// 4xx → WrapPermanent (every image skipped, captioning configured but idle).
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
	br      *breaker
}

// NewOllama returns an Ollama captioner pointing at baseURL using the vision model.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{baseURL: baseURL, model: model, client: &http.Client{Timeout: 120 * time.Second}, br: newBreaker()}
}

// Model returns the vision model identifier (provenance on the caption sidecar).
func (o *Ollama) Model() string { return o.model }

// captionPrompt instructs the model to describe the image for search, with a
// chart-data emphasis (FR-004: chart captions describe the data, not just "a chart").
const captionPrompt = `Describe this image so its content is searchable in a document database. If it is a chart, graph, plot, or diagram, describe the DATA: the trend, the key values, the comparisons, and the axes. If it is a screenshot or photograph, describe what it visibly shows. Respond with one or two concise sentences and no preamble.`

type chatImageMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // raw base64 (Ollama native format)
}

type chatRequest struct {
	Model    string             `json:"model"`
	Messages []chatImageMessage `json:"messages"`
	Stream   bool               `json:"stream"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// Caption asks the local vision model for a description, guarded by a circuit
// breaker. An open breaker fast-fails with ErrCircuitOpen (transient — the
// caption chunk is left unwritten for a later retry).
func (o *Ollama) Caption(ctx context.Context, imageBytes []byte, hint string) (string, error) {
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

// chat performs one unguarded captioning call. Empty bytes → ErrNothingToCaption;
// a 4xx / unparseable response → a permanent failure (WrapPermanent); a network/
// 5xx error → a transient error (retried later).
func (o *Ollama) chat(ctx context.Context, imageBytes []byte) (string, error) {
	if len(imageBytes) == 0 {
		return "", ErrNothingToCaption
	}
	b64 := base64.StdEncoding.EncodeToString(imageBytes)
	body, _ := json.Marshal(chatRequest{
		Model:    o.model,
		Stream:   false,
		Messages: []chatImageMessage{{Role: "user", Content: captionPrompt, Images: []string{b64}}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err // transient (network / model unreachable)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("ollama returned %d", resp.StatusCode) // transient
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", WrapPermanent(fmt.Errorf("ollama returned %d: %s", resp.StatusCode, rb))
	}
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", WrapPermanent(fmt.Errorf("decode ollama chat response: %w", err))
	}
	c := strings.TrimSpace(cr.Message.Content)
	if c == "" {
		return "", ErrNothingToCaption
	}
	return c, nil
}
