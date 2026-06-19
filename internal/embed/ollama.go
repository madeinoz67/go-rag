// Package embed abstracts embedding generation (PRD §4, §9.1).
//
// The only v1 provider is Ollama's /api/embed HTTP endpoint, but the Embedder
// interface keeps the door open for future providers (PRD non-goal N9).
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder generates vector embeddings for text (PRD §4 embedding client).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	Model() string
}

// Ollama is the v1 Embedder: it calls a local Ollama /api/embed endpoint.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
	dims    int
}

// NewOllama returns an Ollama embedder pointing at baseURL using model.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed calls Ollama once for all texts, retrying transient (5xx/network) errors.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(embedRequest{Model: o.model, Input: texts})
	url := o.baseURL + "/api/embed"

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("ollama returned %d", resp.StatusCode)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			rb, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, rb)
		}
		var er embedResponse
		if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
			return nil, fmt.Errorf("decode ollama response: %w", err)
		}
		if len(er.Embeddings) != len(texts) {
			return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(er.Embeddings), len(texts))
		}
		if o.dims == 0 && len(er.Embeddings) > 0 {
			o.dims = len(er.Embeddings[0])
		}
		return er.Embeddings, nil
	}
	return nil, fmt.Errorf("ollama embed failed after retries: %w", lastErr)
}

func (o *Ollama) Dimensions() int { return o.dims }
func (o *Ollama) Model() string   { return o.model }

func backoff(attempt int) time.Duration {
	return time.Duration(50*(attempt)) * time.Millisecond
}
