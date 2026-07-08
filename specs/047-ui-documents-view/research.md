# Research — go-rag Management Console, Documents View (Slice 1)

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

Phase 0 output. Resolves the spec's Open Questions and the Technical Context unknowns
by grounding every decision in the existing engine surface (read this session via the
graph). Each decision is tagged `Rn` and cross-referenced from `plan.md` / `data-model.md`
/ `contracts/`.

---

## R1 — The detail view needs a new `Engine.ListChunks` accessor

**Decision**: Add a read-only `Engine.ListChunks(documentID string, req ListChunksRequest)
(*ListChunksResult, error)` — a paginated scan over the chunk prefix (0x03) filtered by
`document_id`, ordered by `chunk_index ASC`, with the same opaque-cursor pagination as
`ListDocuments`.

**Rationale**: There is **no existing way to enumerate a document's chunks**. The chunk
accessors are all point/window/batch-by-id: `GetChunk(chunkID)` (035),
`GetChunkContext(chunkID, window)` (037), `BatchGetChunks(chunkIDs)` (038). The
`Document` stores `ChunkCount` but not its chunk IDs, so BatchGetChunks cannot bootstrap
a listing. The detail view's core promise ("inspect a document's chunks") is unreachable
without a scan. A prefix scan over 0x03 + in-memory filter by `document_id` is the
minimal, idempotent, read-only mechanism — the direct analogue of how `ListDocuments`
scans 0x02 (spec 039). Pure read: no new key, no migration.

**Alternatives considered**:
- *Walk the chunk linked list via `GetChunkContext` from chunk 0* — rejected: requires
  chunk-0's ID, which the document does not store; would still need a scan to find it.
- *Store chunk IDs on the document* — rejected: storage-layout change → migration,
  violates the "no new storage" scope and the async-after-ACK write path.
- *Client-side: fetch all chunks via BatchGetChunks over a stored ID list* — rejected:
  same storage-change problem; also unbounded batch.

---

## R2 — Content search reuses `Engine.Query` (no new accessor)

**Decision**: US3's "search documents by chunk content" calls the existing
`Engine.Query(ctx, QueryRequest)` (hybrid BM25 + vector + rerank) and projects the
`QueryHit[]` to **distinct parent documents** (dedup on `document_id`, ranked by the
query's ordering).

**Rationale**: `Query` is go-rag's retrieval engine — exactly the right tool for "find
documents whose content matches." Ranked retrieval (rather than a flat FTS scan) is a
feature for a corpus browser: the most relevant documents surface first. Reusing it adds
**zero new engine surface**.

**Alternatives considered**:
- *Add a dedicated "search documents by content" engine accessor* — rejected: duplicates
  `Query`'s purpose; the projection (hits → distinct docs) is a thin UI-side fold.
- *BM25-only FTS scan returning all matching docs* — rejected: unbounded result set and
  loses the ranking that makes retrieval useful.

**Name/path/content match granularity** (spec Open Q): the list-level search box searches
**document name / file_path client-side within the request** AND **chunk content via
`Query` server-side**; results are document rows. Jump-to-matching-chunk is a plan-level
defer (the detail view already lists chunks; a deep-link can land in a later slice).

---

## R3 — Tag filter = additive optional param on `ListDocumentsRequest`

**Decision**: Add `Tags []string` (match-any semantics) to the existing
`engine.ListDocumentsRequest`, applied in the existing in-memory filter pass alongside
`Status`. Empty = all documents (current behaviour, backward-compatible). Wired in the UI
handler; binding into REST/gRPC/MCP request shapes is additive and non-breaking.

**Rationale**: A tag filter that works only client-side breaks cursor pagination (you
cannot filter a page and keep correct totals). The filter pass already exists (for
`Status`); adding a `Tags` clause is a few lines. Because the UI calls the engine
in-process (R4), the UI gets correct paginated tag-filtered results immediately.

**Alternatives considered**:
- *Client-side tag filter within a page* — rejected: wrong results on corpora larger
  than one page; confusing UX.
- *A separate tag → document index* — rejected: storage change + migration; out of scope
  for a read-only slice. (The existing `--tags` query filter already proves tags are
  stored on the document and scannable.)

---

## R4 — The UI calls the engine in-process, not via REST

**Decision**: Documents-view handlers call `s.eng.ListDocuments(...)`,
`s.eng.ListChunks(...)`, `s.eng.GetChunk(...)`, `s.eng.Query(...)` directly (the UI
`Server` already holds `*engine.Engine`, exactly as `handleDashboardStats` calls
`s.eng.Status()`). The view defines its **own** `/api/documents*` routes.

**Rationale**: This is the spec 046 precedent — the UI is a 4th adapter over the engine,
not a REST proxy. In-process calls avoid a loopback HTTP hop, keep the view self-contained,
and match how the Dashboard already works.

**Alternatives considered**:
- *UI proxies `GET /v1/documents` over REST* — rejected: an extra hop, an extra auth
  pass, and it would force every view-specific need (tag filter, chunk listing) through
  the REST surface first. The Dashboard does not do this; neither does this view.

---

## R5 — Manual refresh this slice (no live watch)

**Decision**: The Documents view fetches on view-entry and on explicit refresh. Live
updates via spec 040 `WatchDocuments` are **deferred** to a later slice.

**Rationale**: Watch adds a streaming surface (SSE/WebSocket) and reconnection state —
real complexity that is not needed to deliver browse/inspect/find. spec 046 explicitly
deferred streaming (Observability live stream is Slice 5). A manual-refresh button (or
re-fetch on view re-entry) covers the single-operator use case.

**Alternatives considered**:
- *Poll on an interval* — a reasonable middle ground, defer the cadence to tasks; the
  plan leaves a poll hook optional but does not require it for Slice 1 acceptance.

---

## R6 — "Source changed since ingestion" indicator deferred

**Decision**: No staleness indicator in Slice 1. The detail view shows **stored document
state only**. The read-only "source changed" indicator (spec 043 reingest-delta) and the
reingest action both ship with the **write-actions slice**.

**Rationale**: A truthful "source changed" verdict requires resolving the live source
file and re-hashing — precisely the work the listing deliberately skips for performance
("the listing does not resolve the per-document Source"). Doing it per-document on detail
open is acceptable but is the thin end of the reingest wedge; cleaner to ship the
indicator together with the action that acts on it. This revises the spec's lean (which
included the indicator here) — recorded as a plan-level scope refinement.

---

## R7 — Pagination = reuse the `ListDocuments` opaque-cursor mechanism

**Decision**: Document listing reuses `engine.ListDocuments`' `page_token` cursor
(`after`/`page_token`, page_size 50 default / 200 max) verbatim. The new `ListChunks`
(R1) uses the **same** cursor shape over `(chunk_index, chunk_id)`.

**Rationale**: Cursor pagination is already the project standard (spec 039), already
tested (`TestListDocuments_CursorAndFilter`, `TestListDocuments_Pagination`), and
correct under writes (the cursor is a resume point, not an offset). Reusing it keeps one
pagination mental model across the console. Resolves spec Open Q (pagination mechanism).

**Sort scope (spec FR-002)**: the engine orders by `(ingested_at ASC, id ASC)`. Sort by
name / size / chunk_count is **page-local (client-side) within the current page** for
Slice 1; corpus-wide multi-column sort would need engine-side sort params and is
deferred. Date sort (ingested_at) is honoured engine-side via the cursor direction.

---

## R8 — UI DTOs mirror the existing cross-transport DTOs (parity-pinned)

**Decision**: The UI defines its own `documentDTO` and `chunkDTO` structs **mirroring**
`rest.documentMetaDTO` / `rest.chunkDTO` (and the CLI `documentOut`/`chunkOut`, proto
`DocumentMeta`/`Chunk`) field-for-field. They are kept byte-identical by a **parity
test** (pattern of `internal/engine/parity_test.go::TestCrossTransport_ListDocumentsParity`),
not by a shared import (package boundary: `internal/ui` does not import `internal/rest`).

**Rationale**: Every existing transport defines its own mirror DTO struct (REST, CLI,
proto) rather than sharing one — kept identical by parity tests. The UI follows the same
pattern. This honours constitution V (cross-transport parity) without creating a coupling
between the UI and REST packages. The new `ListChunks` ships the same `chunkDTO` shape on
every transport.

**Document list row** = the `documentMetaDTO` field set (id, content_hash, file_path,
file_name, file_type, chunk_count, file_size, status, ingested_at, tags, summary,
enrichment_status). Note (from spec 039): the listing does **not** resolve the per-document
`Source`, so `source_path` is empty in the list row (only the detail view resolves it).

---

## Summary table

| Tag | Decision | New engine surface? | Storage change? |
|-----|----------|---------------------|-----------------|
| R1 | Add `Engine.ListChunks` (paginated chunk scan) — full cross-transport parity | Yes (read-only accessor) | No |
| R2 | Content search reuses `Engine.Query` → distinct docs | No | No |
| R3 | `Tags []string` additive optional filter on `ListDocumentsRequest` | Yes (additive param) | No |
| R4 | UI calls engine in-process (own `/api/documents*` routes) | No | No |
| R5 | Manual refresh this slice (watch deferred) | No | No |
| R6 | "Source changed" indicator deferred to write-actions slice | No | No |
| R7 | Reuse `ListDocuments` opaque-cursor pagination; sort is page-local | No | No |
| R8 | UI DTOs mirror existing DTOs; parity-pinned by test | No | No |

**Net new engine surface**: one read-only accessor (`ListChunks`) + one additive optional
request field (`Tags`). No new storage, no migration, no write path.
