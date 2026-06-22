# Data Model — Query Caching (H06)

> Phase 1 output. Describes the new in-process entities and the (additive)
> deltas to existing `QueryRequest` / `StatusInfo` / `Config`. No persisted
> schema changes — the caches are in-process memory, empty on restart.

## New entities (in-process, `internal/engine/cache.go` + `epoch.go`)

### `cache.LRU[K, V]` (the shared bounded-LRU shape)

A minimal generic-shaped LRU used by both caches. (Go generics; `K = string`, `V = *QueryResult` or `[][]float32`.)

| Field | Type | Notes |
|-------|------|-------|
| `max` | `int` | capacity; `<=0` means "disabled" (all ops no-op) |
| `mu` | `sync.RWMutex` | readers RLock; miss-path Lock for insert/evict |
| `ll` | `*list.List` | front = most-recently-used |
| `index` | `map[string]*list.Element` | key → element |
| `hits`, `misses` | `atomic.Uint64` | cumulative counters for `status` |

**Methods**: `Get(key) (V, bool)` (bumps to front on hit; increments `hits`/`misses`); `Put(key, V)` (evict back at capacity); `Flush()`; `Stats() CacheStats` (`{Enabled, Size, Capacity, Hits, Misses}`). All concurrency-safe. When `max<=0`, `Get` always reports a miss and `Put` is a no-op (this is how the global kill-switch and the eval-harness cold path are expressed).

### `ResultCache` = `LRU[string, *QueryResult]`

- **Key**: `cacheKey{}.hash()` (D3).
- **Value**: the exact `*QueryResult` returned by a cold `Engine.Query` (shared pointer is safe — `QueryResult` is read-only after construction).
- **Owned by**: `Engine`.
- **Lifecycle**: created in `NewWithDB`/`NewWithEmbedder` (capacity from config); flushed on `Migrate` and `Close`.

### `EmbeddingCache` = `LRU[string, [][]float32]`

- **Key**: `profileFingerprint(model|dim|convention) + "\x00" + prefixedQueryText`.
- **Value**: the vector slice from `em.Embed` for that single query text.
- **Owned by**: `Engine`.
- **Lifecycle**: same as ResultCache; flushed on `Migrate` and `Close`.

### `indexEpoch`

| Field | Type | Notes |
|-------|------|-------|
| `n` | `atomic.Uint64` | monotonic; 0 at construction, bumps on every corpus mutation |

**Methods** (in `epoch.go`, on `*Engine`): `markIndexChanged()` → `atomic.AddUint64(&e.epoch, 1)`; `epoch() uint64` → `atomic.LoadUint64`. Bound into the pipeline as the `onChange` callback (D2).

### `cacheKey` (value type, hashed for the map key)

| Field | Type | Source |
|-------|------|--------|
| `Query` | `string` | normalized `req.Query` (`query.go:32`) |
| `Mode` | `string` | `req.Mode` |
| `K` | `int` | clamped `req.K` (`query.go:36-41`) |
| `Threshold` | `float64` | `req.Threshold` |
| `RRFK` | `int` | resolved effective k (`query.go:72-75`) |
| `Filter` | `filterKey{Source,Type,Tags []string}` | `req.Filter`; zero-value when nil |
| `ContextWindow` | `int` | `req.ContextWindow` |
| `RerankEnabled` | `bool` | `cfg.RerankModel != "" && !req.NoRerank` |
| `RerankModel` | `string` | `cfg.RerankModel` (only meaningful when enabled) |
| `Epoch` | `uint64` | current `indexEpoch` |

**Method**: `hash() string` — deterministic FNV-1a over the canonical encoding (sorted tag slice; stable field order). `req.NoCache` is **not** part of the key — it is a serve-bypass flag (D5).

## State transitions (result cache, per query)

```
Query enters → normalize → clamp K
  → build cacheKey (with current epoch)
  → if enabled && !req.NoCache: ResultCache.Get(key)
       HIT  → increment hits → return cached *QueryResult        (done; no embed/retrieve)
       MISS → increment misses → continue cold path
  → [cold retrieve/rerank/context] → build *QueryResult
  → if enabled && !RerankFailed && err==nil: ResultCache.Put(key, result)
  → return result
```

Embedding cache wraps the `queryEmbed` closure on the cold path (D9): `Get` before `em.Embed`, `Put` after.

## Validation rules

- **Capacities**: `query_cache_results` and `query_cache_embeddings` must be `>= 0` (`0` = that cache disabled); a negative value is rejected in `Config.Validate()`.
- **Transparency**: a cache hit MUST equal the cold result for the same key — asserted by a test that computes cold, then compares to a served hit (FR-008 / SC-004).
- **Exclusion**: `RerankFailed` results and any erroring query are never stored (FR-009).
- **Epoch monotonicity**: the epoch only ever increases; a query composed with epoch E is never served after the epoch has advanced past E (SC-002).
- **Restart**: both caches start empty (in-process; verified by a test that reopens the DB and confirms a miss).

## Deltas to existing types (additive only)

### `engine.QueryRequest` (`internal/engine/types.go`) — **+1 field**

```
NoCache bool   // H06/spec 016: bypass serving from the result cache for this query (still stores on success)
```

### `engine.StatusInfo` (`internal/engine/types.go`) — **+2 fields**

```
ResultCache    CacheStats   // H06/spec 016
EmbeddingCache CacheStats   // H06/spec 016
```

where:

```
type CacheStats struct {
    Enabled  bool
    Size     int
    Capacity int
    Hits     uint64
    Misses   uint64
}
```

(Hit rate = `Hits / (Hits+Misses)`; rendered by the CLI/REST/gRPC/MCP status adapters.)

### `config.Config` (`internal/config/config.go`) — **+3 fields**

```
QueryCacheEnabled    bool  // default true (D4); false = global kill-switch + the eval-harness cold path
QueryCacheResults    int   // default 256 (D6); 0 = result cache disabled
QueryCacheEmbeddings int   // default 512 (D6); 0 = embedding cache disabled
```

Wired into `Default()`, `Validate()`, `Get()`/`Set()` (new keys `query_cache_enabled`, `query_cache_results`, `query_cache_embeddings`), and surfaced read-only via `engine.knownConfigKeys`.

## Relationships

- `Engine` **1—1** `ResultCache`, **1—1** `EmbeddingCache`, **1—1** `indexEpoch`.
- `Pipeline` **1—1** `onChange func()` callback (provided by Engine, bound to `markIndexChanged`) — fires at the 3 mutation sites (D2).
- No new persisted entities; no new Pebble key prefixes (the constitution's single-Pebble-instance / fixed-prefix rule is untouched).
- `QueryResult` / `QueryHit` / `ContextChunk` shapes are **unchanged** — the cache stores and returns them verbatim (cross-transport parity preserved).
