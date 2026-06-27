// Package embed abstracts text embedding: converting text into dense vectors for
// vector-similarity retrieval (PRD §4). The Embedder interface keeps the pipeline
// decoupled from the embedding provider; the v1 provider is Ollama, with an
// OpenAI-compatible alternative (spec 031 FU-1).
package embed

import (
	"context"
	"strings"
)

// Embedder generates vector embeddings for text (PRD §4 embedding client). The
// pipeline calls Embed for both ingest (chunk embeddings) and query (query
// embedding); Dimensions must return the vector length (0 until the first
// successful Embed call populates it).
type Embedder interface {
	// Embed generates embeddings for texts (one vector per text). Empty input
	// returns nil, nil.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions returns the embedding vector length. Returns 0 until the first
	// successful Embed call populates it.
	Dimensions() int
	// Model returns the embedding model identifier (provenance + the 0x14 queue).
	Model() string
}

// New constructs the configured Embedder provider (spec 031 FU-1). provider "ollama"
// or "" (default) → the Ollama embedder; "openai" → an OpenAI-compatible embedder
// (covers OpenAI text-embedding-3-*, Azure, vLLM, LM Studio). endpoint is the base
// URL (resolved by the caller — empty falls back to OllamaURL). apiKey is used by
// cloud providers (Bearer token).
func New(provider, endpoint, model, apiKey string) Embedder {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai-compatible":
		return NewOpenAI(endpoint, model, apiKey)
	case "native", "gomlx", "hugot":
		return NewHugot()
	default:
		return NewOllama(endpoint, model)
	}
}
