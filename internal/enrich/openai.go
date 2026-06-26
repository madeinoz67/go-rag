package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI is an alternative Enricher (spec 031 FU-1): it calls an OpenAI-compatible
// /chat/completions endpoint for document tag+summary generation. Covers OpenAI
// (GPT-4o), Azure, vLLM, LM Studio, etc. Constitution I: enables LOCAL
// OpenAI-compatible servers; cloud is the operator's choice.
type OpenAI struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

func NewOpenAI(endpoint, model, apiKey string) *OpenAI {
	return &OpenAI{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OpenAI) Model() string { return o.model }

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oaiChatRequest struct {
	Model     string       `json:"model"`
	Messages  []oaiMessage `json:"messages"`
	Stream    bool         `json:"stream"`
	MaxTokens int          `json:"max_tokens,omitempty"`
}
type oaiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Enrich asks the OpenAI-compatible model for tags + summary. Reuses the same
// prompt + JSON parsing + normalization as the Ollama provider (shared package).
func (o *OpenAI) Enrich(ctx context.Context, docText string) ([]string, string, error) {
	docText = strings.TrimSpace(docText)
	if docText == "" {
		return nil, "", ErrNothingToEnrich
	}
	if len(docText) > maxDocChars {
		docText = docText[:maxDocChars]
	}
	body, _ := json.Marshal(oaiChatRequest{
		Model: o.model,
		Messages: []oaiMessage{
			{Role: "system", Content: enrichPrompt},
			{Role: "user", Content: docText},
		},
		Stream:    false,
		MaxTokens: 200,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, "", fmt.Errorf("openai-compatible returned %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return nil, "", WrapPermanent(fmt.Errorf("openai-compatible returned %d: %s", resp.StatusCode, rb))
	}
	var cr oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, "", WrapPermanent(fmt.Errorf("decode response: %w", err))
	}
	if len(cr.Choices) == 0 {
		return nil, "", WrapPermanent(fmt.Errorf("no choices in response"))
	}
	tags, summary, err := parseEnrichJSON(cr.Choices[0].Message.Content)
	if err != nil {
		return nil, "", WrapPermanent(fmt.Errorf("parse enrichment output: %w", err))
	}
	if len(tags) == 0 && strings.TrimSpace(summary) == "" {
		return nil, "", ErrNothingToEnrich
	}
	return normalizeTags(tags), strings.TrimSpace(summary), nil
}
