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
	"sync"
	"time"
)

// The Embedder interface is defined in embedder.go (spec 031 FU-1 provider abstraction).

// Ollama is the v1 Embedder: it calls a local Ollama /api/embed endpoint. Safe for
// concurrent use (the pipeline's background workers call Embed in parallel).
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client

	mu   sync.Mutex
	dims int
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

// embedBatchSize caps how many texts a single Ollama /api/embed request carries
// (H12/spec 010). A large document's chunks are split into batches of this size
// so per-request memory and per-request time stay bounded regardless of chunk
// count — the unbounded single request was the OOM/timeout cliff. Within the
// audit's 32–64 range; an internal constant (not config-exposed).
const embedBatchSize = 32

// Embed generates embeddings for texts by sending them to Ollama in bounded
// batches (H12/spec 010). It splits texts into embedBatchSize slices, requests
// each batch with its own 3-attempt retry, and returns the concatenated vectors
// in input order. If any batch fails permanently, Embed returns the error and no
// partial result (the caller — the ingest pipeline — then marks the document
// errored and stores nothing, so no partial index is ever committed). Empty input
// is a no-op; sub-cap input behaves exactly as a single request. The Embedder
// contract (one vector per text, in order) is unchanged.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		// Honour cancellation between batches so a cancelled ingest returns
		// promptly rather than issuing the remaining requests. The per-request
		// ctx binding (embedBatch → http.NewRequestWithContext) covers the rest.
		if start > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := o.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err // FR-006: no partial result on a failed batch.
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// embedBatch sends one batch of texts to Ollama with the standard 3-attempt
// retry (5xx/network → retry with backoff, 4xx → fail fast, ctx-respecting).
// Returns the batch's vectors in input order. A response whose vector count !=
// len(batch) is an error (the integrity guard, applied per batch so a truncated
// response can never silently misalign later batches). On success the first
// embedding seeds the set-once dimensionality. (H12/spec 010: per-batch
// primitive extracted from the former unbounded Embed.)
func (o *Ollama) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	body, _ := json.Marshal(embedRequest{Model: o.model, Input: batch})
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
		if resp.StatusCode != http.StatusOK {
			rb, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, rb)
		}
		var er embedResponse
		decErr := json.NewDecoder(resp.Body).Decode(&er)
		resp.Body.Close()
		if decErr != nil {
			return nil, fmt.Errorf("decode ollama response: %w", decErr)
		}
		if len(er.Embeddings) != len(batch) {
			return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(er.Embeddings), len(batch))
		}
		o.setDims(len(er.Embeddings[0]))
		return er.Embeddings, nil
	}
	return nil, fmt.Errorf("ollama embed failed after retries: %w", lastErr)
}

func (o *Ollama) setDims(d int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.dims == 0 {
		o.dims = d
	}
}

func (o *Ollama) Dimensions() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dims
}

func (o *Ollama) Model() string { return o.model }

func backoff(attempt int) time.Duration {
	return time.Duration(50*(attempt)) * time.Millisecond
}
