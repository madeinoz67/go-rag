//go:build integration

package embed

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
)

// longRAGText is a coherent ~700-word passage about retrieval-augmented generation.
// At ~1.3 tokens/word for prose (and more for the URLs / hyphenated terms / code-ish
// tokens sprinkled in) it tokenizes to well over bge-small's 512-token
// max_position_embeddings — the exact shape that crashed GoMLX graph compilation
// before the sub-chunk fix (spec 032). It is topic-coherent so the semantic-sanity
// check (cosine closer to a relevant query than an unrelated one) is meaningful.
func longRAGText() string {
	para := "Retrieval-augmented generation (RAG) combines a retriever over a local " +
		"document store with a generator, feeding retrieved context into the prompt. " +
		"A RAG database indexes chunks with both a BM25 full-text index and a dense " +
		"vector index, fusing the two rankings via reciprocal-rank fusion (RRF). " +
		"Documents are split into chunks of roughly chunk_size tokens, embedded with a " +
		"local embedding model such as bge-small-en-v1.5, and stored content-addressed " +
		"by SHA-256. See https://example.com/rag-database for the architecture. " +
		"The retrieval loop runs the query through the same embedder, searches the " +
		"vector index for k nearest neighbours, hydrates the BM25 hits, and merges. " +
		"Re-ranking with a cross-encoder improves precision at the cost of latency. "
	return strings.Repeat(para, 14) // ~14 paragraphs ≈ well over 512 real tokens
}

// TestHugotEmbedder_OverLengthTextNoCrash is the core spec-032 regression: an input
// that tokenizes past the model's 512-token max_position_embeddings MUST embed
// successfully (sub-chunk + mean-pool inside HugotEmbedder.Embed) and return a valid
// vector of the pinned dimension. Pre-fix this panicked/crashed the whole GoMLX
// batch with "got shapes [N 757 384] and [1 512 384]".
//
// Build-tagged `integration` (needs the model present). Run with:
//
//	go test -tags integration ./internal/embed/
func TestHugotEmbedder_OverLengthTextNoCrash(t *testing.T) {
	ctx := context.Background()
	if _, err := modelbundle.EnsureModel(ctx); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	e := NewHugot()
	long := longRAGText()
	if len(long) <= modelbundle.MaxSeqLen*3 {
		t.Fatalf("test text too short to exercise the overflow path: %d chars", len(long))
	}

	vecs, err := e.Embed(ctx, []string{long})
	if err != nil {
		t.Fatalf("Embed of >512-token text failed (sub-chunk regression): %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("want 1 vector for 1 text, got %d", len(vecs))
	}
	if len(vecs[0]) != modelbundle.EmbeddingDim {
		t.Fatalf("pooled vector dim = %d, want %d", len(vecs[0]), modelbundle.EmbeddingDim)
	}
	// the pooled result must be L2-normalized (unit length) — re-normalize is part
	// of the fix; a non-unit vector would skew cosine similarity downstream.
	var mag float64
	for _, x := range vecs[0] {
		mag += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(mag)-1.0) > 1e-3 {
		t.Errorf("pooled vector magnitude = %f, want ~1.0 (L2-normalize regression)", math.Sqrt(mag))
	}
	// all-zero is the silent-failure signature (garbage pooling); reject it.
	nonZero := false
	for _, x := range vecs[0] {
		if x != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("pooled vector is all zeros — silent pooling failure")
	}
}

// TestHugotEmbedder_OverLengthPooledSemanticSanity verifies the sub-chunk + mean-pool
// output is semantically meaningful (not garbage): the pooled vector for the long
// passage must be more cosine-similar to a relevant short query than to an unrelated
// one. Catches a pooled-but-nonsense regression (e.g. dimensions averaged to mush).
func TestHugotEmbedder_OverLengthPooledSemanticSanity(t *testing.T) {
	ctx := context.Background()
	if _, err := modelbundle.EnsureModel(ctx); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	e := NewHugot()
	long := longRAGText()
	docs, err := e.Embed(ctx, []string{long})
	if err != nil || len(docs) != 1 {
		t.Fatalf("embed long doc: err=%v len=%d", err, len(docs))
	}
	q, err := e.Embed(ctx, []string{"how does a retrieval augmented generation database index and search documents"})
	if err != nil || len(q) != 1 {
		t.Fatalf("embed query: err=%v len=%d", err, len(q))
	}
	unrelated, err := e.Embed(ctx, []string{"pasta recipes with tomato sauce basil and olive oil"})
	if err != nil || len(unrelated) != 1 {
		t.Fatalf("embed unrelated: err=%v len=%d", err, len(unrelated))
	}
	simRel := cosine(q[0], docs[0])
	simIrr := cosine(q[0], unrelated[0])
	if simRel <= simIrr {
		t.Fatalf("pooled long-doc vector must be closer to the relevant query: rel=%.4f irr=%.4f", simRel, simIrr)
	}
}

// TestHugotEmbedder_MixedBatchShortAndLong verifies the partitioning path: a batch
// mixing one over-length and several short texts embeds all of them (the long one
// via sub-chunk, the short ones via the shared batch) — 1:1 text→vector preserved,
// and the long text does not crash the short ones' batch.
func TestHugotEmbedder_MixedBatchShortAndLong(t *testing.T) {
	ctx := context.Background()
	if _, err := modelbundle.EnsureModel(ctx); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	e := NewHugot()
	texts := []string{
		"short query about databases",
		longRAGText(),
		"another short text about embeddings",
	}
	vecs, err := e.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("mixed batch failed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vectors (1:1 text→vector), got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != modelbundle.EmbeddingDim {
			t.Errorf("vec[%d] dim = %d, want %d", i, len(v), modelbundle.EmbeddingDim)
		}
	}
}
