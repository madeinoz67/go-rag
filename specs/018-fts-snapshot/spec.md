# Feature Specification: Pebble-backed Async FTS Index (Cold-Start-Free Keyword Search)

**Feature Branch**: `018-fts-snapshot` *(commits directly to `main` per project convention. Renamed in spirit from "FTS snapshot" — the design pivoted; see Clarifications.)*

**Created**: 2026-06-23 · **Status**: Draft (pivoted 2026-06-23)

**Input**: "next backlog item" → **H16** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 4, last open item):
*"No persistent index snapshot (cold-start full rebuild). Persist an FTS postings snapshot under a new prefix (0x06); load-on-start + incremental watcher updates."*
Source: `RAG_BOOK_AUDIT.md` §1.3 (P0: "No persistent index — recovery is a full re-scan… every cold start pays the full rebuild").

**Problem:** go-rag's BM25 full-text index lives **only in memory** (`internal/index/fts.go` — a `map[string]map[string]float64`). Every **cold start** — a daemon boot, or every one-shot `go-rag query` / `add` (each spins up a fresh engine) — pays the full `LoadIndex` cost: a scan of every chunk **re-tokenizing all chunk text** to rebuild the map. On a large corpus that is a multi-second tax on the fast path. Worse, the FTS is indexed **synchronously on the ACK path** (`storeDocument`, H01/spec 011), which bends Principle IV (BM25 indexing should be async post-ACK).

**The fix (pivoted design):** make the FTS a **durable, Pebble-backed inverted index** — postings stored as keys under the reserved `0x05` prefix, queried in place via per-term prefix scans, and indexed **asynchronously** (post-ACK, alongside the vector embeddings). This eliminates the cold-start rebuild *entirely* (the index *is* the durable key space — nothing to rebuild), removes the in-memory posting map, moves BM25 indexing off the ACK path (Principle IV compliance), and **deletes the entire class of problem** a snapshot would only patch (no snapshot blob, no staleness marker, no checkpoint-on-close, no size/gob/int-key sizing). The design is patterned on MuninnDB's proven Pebble-backed FTS (`scrypster/muninndb`), and was de-risked by benchmark: a Pebble prefix-scan BM25 query measures **~0.3 ms worst-case** vs ~0.24 ms in-memory (both ~170× under the 50 ms keyword budget), and the durable store is **6.7 MB** for ~2.9 K chunks (LSM-compressed — smaller than the 10.5 MB snapshot blob it replaces).

## Clarifications

### Session 2026-06-23

- Q: What should the persistent snapshot cover — FTS only, FTS+vectors, or FTS-now/vectors-later? → **A: FTS only.** The vector-map reload is pure JSON unmarshal (no recomputation); persisting it is out of scope. *(Superseded by the pivot below — "FTS only" still holds; the vector map stays as-is.)*
- **Pivot (2026-06-23):** Researching how MuninnDB solves the same problem revealed that **snapshotting is the wrong frame** — it patches an in-memory index that shouldn't be the source of truth. MuninnDB persists FTS postings **incrementally as Pebble keys** and queries them in place (no in-memory map, no rebuild, no snapshot). Adopting that design in go-rag **eliminates H16's snapshot/marker/size machinery entirely** and aligns with Principle IV (BM25 indexing async post-ACK — which the current sync-FTS bends). A benchmark confirmed query latency is unchanged (sub-ms) and the durable store is smaller than the snapshot blob. **Decision: pivot the feature from "persist a snapshot of the in-memory FTS" to "make the FTS a durable Pebble-backed inverted index, indexed async."** The H16 backlog item is satisfied more thoroughly (no cold-start rebuild *at all*, not just a cached one).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Cold start has no FTS rebuild (the index is on disk) (Priority: P1) 🎯 MVP

A one-shot CLI query (or daemon boot) opens the database and the FTS is **already there** — it's the durable Pebble key space under `0x05`, not an in-memory map to reconstruct. There is no snapshot to load and no re-tokenization; the keyword query runs prefix scans over Pebble and returns results immediately.

**Why this priority**: H16's headline value — eliminating the cold-start rebuild. The FTS becomes durable-first, so cold start is O(query) not O(corpus). Transparent (identical results).

**Independent Test**: Ingest a corpus; cold-start a fresh engine and run a keyword query; assert it returns the **same** hits as before restart, with no re-tokenization step (no `LoadIndex` FTS rebuild), in ≪ the pre-pivot rebuild time.

**Acceptance Scenarios**:

1. **Given** a corpus indexed into the Pebble-backed FTS, **When** a fresh engine cold-starts and runs a keyword query, **Then** results return without any FTS rebuild (the postings are read from Pebble).
2. **Given** a cold start with the Pebble-backed FTS, **When** a keyword query runs, **Then** it returns the same hits (chunk IDs + BM25 order) as a query before restart — transparency.
3. **Given** an empty corpus, **When** a keyword query runs, **Then** no hits, no error.

---

### User Story 2 - FTS indexing is async, off the ACK path (Priority: P1)

Chunk ingest ACKs after only the durable chunk write; the FTS postings are written **asynchronously** (post-ACK, by the same background path that embeds vectors). This moves BM25 indexing off the ACK path — bringing go-rag into compliance with Principle IV — and makes keyword search **eventually consistent**, symmetric with vector search (which is already async).

**Why this priority**: Constitution compliance (Principle IV: BM25 indexing MUST be async post-ACK) + ACK-path latency. The current sync-FTS-in-`storeDocument` is the deviation this corrects.

**Independent Test**: Ingest a chunk; immediately query (before async drain) — the chunk may not yet be keyword-visible; after the async worker drains (`waitEmbedded`), it is. Assert ACK time is unaffected by FTS work (it's only the chunk write).

**Acceptance Scenarios**:

1. **Given** an ingest, **When** the ACK returns, **Then** the FTS posting write has NOT happened yet on the ACK path (it's queued async) — the ACK carries only the durable chunk write.
2. **Given** an ingest whose async FTS worker has drained, **When** a keyword query runs, **Then** the new chunk is findable.
3. **Given** concurrent ingests, **When** FTS postings are written async, **Then** no ACK latency regression vs the pre-pivot path (Principle IV < 10 ms ACK intact).

---

### User Story 3 - Postings stay current + survive restart (no marker, no snapshot) (Priority: P1)

Every chunk add writes its postings (atomic Pebble batch); every delete removes them. The FTS is therefore **always current** and **durable** — there is no separate snapshot to keep consistent and no staleness marker to check. A crash may lose the in-flight async batch (bounded, like the vector path), and on restart the durable postings are exactly what's served.

**Why this priority**: Correctness + the simplicity win. This is where the pivot pays off — the snapshot/marker/checkpoint/crash-ordering machinery from the original H16 design simply doesn't exist.

**Independent Test**: Ingest doc A (postings written); restart; query → A found. Delete A (postings removed); restart; query → A gone. Ingest doc B; restart; query → B found, A still gone.

**Acceptance Scenarios**:

1. **Given** postings on disk, **When** a chunk is ingested (async), **Then** its postings are written so a subsequent cold start finds its keywords.
2. **Given** postings on disk, **When** a chunk is deleted, **Then** its postings are removed so a subsequent cold start no longer returns it.
3. **Given** a crash mid-async-batch, **When** the daemon restarts, **Then** the durable postings are served (the lost in-flight batch is the same ≤async-window eventual-consistency the vector path already has — no wrong results from a stale snapshot, because there is no snapshot).

---

### User Story 4 - Backward compat + no quality regression (Priority: P2)

A pre-pivot vault (in-memory FTS, no Pebble postings) is upgraded: on first start it builds the Pebble postings from existing chunks (a one-time migration), then runs purely off Pebble. Retrieval quality is unchanged (the H02 eval harness confirms). The vector map reload is unchanged (FTS-only scope).

**Why this priority**: Upgrade safety + confidence. Existing vaults must migrate without re-ingestion, and the pivot must not change results.

**Independent Test**: Open a pre-pivot vault; assert the Pebble postings are backfilled on first start (no re-ingest); `make test-eval` recall@10 unchanged.

**Acceptance Scenarios**:

1. **Given** a pre-pivot vault (chunks present, no `0x05` postings), **When** the daemon first starts, **Then** the Pebble postings are built from existing chunks (one-time migration) and subsequent starts use them directly.
2. **Given** the H02 eval harness, **When** run with the Pebble-backed FTS, **Then** recall@10 is unchanged.
3. **Given** the Pebble-backed FTS, **When** a hybrid query runs, **Then** the FTS contribution is identical (BM25 over the same postings) — only the backing changed, not the math.

---

### Edge Cases

- **Async visibility window**: a keyword query in the brief window between ACK and the async posting write won't see a brand-new chunk — the same eventual-consistency window vector search already has. `waitEmbedded` (which drains async workers) covers it for tests/operator flows.
- **Dropped async job (queue pressure)**: if the FTS worker queue overflows, a job may be dropped (logged); the chunk is durable, only keyword visibility is delayed until the next ingest of overlapping terms or a reindex. (Pattern from MuninnDB's worker.)
- **Concurrent writer + cold reader**: Pebble is single-writer (LOCK-guarded); a cold open during a write is refused — no posting read/write race beyond Pebble's guarantees.
- **Delete of a chunk whose postings were partially written**: delete is idempotent over Pebble (deleting absent keys is a no-op) — re-deleting or deleting before the async add completes is safe.
- **BM25 stats (N, avgdl, DF)**: stored as Pebble keys alongside postings; updated in the same atomic batch as the postings (no separate stat drift). IDF computed on read (cached in memory, like MuninnDB).
- **Interaction with H01 (shared in-memory index)**: the in-memory `*FTS` becomes a **thin Pebble-backed adapter** (no posting map; holds the `*pebble.DB` + an IDF cache). H01's "seed once, reuse across queries" still holds — the adapter is cheap to construct (no postings to load).
- **Interaction with H06 (query cache)**: the cache epoch still bumps on FTS mutation (now from the async worker). The cache is invalidated as today; the FTS backing change is transparent to it.
- **Vector map reload**: unchanged — `LoadIndex` still reloads vectors from `0x04` (the only in-memory index that remains). Persisting vectors too is out of scope (FTS-only).
- **Per-query cost on very long posting lists**: benchmarked at ~0.3 ms worst-case on ~2.9 K chunks; an in-memory LRU of hot posting lists (MuninnDB's IDF-cache pattern) is the escape hatch if a future huge-corpus term ever approaches budget.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The FTS MUST be a Pebble-backed inverted index: postings stored as keys under the reserved `0x05` prefix (`0x05 | term | sep | field | chunkID`), with per-posting values carrying tf + field + doc-len.
- **FR-002**: A keyword query MUST execute as per-term Pebble prefix scans that decode postings and accumulate BM25 — NOT via an in-memory posting map. BM25 math (k1, b, field weights, IDF) MUST be unchanged from the current FTS.
- **FR-003**: FTS postings MUST be written **asynchronously**, off the ACK path (post-ACK, by the background worker that also embeds vectors), so the ACK carries only the durable chunk write (Principle IV).
- **FR-004**: Every chunk add MUST write its postings (atomic Pebble batch incl. posting keys + document-frequency stats); every chunk delete MUST remove them — the FTS stays current with no separate snapshot or staleness marker.
- **FR-005**: Cold start MUST NOT rebuild the FTS — `LoadIndex` loads only the vector map; the FTS is queried in place from Pebble.
- **FR-006**: A pre-pivot vault (no `0x05` postings) MUST be migrated on first start: postings built from existing chunks (one-time, no re-ingestion), then operated purely off Pebble.
- **FR-007**: BM25 global stats (chunk count, avg doc-len) and per-term document-frequency MUST be stored as Pebble keys and updated in the same atomic batch as the postings (no stat drift).
- **FR-008**: Loading/serving the Pebble-backed FTS MUST produce keyword results byte-identical to the pre-pivot in-memory FTS (transparency) — the pivot changes only the backing, not the ranking.
- **FR-009**: A delete MUST be idempotent over Pebble (deleting absent postings is a no-op) and MUST NOT require the original chunk text (postings are removed by key prefix).
- **FR-010**: With the Pebble-backed FTS, the H02 eval harness MUST show no retrieval-quality regression (recall@10 unchanged), and a keyword query MUST stay well inside the 50 ms budget (benchmarked ~0.3 ms worst-case on ~2.9 K chunks).

### Key Entities

- **Pebble-backed FTS**: an inverted index whose postings live as Pebble keys under `0x05` (term → field → chunkID → tf/doclen value), queried via prefix scans. The in-memory `*FTS` becomes a thin adapter (`*pebble.DB` + an IDF/stats cache) — no posting map.
- **Posting key**: `0x05 | term | 0x00 | field | chunkID(16)`, value = tf(float32) + field(uint8) + docLen(uint16) (mirrors MuninnDB's 7-byte posting).
- **FTS stats keys**: chunk count + avg doc-len (under `0x05`) and per-term document-frequency (under `0x09`, the term-stats prefix MuninnDB uses) — updated atomically with postings.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A cold-start keyword query with the Pebble-backed FTS returns byte-identical results to the pre-pivot in-memory FTS (transparency, FR-008).
- **SC-002**: A cold start performs **no FTS rebuild** (verifiable: no chunk re-tokenization; `LoadIndex` loads only vectors) and is dramatically faster than the pre-pivot rebuild on a non-trivial corpus.
- **SC-003**: After an ingest or delete + async drain, a subsequent cold start reflects the change (FR-004).
- **SC-004**: A pre-pivot vault migrates its postings on first start without re-ingestion (FR-006).
- **SC-005**: `make test-eval` recall@10 is unchanged (FR-010).
- **SC-006**: A keyword query stays inside the 50 ms budget on a non-trivial corpus (benchmarked ~0.3 ms worst-case at ~2.9 K chunks; SC target: < 5 ms p99 to leave wide headroom, FR-010).
- **SC-007**: Ingest ACK latency is unchanged vs pre-pivot (the FTS work moved off the ACK path — FR-003/Principle IV).

## Assumptions

- **Patterned on MuninnDB** (`scrypster/muninndb` `internal/index/fts/{fts,worker}.go` + `internal/storage/wal_syncer.go`), read in full 2026-06-23 — Pebble postings, async worker, prefix-scan query. go-rag adopts the posting shape + the async model, not MuninnDB's WAL-syncer (go-rag's durability is the existing Pebble Sync on chunk writes + async-after-ACK).
- **Posting key shape**: `0x05 | term | 0x00 | field | chunkID[16]`, 7-byte value (tf float32 + field uint8 + docLen uint16). `0x05` is within the range `storage.go` reserves for the BM25 FTS — collision-free.
- **FTS-only scope** (locked clarification): the vector-map reload in `LoadIndex` is unchanged. Persisting vectors is out of scope.
- **Async home**: FTS posting writes happen in the existing background `processJob` (which already embeds vectors async post-ACK) — NOT a new worker pool initially. A dedicated bounded worker (MuninnDB's pattern, with drop-on-overflow) is a plan-level option if back-pressure becomes a concern; default = reuse `processJob`.
- **Durability of postings**: written with the same Pebble sync policy as the async vector writes (eventual; bounded loss on crash = the async window, identical to vectors today). No new WAL-syncer.
- **Behavior change (explicit)**: keyword search becomes **eventually consistent** (async), symmetric with vector search. The H01 "keyword visible immediately after ACK" guarantee becomes "keyword visible after the async worker drains" — the same window vectors have. Tests using `waitEmbedded` are unaffected.
- **Migration**: first-start backfill scans chunks once to write `0x05` postings + stats; gated by a "postings present?" check (e.g., the FTS-stats key exists) so it runs once. No re-ingestion.
- **Transparency escape**: an internal test path forces the old in-memory rebuild for diffing against the Pebble path (not user-facing), so FR-008 is verifiable.
- **Out of scope**: vector-map persistence, HNSW/`Index`-interface extraction (H27), a dedicated FTS worker pool (unless back-pressure warrants), online index compaction, and any change to BM25 ranking.

## Notes

- **Why this replaced the snapshot design**: a snapshot caches an in-memory index that shouldn't be the source of truth; the Pebble-backed FTS makes the index durable-first and *deletes* the snapshot/marker/size/crash-ordering problem class instead of mitigating it. Benchmark (2026-06-23): Pebble prefix-scan query ~0.3 ms worst-case (vs ~0.24 ms in-memory); durable store 6.7 MB for ~2.9 K chunks (smaller than the 10.5 MB snapshot blob). Constitution Principle IV passes by the letter (BM25 indexing async post-ACK).
- The H16 backlog item ("no persistent index — cold-start full rebuild") is satisfied: there is no cold-start rebuild because the index *is* persistent.
