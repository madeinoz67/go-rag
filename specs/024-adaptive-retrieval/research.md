# Research — Adaptive Retrieval Depth & Pool-Size Tuning (H22)

> Phase 0 output. Resolves every Technical Context unknown by reading the live codebase (tokensave-indexed, verified 2026-06-23). Each entry: **Decision → Rationale → Alternatives rejected**.

---

## R1 — What is "the pool" today, and where does its default (60) come from?

**Decision**: The candidate pool is `Retrieval.poolSize`, currently **hardcoded to 60** in `NewRetrieval` (`internal/index/retrieval.go:63`) with no `SetPoolSize`, no config key, and no per-query override. It drives three things: the FTS fetch (`r.fts.Search(query, r.poolSize)`), the vector fetch (`r.vec.Query(vecs[0], r.poolSize)`), and the rerank candidate count (`SearchWithRerank` grows `pool` to at least `k` then passes it to `attemptRerank`). H22 promotes this one field to a first-class config key **`pool_size`** (default **60**, today's value) overridable per query.

**Rationale**: Verified by grep — `poolSize`/`PoolSize`/`SetPoolSize` appear *only* in `internal/index/retrieval.go`; no other package reads or sets it. Defaulting the new config key to 60 makes FR-002/FR-007/SC-005 (byte-identical default) hold by construction.

**Alternatives rejected**:
- *Reuse the existing `rerank_candidates` config key (default 20).* Rejected — see R2; that key is dead and semantically narrower (rerank-only), and repurposing it would silently change meaning for anyone who set it.
- *Two separate knobs (fusion-fetch pool vs rerank pool).* Explicitly out of scope (spec Assumptions: "splitting those into two separate knobs is out of scope for H22").

---

## R2 — Is `rerank_candidates` (config default 20) the same thing as the pool?

**Decision**: **No — and it is dead config.** `Config.RerankCandidates` (default 20) is defined in `internal/config/config.go:33`, surfaced in `Get()`/`Set()`/`Validate()` and the CLI config list, but it is **never read by the engine or the retrieval layer** (grep across `internal/` confirms zero read sites outside config itself). The live pool is the hardcoded `poolSize = 60`. H22 leaves `rerank_candidates` untouched (do **not** remove — backward compat; removing is unrelated cleanup) and adds a fresh `pool_size` key.

**Rationale**: Repurposing `rerank_candidates` would (a) change its semantics from "rerank candidates" to "fusion+rerank pool", and (b) risk affecting anyone who configured it expecting future effect. A new key with today's real default (60) gives the cleanest no-regression story and the clearest name.

**Alternatives rejected**:
- *Delete `rerank_candidates`.* Out of scope; it's pre-existing dead code unrelated to H22's behavior. Touching it bakes an unrelated change into this spec.
- *Alias `pool_size` to `rerank_candidates`.* Rejected — two names for one thing breeds drift.

---

## R3 — What is the classifier's extension-point contract, and where does it live?

**Decision**: Mirror the **`QueryTransformer`** pattern (`internal/index/transform.go`) exactly:

```go
// internal/index/classify.go  (NEW — same package as transform.go)
type QueryClassification struct {
    K        int    // recommended retrieval depth; 0 == "no recommendation" (use default)
    Rationale string // human-readable, for status/logging only
}

type QueryClassifier interface {
    Classify(ctx context.Context, query string) QueryClassification
}
```

The default **`RuleBasedClassifier`** is pure Go (stdlib `strings`/unicode), lives in `internal/index` (keeping the package embedder-free, FR-008/Constitution V), and returns **`K` only — never mode** (clarification Q1 = Option A). The engine holds it as `e.classifier index.QueryClassifier`; it is set to `RuleBasedClassifier{}` when `adaptive_depth_enabled` is true and left **nil** otherwise (default posture = no classification, FR-007). A future LLM-based classifier implements the same interface **in an adapter package** (e.g. `internal/classify`), the same way `Reranker`'s Ollama impl lives in `internal/rerank`, not `internal/index`.

**Rationale**: FR-004/FR-005 mandate "the same extension-by-interface pattern already used for query transformation and reranking." `transform.go` is the closest precedent (pre-retrieval seam, pure-Go default, interface-in-index/impl-in-adapter). Matching it makes the feature instantly legible to anyone who knows the existing code.

**Alternatives rejected**:
- *Classifier returns `(k, mode)`.* Rejected by clarification Q1 — recommending mode is deferred to keep H22 at S effort and preserve the cleanest no-regression story; the interface can widen later without breaking callers.
- *Rule-based impl in `internal/rerank` or a new top-level package.* Rejected — the classifier runs **before** retrieval (it decides `k`), is pure Go, and belongs with the other pre-retrieval seam (`transform.go`) in `index`.
- *Model-based classifier in v1.* Out of scope (FR-008 / spec Assumptions) — it would couple `index` to Ollama, breaking the established pattern.

---

## R4 — How is the effective pool computed when the classifier recommends a shallow `k`? (FR-011)

**Decision**: Resolve two values once at the top of `Engine.Query`, in this order:

1. **Effective `k`** = `explicit > recommended > default` (FR-006):
   - if the caller set `k` (`req.K > 0` after the existing clamp to [1,100]) → use it, skip classification;
   - else if the classifier is enabled and returns `K > 0` → use the recommended `k`;
   - else → today's default (5).

2. **Effective pool** = `configured ceiling` when no recommendation / classifier off (byte-identical default), else **clamped**:
   ```
   recommended_pool = recommended_k + slack
   effective_pool   = clamp(recommended_pool, FLOOR, configured_ceiling)
   ```
   where `configured_ceiling = cfg.EffectivePoolSize()` (default 60), and a per-query `req.PoolSize > 0` overrides the ceiling. The existing `pool < k ⇒ pool = k` floor in `SearchWithRerank` is preserved so the request is always satisfiable (edge case: pool smaller than `k`).

**Starting constants** (chosen to be obviously safe, then tuned via the eval harness against SC-001/SC-003): `slack = 10`, `FLOOR = 20`. These are package-level `const`s in `internal/index/classify.go` (or config keys if tuning shows operators need them — **deferred**: hard-coded constants first, promote to config only if SC-001 demands it, to keep the config surface minimal).

**Rationale**: FR-011 requires that a shallow recommendation actually shrinks the pool (otherwise reduced depth saves nothing, since `poolSize` is what drives FTS/vector fetch + rerank cost). The floor prevents a tiny `k` from starving rerank on the rare deep query that was misread as factoid; the ceiling preserves the byte-identical default. `slack` decouples pool from `k` so rerank still sees a few more candidates than it returns.

**Alternatives rejected**:
- *Pool = exactly `k`.* Rejected — rerank needs more candidates than it returns to be meaningful; `k` alone starves it.
- *Pool = fixed multiple (e.g. `2k`).* Rejected — unbounded growth for large recommended `k`; the ceiling clamp is essential.
- *Expose slack/floor as config on day one.* Rejected for now — YAGNI; two more keys bloat the surface. Promote only if the harness shows operators need per-corpus tuning (tracked as a follow-up).

---

## R5 — Does the result-cache key need to change?

**Decision**: **Yes — this is a required correctness change, not optional.** Today `cacheKey` (`internal/engine/cache.go:154`) folds query/mode/k/threshold/rrf_k/filter/context-window/rerank/quarantine/epoch but **not pool**, because pool was a constant 60 and therefore not a result differentiator. Once pool varies (per-query override + classifier-driven shrinking), two queries identical except for pool would **collide** and return the wrong cached result. The fix: add **`EffK int`** (the *effective* k, which may differ from `req.K` when the classifier recommended it) and **`EffPool int`** to `cacheKey`, and hash both.

**Rationale**: The cache invariant (cache.go:148–153) is "two queries that would produce different results always get different keys." A different pool produces different fusion/rerank results, so it must produce a different key. Folding *effective* (not *requested*) k means a query with explicit `k=5` and a classifier-recommended `k=5` correctly share a key (same results), while different effective depths diverge.

**Migration safety**: the result cache is in-process, empty on restart, never persisted (FR-007 per cache.go:40–43). Changing the key schema therefore cannot corrupt or invalidate persisted state — a restart starts fresh. No migration code needed; document the key change in `contracts/status-and-cache.md`.

**Alternatives rejected**:
- *Keep pool out of the key; flush cache on config change.* Rejected — does not protect per-query overrides, and per-query pool can change between calls without any config event.
- *Separate cache namespace per pool.* Rejected — folds into the existing single-key hash more cleanly.

---

## R6 — How does the per-query `pool_size` knob round-trip across all four transports? (FR-001/FR-009)

**Decision**: Clone the **`rrf_k`** plumbing end to end (it is the most recent precedent for a per-query retrieval knob landing on all four transports), substituting `pool_size`:

| Transport | Today's `rrf_k` (template) | H22 `pool_size` (new) |
|-----------|----------------------------|------------------------|
| **CLI** | `cmd.Flags().Int("rrf-k", 0, …)` in `internal/cli/query.go` | `cmd.Flags().Int("pool-size", 0, "reranker candidate-pool override (0 = use configured pool_size / default 60)")` |
| **REST** | `RRFK` field on the query request struct (`internal/rest/server.go`) → `engine.QueryRequest{RRFK: req.RRFK}` (`engine_adapter.go`) | `PoolSize` field → `engine.QueryRequest{PoolSize: req.PoolSize}` |
| **gRPC** | `int32 rrf_k = 6` in `proto/gorag.proto` | `int32 pool_size = 13` (next free tag) |
| **MCP** | `"rrf_k": {type: integer, default: 60}` in `go_rag_query` tool (`internal/mcp/server.go`) | `"pool_size": {type: integer, default: 60}` |

Resolution is identical on every transport: `req.PoolSize > 0 ? req.PoolSize : cfg.EffectivePoolSize()`, computed once in `Engine.Query`. The classifier enable/disable is **config-only** (`adaptive_depth_enabled`, default false) — NOT a per-query flag — matching US2 scenario 3 ("classifier disabled (default posture)") and US3 scenario 1 (enablement read from `status`).

**Rationale**: `rrf_k` (spec 009) is the exact precedent — a retrieval-affecting, per-query, cross-transport knob with `0 = config/default` semantics. Cloning it guarantees FR-009 (cross-transport parity) for free, because every transport already funnels through the one `Engine.Query` resolution site.

**Alternatives rejected**:
- *Per-query `--classify`/`--no-classify` flag.* Rejected — the classifier is a system posture (enabled/disabled in config), not a per-call toggle; the spec's stories treat enablement as a status-read, not a query param. (Explicit `k` already gives callers per-query control over depth without enabling the classifier.)
- *Make `pool_size` a transport-specific concept.* Rejected — violates cross-transport parity (FR-009).

---

## R7 — How is "no quality regression" actually verified, and what is the latency evidence?

**Decision**: Two gates, both already in the repo:

1. **Recall gate (FR-010/SC-003)** — `make test-eval` runs `./bin/go-rag eval --embedder offline --baseline testdata/golden/baseline.json --tolerance 2.0` (Makefile:18–19). It drives the **real** `Engine.Query` path with a deterministic offline embedder (`NewWithEmbedder`, spec 004), so any regression in fusion/rerank/cache surfaces as a recall@10 drop against the golden baseline. Because H22 is default-OFF and byte-identical (R1/R3), the baseline run is unchanged; enabling the classifier / raising-lowering pool is then measured *against* that baseline.

2. **Latency gate (SC-001)** — no new harness; SC-001 is anchored to the constitution's existing budgets (<500ms hybrid, <50ms keyword-only). Evidenced by a quickstart spot-check: a factoid query at classifier-reduced depth+pool approaches the keyword-only budget, and no query exceeds the hybrid budget. The mechanism that makes this true is R4/FR-011 — shrinking `poolSize` shrinks the actual fetch+rerank cost.

**Rationale**: The eval harness + the existing latency budgets are "the correctness gates, consistent with every prior retrieval spec (H05/H06/H08/H09/H14)" (spec Assumptions). Inventing a new SLA was explicitly rejected (clarification Q2 = Option A).

**Alternatives rejected**:
- *A new latency benchmark target.* Rejected — adds build surface for a budget that already exists in the constitution; SC-001 deliberately reuses it.
- *Online A/B comparison.* Out of scope for a local single-binary tool.

---

## Summary of resolved decisions

| ID | Decision |
|----|----------|
| R1 | Pool = today's hardcoded `poolSize` (60) → new config `pool_size` (default 60), per-query overridable. |
| R2 | `rerank_candidates` is dead config; leave it, add fresh `pool_size`. |
| R3 | Classifier mirrors `QueryTransformer` (interface + pure-Go default in `internal/index`, impl-in-adapter later); `K`-only, never mode. |
| R4 | Effective pool = `clamp(recommended_k + slack, FLOOR, ceiling)`; constants `slack=10`, `FLOOR=20` to start, tuned via eval. |
| R5 | Cache key MUST fold effective `k` + effective pool (correctness; safe — cache is in-process only). |
| R6 | Per-query `pool_size` clones the `rrf_k` plumbing on all 4 transports; classifier enable is config-only. |
| R7 | No-regression = `make test-eval` (recall) + constitution latency budgets (SC-001); no new SLA. |

All NEEDS CLARIFICATION resolved. No outstanding research tasks.
