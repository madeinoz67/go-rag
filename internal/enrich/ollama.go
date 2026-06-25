package enrich

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

// Ollama is the v1 Enricher (spec 029): it calls a local Ollama generation
// endpoint (/api/generate) to produce a document's tags + summary. It reuses the
// same loopback base URL as the embedding client, but a generation endpoint. Safe
// for concurrent use (the pipeline's background workers call it).
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama returns an Ollama enricher pointing at baseURL using model.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{baseURL: baseURL, model: model, client: &http.Client{Timeout: 120 * time.Second}}
}

// Model returns the generation model identifier (provenance on the sidecar).
func (o *Ollama) Model() string { return o.model }

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format,omitempty"` // "json" → structured output
}

type generateResponse struct {
	Response string `json:"response"`
}

// enrichOut is the structured shape requested from the model.
type enrichOut struct {
	Tags    []string `json:"tags"`
	Summary string   `json:"summary"`
}

// enrichPrompt instructs the model to emit compact, predictable JSON: a small
// lowercase tag set + a one-line summary. Kept tight so a small local model can
// satisfy it reliably.
const enrichPrompt = `You tag and summarize a document for a local document database.
Read the document under --- DOCUMENT --- and respond with ONLY compact JSON, no prose:
{"tags":["at most 5 short lowercase topic tags"],"summary":"one concise sentence on what the document is about"}.
Rules: tags are single words or short hyphenated phrases, lowercase, no punctuation; pick the most specific topics. If the document is empty or meaningless, return {"tags":[],"summary":""}.`

// maxDocChars bounds the text sent to the model (context safety + latency).
const maxDocChars = 4000

// Enrich asks the local model for tags + summary. Empty/trivial text →
// ErrNothingToEnrich; a 4xx / unparseable response → a permanent failure
// (WrapPermanent); a network/5xx error → a transient error (retried later).
func (o *Ollama) Enrich(ctx context.Context, docText string) ([]string, string, error) {
	docText = strings.TrimSpace(docText)
	if docText == "" {
		return nil, "", ErrNothingToEnrich
	}
	if len(docText) > maxDocChars {
		docText = docText[:maxDocChars]
	}
	body, _ := json.Marshal(generateRequest{
		Model:  o.model,
		Prompt: enrichPrompt + "\n\n--- DOCUMENT ---\n" + docText,
		Stream: false,
		Format: "json",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, "", err // transient (network / model unreachable)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, "", fmt.Errorf("ollama returned %d", resp.StatusCode) // transient
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return nil, "", WrapPermanent(fmt.Errorf("ollama returned %d: %s", resp.StatusCode, rb))
	}
	var gr generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, "", WrapPermanent(fmt.Errorf("decode ollama generate response: %w", err))
	}
	tags, summary, err := parseEnrichJSON(gr.Response)
	if err != nil {
		return nil, "", WrapPermanent(fmt.Errorf("parse enrichment output: %w", err))
	}
	if len(tags) == 0 && strings.TrimSpace(summary) == "" {
		return nil, "", ErrNothingToEnrich
	}
	return normalizeTags(tags), strings.TrimSpace(summary), nil
}

var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseEnrichJSON extracts the {tags,summary} JSON from the model output, which
// may carry surrounding prose despite the format=json instruction.
func parseEnrichJSON(s string) ([]string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, "", fmt.Errorf("empty response")
	}
	var out enrichOut
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		m := jsonObjectRe.FindString(s)
		if m == "" {
			return nil, "", fmt.Errorf("no JSON object in response: %w", err)
		}
		if err := json.Unmarshal([]byte(m), &out); err != nil {
			return nil, "", err
		}
	}
	return out.Tags, out.Summary, nil
}

// normalizeTags lowercases, trims punctuation, collapses spaces to hyphens,
// dedupes, and caps at 5 — keeping the tag set small and stable for filtering.
func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		t = strings.ReplaceAll(t, " ", "-")
		t = strings.Trim(t, ",.;:/()[]")
		if t == "" || seen[t] || len(out) >= 5 {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
