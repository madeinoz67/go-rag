# Research — Query Caching (H06)

> Phase 0 output. Resolves every Technical Context unknown and the spec's open
> design questions (D1–D10) against the live codebase (read 2026-06-22).
> Each decision cites the file/line evidence it rests on.

## D1 — LRU implementation: hand-rolled vs `golang-lru` dependency

**Decision**: Hand-rolled bounded LRU on stdlib (`container/list` + `sync.RWMutex` + a `map[string]*list.Element`).

**Rationale**: `go.mod` has **no** cache dependency today (Pebble is the only KV). A bounded LRU is ~50 lines of trivial, well-understood code. The constitution (Principle III) and the project's single-binary/minimal-deps ethos both favour stdlib. `ResultCache` and `EmbeddingCache` share one generic-shaped implementation parameterised by value type.

**Alternatives rejected**:
- `github.com/hashicorp/golang-lru/v2` — correct, permissively-licensed (MPL-2.0), but adds a supply-chain dependency and a transitive surface for ~50 lines of code we can own and race-test ourselves. Not worth it.
- `github.com/cockroachdb/fifo` (already an indirect Pebble dep) — a FIFO cache, not LRU; wrong eviction semantics.

**Evidence**: `go.mod` require block (read in full); constitution Principle III.

## D2 — Index epoch: where it lives, what bumps it

**Decision**: An **engine-owned `atomic.Uint64` epoch** (`internal/engine/epoch.go`). The Engine binds an `onChange func()` callback into the pipeline at construction (`pipeline.New` gains the param); the pipeline calls it at **every shared-index mutation**:

| Site | File:line | Sync/Async | Mutation |
|------|-----------|------------|----------|
| `storeDocument` | `internal/pipeline/pipeline.go:246` (FTS add, confirmed at :266–267) | sync (pre-ACK) | chunk → FTS |
| `processJob` | `internal/pipeline/workers.go:55` (`p.vec.Add`) | **async** (worker goroutine) | chunk → Vector |
| `DeleteDoc` | `internal/pipeline/delete.go:16` | sync | FTS + Vector removal |

`LoadIndex` (`internal/pipeline/load.go:40,46`) does **not** bump — it is the one-time seed, and the cache starts empty at seed. `Engine.Close()` flushes both caches (the index is dropped for re-seed at `engine.go:156`).

**Rationale — why not a cheaper bump**: `processJob` runs on background workers **after** the write ACKs (async-after-ACK, Principle IV). A query that caches at epoch E right after an `Add` ACK, but before that doc's vector lands in `processJob`, would otherwise freeze a result that lacks the new vector hit. Bumping the epoch on the async vector-add is what makes the result cache correct under the existing eventual-consistency model. This is the single highest-risk correctness point and gets a dedicated regression test.

**Alternatives rejected**:
- Version counters inside `index.FTS`/`index.Vector` — would scatter a caching concern into the deliberately-dumb retrieval layer (constitution: internal/index stays free of higher-level concerns) and couples the index package to the cache concept. Rejected.
- Bump only at Engine write-method return (ACK time) — misses async `processJob` vector-adds → stale results. Rejected (the bug this spec exists to avoid).
- Derive epoch from a Pebble count on each query — a prefix scan per query is far too expensive. Rejected.

**Concurrency**: `markIndexChanged` is a standalone `atomic.Add` — it acquires **no** lock, so calling it from the async `processJob` worker cannot deadlock against `pipeMu`/`idxMu`. Lock ordering is unchanged (`pipeMu → idxMu`; the epoch/cache locks are independent).

## D3 — Result-cache key composition (exact set)

**Decision**: The key is a deterministic hash (FNV-1a → uint64, or SHA-256 truncated if collisions ever matter; FNV is fine at this scale) over the `cacheKey` struct:

```
normalizedQuery   string   // post-QueryTransformer (query.go:32)
mode              string   // req.Mode (hybrid|semantic|keyword)
k                 int      // req.K AFTER clamping (query.go:36-41)
threshold         float64  // req.Threshold
effectiveRRFK     int      // req.RRFK>0 ? req.RRFK : cfg.EffectiveRRFK() (query.go:72-75)
filter            filterKey{source,type,tags}  // req.Filter; empty when nil
contextWindow     int      // req.ContextWindow
rerank            rerankKey{enabled, model}     // enabled = cfg.RerankModel!="" && !req.NoRerank; model only when enabled
indexEpoch        uint64   // D2
```

**Rationale**: every component above is observable in `Engine.Query` (`internal/engine/query.go`) to affect the ranked output, so omitting any one yields a stale-on-parameters hit. `req.Query` is already the normalized string by the time the cache check runs (the check is placed *after* `query.go:32`). `effRRFK` is the *resolved* value so a request omitting `rrf_k` and one asking for the configured default collide correctly. `rerank.model` is included only when reranking is on, so swapping the reranker model invalidates reranked results (and turning rerank off is a separate key from any on-state).

**Alternatives rejected**: keying on the raw (pre-transform) query — would miss the spec-012 normalization sharing benefit and re-hash punctuation/whitespace variants separately. Rejected.

## D4 — Default-on vs opt-in

**Decision**: **Default-on** (transparent), with (a) a per-query `nocache` override (D5), (b) a global `query_cache_enabled=false` kill-switch (D6/config), and (c) cache stats in `status`.

**Rationale**: the cache is transparent (FR-008 — a hit is byte-identical to a cold computation), so default-on has no downside once invalidation is correct (D2), and it is the only way the documented latency win actually materialises "for free." The eval harness is exempted (D8). Trivially flipped to opt-in at implementation time if desired; the spec flags this.

## D5 — `nocache` override semantics

**Decision**: `nocache` **bypasses serving** from the cache for that call but **still stores** the freshly-computed result (so the next normal query can hit). Errors and `RerankFailed` results are still never stored (FR-009), `nocache` or not.

**Rationale**: "I want a guaranteed-fresh answer this once, but keep caching working" is the common intent (a developer forcing a re-run, an agent verifying). A full read-and-write bypass is less useful and surprising. Cheap to revisit.

## D6 — Capacity defaults + memory budget

**Decision**: defaults `query_cache_results = 256`, `query_cache_embeddings = 512`; both configurable (config Get/Set/Validate); LRU eviction at capacity.

**Memory math** (worst case, 768-dim nomic vectors):
- Embeddings: 512 × (768 × 4 B) ≈ **1.5 MB**.
- Results: 256 × (~5 hits × (~1 KB chunk text + metadata)) ≈ **1–2 MB**.
- Total ≈ **3–4 MB** — comfortably inside the < 50 MB idle / < 500 MB under-load budget (constitution §Performance).

**Alternatives rejected**: byte-bounded caches (more accurate but needs estimating `QueryResult` size — added complexity for no practical gain at this scale); unbounded (violates the memory budget). Both rejected.

## D7 — Migrate flush semantics

**Decision**: `Migrate` (`internal/engine/ingest.go:92`) flushes **both** caches at start. The ongoing `ReprocessAll` → `processJob` vector-adds bump the epoch as they land (D2), so the result cache naturally stays invalid during the (async) re-embedding; the embedding cache, keyed on the embedding profile (model+dim+convention), would auto-evict by key after the model change anyway, but the explicit flush frees memory and is defence-in-depth.

**Rationale**: the audit (§1.8) explicitly requires "cache-invalidation on embedding-model change… flush on `Migrate`." Doing it at Migrate start (not just relying on key changes) is robust against a no-op profile (e.g. convention-only change) and is the documented contract.

## D8 — Eval harness (H02) interaction

**Decision**: the H02 eval harness (`internal/eval/`, constructs its engine via `engine.NewWithEmbedder`) builds its engine with **`QueryCacheEnabled=false`** so every measurement is cold retrieval. Quality eval (recall@k — what `make test-eval` runs) is unaffected by caching regardless (a hit is identical to a cold result, SC-006), but disabling keeps any future latency measurement honest and avoids the harness ever reading a cached result.

**Rationale**: SC-006 demands no quality regression; a cold eval engine is the clean way to guarantee the gate measures retrieval, not cache luck.

## D9 — Where the query-embedding cache hooks

**Decision**: wraps the `queryEmbed` closure in `query.go:61-63`. Before calling `em.Embed(ctx, qpre.ApplyAll(RoleQuery, texts))`, check the embedding cache keyed on `(profileFingerprint, prefixedQueryText)`; on miss, call `Embed` and store. The profile fingerprint = `model|dim|convention` derived from `CorpusProfile`/`Prefixer` (the same inputs the H03/H07 mismatch guard uses).

**Scope**: the cache covers the **query-role** embed path only. The dimensionality-probe embed inside `checkEmbeddingMismatch` (`query.go:232`, unprefixed single text) is **not** cached — it is a rare probe, and keying it separately adds complexity for no measured win. Document embeddings are not cached (content-addressed/deduplicated by Principle II; re-embedded only on Migrate/reprocess).

## D10 — Concurrency model

**Decision**: each cache holds its own `sync.RWMutex` (many query-goroutine readers via `RLock`; the owning miss-path takes `Lock` to insert/evict). The epoch is `atomic.Uint64`. Hit/miss counters are `atomic.Uint64`. No cache/epoch lock is ever held while acquiring `pipeMu`/`idxMu`, and vice-versa — the cache is queried and populated purely on the query path after `indexes()` returns, and the epoch is bumped lock-free from the pipeline workers. Existing concurrency tests (`internal/engine/concurrency_test.go`, `index_cache_test.go`) are extended with cache-on `-race` scenarios.

**Rationale**: the constitution's "single-writer, concurrent-readers, eventual-consistency" model maps directly onto RWMutex + atomic. A reader that observes a just-bumped epoch simply misses and recomputes — correctness never depends on cross-goroutine epoch synchronisation.

---

## Summary of resolved decisions

| ID | Decision | One-line |
|----|----------|----------|
| D1 | LRU impl | Hand-rolled stdlib LRU; no new dependency |
| D2 | Index epoch | Engine atomic epoch; pipeline `onChange` at storeDocument/processJob/DeleteDoc (async vector-add included) |
| D3 | Result key | normalizedQuery+mode+k+threshold+effRRFK+filter+contextWindow+rerank{enabled,model}+epoch |
| D4 | Default | On by default (transparent) + kill-switch + override |
| D5 | nocache | Bypasses serving, still stores; never stores errors/RerankFailed |
| D6 | Capacity | 256 results / 512 embeddings (≈3–4 MB); configurable |
| D7 | Migrate | Flushes both caches at start; epoch bumps during async re-embed |
| D8 | Eval | Eval engine built with cache disabled (cold measurements) |
| D9 | Embed cache hook | Wraps the `queryEmbed` closure (query-role only) |
| D10 | Concurrency | Per-cache RWMutex + atomic epoch/counters; no lock-ordering change |

**No unresolved NEEDS CLARIFICATION.** Ready for Phase 1 design.
