package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OpenAI is an alternative Embedder (spec 031 FU-1): it calls an OpenAI-compatible
// /embeddings endpoint. This covers OpenAI (text-embedding-3-*), Azure, vLLM, LM
// Studio, and other servers that expose the OpenAI Embeddings API.
//
// Constitution I (Local-First): enables LOCAL OpenAI-compatible servers (vLLM, LM
// Studio). A cloud endpoint is the operator's choice; the default remains local Ollama.
type OpenAI struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
	mu       sync.Mutex
	dims     int
}

// NewOpenAI returns an OpenAI-compatible embedder at endpoint using model+apiKey.
func NewOpenAI(endpoint, model, apiKey string) *OpenAI {
	return &OpenAI{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OpenAI) Model() string { return o.model }

// Embed sends texts to the OpenAI-compatible embeddings endpoint and returns one
// vector per text. Dimensions are cached from the first successful response.
func (o *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{"model": o.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai embeddings returned %d: %s", resp.StatusCode, rb)
	}
	var er struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("decode openai embeddings response: %w", err)
	}
	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		out[i] = d.Embedding
	}
	if len(out) > 0 {
		o.mu.Lock()
		o.dims = len(out[0])
		o.mu.Unlock()
	}
	return out, nil
}

func (o *OpenAI) Dimensions() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dims
}
