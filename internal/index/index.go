// index.go holds package-level declarations and the VectorIndex contract. The
// BM25 FTS implementation lives in fts.go; the brute-force vector store (the
// reference VectorIndex implementation) lives in vector.go.
package index

// VectorIndex is the implementation-neutral vector-store contract — the
// nearest-neighbour backend is substitutable without touching retrieval (audit
// H27 / spec 027). The brute-force *Vector is the reference implementation; a
// future approximate-nearest-neighbour (ANN) backend is another.
//
// Implementations MUST honour the three invariants below — they are correctness
// obligations, not optional. A backend that cannot honour them must be wrapped
// or rejected at construction (FR-009); never silently accepted. Full contract:
// specs/027-vector-index-interface/contracts/vector-index.md.
//
//  1. Dimensionality-skip (FR-002): a stored vector whose length differs from
//     the query vector's length is excluded from Query results — never scored
//     over min(len(a), len(b)) dimensions. (H03 anti-silent-corruption guard.)
//  2. Determinism (FR-003): an identical (corpus, query, k) yields identical
//     results in identical order; equal-score results resolve by ascending
//     chunk-ID.
//  3. Concurrency-safety (FR-004): safe for concurrent Add/Delete (ingest
//     workers) and Query (retrieval) without external synchronisation.
//
// Persistence (Save/Load) is intentionally NOT part of this interface: the store
// is seeded from the durable embeddings (Pebble prefix 0x04) by
// pipeline.LoadIndex, and persistence is backend-specific (FR-007).
type VectorIndex interface {
	// Add stores (or replaces) the vector for chunk id.
	Add(id string, vec []float32)

	// Delete removes the vector for chunk id (no-op if absent).
	Delete(id string)

	// Query returns the top-k chunks by similarity to vec, most-similar first,
	// as Hit{ChunkID, Score}. Score is cosine similarity in [-1, 1] for the
	// reference backend. Excludes length-mismatched vectors (Invariant 1).
	Query(vec []float32, k int) []Hit
}
