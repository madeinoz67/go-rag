package rerank

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

// OpenAIReranker is an alternative reranker (spec 031 FU-1): it calls an
// OpenAI-compatible /chat/completions endpoint with a scoring prompt. Covers OpenAI,
// Azure, vLLM, LM Studio. Constitution I: enables LOCAL OpenAI-compatible servers.
type OpenAIReranker struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

func NewOpenAIReranker(endpoint, model, apiKey string) *OpenAIReranker {
	return &OpenAIReranker{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (r *OpenAIReranker) Model() string { return r.model }

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oaiChatRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream"`
}
type oaiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Score returns a normalised relevance score per candidate (0.0–1.0), using the
// same scoring prompt as the Ollama reranker but via /chat/completions.
func (r *OpenAIReranker) Score(ctx context.Context, query string, candidates []string) ([]float64, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("Rate each passage's relevance to the query on a scale of 0 to 9 ")
	b.WriteString("(0=irrelevant, 9=perfect). Return ONLY the scores as a comma-separated ")
	b.WriteString("list of integers, nothing else.\n\n")
	fmt.Fprintf(&b, "Query: %s\n\n", query)
	for i, c := range candidates {
		p := strings.ReplaceAll(c, "\n", " ")
		if len(p) > 200 {
			p = p[:200] + "…"
		}
		fmt.Fprintf(&b, "[%d] %s\n", i+1, p)
	}
	b.WriteString("\nScores:")

	body, _ := json.Marshal(oaiChatRequest{
		Model: r.model,
		Messages: []oaiMessage{
			{Role: "user", Content: b.String()},
		},
		Temperature: 0,
		MaxTokens:   100,
		Stream:      false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai-compatible reranker returned %d: %s", resp.StatusCode, rb)
	}
	var cr oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("decode reranker response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("no choices in reranker response")
	}
	return parseScores(cr.Choices[0].Message.Content, len(candidates)), nil
}
