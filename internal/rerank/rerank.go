// Package rerank implements cross-encoder-style reranking via an Ollama LLM. After
// fast bi-encoder retrieval (BM25 + vector RRF), the reranker scores each
// candidate's relevance to the query directly, cutting semantic noise (e.g.
// unrelated chunks that have low-but-nonzero vector similarity).
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Reranker scores query-passage relevance using an Ollama LLM (generate) as a
// second-pass scorer. One call scores all candidates at once.
type Reranker struct {
	url    string
	model  string
	client *http.Client
}

// New returns a reranker that calls Ollama at url using model for scoring.
func New(url, model string) *Reranker {
	return &Reranker{
		url:    url,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Score returns a normalised relevance score per candidate (0.0–1.0, higher =
// more relevant). Sends one Ollama generate call with all candidates and parses
// the comma-separated scores from the response.
func (r *Reranker) Score(ctx context.Context, query string, candidates []string) ([]float64, error) {
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

	body, _ := json.Marshal(map[string]any{
		"model":  r.model,
		"prompt": b.String(),
		"stream": false,
		"options": map[string]any{
			"temperature": 0,
			"num_predict":  100,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama rerank response: %w", err)
	}

	return parseScores(result.Response, len(candidates)), nil
}

// parseScores splits the LLM response by commas, parses each field as a number,
// and normalises to 0.0–1.0 (divide by 9). Falls back to 0.5 for any unparseable
// field so positions are preserved (a junk value in position 2 doesn't shift 3+).
func parseScores(response string, n int) []float64 {
	scores := make([]float64, n)
	for i := range scores {
		scores[i] = 0.5
	}
	parts := strings.Split(strings.TrimSpace(response), ",")
	for i := 0; i < n && i < len(parts); i++ {
		var v float64
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[i]), "%f", &v); err == nil {
			scores[i] = v / 9.0
		}
	}
	return scores
}
