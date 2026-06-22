# Contract: `QueryTransformer` interface (the H05 seam)

> H05 introduces one new **internal interface** — the extension point future
> query-transform implementers (HyDE, multi-query, synonym/acronym expansion)
> satisfy. It is not a user-facing/transport surface (no CLI/REST/gRPC/MCP change).
> This file pins the interface contract so a future implementer can build against
> it without reading the engine internals.

## The interface

```go
// QueryTransformer alters the query before retrieval (audit H05/spec 012). The
// default is a pure normalizer; advanced transforms (HyDE, multi-query, synonym
// expansion) implement this interface in an adapter package — internal/index
// never imports the embedding model (the Reranker pattern).
type QueryTransformer interface {
    Transform(ctx context.Context, query string) ([]string, error)
}
```

## Contract every implementer MUST satisfy

| Guarantee | Statement |
|-----------|-----------|
| Output count | Returns ≥ 1 transformed query on success (the default normalizer returns exactly 1). |
| Multi-query | MAY return > 1 (for future multi-query); the engine currently consumes the first, with multi-query fan-out as future work. |
| Empty handling | If transformation yields no usable query (e.g. whitespace-only input), return a non-nil `error` — never `[]string{""}` (FR-006). |
| Determinism | A pure normalizer MUST be idempotent: `T(T(q)) == T(q)` (FR-007). |
| Unicode safety | MUST NOT corrupt non-ASCII (CJK, accents) input (FR-008). |
| Context | MUST honour `ctx` (an LLM-backed transform like HyDE will be slow/cancellable). |
| No side effects | MUST NOT mutate persisted data or the index (query-side only; Principle II). |

## The default (`NormalizingTransformer`)

Always on; no configuration. Operations: Unicode case-fold (`strings.ToLower`) +
collapse whitespace runs + trim. Applied once per query at the top of `Engine.Query`,
reaching both the keyword (FTS) and semantic (vector) paths and the H03 mismatch
guard.

## How a future implementer plugs in (enables, not implemented here)

1. Implement `QueryTransformer` in a new adapter package (e.g. `internal/hyde`),
   depending on the embedder/LLM there — **not** in `internal/index`.
2. Inject it onto the `Engine` (a constructor option will be added when the first
   production custom transformer exists; today the default is wired and the seam is
   injection-tested in-package).
3. The retrieval layer and transports require **no change** — that is the point of
   the seam (the audit's "land behind `internal/index` without Ollama coupling").

## Parity / regression anchor

Because the transform runs in the shared `Engine.Query` path, results stay identical
across CLI/REST/gRPC/MCP (the spec 003 parity contract holds). The H02 eval harness
(SC-002) is the no-regression gate — for its already-clean queries, normalization is
a no-op, so the gate passes by construction and catches only accidental breakage.
