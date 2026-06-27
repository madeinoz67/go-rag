package embed

import (
	"context"
	"fmt"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
)

// HugotEmbedder is a pure-Go (CGo-free) Embedder backed by Hugot's GoMLX backend
// (spec 032). The GoMLX session + feature-extraction pipeline are built lazily on the
// first Embed to keep cold start under the <1s budget. The model MUST already be
// present locally (fetched via `go-rag model install` / `init`); Embed never fetches —
// the ingest/query path runs offline once the model is present (Constitution I).
type HugotEmbedder struct {
	once    sync.Once
	pipe    *pipelines.FeatureExtractionPipeline
	dim     int
	initErr error
}

// NewHugot constructs the bundled pure-Go embedder. The model path is resolved from
// the pinned manifest (modelbundle.ModelDir) lazily on first Embed.
func NewHugot() *HugotEmbedder { return &HugotEmbedder{} }

// ensure builds the GoMLX session + feature-extraction pipeline on first use.
func (h *HugotEmbedder) ensure() error {
	h.once.Do(func() {
		modelPath, err := modelbundle.ModelDir()
		if err != nil {
			h.initErr = fmt.Errorf("resolve model dir: %w", err)
			return
		}
		if !modelbundle.IsPresent() {
			// Actionable error — never auto-fetch on the query/ingest path (FR-006).
			h.initErr = fmt.Errorf("bundled model %q not present at %s — run `go-rag model install`",
				modelbundle.ModelID, modelPath)
			return
		}
		ctx := context.Background()
		s, err := hugot.NewGoSession(ctx)
		if err != nil {
			h.initErr = fmt.Errorf("gomlx session: %w", err)
			return
		}
		pipe, err := hugot.NewPipeline[*pipelines.FeatureExtractionPipeline](s, hugot.FeatureExtractionConfig{
			ModelPath:    modelPath,
			Name:         "gorag-default",
			OnnxFilename: modelbundle.ModelFilename,
			Options: []hugot.FeatureExtractionOption{
				pipelines.WithNormalization(),
			},
		})
		if err != nil {
			h.initErr = fmt.Errorf("gomlx pipeline: %w", err)
			return
		}
		h.pipe = pipe
		h.dim = modelbundle.EmbeddingDim
	})
	return h.initErr
}

// Embed generates embeddings for texts (one vector per text). Empty input → nil, nil.
func (h *HugotEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := h.ensure(); err != nil {
		return nil, err
	}
	out, err := h.pipe.RunPipeline(ctx, texts)
	if err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// Dimensions returns the vector length (0 until the first successful Embed loads the
// pipeline; then the pinned model's dimensionality).
func (h *HugotEmbedder) Dimensions() int { return h.dim }

// Model returns the bundled model identity (provenance + re-embed key).
func (h *HugotEmbedder) Model() string { return modelbundle.ModelID }
