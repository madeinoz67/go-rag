# Contract — Status Additions & Result-Cache Key Change

> Two observability/correctness surfaces: (A) new `status` fields (US3/FR-003/SC-004) and (B) a required change to the result-cache key (R5). Neither changes persisted storage.

## A. Status additions

`Engine.Status()` (`internal/engine/status.go`) returns `*StatusInfo` (`internal/engine/types.go`). H22 adds three fields, surfaced identically by `go_rag_status` (MCP), the REST/gRPC status endpoints, and the CLI `status` command:

```go
// internal/engine/types.go — additions to StatusInfo
type StatusInfo struct {
    // ... existing fields unchanged ...

    // H22/spec 024
    PoolSize             int             // effective configured candidate-pool ceiling (cfg.EffectivePoolSize())
    AdaptiveDepthEnabled bool            // classifier posture (default false)
    PoolUtilization      PoolUtilization // aggregate, process-lifetime — data-model.md Entity 3
}
```

```go
type PoolUtilization struct {
    Queries    uint64  // observed queries since start (denominator; 0 ⇒ averages are zero)
    AvgFetched float64 // mean effective pool fetched
    AvgKept    float64 // mean results returned (AvgFetched/AvgKept = expansion ratio)
    Saturated  uint64  // queries where the pool couldn't be filled (short corpus / reranker absent)
}
```

**JSON shape (REST/MCP)** — additive keys on the existing status object:
```json
{
  "documents": 42, "chunks": 1042, "...": "...",
  "pool_size": 60,
  "adaptive_depth_enabled": false,
  "pool_utilization": { "queries": 0, "avg_fetched": 0, "avg_kept": 0, "saturated": 0 }
}
```
Fresh process ⇒ `pool_utilization.queries == 0` and zero averages (no division by zero).

**Aggregate-only** (clarification Q3 = Option A): utilization is **not** on individual query responses. The per-query signal that *is* on the response is the smaller effective-depth/pool/mode triple (see `query-pool-knob.md` → "Response: effective values").

**Maintenance**: the engine updates the running means after each non-cached query (a cache hit reuses an already-counted result; not double-counted). State is in-memory, process-lifetime, cleared on restart — same posture as `CacheStats`.

## B. Result-cache key change (required — R5)

`cacheKey` (`internal/engine/cache.go:154`) currently folds every result-affecting input **except** pool (pool was constant 60, so it was not a differentiator). Once pool varies it MUST be folded, or two queries differing only in pool collide and return the wrong cached result.

**Change**: add two fields and hash them.

```go
// internal/engine/cache.go
type cacheKey struct {
    // ... existing fields ...
    EffK    int  // H22: the EFFECTIVE k (explicit | recommended | default) — may differ from req.K
    EffPool int  // H22: the EFFECTIVE candidate pool (per-query | classifier-derived | config)
    Epoch   uint64
}

// in hash(): add
write(strconv.Itoa(k.EffK))
write(strconv.Itoa(k.EffPool))
```

`resultKey(req, effRRFK, effK, effPool, epoch)` gains the two resolved-effective params and folds them in. The existing `K` field is **kept** (it records the requested `k` for debug symmetry) but `EffK` is what guarantees correctness — folding effective (not requested) `k` means an explicit-`k=5` query and a classifier-recommended-`k=5` query correctly share a key.

**Migration**: **none required.** The result cache is in-process, empty on restart, never persisted (FR-007, cache.go:40–43). Changing the key schema cannot corrupt persisted state. Documented here so reviewers see it is deliberate, not an oversight.

**Correctness invariant preserved**: "two queries that would produce different results always get different keys" (cache.go:148). A different pool ⇒ different fusion/rerank results ⇒ different key. ✓

## No other cache/observability change
- Query-embedding cache (`embedCache`): unaffected — it keys on `(profile, prefixed-text)`, not pool/k.
- Epoch invalidation: unaffected — pool/k changes do not advance the epoch (only corpus mutation does); distinct effective depths correctly produce distinct keys without epoch churn.
- Audit log (H18): the audit event already records `req.K`; it MAY additionally record `effective_k`/`effective_pool` for completeness — tracked in `tasks.md` as a minor enhancement, not a contract requirement.
