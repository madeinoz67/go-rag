package engine

import "errors"

// ErrInvalid marks a client-input error — an empty query, a missing path, an
// unknown config key. Transport adapters map it to HTTP 400 / gRPC
// InvalidArgument; every other error returned by the facade (storage, index, or
// embedder failures) is treated as an internal/server fault (HTTP 500 / gRPC
// Internal). Validation sites wrap it: fmt.Errorf("query is required: %w", ErrInvalid).
var ErrInvalid = errors.New("invalid request")

// ErrEmbeddingMismatch marks a query whose embedding model or dimensionality does
// not match the corpus's stored majority (audit H03). It prevents silent
// corruption: a query embedded under a different model/dim than the corpus would
// otherwise be scored over mismatched vectors and return plausible-but-wrong
// results. Transport adapters surface the wrapped message verbatim (the guard
// lives in the one shared Query path, so CLI/REST/gRPC/MCP refuse identically).
// Detect with errors.Is(err, ErrEmbeddingMismatch).
var ErrEmbeddingMismatch = errors.New("embedding mismatch")

// ErrNotFound marks a read that addressed a key the bound vault does not hold —
// used by GetChunk (spec 035) for a missing/stale chunk_id, and recommended for
// the chunk-scoped poison RPCs. It is a NORMAL client outcome for a point lookup
// (not a server fault), so transport adapters map it to a real not-found status:
// gRPC codes.NotFound / HTTP 404 / MCP JSON-RPC -32001 / CLI non-zero exit. This
// is distinct from ErrInvalid (malformed input → 400 / InvalidArgument). Wrap at
// the lookup site: fmt.Errorf("%w: chunk %s", ErrNotFound, chunkID). Because the
// engine is single-vault-per-process, a chunk_id from another vault resolves to
// ErrNotFound too — the chunk is simply absent from this store (cross-vault
// isolation, FR-003, falls out for free).
var ErrNotFound = errors.New("not found")
