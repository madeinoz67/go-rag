// Package index holds the two retrieval indexes (PRD §6.6):
//
//   - FTS: a field-weighted BM25 inverted index (title 3.0x, headings 2.0x,
//     body 1.0x) stored under Pebble prefixes 0x05–0x08.
//   - Vector: an HNSW approximate-nearest-neighbour index managed by chromem-go.
//
// Hybrid retrieval fuses the two via Reciprocal Rank Fusion (PRD §4.3).
// TODO(later): implement both indexes.
package index

// FTS is the BM25 full-text index (PRD §6.6). Stub.
type FTS struct{}

// Vector is the chromem-go-backed HNSW vector index (PRD §6.6). Stub.
type Vector struct{}
