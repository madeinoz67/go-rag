# Research — ListDocuments (BL-007)

> Phase 0 output for `/speckit-plan`. Resolves the spec's two Research-Note questions (page_token encoding; `ingested_at` reliability) and confirms the iteration + status-filter grounding. Verified by direct read of `internal/model/model.go`, `internal/pipeline/pipeline.go`, `internal/storage/db.go`, and the spec-035 projection helpers.

## R1 — `page_token` encoding (NEW pagination pattern)

**Decision**: `page_token` is an **opaque, URL-safe string** encoding the last-returned `(ingested_at, document_id)` pair, so the next page resumes strictly after it under the total ordering `ingested_at ASC, document_id ASC`. Encoding: base64 (URL-safe, no padding) of the string `<ingested_at RFC3339Nano> \x1f <document_id>` (unit separator `\x1f` avoids collision with either field). Decoding validates the format; a malformed token → `ErrInvalid`.

**Rationale**:
- go-rag has **no existing pagination** primitive (every list operation — `Files`, `Dirs`, `Status` — returns a flat slice). This spec introduces one; an opaque token is the standard, forward-compatible choice (the server can change the encoding later without breaking clients).
- Encoding the resume point (not an offset) is **stable under concurrent writes**: a document's `(ingested_at, id)` never changes (immutable once written), so paging through a snapshot resumes deterministically. Documents ingested *during* a paged read have `ingested_at ≥ now`; they may appear on a later page — acceptable (and desirable) for an incremental-sync cursor, never cause a duplicate or a skip within the existing set.
- Tie-break by `document_id` makes the ordering **total** (two docs can share an `ingested_at` timestamp), so the cursor is unambiguous.
- The token carries **only the resume point, not the filter** — the client re-sends `after`/`status`/`page_size` on every page call (FR-007). This keeps the token short, stateless, and immune to "client changed filter mid-pagination" ambiguity: the new filter applies, and the resume point is interpreted within the newly-filtered, re-sorted set.

**Alternatives considered**:
- *Offset integer* (`page_token = "42"`): simple, but unstable under concurrent inserts before the offset (skips/dupes). Rejected for a corpus that grows.
- *Filter-embedded token* (token carries `after`/`status`): avoids re-sending params but couples the token to the filter, complicates REST (query params vs token) and makes "change filter mid-pagination" undefined. Rejected.
- *Secondary `ingested_at` index* (a new Pebble prefix ordering docs by ingested_at): enables a true range scan but adds a new key prefix → a migration (Constitution schema-evolution) + write-path changes. Rejected for v1 — `PrefixScan` + in-memory sort is adequate for v1 vault sizes; revisit if profiling shows otherwise.

## R2 — `ingested_at` reliability (no migration)

**Decision**: affirmed — every document record has a non-empty `ingested_at`, and re-ingest of changed content reflects the re-ingest time. **No backfill migration is needed**; `migrate.ExpectedVersion` is unchanged.

**Rationale (verified this session)**:
- `internal/model/model.go` `Document.IngestedAt time.Time` with comment confirming it is the ingest timestamp.
- `internal/pipeline/pipeline.go` `processFile` sets `IngestedAt: now` (line ~298) on every document it creates — there is no ingest path that skips it.
- `Document.ID = GenerateID(content + mime + metadata)` (Constitution II) — content-addressed. Re-ingesting a file whose **content changed** produces a different SHA-256 → a different `Document.ID` → a **new record** with a fresh `IngestedAt`. Re-ingesting **unchanged** content is a no-op (idempotent) — the existing record (and its `IngestedAt`) is correctly retained. So "re-ingested docs reflect re-ingest time" holds by construction: a changed doc is a new doc.
- `Document.Status` values are exactly **`pending` | `embedded` | `error`** (model.go field comment + the `StatusPending`/`StatusEmbedded` constants in `internal/pipeline/pipeline.go`) — a 1:1 match for the spec's `status` filter values. No translation needed.

**Edge case (for the plan to note, not block)**: a record written by a hypothetical older binary that predates `IngestedAt` would lack it. go-rag's `IngestedAt` has been set since the field existed (no known older binary in this single-author repo), so the population is complete. The `ListDocuments` handler treats a zero `IngestedAt` as "earliest" (sorts first; passes the `after` filter for any non-empty `after`) — defensive, never an error.

## R3 — Iteration + filter + ordering mechanism

**Decision**: `Engine.ListDocuments` performs one `storage.PrefixScan(storage.PrefixDocument)` (prefix `0x02`), decoding each value as a `model.Document`; filters in-memory by `after` (ingested_at > T) and `status` (exact match, when non-empty); sorts the survivors by `(ingested_at ASC, id ASC)`; then applies the page: skip-to-resume-point (from `page_token`, if any), take `page_size`, emit `next_page_token` if more remain.

**Rationale**:
- `PrefixScan` is the existing, idiomatic whole-prefix reader (`internal/storage/db.go`). Document records are JSON under `0x02`. One scan = one logical read (FR-013).
- In-memory filter + sort is O(documents) per call. For v1 vault sizes (single-user, local) this is sub-millisecond-to-low-millisecond; no secondary index is warranted (R1 rejected it). The `after` cursor makes incremental polls cheap (the client only re-scans when it knows there's new work, and the scan is local).
- Sorting by `(ingested_at, id)` gives the total ordering the page_token resume relies on (R1).

**Alternatives considered**: a streaming/pooled scan to avoid materialising the whole prefix — premature for v1 (corpus sizes are modest); the materialise-filter-sort-paginate path is simple and correct. Revisit if a vault reaches tens-of-thousands of documents and the scan shows up in profiling.

## R4 — Pagination stability + concurrent writes

**Decision**: pagination is **stable for the existing set** and **eventually-inclusive for new documents**, which is the correct semantics for an incremental-sync cursor.

**Rationale**:
- Existing documents are immutable in `(ingested_at, id)` (R2), so a paged read never duplicates or skips them regardless of concurrent writes.
- A document ingested *during* a paged read has `ingested_at ≥ now`; under `ingested_at ASC` it lands at or after the current resume point, so it appears on the current or a later page — never before an already-returned page. For the bridge's poller this is the desired behaviour: the next poll's `after` cursor picks it up cleanly.
- A document *deleted* during a paged read (re-ingest replaces the old id with a new one) simply isn't seen — the old id is gone; the new id appears with a fresh `ingested_at`. No torn read.
- This matches the constitution's concurrency model (single-writer, concurrent reads are eventual-consistent) — `ListDocuments` is a read, so it observes a consistent per-record view with the above eventually-inclusive semantics for new records.
