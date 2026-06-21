// Package eval implements go-rag's retrieval-quality evaluation harness
// (audit item H02 — the project's #1 risk per RAG_BOOK_AUDIT.md §6).
//
// go-rag tests retrieval *mechanics* (ordering, collapse, cross-transport parity)
// but, before this package, had no way to measure retrieval *quality*. eval fills
// that gap: it computes standard information-retrieval metrics — recall@k,
// precision@k, MRR, NDCG@k — over a committed, hand-labeled golden dataset of
// query→relevant-chunk pairs, driving the same shared engine.Query path that
// CLI/REST/gRPC/MCP use, so measured quality reflects what real clients receive.
//
// The harness is pure Go (no third-party deps, Principle III), runs offline and
// reproducibly via a deterministic feature-hashing embedder (no Ollama required),
// and is read-only with respect to the user's vault. See specs/004-retrieval-eval-harness/.
package eval
