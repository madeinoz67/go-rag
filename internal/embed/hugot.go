package embed

import (
	"context"
	"fmt"
	"math"
	"strings"
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
//
// Over-length handling (spec 032 overflow fix): a text longer than the model's
// max_position_embeddings (modelbundle.MaxSeqLen, 512 for bge-small) would crash
// GoMLX graph compilation — the position-embedding buffer is fixed at that width, and
// a 757-token input blows it with "got shapes [N 757 384] and [1 512 384]". Worse, the
// crash kills the whole batch (one bad text poisons 31 good ones) and the queue never
// drains. Such texts are now split into ≤MaxSeqLen sub-windows (at real token
// boundaries via the pipeline tokenizer when reachable, else at word boundaries as a
// conservative char-proxy), each sub-window embedded individually, and the sub-vectors
// mean-pooled into one L2-normalized vector. Transparent to callers: one text in, one
// vector out. Texts within the limit take the original single-batch path so
// cross-document micro-batching (FR-005) is preserved for the common case.
func (h *HugotEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := h.ensure(); err != nil {
		return nil, err
	}
	maxSeq := modelbundle.MaxSeqLen
	out := make([][]float32, len(texts))

	// Partition: short texts share one RunPipeline batch (FR-005 cross-doc batching);
	// over-length texts are sub-chunked + mean-pooled individually so a long text
	// never crashes its batch-mates.
	shortIdx := make([]int, 0, len(texts))
	for i, t := range texts {
		if h.isOverLength(t, maxSeq) {
			v, err := h.embedLong(ctx, t, maxSeq)
			if err != nil {
				return nil, err
			}
			out[i] = v
			continue
		}
		shortIdx = append(shortIdx, i)
	}
	if len(shortIdx) > 0 {
		short := make([]string, len(shortIdx))
		for j, idx := range shortIdx {
			short[j] = texts[idx]
		}
		res, err := h.pipe.RunPipeline(ctx, short)
		if err != nil {
			return nil, err
		}
		for j, idx := range shortIdx {
			if j < len(res.Embeddings) {
				out[idx] = res.Embeddings[j]
			}
		}
	}
	return out, nil
}

// isOverLength reports whether text exceeds the model's max sequence length. It
// prefers the real token count (including special tokens — the same measure the
// pipeline uses to size the graph) from the pipeline tokenizer, falling back to a
// conservative byte-count proxy (~3 chars/token) when the tokenizer is unreachable.
func (h *HugotEmbedder) isOverLength(text string, maxSeq int) bool {
	if n := h.tokenCount(text); n >= 0 {
		return n > maxSeq
	}
	// Tokenizer unreachable — char-proxy. 3 chars/token is the standard heuristic;
	// the chunker's own 1.3-tokens/word estimate undercounts code/URLs, so the
	// byte proxy is the safer overflow signal here.
	return len(text) > maxSeq*3
}

// tokenCount returns the real token count for text (including special tokens) when
// the pipeline tokenizer is reachable, or -1 if not (caller falls back to char-proxy).
// Read-only: it never calls the tokenizer's With() (option mutation), so it cannot
// disturb the pipeline's own tokenize path. A recover guards against any nil-field
// panic in the hugot object graph — on panic it reports -1 (char-proxy fallback)
// rather than crashing the embed path.
func (h *HugotEmbedder) tokenCount(text string) (n int) {
	defer func() {
		if r := recover(); r != nil {
			n = -1
		}
	}()
	if h.pipe == nil {
		return -1
	}
	m := h.pipe.GetModel()
	if m == nil || m.Tokenizer == nil || m.Tokenizer.GoTokenizer == nil {
		return -1
	}
	tk := m.Tokenizer.GoTokenizer.Tokenizer
	if tk == nil {
		return -1
	}
	return len(tk.EncodeWithAnnotations(text).IDs)
}

// embedLong embeds a text exceeding the model's max sequence length by splitting it
// into ≤maxSeq sub-windows, embedding each as a single-text RunPipeline batch (each
// ≤maxSeq → no graph-buffer crash), and mean-pooling the sub-vectors into one
// L2-normalized vector. Each sub-vector is already unit-length (the pipeline applies
// WithNormalization), so mean-pool then re-normalize is the correct BGE recipe.
func (h *HugotEmbedder) embedLong(ctx context.Context, text string, maxSeq int) ([]float32, error) {
	subs := h.splitLong(text, maxSeq)
	if len(subs) == 0 {
		subs = []string{text}
	}
	vecs := make([][]float32, 0, len(subs))
	for _, sub := range subs {
		res, err := h.pipe.RunPipeline(ctx, []string{sub})
		if err != nil {
			return nil, fmt.Errorf("embed sub-window (%d chars): %w", len(sub), err)
		}
		if len(res.Embeddings) != 1 {
			return nil, fmt.Errorf("embed sub-window: pipeline returned %d vectors, want 1", len(res.Embeddings))
		}
		if len(res.Embeddings[0]) != h.dim {
			return nil, fmt.Errorf("embed sub-window: vector dim %d, want %d", len(res.Embeddings[0]), h.dim)
		}
		vecs = append(vecs, res.Embeddings[0])
	}
	return normalizeL2(meanPool(vecs)), nil
}

// splitLong splits text into ≤maxSeq sub-windows. It prefers real token boundaries
// (via the pipeline tokenizer's byte spans — precise; each sub re-tokenizes to ≤maxSeq)
// and falls back to word-boundary char-proxy splitting when the tokenizer is
// unreachable. The char-proxy target is conservative (maxSeq*2 bytes per sub ≈ half
// the limit even for code/URL-heavy text) to absorb re-tokenization variance.
func (h *HugotEmbedder) splitLong(text string, maxSeq int) []string {
	if subs := h.splitByTokens(text, maxSeq); len(subs) > 0 {
		return subs
	}
	return splitTextByWords(text, maxSeq*2)
}

// splitByTokens splits text at real token boundaries using the pipeline tokenizer's
// byte spans (Spans[i] = byte offsets of token i in the original text). Returns nil
// (→ char-proxy fallback) if the tokenizer is unreachable, the text actually fits, or
// spans were not populated for this encode. A recover guards the whole walk.
func (h *HugotEmbedder) splitByTokens(text string, maxSeq int) (subs []string) {
	defer func() { _ = recover() }() // non-fatal: nil-out on any tokenizer panic
	if h.pipe == nil {
		return nil
	}
	m := h.pipe.GetModel()
	if m == nil || m.Tokenizer == nil || m.Tokenizer.GoTokenizer == nil {
		return nil
	}
	tk := m.Tokenizer.GoTokenizer.Tokenizer
	if tk == nil {
		return nil
	}
	enc := tk.EncodeWithAnnotations(text)
	if len(enc.IDs) <= maxSeq {
		return nil // tokenizer-precise path says it fits — no split needed
	}
	spans := enc.Spans
	if len(spans) == 0 {
		return nil // spans not populated for this encode — char-proxy fallback
	}
	// Reserve room for the special tokens ([CLS]/[SEP]) the tokenizer re-adds on
	// each sub-window's re-encode, plus boundary slack.
	budget := maxSeq - 4
	if budget < 1 {
		budget = 1
	}
	segStart := 0
	count := 0
	for _, sp := range spans {
		if sp.Start == sp.End {
			continue // special token (empty span) — doesn't count toward the budget
		}
		count++
		if count == budget {
			if sp.End > segStart {
				subs = append(subs, text[segStart:sp.End])
			}
			segStart = sp.End
			count = 0
		}
	}
	if segStart < len(text) {
		subs = append(subs, text[segStart:])
	}
	return subs
}

// Dimensions returns the vector length (0 until the first successful Embed loads the
// pipeline; then the pinned model's dimensionality).
func (h *HugotEmbedder) Dimensions() int { return h.dim }

// Model returns the bundled model identity (provenance + re-embed key).
func (h *HugotEmbedder) Model() string { return modelbundle.ModelID }

// splitTextByWords splits text into substrings of at most maxChars bytes each,
// breaking at the nearest ASCII whitespace boundary at or before maxChars. This is
// the conservative char-proxy fallback for when the pipeline tokenizer is unreachable.
// Pure function (no model dependency) so it is unit-testable offline. A run with no
// whitespace in the window is hard-split at maxChars to preserve the bound.
func splitTextByWords(text string, maxChars int) []string {
	if maxChars < 1 {
		maxChars = 1
	}
	if len(text) <= maxChars {
		return []string{text}
	}
	var subs []string
	for len(text) > maxChars {
		cut := strings.LastIndexAny(text[:maxChars], " \n\r\t")
		if cut <= 0 {
			cut = maxChars // no whitespace in window — hard split to preserve the bound
		}
		subs = append(subs, text[:cut])
		text = strings.TrimLeft(text[cut:], " \n\r\t")
	}
	if len(text) > 0 {
		subs = append(subs, text)
	}
	if len(subs) == 0 {
		return []string{text[:maxChars]}
	}
	return subs
}

// meanPool returns the elementwise mean of the input vectors. Inputs are expected to
// be already L2-normalized (the pipeline normalizes per-input with WithNormalization);
// the pooled result is NOT unit-length and must be re-normalized by the caller
// (normalizeL2). Returns nil for empty input.
func meanPool(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	out := make([]float32, dim)
	for _, v := range vecs {
		for i := 0; i < dim && i < len(v); i++ {
			out[i] += v[i]
		}
	}
	inv := float32(1) / float32(len(vecs))
	for i := range out {
		out[i] *= inv
	}
	return out
}

// normalizeL2 returns an L2-normalized copy of v (unit length). A zero vector is
// returned unchanged (avoids div-by-zero). BGE embeddings are L2-normalized for
// cosine similarity; the pipeline normalizes per-input, but after mean-pooling the
// result is no longer unit-length and must be re-normalized.
func normalizeL2(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1) / float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}
