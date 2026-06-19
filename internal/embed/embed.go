// Package embed abstracts embedding generation (PRD §4, §9.1).
//
// The only v1 provider is Ollama's /api/embed HTTP endpoint, but the interface
// keeps the door open for future providers (PRD non-goal N9 documents the v1
// single-provider limit).
package embed

import "context"

// Embedder generates vector embeddings for text (PRD §4 embedding client).
type Embedder interface {
	// Embed returns one vector per input text, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions is the fixed width of vectors this embedder produces.
	Dimensions() int
	// Model is the model name (e.g. "nomic-embed-text").
	Model() string
}
