# Feature Specification: Cached Loaded Index (No Per-Query Rebuild)

**Feature Branch**: `011-index-cache` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "next backlog item" → resolved to **H01** from
`RAG_BOOK_AUDIT_BACKLOG.md` (Phase 2, the only item there; P0): *"Per-query full
index rebuild (`LoadIndex` every Query). Cache the loaded `(FTS, *Vector)` on the
Engine with a content-hash generation counter; invalidate on ingest/delete/
watcher. The single biggest latency win; also makes eval timing realistic."*
Source detail: `RAG_BOOK_AUDIT.md` §1.3 (P0) and §1.7 (P0, "Single biggest latency
win available").

**Problem (grounded in current code):** Every query rebuilds the entire in-memory
search index from disk — re-reading every chunk and every embedding, re-parsing
their JSON, and re-tokenizing — before it can search. On a corpus of thousands of
chunks this is the dominant cost of every single query, repeated identically for
back-to-back queries that see the exact same data. The work is pure waste: the
index only changes when something is ingested, deleted, migrated, or re-ingested
via the watcher. The fix is to build the index once and reuse it until the
underlying data actually changes.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Repeated queries are fast (no per-query rebuild) (Priority: P1) 🎯 MVP

A user — most often an AI agent or a long-running service issuing many queries
against the same vault — expects the second and later queries to be dramatically
faster than the first, because the index is already built and is reused. Today
every query pays the full rebuild cost, so query latency scales with corpus size
on every call; after this change it scales with corpus size once (the first
query), then stays flat.

**Why this priority**: This is H01's headline benefit — the audit calls it the
single biggest latency win available. It is also what makes the retrieval-quality
harness (spec 004) measure real retrieval cost rather than rebuild cost.

**Independent Test**: Against a fixed multi-thousand-chunk corpus, time the first
query and a subsequent identical query; assert the subsequent query is a small
fraction of the first (the first pays the build, the rest reuse it). Verify the
results are identical (the cache returns correct results, not just fast ones).

**Acceptance Scenarios**:

1. **Given** a populated vault, **When** the user issues the same query twice in
   a row, **Then** the second returns identical results and completes in a small
   fraction of the first's time.
2. **Given** a populated vault, **When** many different queries run back-to-back,
   **Then** each reuses the already-built index — latency stays flat across the
   sequence rather than re-climbing per query.
3. **Given** an empty vault, **When** the first query runs, **Then** it builds an
   empty index cheaply and subsequent queries reuse it.

---

### User Story 2 - The cached index stays correct (read-after-write) (Priority: P1)

A cache that serves stale results is worse than the slow rebuild it replaces, so
freshness is mandatory. After a document is ingested (and its embeddings finish
landing in the background), the next query MUST see it. After a document is
deleted, re-embedded via migrate, or changed on disk and picked up by the watcher,
the cache MUST reflect the change — never serving stale hits or phantom (deleted)
hits.

**Why this priority**: Correctness is non-negotiable and co-equal with US1 — a
fast stale cache is a regression, not a win. This story MUST ship with US1; the
cache is only valid if it is both fast and fresh.

**Independent Test**: Ingest a document, wait for its embeddings to complete,
then query for its content and assert it appears; delete it and assert the next
query no longer returns it; trigger a migrate and assert the new embeddings are
served. None of these may require a restart or a manual cache flush.

**Acceptance Scenarios**:

1. **Given** a document has just been ingested, **When** its background embedding
   finishes and the user queries for its content, **Then** the cached index
   reflects the new document — the query returns it (read-after-write).
2. **Given** a document has been deleted, **When** the user next queries, **Then**
   the deleted document's chunks no longer appear (no phantom hits).
3. **Given** the corpus has been re-embedded (migrate) or a watched file changed
   on disk, **When** the user next queries, **Then** the results reflect the new
   embedding/content state, not the pre-change state.
4. **Given** embeddings are still landing in the background (the async-after-ACK
   window), **When** a query runs, **Then** it never pins and serves a partially-
   indexed state as if it were complete — once embeddings land, the next query
   sees the complete index.

---

### User Story 3 - Safe under concurrent queries and concurrent writes (Priority: P2)

Multiple queries can run at once (the daemon serves several transports), and
writes can be happening in the background at the same time. The user must never
see an error, a crash, or a half-rebuilt index view — each query sees one
coherent, atomic snapshot of the index.

**Why this priority**: Durability under load. The single-writer/local model means
only one process mutates, but within that process queries and background ingest
interleave; the cached index must tolerate that without tearing.

**Independent Test**: Run many concurrent queries while documents are being
ingested in the background; assert no errors, no panics, and every query returns a
self-consistent result set.

**Acceptance Scenarios**:

1. **Given** many concurrent queries, **When** they run simultaneously, **Then**
   each returns a complete, consistent result set — no partial/missing hits, no
   crashes.
2. **Given** a query in progress while a background ingest updates the index,
   **When** both complete, **Then** neither corrupts the other; the in-flight
   query finishes against a coherent view and the next query sees the update.
3. **Given** two queries arrive while the cache needs rebuilding, **When** both
   request the index, **Then** the rebuild happens once, not twice (no thundering
   herd), and both reuse the single result.

---

### Edge Cases

- **First/cold query**: MUST pay the one-time build; this is expected and is the
  cost the cache amortizes. It must not be slower than today's per-query rebuild.
- **Empty corpus**: building an empty index is cheap; reuse it; no special-casing
  needed.
- **Async-after-ACK window**: a query that runs between the durable write ACK and
  the background embedding completion MUST NOT cache the pre-embedding (chunk-only)
  state as if complete. Invalidation is keyed to embedding *completion*, not ACK.
- **Rapid writes**: many documents ingested in quick succession must not thrash
  the cache (one rebuild per logical change, not one per write event).
- **Delete + immediate query**: the cache must reflect the deletion before serving
  (no phantom hits from the deleted doc).
- **Migrate (corpus-wide re-embed)**: invalidates fully; the next query rebuilds
  against the new embeddings.
- **Watcher file change**: a watched file modified on disk triggers reingest,
  which invalidates the cache.
- **One-shot CLI queries**: a fresh process per `go-rag query` invocation has a
  cold cache every time and cannot benefit from an in-process cache across
  invocations — that is inherent to the one-shot CLI and is **out of scope** (a
  persistent on-disk snapshot is a separate item). The win applies to the
  long-running daemon serving repeated queries.
- **Single-writer assumption**: go-rag enforces one writer process (Pebble lock).
  No cross-process cache coherence is required — each process owns its cache.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The search index MUST be built at most once per process per change,
  and reused across queries that see the same underlying data — no per-query full
  rebuild.
- **FR-002**: The cached index MUST be invalidated (rebuilt on next query, or kept
  incrementally current) whenever index-affecting data changes: document ingest
  (on embedding completion), document delete, migrate/re-embed, and watcher-
  detected file changes.
- **FR-003 (read-after-write)**: After a document's embeddings have landed, the
  next query MUST return that document (US2 acceptance 1). After a delete, the
  next query MUST NOT return the deleted document's chunks (US2 acceptance 2).
- **FR-004 (async-aware invalidation)**: Because embedding is async-after-ACK,
  invalidation MUST be tied to embedding/index *completion*, not to the durable
  write ACK — a query MUST never cache and serve a chunk-only (vector-less) state
  as a complete index.
- **FR-005**: Concurrent queries MUST each observe a coherent, atomic snapshot of
  the index — no partial rebuilds, no tearing, no crashes (US3 acceptance 1/2).
- **FR-006**: When the cache needs rebuilding, concurrent waiters MUST reuse a
  single rebuild rather than each triggering their own (no thundering herd) (US3
  acceptance 3).
- **FR-007**: The cache MUST NOT regress the write path — the durable write ACK
  budget stays intact; any cache update happens as part of the existing background
  indexing work, not on the ACK critical path.
- **FR-008**: Results returned from the cached index MUST be identical to what the
  current per-query rebuild produces for the same underlying data (correctness
  preservation — the cache changes latency, not results).

### Key Entities *(include if feature involves data)*

- **Index cache**: the in-memory search index (the full-text and vector indexes
  together), held by the long-running engine process and reused across queries
  until the underlying data changes.
- **Index generation**: a monotonic tag representing the current state of the
  indexed data. The cache is fresh iff its stored generation matches the current
  one; any index-affecting write advances the current generation (invalidating the
  cached snapshot). An in-process concept only.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a multi-thousand-chunk corpus, a repeated query returns in a
  small fraction of the first query's time (e.g., the 2nd and later queries
  complete in under 5% of the 1st query's time), with identical results —
  measurable end-to-end on the daemon path.
- **SC-002**: A document ingested (embeddings complete) is returned by the very
  next query; a document deleted is absent from the very next query — read-after-
  write holds with no restart or manual flush.
- **SC-003**: The retrieval-quality harness (spec 004) reports query timings that
  reflect retrieval cost rather than per-query rebuild cost — the harness becomes
  a realistic latency measurement.
- **SC-004**: Under concurrent queries with background ingest ongoing, no query
  errors, crashes, or inconsistent result sets occur.

## Assumptions

- **In-process, in-memory cache** scoped to one long-running engine process. The
  win applies to the daemon (MCP/REST/gRPC serving many queries). Cross-process
  or cold-start persistence (a snapshot on disk) is a separate item and out of
  scope here.
- **Single-writer process model** (Pebble lock enforces it): exactly one process
  mutates the indexed data, so no cross-process cache coherence is required. Each
  process owns its cache and rebuilds it once on cold start.
- **The cached index is read-only to queries** — retrieval (RRF fusion, rerank)
  reads the indexes; it does not mutate them. This is what makes a shared cached
  snapshot safe for concurrent readers.
- **Write path untouched** (Constitution Principle IV): the durable write ACK
  (<10 ms) is unchanged. Any cache refresh is part of the existing background
  indexing work (post-ACK), not the ACK critical path.
- **No new external surface**: this is an internal performance change. No CLI
  flag, no config key, no transport (REST/gRPC/MCP) change — query behavior and
  results are identical, only faster on repeat.
- **Whether the cache is a snapshot rebuilt on invalidation or the live
  incrementally-maintained index is a design decision for the plan**, not fixed
  here. The spec requires the *outcomes*: no per-query rebuild (FR-001), freshness
  (FR-002/003/004), concurrency safety (FR-005/006), and no write-path regression
  (FR-007). A design that keeps the cache incrementally consistent on writes is
  acceptable; one that rebuilds the whole index on every write is NOT, because it
  would move the per-query cliff onto the write path.
- **Out of scope**: a query/result cache (H06 — caching query answers, not the
  index), a persistent on-disk index snapshot (H16 — cold-start rebuild
  elimination), and the one-shot CLI query cold-start.
