# Data Model — Adaptive Retrieval Depth & Pool-Size Tuning (H22)

> Phase 1 output. Entities are in-memory Go types (no Pebble storage change — see plan.md Constitution Check II). Derived from `spec.md` §Key Entities + the live types in `internal/engine/types.go` and `internal/index/retrieval.go`.

H22 adds **no persisted entities**. Every addition is a stateless or in-memory type that lives one GC cycle or one process lifetime. This section documents the three conceptual entities from the spec and how they attach to existing engine types.

---

## Entity 1 — `QueryClassification`

*Spec: "the recommended retrieval depth (`k`) for a query, plus a human-readable rationale, produced by a classifier; applied only when the caller has not explicitly set `k`."*

```go
// internal/index/classify.go (NEW)
type QueryClassification struct {
    K         int    // recommended retrieval depth; 0 == "no recommendation" (caller's default applies)
    Rationale string // human-readable, surfaced in logs/status only; never affects ranking
}
```

**Fields**
- `K int` — the recommended top-`k`. `0` is the explicit "no opinion" sentinel: the engine falls back to today's default (5). This is distinct from a recommendation of "shallow", which is a small positive `K` (e.g. 3). Range: `0` or `[1, 100]` (the existing query-`k` clamp applies at resolution).
- `Rationale string` — short, human-readable signal (e.g. `"short factoid (3 tokens)"`). Used only for observability (status/logging). Never parsed, never affects scoring.

**Validation rules**
- `K < 0` is invalid; classifiers return `0` for "no recommendation", never negative.
- `K > 100` is clamped by the engine's existing `k` clamp, not by the classifier.
- Mode is **never** part of this struct (clarification Q1 / FR-004) — there is no `Mode` field by design.

**State transitions**: none — `QueryClassification` is a pure value produced per query and discarded. It is not stored.

**Producer**: `QueryClassifier.Classify(ctx, query) QueryClassification` (Entity: the interface, see `contracts/classifier-interface.md`).

**Consumer**: `Engine.Query`, exactly once, only when `req.K` is unset AND `adaptive_depth_enabled` is true. The recommended `K` becomes the effective `k` (FR-006: explicit > recommended > default).

---

## Entity 2 — `PoolSize`

*Spec: "the number of candidate passages entering reranking (and the fusion candidate budget); set via configuration, overridable per query."*

This is **not a new struct** — it is a resolution policy over three existing-shaped sources. The resolved value is an `int` passed to `Retrieval.SetPoolSize(n)` (new method, mirrors `SetRRFK`).

**Resolution (in `Engine.Query`, once, after transform)** — the "effective pool":

| Source | Value | When it wins |
|--------|-------|--------------|
| Per-query override | `req.PoolSize` (`> 0`) | caller set `--pool-size` / `pool_size` field on any transport |
| Configured ceiling | `cfg.EffectivePoolSize()` (default **60**) | no per-query override, AND (classifier off OR no recommendation) |
| Classifier-derived | `clamp(recommended_k + slack, FLOOR, ceiling)` | classifier enabled, recommended `K > 0`, no per-query override (FR-011) |

**Constants** (package-level, `internal/index`): `poolSlack = 10`, `poolFloor = 20`. The configured ceiling (or per-query override) is the hard upper bound; the floor protects rerank from a starvingly small recommended `k`.

**Invariants**
- `effective_pool >= effective_k` always (preserved by the existing `if pool < k { pool = k }` in `SearchWithRerank`). The system never silently returns fewer than the requested top-`k` (spec edge case).
- Default-off posture ⇒ `effective_pool == 60 == pre-H22 value` ⇒ byte-identical results (FR-007/SC-005).

**Lifecycle**: resolved per query, not stored. Feeds (a) the retrieval layer and (b) the cache key (R5).

---

## Entity 3 — `PoolUtilization`

*Spec: "an observability signal describing how the candidate pool was consumed … surfaced as an aggregate in system status (not per-query)."*

```go
// internal/engine/types.go (additions to StatusInfo — see contracts/status-and-cache.md)
type PoolUtilization struct {
    Queries     uint64  // queries observed since process start (denominator)
    AvgFetched  float64 // mean candidates fetched (FTS/vector fetch size = effective pool)
    AvgKept     float64 // mean candidates surviving to the returned top-k
    Saturated   uint64  // queries where rerank saw fewer candidates than the pool (reranker-absent / short corpus)
}
```

**Fields**
- `Queries uint64` — cumulative count of observed queries (process-lifetime; resets on restart, like `CacheStats.Hits`).
- `AvgFetched float64` — running mean of the effective pool actually fetched.
- `AvgKept float64` — running mean of `len(results)` returned. `AvgFetched / AvgKept` is the "expansion ratio" an operator uses to size the pool.
- `Saturated uint64` — count of queries where the pool could not be filled (corpus smaller than pool, or reranker absent so rerank saw the fused set). Surfaced so a low-recall query is easy to diagnose (US1 scenario 3 / US3).

**Aggregation semantics**: an **aggregate in status only** (clarification Q3 = Option A). It is a running mean over the process lifetime, updated after each non-cached query, and is **not** attached to individual `QueryResponse`s. The per-query signal that *does* stay in the response is the smaller "effective depth/mode/pool" triple (US3 scenario 2) — a separate, smaller field set (see `contracts/status-and-cache.md`).

**Validation rules**: `Queries == 0` ⇒ the struct reports zero averages (no division by zero; guard at the read site). N/A when the result cache serves a hit (no fresh retrieval; not counted).

**Lifecycle**: in-memory on the `Engine`, process-lifetime only, never persisted. Cleared on `Engine.Close` / restart (same posture as the result cache).

---

## How the entities attach to existing types

```text
QueryRequest (internal/engine/types.go)
  + PoolSize int            // 0 = use config/classifier resolution (Entity 2 per-query source)

QueryResult (internal/engine/types.go)
  + EffectiveK    int       // the k actually used (explicit | recommended | default)  — US3
  + EffectivePool int       // the pool actually used                                  — US3
  + EffectiveMode string    // echo of the mode used (unchanged; for symmetry/observability)

StatusInfo (internal/engine/types.go)
  + PoolSize             int             // cfg.EffectivePoolSize() — the configured ceiling
  + AdaptiveDepthEnabled bool            // classifier posture (default false)
  + PoolUtilization      PoolUtilization // Entity 3 — aggregate

Retrieval (internal/index/retrieval.go)
  poolSize int           // already exists; now set via SetPoolSize(effective) instead of hardcoded 60

Engine (internal/engine/engine.go)
  + classifier index.QueryClassifier   // nil when adaptive_depth_enabled=false
```

**No changes to**: `model.Source/Document/Chunk/Embedding`, Pebble key prefixes (`internal/storage`), ingest pipeline, watcher, or any persisted JSON. H22 is query-path-only and storage-stable.
