# Feature Specification: Persistent FTS Index Snapshot (Fast Cold Start)

**Feature Branch**: `018-fts-snapshot` *(commits directly to `main` per project convention.)*

**Created**: 2026-06-23 · **Status**: Draft

**Input**: "next backlog item" → **H16** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 4, last open item):
*"No persistent index snapshot (cold-start full rebuild). Persist an FTS postings snapshot under a new
prefix (0x06); load-on-start + incremental watcher updates."* Source: `RAG_BOOK_AUDIT.md` §1.3 (P0:
"No persistent index — recovery is a full re-scan… BM25 postings + any graph live only in RAM and
vanish on restart… every cold start pays the full rebuild"). Book reference: ch006 §5.3 ("always back
up your index metadata separately").

**Problem:** go-rag's BM25 full-text index lives only in memory. Every **cold start** — a daemon boot,
or every one-shot `go-rag query` / `go-rag add` invocation (each spins up a fresh engine) — pays the
full `LoadIndex` cost: a scan of every stored chunk, **re-tokenizing all chunk text** to reconstruct the
inverted index, plus a reload of every embedding. On a large corpus that is a multi-second tax on the
fast path — the retrieval book's named anti-pattern ("streaming inserts, no rebuilds"). H01/spec 011
already eliminated the *per-query* rebuild (the index is seeded once per engine and reused); H16 attacks
the remaining *per-cold-start* rebuild by **persisting the built FTS postings** and loading the snapshot
on start instead of re-deriving it.

The chunks remain the authoritative source (idempotent SHA-256 content addressing — the audit notes the
absent persistent index is a **latency** cost, not a data-loss risk), so the snapshot is a derived
cache: if it is absent, stale, or corrupt, the system rebuilds from chunks exactly as it does today and
writes a fresh snapshot. The fix is pure performance + the book's "back up your index metadata" hygiene,
with no change to results.

## Clarifications

### Session 2026-06-23

- Q: What should the persistent snapshot cover — FTS postings only, FTS + vectors, or FTS-now/vectors-
  later? → **A: FTS only.** The snapshot persists the BM25 FTS postings (the audit's literal H16 fix and
  the dominant, CPU-bound cold-start cost — re-tokenizing all chunk text). The vector-map reload is pure
  JSON unmarshal with no recomputation (vectors already content-addressed/dedup'd), so it stays as-is;
  persisting it (the audit's unused Vector Save/Load hooks) is **out of scope** for H16, not merely the
  default. Rationale: tighter scope, lower risk on the currency/staleness mechanism, and the FTS is the
  cost worth paying down first.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Cold start loads the snapshot, not a full rebuild (Priority: P1) 🎯 MVP

A one-shot CLI query (or a daemon boot) opens the database and needs the FTS ready. Instead of
re-tokenizing every chunk, it loads the persisted FTS postings snapshot — a fast deserialize — and is
ready to query. The result is byte-identical to a cold rebuild; only the startup time drops.

**Why this priority**: This is H16's headline value — the cold-start latency win the book calls out. It
turns every one-shot CLI command from O(corpus-text) re-tokenization into a snapshot load, and speeds
daemon boot. Transparent (identical results).

**Independent Test**: Ingest a corpus, let a snapshot be written, then time a cold query; assert it
returns the **same** hits as a forced-rebuild query and is materially faster (snapshot load ≪ rebuild).

**Acceptance Scenarios**:

1. **Given** a corpus with a written FTS snapshot, **When** a fresh engine cold-starts and queries,
   **Then** the FTS is loaded from the snapshot (not rebuilt), and the keyword results are identical to
   a rebuild.
2. **Given** a cold start with a snapshot, **When** a keyword query runs, **Then** it returns the same
   hits (chunk IDs + order) as a cold start with the snapshot disabled (forced rebuild) — transparency.
3. **Given** an empty corpus (no chunks, no snapshot), **When** a cold start + query runs, **Then** it
   returns no hits with no error (the snapshot is a no-op on an empty index).

---

### User Story 2 - The snapshot stays current with the corpus (Priority: P1)

Ingest, delete, and watcher changes that mutate the live FTS also keep the persisted snapshot
consistent, so a subsequent cold start loads a snapshot that reflects the current corpus — never one
missing a just-added chunk or retaining a deleted one.

**Why this priority**: Correctness — a snapshot that goes stale silently serves wrong keyword results on
the next cold start. The currency mechanism is the highest-risk item in the feature.

**Independent Test**: Ingest doc A (snapshot written); cold-start and query → A is found. Delete A; the
snapshot is updated; cold-start and query → A is gone. Ingest doc B; cold-start and query → B is found,
A stays gone.

**Acceptance Scenarios**:

1. **Given** a snapshot and a corpus, **When** a document is ingested (chunk added), **Then** the
   snapshot is updated so a subsequent cold start finds the new chunk's keywords.
2. **Given** a snapshot and a corpus, **When** a document is deleted (chunks removed), **Then** the
   snapshot is updated so a subsequent cold start no longer returns the deleted chunks.
3. **Given** a snapshot, **When** a watcher change (add/modify/delete) is applied, **Then** the snapshot
   reflects the change before the next cold start.

---

### User Story 3 - A stale or missing snapshot is detected and rebuilt (never wrong) (Priority: P1)

If the snapshot is absent (a pre-H16 vault, or a fresh database), corrupt, or stale (e.g. the process
was killed before the snapshot was updated, or chunks changed out-of-band), the system detects this,
rebuilds the FTS from chunks exactly as it does today, and writes a fresh snapshot — so results are
**always** correct, and the performance benefit self-heals.

**Why this priority**: Safety net for the currency mechanism. An unclean shutdown or any drift between
the snapshot and the chunks must never produce wrong results; it must fall back to the rebuild and
re-establish the snapshot.

**Independent Test**: Write a snapshot, then delete/corrupt it or add a chunk out-of-band without
updating it; cold-start and query → results match a forced rebuild (the staleness was detected and the
FTS rebuilt), and a fresh snapshot is written.

**Acceptance Scenarios**:

1. **Given** no snapshot (pre-H16 vault), **When** a cold start runs, **Then** the FTS is rebuilt from
   chunks (today's behavior) and a snapshot is written for the next start.
2. **Given** a corrupt/unparseable snapshot, **When** a cold start runs, **Then** it is ignored, the FTS
   is rebuilt, and a fresh snapshot overwrites it.
3. **Given** a snapshot whose staleness marker no longer matches the current chunks, **When** a cold
   start runs, **Then** the FTS is rebuilt and the snapshot rewritten (no stale/missing hits).
4. **Given** a detected rebuild, **When** the next cold start runs, **Then** it loads the freshly-written
   snapshot (the benefit self-heals).

---

### User Story 4 - No quality regression + bounded footprint (Priority: P2)

The snapshot is a performance optimization only: retrieval quality is unchanged (the H02 eval harness
confirms), and the snapshot's disk + memory footprint stays modest and within budget. The one-shot CLI
and daemon both benefit.

**Why this priority**: Confidence + hygiene — the optimization must not change results or balloon
storage.

**Independent Test**: `make test-eval` recall@10 unchanged; the snapshot file is bounded relative to the
corpus; a one-shot CLI query is faster than before on a non-trivial corpus.

**Acceptance Scenarios**:

1. **Given** the H02 eval harness, **When** run with the snapshot enabled, **Then** recall@10 is
   unchanged from baseline.
2. **Given** a corpus, **When** the snapshot is written, **Then** its footprint is bounded (comparable to
   the chunk data, not an unbounded multiple).
3. **Given** a one-shot CLI query against a non-trivial corpus, **When** timed, **Then** it is faster
   than the pre-H16 rebuild path (snapshot load).

---

### Edge Cases

- **Unclean shutdown (SIGKILL)**: a snapshot updated in memory but not yet flushed may be stale on the
  next start — the staleness marker MUST catch this and rebuild (US3). The snapshot is never a source of
  wrong results.
- **Concurrent writer + cold reader**: a fresh process cold-starting while another writes — Pebble is
  single-writer (LOCK-guarded), so a concurrent open is refused; no snapshot read/write race beyond the
  DB's own guarantees.
- **Snapshot vs the H01 in-memory index**: the snapshot is what a cold start *loads*; H01's shared
  in-memory index (seeded once per engine) is then reused across queries as today. H16 changes the seed
  source (snapshot vs rebuild), not the per-engine reuse.
- **Snapshot vs the H06 query cache**: orthogonal. The H06 epoch invalidates query *results*; the FTS
  snapshot is invalidated/rebuilt by the staleness marker. A cold start loads the snapshot, then the
  query cache starts empty (H06, in-process).
- **Vector map reload**: H16 scopes the snapshot to the **FTS postings** (the dominant cold-start cost:
  re-tokenization). The vector-map reload (JSON unmarshal, no recomputation) stays as-is; persisting it
  too is a follow-up, not this item.
- **Mixed/corrupt records**: a chunk record that fails to unmarshal is skipped in a rebuild (today's
  behavior); the snapshot is built from the valid chunks only.
- **Watcher rapid changes**: a burst of watcher add/delete must not write a snapshot per change at
  O(snapshot) each (perf cliff); the currency mechanism batches/debounces or flushes on a checkpoint.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST persist a snapshot of the built BM25 FTS postings to disk (a new key space
  reserved for the FTS index), suitable for deserializing back into the in-memory FTS on start.
- **FR-002**: On cold start (a fresh engine seeding its index), the system MUST load the FTS from the
  persisted snapshot when the snapshot is present and valid, instead of re-tokenizing all chunks.
- **FR-003**: The system MUST keep the snapshot consistent with the corpus: chunk add (ingest) and chunk
  delete (delete/watcher) MUST be reflected in the persisted snapshot so a subsequent cold start sees
  the current FTS.
- **FR-004**: The system MUST detect a stale or missing snapshot via a checkable staleness marker and
  rebuild the FTS from chunks (today's path) in that case, then write a fresh snapshot — never serving
  wrong results from a stale snapshot.
- **FR-005**: A corrupt or unparseable snapshot MUST be ignored (treated as missing) — rebuild +
  overwrite, no error to the caller.
- **FR-006**: A pre-H16 vault (no snapshot) MUST cold-start via the rebuild and write a snapshot for the
  next start — no re-ingestion, no operator action.
- **FR-007**: Loading from the snapshot MUST produce keyword results byte-identical to a forced rebuild
  (transparency) — the snapshot is a pure performance optimization.
- **FR-008**: The snapshot's staleness check MUST be checkable without a full chunk scan (so the cold
  start keeps its speed advantage) — e.g., a persisted marker bumped on every chunk mutation, compared
  in O(1).
- **FR-009**: The currency mechanism MUST NOT write a full snapshot per individual chunk mutation during
  bulk ingest (which would be O(snapshot) per chunk); it MUST batch, debounce, or checkpoint so bulk
  ingest stays fast.
- **FR-010**: With the snapshot enabled, the H02 eval harness MUST show no retrieval-quality regression
  (recall@10 unchanged).

### Key Entities

- **FTS snapshot**: a persisted serialization of the BM25 inverted index (the postings), loadable back
  into the in-memory FTS. Stored under the reserved FTS key space. Non-authoritative (derived from
  chunks); rebuildable on demand.
- **Staleness marker**: a persisted, O(1)-checkable value (e.g., a chunk-version counter bumped on
  every add/delete) recorded with the snapshot; a mismatch with the current marker on load ⇒ the
  snapshot is stale ⇒ rebuild. Enables correctness without a full scan.
- **Cold-start seed path**: the LoadIndex decision tree — load-snapshot-if-valid, else rebuild-from-
  chunks-and-write-snapshot.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A cold-start keyword query with a valid snapshot returns byte-identical results to a
  forced-rebuild query (transparency, FR-007).
- **SC-002**: A cold start with a snapshot is materially faster than the rebuild path on a non-trivial
  corpus — measurable as the snapshot-load time being a small fraction of the re-tokenization time
  (e.g., ≥5× faster on a multi-thousand-chunk corpus).
- **SC-003**: After an ingest or delete, a subsequent cold start reflects the change (new chunks found;
  deleted chunks gone) — the snapshot stayed current (FR-003).
- **SC-004**: After deleting/corrupting the snapshot or changing chunks out-of-band, a cold start
  returns correct results (matching a rebuild) and writes a fresh snapshot (FR-004/FR-005).
- **SC-005**: A pre-H16 vault cold-starts correctly and writes a snapshot for the next start (FR-006).
- **SC-006**: `make test-eval` recall@10 is unchanged with the snapshot enabled (FR-010).
- **SC-007**: Bulk ingest of a large document does not regress wall-time versus today (the currency
  mechanism does not write a snapshot per chunk — FR-009).

## Assumptions

- **Scope = FTS postings snapshot (locked — clarification 2026-06-23).** The dominant cold-start cost is
  BM25 re-tokenization; that is what H16 persists. The vector-map reload (JSON unmarshal, no
  recomputation) stays as-is — persisting it too (the audit notes unused Vector Save/Load hooks) is
  **out of scope** for H16, not merely the default. It can be revisited as a follow-on if profiling
  later shows the vector JSON reload is the bottleneck.
- **Key space**: the snapshot lives under the reserved FTS range (`0x05`–`0x08` in `storage.go`; the
  audit names `0x06`). Plan picks the exact prefix; no collision with existing data (0x01–0x04, 0x09–
  0x10).
- **Currency strategy** (plan decision; default = checkpoint-on-close + a persisted staleness marker):
  the snapshot is written/refreshed on a checkpoint (engine close/drain, and/or a debounced write
  during sustained ingest), and a staleness marker (a chunk-version counter bumped on every add/delete)
  gates its use on load. An unclean shutdown leaves a stale marker ⇒ next cold start rebuilds (correct,
  self-healing). Alternative — per-mutation incremental — rejected for bulk ingest (O(snapshot) per
  chunk); a debounced variant is acceptable. Plan confirms.
- **Staleness marker = a persisted chunk-version counter** (bumped on every chunk add/delete), stored
  with the snapshot and checkable in O(1) on load (one Get). A mismatch ⇒ rebuild. This keeps the cold
  start fast (no full chunk scan to validate). Plan may choose a stronger marker (e.g., a hash of chunk
  IDs) if the counter proves insufficient, at a cost.
- **Snapshot format**: a serialization the in-memory FTS can `Save`/`Load` (the FTS gains these; the
  Vector type already has unused Save/Load hooks per the audit). One snapshot blob per vault (sharding
  is YAGNI at local <10K-chunk scale).
- **Transparency**: the snapshot changes ONLY cold-start latency, never results — a forced-rebuild path
  is retained (config toggle or internal escape) so transparency is testable.
- **Interaction with H01/H06**: the snapshot is the cold-start *seed source*; H01's per-engine reuse
  and H06's query-result cache are unchanged. The snapshot's staleness marker is independent of H06's
  in-memory epoch (which resets each run).
- **No new dependencies**: stdlib + existing `internal/index`, `internal/storage`, `internal/pipeline`.
  Pure Go (Principle III).
- **Out of scope**: vector-map persistence (follow-on), HNSW / `Index` interface extraction (H27),
  sharded snapshots, online index compaction, and any change to retrieval results.
