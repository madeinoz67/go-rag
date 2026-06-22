# Contract — Query Cache `nocache` Override + Status + Config (H06)

> Phase 1 output. go-rag is a CLI + multi-transport server, so the public
> interface contract spans the four transports plus config/status. This fixes the
> exact surface so `/speckit-tasks` can implement against it. **The cache itself
> is transparent — it never changes a result, only the latency — so there is no
> response-shape change.** The only public-surface additions are (1) one optional
> request flag, (2) new status fields, (3) three new config keys.

## 1. The `nocache` request override (all four transports)

Semantics (D5): when `nocache` is true, the engine **bypasses serving** from the result cache for that call but **still stores** the freshly-computed successful result (so the next normal query can hit). `RerankFailed` results and errors are never stored, `nocache` or not (FR-009). Default `false`.

| Transport | Field / flag | Wire |
|-----------|--------------|------|
| **CLI** | `--no-cache` (bool, default false) | `internal/cli/query.go`: new flag; `QueryRequest.NoCache = noCache` in the request build (alongside `NoRerank`). |
| **REST** | JSON body `"no_cache": bool` | `internal/rest/types.go`: add `NoCache bool \`json:"no_cache,omitempty"\``; `engine_adapter.go` maps → `QueryRequest.NoCache`. |
| **gRPC** | `QueryRequest.no_cache` (proto field **11**) | `proto/gorag.proto`: add `bool no_cache = 11;` to `message QueryRequest`; regen `proto/gen`; `internal/grpc/engine_adapter.go` maps → `QueryRequest.NoCache`. |
| **MCP** | tool input `no_cache` (boolean) | `internal/mcp/server.go`: add to the query tool's input schema; pass through; (no result-rendering change). |

**Parity invariant** (FR-010 / SC-007): identical `nocache` value across transports yields identical results, because all four converge on the same `Engine.Query` and the same `QueryRequest.NoCache`.

## 2. Status surface (all four transports)

`Engine.Status()` returns the existing `StatusInfo` **plus** two new fields:

```
ResultCache    CacheStats  // { Enabled, Size, Capacity, Hits, Misses }
EmbeddingCache CacheStats  // { Enabled, Size, Capacity, Hits, Misses }
```

Each transport renders these in its status output (CLI `go-rag status` prints a cache section; REST/gRPC/MCP add the fields to their status response). Hit rate is derived by the consumer as `Hits/(Hits+Misses)` (avoid divide-by-zero when both are 0).

| Transport | Wire |
|-----------|------|
| **CLI** | `internal/cli/status.go`: render a "Cache" section (enabled/size/capacity/hits/misses for each). |
| **REST** | status response JSON gains `result_cache` + `embedding_cache` objects. |
| **gRPC** | `StatusResponse` gains the two cache stats (proto regen). |
| **MCP** | status tool output gains the cache fields. |

## 3. Config keys (existing Get/Set surface)

Three new keys, all optional with sensible defaults, surfaced read-only in `engine.knownConfigKeys` and settable via `go-rag config set` / REST/gRPC/MCP config endpoints:

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `query_cache_enabled` | bool | `true` | Global kill-switch. `false` disables both caches (every Get is a miss, every Put a no-op). |
| `query_cache_results` | int | `256` | Result-cache capacity (entries). `0` disables the result cache only. Negative rejected by `Validate`. |
| `query_cache_embeddings` | int | `512` | Embedding-cache capacity (entries). `0` disables the embedding cache only. Negative rejected. |

`Config.Set` parses bool/int and validates `>= 0` (capacities) before persisting (`internal/config/config.go`). `Config.Get` returns the string form (mirroring the existing keys).

## 4. Behavioural contract (transport-invariant)

These hold identically over CLI/REST/gRPC/MCP because they are properties of `Engine.Query`, not of any adapter:

1. **Transparency** — a cache hit is byte-identical to a cold computation for the same key (FR-008 / SC-004).
2. **Invalidation** — after any ingest/delete/migrate, a previously-cached query returns a result consistent with the new corpus (FR-003 / SC-002), because the index epoch advanced.
3. **Migrate flush** — `go-rag migrate` flushes both caches (FR-006 / SC-003).
4. **No-degraded-cache** — `RerankFailed` results and errors are never cached (FR-009).
5. **Restart-empty** — the cache is in-process; a fresh process starts cold (FR-007).

## 5. Out-of-contract (explicitly not exposed)

- No API to manually flush/clear the cache (ingest/delete/migrate/`nocache`/restart cover every real need; a manual flush knob is YAGNI).
- No cache inspection per-key (only aggregate stats).
- No persisted cache (H16 territory; out of scope).
- No semantic/near-duplicate query matching (H20; out of scope).
