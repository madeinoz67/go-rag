# Implementation Plan: Query Caching — Result + Query-Embedding LRU (H06)

**Branch**: `main` | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/016-query-cache/spec.md` (audit backlog item **H06**, P1 — first Phase 4 item).

## Summary

Add two in-process, bounded, exact-match LRU caches to the engine query path: a **result cache** (full query shape + index epoch → complete `QueryResult`) checked at the top of `Engine.Query`, and a **query-embedding cache** (embedding profile + prefixed query → vector) wrapping the query-embed closure. Invalidated by a new **engine-owned index epoch** that bumps at every shared-index mutation — including the **asynchronous** vector-add in `processJob`, which a write-ACK-only bump would miss. Both flush on `Migrate`. Hand-rolled LRU (stdlib `container/list` + `sync.RWMutex`) — **no new dependency**. Default-on (transparent), with a `nocache` override on all four transports and cache stats in `status`.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`; satisfies the CLAUDE.md "1.22+" floor). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib only for the cache (`container/list`, `sync`, `sync/atomic`, `crypto/fnv` for key hashing); existing `internal/engine`, `internal/pipeline`, `internal/index`, `internal/config`. Proto regen for gRPC field 11. **No new module dependencies** (decision D1).

**Storage**: Pebble KV — **N/A**. Both caches are in-process memory, empty on restart, NOT persisted (they hold non-authoritative derived data; persisting them would blur into H16 and violate the single-writer/durability model for derived state). Principle II intact (no identity/hash change — caches are keyed lookups over existing results).

**Testing**: `go test -race -cover ./...`; new cache tests (hit/miss, epoch invalidation on ingest/delete/migrate, capacity eviction, concurrency under `-race`, nocache bypass, rerank-failed-not-cached); H02 eval gate for no quality regression (SC-006). Cross-transport parity test extended with `nocache`.

**Project Type**: CLI + multi-transport server. Touches: engine (caches + epoch + Query wiring + status + config), pipeline (epoch-bump callback at the 3 mutation sites), all four transport adapters + proto, config.

**Performance Goals**: A result-cache hit returns in microseconds (vs the < 500 ms hybrid / < 50 ms keyword / < 100 ms vector cold budgets). Book targets: 40–60 % result hit rate, 30–50 % embedding hit rate on technical docs — measurable via the new `status` counters.

**Constraints**: Pure Go, no new deps; bounded footprint within the < 50 MB idle / < 500 MB under-load memory budget; single-writer/concurrent-reader safe (RWMutex on each cache, `atomic.Uint64` epoch); exact-match only (semantic/near-dup is H20); cache transparent (hit byte-identical to cold).

**Scale/Scope**: local single-user scale (< 10 K docs); modest default capacities (256 results / 512 embeddings).

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | In-process memory caches; no network/cloud. Single binary unchanged (no new dep — D1). |
| II | Content-Addressed Identity | ✅ Pass | Caches are keyed lookups over already-computed `QueryResult`/vectors; no document identity, hash, or persisted-record change. Non-authoritative derived data, intentionally not persisted. |
| III | Pure Go — No CGo/External Runtime | ✅ Pass | Hand-rolled LRU on stdlib (`container/list` + `sync`); `golang-lru` dependency explicitly rejected (D1). Proto regen is build-time. |
| IV | Async-After-ACK Writes | ✅ Pass | Caches live on the query path (post-ACK). The epoch bump is a non-blocking `atomic.Add` safe to call from the async `processJob` worker; it never blocks the < 10 ms write-ACK. Cache *improves* query latency. |
| V | Extension by Interface, MCP-First | ✅ Pass | `nocache` override exposed on CLI/REST/gRPC/MCP (parity); cache stats surfaced in `status`; capacities configurable via the existing config Get/Set surface. |

**No violations.** (Re-check after Phase 1 design — still clean; the design adds no persisted state, no new process, no new dependency, and the only public-surface additions are an optional request flag and status/config fields.)

## Project Structure

### Documentation (this feature)

```text
specs/016-query-cache/
├── plan.md                         # this file
├── research.md                     # Phase 0 — D1–D10 decisions resolved
├── data-model.md                   # Phase 1 — cache entities, key composition, config/status deltas
├── contracts/
│   └── query-cache-contract.md     # Phase 1 — nocache override + status + config contract (4 transports)
├── quickstart.md                   # Phase 1 — runnable validation scenarios
└── tasks.md                        # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/engine/
├── cache.go          # NEW: ResultCache + EmbeddingCache (hand-rolled LRU, RWMutex); cacheKey + Hash()
├── epoch.go          # NEW: engine-owned atomic index epoch; markIndexChanged(); read in cache-key
├── engine.go         # EDIT: Engine gains resultCache, embedCache, epoch; Close() flushes both; bind onChange to pipeline
├── query.go          # EDIT: result-cache check (after K-clamp, before indexes()); wrap queryEmbed closure with embed cache; store on success (skip if RerankFailed)
├── ingest.go         # EDIT: Add/Scan/Reprocess/Migrate wire the onChange callback; Migrate flushes both caches
├── status.go         # EDIT: StatusInfo gains ResultCache + EmbeddingCache stats
├── types.go          # EDIT: QueryRequest.NoCache bool; StatusInfo cache-stats fields; CacheStats struct
└── config.go         # EDIT: surface cache config keys in knownConfigKeys (read-only via existing Get)
internal/pipeline/
├── pipeline.go       # EDIT: pipeline.New gains onChange func(); storeDocument calls it after FTS add
├── workers.go        # EDIT: processJob calls onChange after vec.Add (async vector landing)
└── delete.go         # EDIT: DeleteDoc calls onChange after FTS/vector removal
internal/config/config.go   # EDIT: QueryCacheEnabled/QueryCacheResults/QueryCacheEmbeddings + Get/Set/Validate/Default
internal/cli/query.go       # EDIT: --no-cache flag → QueryRequest.NoCache
internal/cli/status.go      # EDIT: render cache stats
internal/rest/{types,engine_adapter}.go  # EDIT: no_cache field; passthrough
proto/gorag.proto + proto/gen/           # EDIT: QueryRequest.no_cache = 11; regen
internal/grpc/engine_adapter.go          # EDIT: map no_cache
internal/mcp/server.go                   # EDIT: schema + renderQuery (cache transparent; no result-shape change)
internal/eval/ (H02 harness)             # EDIT: construct eval engine with QueryCacheEnabled=false (cold measurements)
```

**Structure Decision**: Three cohesive layers, each additive and independently testable:

1. **Cache primitives** (`cache.go`, `epoch.go`) — two bounded LRUs (RWMutex + `container/list`) and an atomic epoch. No imports of pipeline/index; pure stdlib. Unit-testable in isolation.
2. **Engine wiring** (`engine.go`, `query.go`, `ingest.go`, `status.go`, `types.go`, `config.go`) — the Engine owns the caches + epoch; `Query` checks/stores; the pipeline gets an `onChange` callback bound to `markIndexChanged` (covering sync FTS-add, async vector-add, and delete); `Migrate` flushes; `Status` reports.
3. **Transport + config exposure** (CLI/REST/gRPC/MCP + config) — `nocache` override on all four (proto field 11); three new config keys; status rendering.

The single highest-risk item is the **epoch bump coverage** (D2): missing a mutation site — especially the async `processJob` vector-add — returns stale results. A dedicated regression test (ingest → query → wait for async embed → query again at the new epoch) gates this.

## Complexity Tracking

*(Empty — Constitution Check passes on all five principles. No violations to justify.)*
