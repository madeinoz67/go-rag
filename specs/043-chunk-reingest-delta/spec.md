# Feature Specification: Chunk Change Deltas on Re-Ingest (RE_INGESTED)

**Feature Branch**: `043-chunk-reingest-delta` *(single-author repo — work commits to `main`; slug identifies the spec)*
**Created**: 2026-07-02
**Status**: Draft
**Input**: "look at BL-010" — [BL-010](../../docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md#bl-010) (chunk delta in `RE_INGESTED` events). Technical design: [`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md) (B-simple, red-team-validated).

## User Scenarios & Testing

### User Story 1 — Incremental update on re-ingest (Priority: P1) 🎯 MVP

A document is re-ingested after an edit. Today the `WatchDocuments` stream surfaces this as an `INGESTED(new)` + `DELETED(old)` pair of two *different* document IDs, forcing a consumer (e.g. the MuninnDB bridge) to re-process the **entire** document — re-promoting every chunk. The consumer should instead receive a single `RE_INGESTED` event carrying a per-chunk **delta**: which chunks are `ADDED`, `REMOVED`, or `UNCHANGED` (byte-identical text), plus the old→new chunk-ID map. The consumer then updates incrementally — promote `ADDED`, patch the stored references of `UNCHANGED`, tag `REMOVED` as superseded.

**Why this priority**: the headline value. Without deltas, every edit triggers full re-processing of all chunks; the delta makes the consumer's cost proportional to the *edit*, not the *document*.

**Independent Test**: re-ingest a document after a localized edit; assert a `RE_INGESTED` event arrives carrying the `ADDED`/`REMOVED`/`UNCHANGED` delta + the old→new chunk-ID map; assert `UNCHANGED` chunks' text is byte-identical across versions.

**Acceptance Scenarios**:
1. **Given** an ingested document, **When** a single paragraph is edited and the document re-ingested, **Then** a `RE_INGESTED` event is emitted with the edited chunk marked `ADDED`, the unchanged-text chunks marked `UNCHANGED`, and any removed chunks marked `REMOVED`.
2. **Given** a `RE_INGESTED` event, a consumer can resolve a stored reference to an `UNCHANGED` chunk's *old* ID to its *new* ID via the event's old→new chunk-ID map (no orphaned references).
3. **Given** a re-ingested document, the consumer observes **one** `RE_INGESTED` event — not a separate `INGESTED` + `DELETED` pair (no double-counting).

### User Story 2 — Skip redundant embedding for unchanged chunks (Priority: P2)

When the embedding configuration (model / dimension / convention) has **not** changed since a chunk was last embedded, re-ingesting a document should **not** re-run embedding generation for chunks whose text is `UNCHANGED` — the existing vector is still current. Only `ADDED` chunks (and `UNCHANGED` chunks when the embedding config **has** changed) incur embedding generation.

**Why this priority**: embedding generation is the dominant per-chunk cost; skipping it for unchanged text is the mechanism behind the write-reduction target.

**Independent Test**: re-ingest a document with an unchanged embedding config; assert `UNCHANGED` chunks are not re-embedded while `ADDED` chunks are.

**Acceptance Scenarios**:
1. **Given** an unchanged embedding config, **When** a document with mostly-unchanged text is re-ingested, **Then** only `ADDED` chunks trigger embedding generation; `UNCHANGED` chunks reuse their existing vectors.
2. **Given** the embedding model changed (a config drift), **When** any document is re-ingested, **Then** all its chunks (including `UNCHANGED`) are re-embedded — stale vectors are never reused.

### User Story 3 — Correct delta under repeated / moved text (Priority: P3)

A document may contain repeated text (boilerplate, signatures) or move a paragraph between positions. The delta must be computed by **content identity** (the chunk's text), not by position: a moved paragraph is `UNCHANGED`, and a paragraph repeated N times that's now repeated M times yields `min(N,M)` `UNCHANGED` + `|N−M|` `ADDED`/`REMOVED`.

**Why this priority**: correctness of the delta under real-world document structures; a naive positional diff would mislabel moved/repeated chunks and erode the saving.

**Independent Test**: ingest a doc with a repeated paragraph + a moved paragraph; edit it (change the repeat count + move the paragraph); assert the delta reports the correct `UNCHANGED`/`ADDED`/`REMOVED` counts.

**Acceptance Scenarios**:
1. **Given** a paragraph repeated 3× that's edited to repeat 2×, the delta reports 2 `UNCHANGED` + 1 `REMOVED` (not "all unchanged because the text is present").
2. **Given** a paragraph moved from position 2 to position 5 with no text change, the delta reports it as `UNCHANGED`.

### Edge Cases

- A document re-ingested with **no textual change** (a metadata-only edit) → all chunks `UNCHANGED`; the event still fires (the document version changed) but the consumer can no-op.
- A source file **deleted** then a *different* file created at the same path → treated as a new document (`INGESTED`), not `RE_INGESTED`.
- **Concurrent** re-ingests of the same document → the delta is computed against the most-recently-committed prior version; a race yields the conservative outcome (treat as `ADDED` rather than miss a change).

## Requirements

### Functional Requirements

- **FR-001**: The system MUST emit a `RE_INGESTED` document lifecycle event (on the existing `WatchDocuments` stream) when a document is re-ingested — i.e. a source path that already maps to a committed document is re-ingested with changed content.
- **FR-002**: The `RE_INGESTED` event MUST carry a per-chunk delta classifying every chunk of the new version as `ADDED`, `REMOVED`, or `UNCHANGED` relative to the prior version.
- **FR-003**: The delta MUST be computed by **chunk content identity** (the chunk's text), not by position — identical text at a different position is `UNCHANGED`.
- **FR-004**: For `UNCHANGED` and `REMOVED` chunks, the event MUST carry the **prior chunk ID**, enabling consumers to remap stored references (an old→new chunk-ID map).
- **FR-005**: `RE_INGESTED` MUST **replace** (not accompany) the `INGESTED(new)` + `DELETED(old)` pair a re-ingest currently surfaces, so consumers do not double-count.
- **FR-006**: The system MUST **skip embedding generation** for an `UNCHANGED` chunk when the embedding configuration (model / dimension / convention) is unchanged since the chunk was last embedded.
- **FR-007**: The system MUST re-embed **every** chunk (including `UNCHANGED`) when the embedding configuration has changed — a stale vector MUST never be reused.
- **FR-008**: The delta MUST handle repeated text as a **multiset** (`min(N_old, N_new)` `UNCHANGED`, the surplus `ADDED`/`REMOVED`), not a set.
- **FR-009**: The system MUST compute the delta against the prior version's chunks captured **before** they are deleted (the prior version's data MUST be available at diff time).
- **FR-010**: Chunk identity for diffing MUST NOT alter the existing content-addressed chunk/document identity scheme (identity stability for already-stored data is preserved — no stored ID changes as a side-effect of this feature).

### Key Entities

- **ChunkDelta** — the per-chunk change classification: `{ change_type ∈ ADDED|REMOVED|UNCHANGED; new_chunk_id; prev_chunk_id (for UNCHANGED/REMOVED) }`. Carried in the `RE_INGESTED` event.
- **RE_INGESTED event** — a document lifecycle event extending the existing `WatchDocuments` vocabulary (`INGESTED` / `EMBEDDED` / `DELETED` / `RE_INGESTED`), signalling a re-ingest with the chunk delta.
- **Embedding config (corpus baseline)** — the model / dimension / convention under which chunks were embedded; the gate for whether an `UNCHANGED` chunk's existing vector is still current.

## Success Criteria

### Measurable Outcomes

- **SC-001**: For a localized edit to a typical note-style document (≤10% of text changed), **≥80% of chunks are classified `UNCHANGED`** (the delta is proportional to the edit, not the document size). *[Target — validate against a representative vault before locking the saving claim externally.]*
- **SC-002**: A consumer acting on `RE_INGESTED` performs full work (embed/promote) only for `ADDED` chunks; `UNCHANGED` chunks incur **no embedding generation** when the embedding config is unchanged.
- **SC-003**: The old→new chunk-ID map lets a consumer resolve **100%** of its stored references to `UNCHANGED` chunks (no orphaned references after a re-ingest).
- **SC-004**: A re-ingest surfaces exactly **one** `RE_INGESTED` event (no duplicate `INGESTED`/`DELETED` pair) — the consumer observes no double-counting.
- **SC-005**: The write-ACK latency for a re-ingest stays within the existing **<10ms** budget — any delta computation or embedding-preservation work on the re-ingest path MUST NOT breach the async-after-ACK write budget (Constitution Principle IV), whether it runs synchronously or is deferred to the async worker.

## Assumptions

- Built on the existing `WatchDocuments` event bus + the existing re-ingest path; no new transport (`RE_INGESTED` is gRPC-stream-only, like the rest of `WatchDocuments`).
- The technical approach is the **B-simple** design — a per-chunk content-hash sidecar for diffing; preserve the embedding for `UNCHANGED` chunks via a direct-key copy gated on the embedding config; recompute the full-text index + near-duplicate clusters normally (no inverted-index rewiring). Red-team-validated design: [`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md).
- **The ≥80% `UNCHANGED` ratio (SC-001) is a target, not a measured fact.** A representative-vault measurement should validate it before the saving is claimed externally; if real-world ratios are much lower, the optimization's value shrinks and the feature may be re-scoped.
- The deeper `document_id` instability (a re-ingest also produces a new document ID) is acknowledged; consumers should key durable references by **source path** (stable), not document ID. Full document-level identity continuity is **out of scope** for this feature.
- Embedding-config drift detection (the gate for FR-006/FR-007) reuses the existing corpus-baseline mechanism.
- **Out of scope**: reviving the vestigial doc→chunks index for O(chunks-in-doc) capture (a perf refinement, deferred); BL-018 (P4 "chunk content diff") overlaps and is served by the same mechanism.
