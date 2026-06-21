package eval

import (
	"context"
	"hash/fnv"
	"math"
	"unicode"
)

// detDimensions is the fixed vector dimensionality of the deterministic offline
// embedder. It MUST stay constant so vectors ingested and queried under offline
// mode share one dimensionality (Principle II / research.md D2).
const detDimensions = 256

// DeterministicEmbedder is a pure-Go embedder used for offline, reproducible
// evaluation. It implements embed.Embedder but lives in the eval package so the
// eval harness has no dependency on an external embedding service (research.md D2).
//
// It is a feature-hashing ("hashing trick") vectorizer: lowercase word tokens are
// hashed into a fixed-size vector, accumulated, and L2-normalized. The result is
// deterministic for a given input (identical run-to-run, machine-to-machine, no
// network) and content-sensitive (texts sharing tokens produce vectors with high
// cosine similarity). This makes the vector leg of hybrid retrieval behave like a
// deterministic lexical-similarity signal — meaningful enough that offline recall
// measures real regressions in chunking, RRF weighting, and rerank fallback,
// rather than noise. It is explicitly NOT a substitute for a real semantic model
// (use the Ollama embedder for the published baseline — research.md D3).
type DeterministicEmbedder struct{}

// NewDeterministicEmbedder returns the offline deterministic embedder.
func NewDeterministicEmbedder() *DeterministicEmbedder { return &DeterministicEmbedder{} }

// Embed vectorizes each input text deterministically. It performs no I/O and
// never returns an error (it cannot reach a network and cannot OOM at this scale).
func (d *DeterministicEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = vectorize(t)
	}
	return out, nil
}

// Dimensions returns the fixed vector dimensionality.
func (d *DeterministicEmbedder) Dimensions() int { return detDimensions }

// Model returns the sentinel model name recorded for offline runs.
func (d *DeterministicEmbedder) Model() string { return "deterministic-hash" }

// vectorize maps text to a normalized fixed-size float vector via feature hashing.
func vectorize(text string) []float32 {
	v := make([]float32, detDimensions)
	for _, tok := range tokenize(text) {
		h := fnv64(tok)
		// Two independent buckets per token reduce collision skew and spread
		// signal across the vector without any per-corpus state.
		v[uint32(h)%uint32(detDimensions)] += 1.0
		v[uint32(h>>32)%uint32(detDimensions)] += 1.0
	}
	// L2-normalize so cosine similarity is well-defined.
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum > 0 {
		inv := 1.0 / math.Sqrt(sum)
		for i := range v {
			v[i] = float32(float64(v[i]) * inv)
		}
	}
	return v
}

// tokenize splits text into lowercase alphanumeric word tokens (unicode-aware).
// Runs of letters/digits are tokens; everything else is a separator. This is
// deliberately simple and deterministic — it mirrors how a lexical retriever
// sees text, which is what makes offline retrieval meaningful.
func tokenize(text string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// fnv64 returns the 64-bit FNV-1a hash of s, split into two 32-bit halves.
func fnv64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
