# Feature Specification: Query Caching — Result + Query-Embedding LRU

**Feature Branch**: `016-query-cache` *(commits directly to `main` per project convention.)*

**Created**: 2026-06-22 · **Status**: Draft

**Input**: "next backlog item" → **H06** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 4, first open item):
*"No caching (query-result + query-embedding). LRU keyed on `(query,mode,k,gen)` for results and
`(model,query)` for embeddings; flush on `Migrate`."* Source: `RAG_BOOK_AUDIT.md` §1.7 (P1: "No
query/result cache… hit rates 40–60% on technical docs"), §4.7 (P2: "No query-embedding cache…
30–50%"), §1.8 (P1 dup: "cache-invalidation on embedding-model change… flush on `Migrate`").

**Problem:** go-rag does identical work over and over for identical queries. Every query re-embeds
the (normalized, prefixed) query string via a loopback Ollama round-trip, then re-runs the full
retrieve → fuse → (optionally rerank) pipeline to produce the same ranked hits. For the common
cases — a developer iterating on the same lookup, an AI agent re-asking, frequent keyword queries on
a stable corpus — this is pure waste: wasted latency against the constitution's query-latency budgets,
and wasted Ollama load. The retrieval book documents response-cache hit rates of **40–60%** and
query-embedding-cache hit rates of **30–50%** on technical documentation. go-rag has **zero caching**
today, and `Migrate` invalidates nothing.

The fix is two layered, in-process, bounded LRU caches:

1. a **result cache** keyed on the full query shape + an **index epoch** → the complete ranked result; and
2. a **query-embedding cache** keyed on the embedding profile + the prefixed query text → the query vector.

Both are exact-match only, transparent (they never change the result a caller sees, only the
latency), and flushed on `Migrate`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Repeated queries return instantly from cache (Priority: P1) 🎯 MVP

A user (or agent) issues the same query twice — same text, same mode/k/filter/etc. The second query
returns the identical ranked result without re-embedding or re-retrieving: a cache hit.

**Why this priority**: This is H06's headline win — the book's 40–60% response-cache hit rate. It
removes the embedding round-trip AND the retrieve/rerank work for any repeated query, the largest
latency/cost reduction available with zero quality change. Caching is transparent: the result is
byte-identical to a cold computation.

**Independent Test**: Issue a query (cold), then issue the exact same query again; assert the second
returns the identical result and is served from the result cache (no embed/retrieve work, observable
via the status hit counter).

**Acceptance Scenarios**:

1. **Given** an empty cache, **When** the user issues query Q, **Then** it is computed (miss) and stored.
2. **Given** Q is cached, **When** the user issues Q again with identical parameters, **Then** the
   identical result is returned from the result cache (hit), with no embedding round-trip and no
   retrieval work.
3. **Given** Q cached, **When** the user issues Q with a different retrieval-affecting parameter
   (mode/k/threshold/rrf_k/filter/context_window/rerank), **Then** it is a miss and recomputed (a
   different result shape is a different cache entry).
4. **Given** Q cached, **When** the user issues Q with a `nocache` override, **Then** Q is recomputed
   fresh (not served from the cache).

---

### User Story 2 - A corpus change evicts stale results (Priority: P1)

The cache never returns a stale result after the index changes. Any ingest/delete/watcher/migrate
that changes the searchable corpus invalidates affected cached results (via the index epoch), so a
query after a change always reflects the current corpus.

**Why this priority**: Correctness — a cache that returns stale results after the corpus changed is a
silent corruption bug. The shared-live index (spec 011) is mutated in place with no generation counter
today; the cache introduces an **index epoch** that bumps on any corpus mutation, evicting stale
entries by key.

**Independent Test**: Query Q (cache it); ingest or delete a document; query Q again; assert the
result reflects the new corpus (recomputed, not the stale cached copy).

**Acceptance Scenarios**:

1. **Given** Q is cached at epoch E, **When** a document is ingested (or deleted, or a watcher change
   lands), **Then** the index epoch advances to E+1.
2. **Given** epoch advanced to E+1, **When** Q is queried again, **Then** Q's old (epoch-E) entry is
   not served — Q is recomputed against the current corpus and cached under E+1.
3. **Given** `Migrate` re-embeds under a new model, **When** it completes, **Then** BOTH caches are
   flushed (result + query-embedding), so no stale pre-migration result or vector survives.

---

### User Story 3 - Identical queries skip the embedding round-trip (Priority: P2)

Even when the result cache is missed (the result entry was evicted by capacity, or a parameter
changed that doesn't affect the query embedding), an identical query string under the same embedding
profile reuses the cached query vector instead of re-calling Ollama.

**Why this priority**: The book's 30–50% query-embedding hit rate — a cheaper, longer-lived second
layer that survives result-cache churn and parameter changes that don't touch the query text/model.
Bounds Ollama load on repeated query terms.

**Independent Test**: Query Q (cold — embeds and caches the vector); force a result-cache miss for Q;
query Q again; assert Q's embedding is served from the embedding cache (no Ollama call).

**Acceptance Scenarios**:

1. **Given** Q's embedding is cached under profile P, **When** Q is re-embedded for any reason under
   the same profile P, **Then** the cached vector is reused (no Ollama call).
2. **Given** Q's embedding cached under profile P, **When** the embedding model/convention changes
   (different profile P'), **Then** the old entry is not served (different key); Q is embedded fresh
   under P'. (`Migrate` flushes the whole embedding cache regardless.)
3. **Given** the query-embedding cache, **When** a document is ingested, **Then** the embedding cache
   is NOT flushed (document content doesn't change query vectors — only a model/convention change does).

---

### User Story 4 - Cache is visible, bounded, and safe (Priority: P2)

The cache is observable via `status` (enabled, size, capacity, hits, misses), bounded so it can't
blow the memory budget, concurrency-safe under the single-writer/concurrent-reader model, and empty
on restart (in-process only, not persisted).

**Why this priority**: Operability — a cache you can't see or size is a liability. The win must be
measurable (hit rate) and the footprint must stay within the constitution's memory budget.

**Independent Test**: Run a mix of repeat/novel queries; `go-rag status` reports a non-zero hit count
and a bounded size; concurrent queries don't race.

**Acceptance Scenarios**:

1. **Given** cache activity, **When** the user runs `go-rag status`, **Then** it reports cache
   enabled, current size, capacity, and cumulative hit/miss counts (hit rate).
2. **Given** the cache at capacity, **When** a new entry is added, **Then** the least-recently-used
   entry is evicted (bounded, LRU).
3. **Given** concurrent queries (readers) while a write/ingest is in progress, **When** they read the
   cache, **Then** no race or corruption occurs (the cache is guarded; a stale-epoch read simply misses).
4. **Given** a restart of go-rag, **When** it starts, **Then** the cache starts empty (it is
   in-process memory, not persisted).

---

### User Story 5 - Cross-transport override + parity (Priority: P2)

The `nocache` override is exposed identically on CLI/REST/gRPC/MCP, and the cache is transparent
across all transports (a cached result over REST == gRPC == MCP).

**Why this priority**: Consistency (parity, constitution Principle V) + control (a caller on any
transport can force a fresh result).

**Independent Test**: Same query with `nocache` over CLI/REST/gRPC/MCP → identical fresh results;
same query without the override → identical cached results across transports.

**Acceptance Scenarios**:

1. **Given** the query operation on any transport, **When** `nocache` is supplied, **Then** the query
   bypasses serving from the cache for that call.
2. **Given** the same query key, **When** served from cache over different transports, **Then** the
   results are identical (parity).

---

### Edge Cases

- **Rerank-failed result**: a result where the reranker failed (`RerankFailed`, spec 006) MUST NOT be
  cached — it is degraded and a retry may succeed; caching would freeze the failure. (Retry is already
  opt-in per spec 006.)
- **Reranker model changes**: when rerank is enabled, the result depends on the reranker model; the
  result-cache key MUST include the reranker-model identity so a reranker swap doesn't return stale
  reranked order. (When rerank is off, it isn't in the key.)
- **Empty/whitespace query, or a query that errors**: errors are never cached; only successful
  results are cached.
- **`nocache` override semantics**: bypasses serving from the cache. Whether it also inhibits
  *storing* the fresh result is a plan decision — default assumption: it still stores, so the next
  non-override query can hit (see Assumptions).
- **Eviction by capacity (LRU)**: a result miss after eviction falls through to the query-embedding
  cache (which may still hit), then to a full recompute.
- **Index-epoch race**: a reader observing a just-bumped epoch simply misses and recomputes;
  correctness never depends on the epoch being synchronized across readers (single-writer model).
- **Context expansion (spec 015)**: the result-cache key MUST include `ContextWindow` — different
  windows yield different outputs. (Plan MAY optimize by caching the pre-context ranked list and
  expanding context after a hit; the observable result must equal caching the full shape.)
- **Filter (spec 014)**: the result-cache key MUST include the full `Filter` (source/type/tags) — a
  filtered query is a different result from an unfiltered one.
- **Query transformation (spec 012)**: the key is on the NORMALIZED query (post-transformer), not the
  raw input — two raw queries that normalize to the same string share an entry. (Normalization is
  deterministic/idempotent per spec 012.)
- **Eval harness (H02)**: the cache must not skew latency measurements — latency eval runs cold
  (cache disabled or flushed per run) so timings reflect real retrieval, not cache hits. Quality
  metrics are unaffected (cached results are identical).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide an in-process **result cache**: an exact-match, bounded LRU
  mapping the full query shape to the complete ranked result.
- **FR-002**: The result-cache key MUST include every retrieval-affecting input: the normalized query
  text, retrieval Mode, k, threshold (if used), rrf_k, Filter, ContextWindow, rerank-enabled +
  reranker-model (when rerank is on), and the current index epoch.
- **FR-003**: The system MUST maintain an **index epoch** — a monotonically increasing counter that
  advances on any change to the searchable corpus (chunk add, chunk delete, watcher update, migrate).
  The epoch is part of the result-cache key, so a corpus change evicts stale results by key.
- **FR-004**: The system MUST provide an in-process **query-embedding cache**: an exact-match, bounded
  LRU mapping the embedding profile (model + dimension + convention) and the prefixed query text to
  the query vector.
- **FR-005**: The query-embedding cache MUST sit in front of the query-embedding step, so a repeated
  query string under the same profile reuses its vector without an Ollama round-trip. Document
  embeddings are NOT cached here (already content-addressed/deduplicated — Principle II).
- **FR-006**: `Migrate` (re-embedding under a new model) MUST flush BOTH caches (result +
  query-embedding). An embedding-profile change (model/convention/dim) MUST invalidate affected
  embedding-cache entries by key.
- **FR-007**: Both caches MUST be bounded (configurable capacity, LRU eviction) and concurrency-safe
  under single-writer/concurrent-readers. They MUST be empty on restart (in-process memory, not
  persisted to Pebble).
- **FR-008**: Caching MUST be transparent: a cache hit MUST return a result byte-identical to a cold
  computation for the same key. Enabling/disabling the cache never changes the result a caller sees,
  only the latency.
- **FR-009**: The system MUST NOT cache unsuccessful or degraded results — specifically, results with
  `RerankFailed` set (spec 006) and any result that returned an error are excluded.
- **FR-010**: A query MUST accept a `nocache` override that bypasses serving from the cache for that
  query. The override MUST be exposed identically on CLI/REST/gRPC/MCP.
- **FR-011**: `go-rag status` MUST surface cache state: enabled/disabled, result-cache and
  embedding-cache size/capacity, and cumulative hits/misses (hit rate).
- **FR-012**: Cache enable/disable and capacities MUST be configurable (config file + sane defaults).
  A global kill-switch (caching off entirely) MUST exist.
- **FR-013**: Caching MUST default ON (transparent, with the escape hatches above) so the documented
  latency win is delivered without opt-in. (Latency eval is exempted — see Edge Cases.)

### Key Entities

- **Result cache**: bounded in-process LRU; key = full query shape (FR-002) + index epoch; value = the
  complete ranked `QueryResult`. Exact-match, transparent, epoch-invalidated.
- **Query-embedding cache**: bounded in-process LRU; key = embedding profile (model+dim+convention) +
  prefixed query text; value = the query vector. Survives result-cache churn/epoch bumps; flushed only
  by Migrate/profile-change.
- **Index epoch**: a monotonic counter advanced on every corpus mutation (ingest/delete/watcher/migrate);
  the invalidation signal for the result cache. Introduced by H06 (the shared-live index from H01 has
  no such counter today).
- **Cache shape**: the tuple of all retrieval-affecting query inputs that determines the ranked output;
  two queries with the same shape and epoch are cache-equivalent.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A repeated identical query (second issue) returns in negligible time versus the cold
  query — no embedding round-trip, no retrieval work (verifiable via the status hit counter and timing).
- **SC-002**: After a corpus change (ingest/delete), a previously-cached query returns a result
  consistent with the NEW corpus (never the stale pre-change result).
- **SC-003**: After `Migrate`, a previously-cached query is recomputed fresh (neither cache serves a
  pre-migration result/vector).
- **SC-004**: A cache hit is byte-identical to a cold computation for the same key (transparency) —
  verifiable by computing cold and comparing.
- **SC-005**: `go-rag status` reports non-zero hit counts after a realistic repeat-query workload, and
  cache size stays within the configured capacity (bounded).
- **SC-006**: With caching on, the H02 eval harness shows no quality regression (recall@10 unchanged —
  cached results are identical); latency-eval is run cold so caching doesn't mask regressions.
- **SC-007**: The `nocache` override produces identical fresh results across CLI/REST/gRPC/MCP (parity).

## Assumptions

- **Caching defaults ON** (transparent, with `nocache` override + global kill-switch + status). This
  delivers the audit's documented latency win without opt-in; the alternative (opt-in) is rejected
  because a transparent cache has no downside once invalidation is correct. Trivially swapped to
  opt-in at plan if Stephen prefers.
- **Index epoch is new**: the shared-live index from H01/spec 011 has no generation counter. H06 adds
  one — the plan MUST identify every corpus-mutation site (chunk store in pipeline, `DeleteDoc`,
  watcher swap-in, migrate) and bump the epoch there. Missing a site = stale results (the critical
  correctness risk; covered by an FR-002/FR-003 test).
- **In-process only, NOT persisted**: caches live in memory and are empty on restart. Persisting the
  result cache to Pebble is out of scope — that blurs into H16 (persistent index snapshot) and would
  persist a non-authoritative derived structure under the single-writer/durability model.
- **Exact-match only**: no semantic/neighbor/fuzzy query caching. Near-duplicate query detection is
  H20 (separate). The audit explicitly scopes H06 to exact-match ("covers the documented win without
  a second embedder").
- **`nocache` still stores**: the override bypasses serving from cache, but the freshly-computed
  result is still written (so the next normal query can hit). Plan may revisit (alternative:
  `nocache` is a full read-and-write bypass).
- **Result-cache key includes ContextWindow + Filter**: different windows/filters are different
  entries. Plan MAY optimize by caching the pre-context, pre-filter-expanded ranked list and applying
  filter/context post-hit, but the observable result must equal caching the full shape. (Simplest
  correct first cut: key on the full shape.)
- **Reranker model in key**: when rerank is on, the reranker-model identity is part of the
  result-cache key (a reranker swap otherwise returns stale order). When rerank is off, it isn't.
- **Embedding cache is query-side only**: document embeddings are content-addressed and embedded once
  (re-embedded only on Migrate/reprocess); they need no cache.
- **Transport exposure**: `nocache` on CLI (`--no-cache`), REST (`nocache`), gRPC (proto field,
  regen), MCP (schema) — same pattern as H08/H14/H15. The caches themselves are internal (not
  query-params), surfaced only via the override and `status`.
- **Capacity defaults**: modest (e.g., 256 result entries / 512 query-embedding entries) — enough to
  capture repeat traffic without threatening the < 50 MB idle / < 500 MB under-load memory budget
  (constitution). Tunable via config.
- **Eval-harness interaction**: `make test-eval` (quality) is unaffected (cached == identical). Any
  latency benchmarking MUST run cold (cache disabled/flushed) so timings reflect retrieval, not cache.
- **Out of scope**: persistent caching (H16), near-duplicate/semantic query caching (H20), request
  batching / streaming (H25), embedding drift monitoring / version-pinning (H11 — H06 flushes on
  Migrate but does not itself persist or monitor model versions), rate limiting.
