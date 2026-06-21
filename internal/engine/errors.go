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
