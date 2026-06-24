# Contract — Per-Query `pool_size` Knob (CLI / REST / gRPC / MCP)

> The reranker candidate-pool override, exposed identically on all four transports (FR-001/FR-009). This contract clones the existing `rrf_k` (spec 009) knob end to end — same semantics, same resolution site — so cross-transport parity is structural, not bespoke.

## Semantics (identical on every transport)

- **Type**: non-negative integer.
- **Default / sentinel**: `0` ⇒ "use the configured `pool_size` (default 60), or the classifier-derived pool when the classifier is enabled and recommends a `k`" (see `data-model.md` Entity 2 resolution table).
- **Valid range**: `0` (sentinel) or `[1, …]`. A value `< 0` is a client error (mirrors `rrf_k` validation in `internal/cli/query.go`). No hard upper clamp at the transport — the effective pool is always grown to at least the effective `k` (spec edge case: "never silently return fewer than the requested top-`k`").
- **Resolution**: computed **once** in `Engine.Query` — `req.PoolSize > 0 ? req.PoolSize : (classifier-derived | cfg.EffectivePoolSize())`. Every transport funnels through this one site, so a query resolved one way over one transport resolves the same way over the others (FR-009).

## Transport surface

### CLI — `internal/cli/query.go`
```go
cmd.Flags().Int("pool-size", 0,
    "reranker candidate-pool override (0 = use configured pool_size / default 60; "+
    "shrinks with classifier-recommended k when adaptive depth is enabled)")
```
Plumbed into `engine.QueryRequest{..., PoolSize: poolSize}` alongside the existing `RRFK: rrfK`.

### REST — `internal/rest/server.go` + `engine_adapter.go`
New field on the query request JSON struct:
```json
{ "query": "...", "k": 5, "mode": "hybrid", "pool_size": 0 }
```
`engine_adapter.go` maps `req.PoolSize → engine.QueryRequest{PoolSize: req.PoolSize}` next to the existing `RRFK` mapping. `POST /v1/query` (route table unchanged).

### gRPC — `proto/gorag.proto`
New field, next free tag (the runtime proto currently ends at field 12, `include_quarantined`):
```proto
message QueryRequest {
  // ... fields 1–12 unchanged ...
  int32 pool_size = 13; // H22/spec 024: reranker candidate-pool override; 0 = config/default (60)
}
```
Regenerate `proto/gen`; the gRPC adapter maps it into `QueryRequest.PoolSize`.

### MCP — `internal/mcp/server.go` `go_rag_query` tool
New input property in `toolDefs()`:
```go
"pool_size": map[string]any{
    "type": "integer", "default": 60,
    "description": "reranker candidate-pool override (0 = configured/default; shrinks with classifier-recommended k)",
},
```

## Response: effective values (US3 scenario 2)

Every transport's `QueryResponse` additionally echoes the **effective** depth/pool actually used (so a caller can see whether the classifier or an override acted), as parallel additions to the existing `QueryResult`:
- `effective_k int` — the `k` used (explicit | recommended | default)
- `effective_pool int` — the pool used
- `effective_mode string` — mode used (echo; mode is never changed by H22)

These are additive JSON fields (`omitempty` not needed — small, always populated). The existing `hits`/`rerank_failed` fields are unchanged, so a pre-H22 client parsing the response is unaffected.

## Out of scope for this knob
- Per-query classifier enable/disable — the classifier is a **config-level** posture (`adaptive_depth_enabled`), not a per-call flag (see `config-keys.md`). Per-query depth control without enabling the classifier is already available via the existing `k` field.
- Splitting fusion-fetch pool from rerank pool — explicitly out of scope (spec Assumptions).
