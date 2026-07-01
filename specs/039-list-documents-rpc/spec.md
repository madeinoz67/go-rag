# Feature Specification: ListDocuments — Reliable Incremental Document Listing

**Feature Branch**: `039-list-documents-rpc` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-07-01 · **Status**: Draft

**Input**: Seventh item (BL-007) of the go-rag ↔ MuninnDB bridge integration backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`), Phase 1. *The bridge's change-event poller (until BL-008's push stream lands) needs reliable incremental document listing — fetch only documents ingested since a cursor, filtered by status, paginated — so MuninnDB can crash-recover (re-read from the last known cursor on startup) and poll efficiently. Today it must load every document and filter client-side.*

> **Grounded like spec 035 / 037 / 038.** Two deltas from the backlog's BL-007 draft, per the engine's actual conventions:
> 1. **This is a NEW `ListDocuments` operation, not an enhancement of `Files`.** The backlog says "the existing `ListDocumentsRequest` gains two fields", but go-rag has no `ListDocuments` today — it has `Engine.Files()` returning a flat `[]FileEntry` (path/type/status/chunk_count; no `ingested_at`, no pagination, no cursor, no status filter). `Files` stays as-is; `ListDocuments` is additive and returns the richer `DocumentMeta` projection (which carries `ingested_at`, `status`, source path, enrichment, … — the same projection `GetChunk` returns).
> 2. **No `vault` field** on the request — the engine is single-vault-per-process; vault is a connection-time concern (`--vault`/`--db-path`), not a per-call field.

**Why this item next:** BL-001/002/003/004 (the chunk-access family) are shipped and released (v0.2.0). BL-005/006 were triaged out (BL-005 ~80% redundant with `SectionContext`; BL-006 marginal without OCR). BL-007 is the last open Phase-1 item and the one the bridge's polling/crash-recovery path depends on. It depends on nothing (documents already store `ingested_at`), is size S-to-M, and reuses the existing `DocumentMeta` projection + `storage.PrefixScan`.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Fetch only documents ingested since a cursor, filtered by status (Priority: P1)

A client — the go-rag↔MuninnDB bridge sync worker, an integration, or an agent — needs to discover what's new since its last poll: "give me the documents ingested after `<last cursor>`, only the ones that are fully embedded." Today the only option is to list everything and filter client-side — O(corpus) work + bandwidth every poll, and no way to skip pending/error documents server-side. `ListDocuments(after, status)` returns exactly the matching documents in ascending `ingested_at` order, so the client can advance its cursor and act only on what's new and ready. One request, one bounded page, no client-side filtering.

**Why this priority**: This is the primitive the bridge's crash-recovery + polling-fallback path cannot be built without. Delivering this story alone is a viable MVP — a client can poll incrementally and never re-process a document it has already seen.

**Independent Test**: Ingest 5 documents, advance time, ingest 3 more; call `ListDocuments(after=<midpoint>)` → exactly the 3 later documents, in ascending `ingested_at` order, each carrying full metadata. Add `status=embedded` → only the embedded subset of those 3.

**Acceptance Scenarios**:

1. **Given** documents ingested at varying times, **When** a client calls `ListDocuments(after=<T>)`, **Then** only documents with `ingested_at > T` are returned (strictly greater), in ascending `ingested_at` order.
2. **Given** documents in mixed states, **When** a client calls `ListDocuments(status="embedded")`, **Then** only documents that have completed async embedding are returned (pending/error excluded).
3. **Given** both `after=<T>` and `status="embedded"`, **When** a client calls `ListDocuments`, **Then** the result is the AND of the two filters (only embedded docs ingested after T).
4. **Given** any returned document, **Then** it carries its complete current metadata (source path, file path/type, content hash, status, `ingested_at`, enrichment) — identical to what `GetChunk`/`BatchGetChunks` return for that document.

---

### User Story 2 - Paginated listing that composes with the cursor and filter (Priority: P2)

A listing over a large vault must be bounded and resumable. `ListDocuments` caps each page at `page_size` (default 50, max 200) and returns an opaque `next_page_token`; the client fetches the next page by echoing it back. Pagination composes with `after` and `status` — the token carries the filter context so every page of a paged, filtered, cursor-limited listing is consistent.

**Why this priority**: Correctness + scalability of the listing. The MVP (Story 1 — "the right documents are returned") is the unblocker; this story makes the listing bounded and resumable for corpora larger than one page. Independently testable: exercise a multi-page corpus with `after` + `status` set.

**Independent Test**: Ingest > page_size matching documents; page through the full result with the same `after`+`status`; assert the concatenation of all pages equals the expected full filtered set, in order, with no duplicates and no gaps.

**Acceptance Scenarios**:

1. **Given** more matching documents than `page_size`, **When** a client pages by echoing `next_page_token`, **Then** every document is returned exactly once across the pages, in ascending `ingested_at` order, and the final page's `next_page_token` is empty.
2. **Given** a request with `after` + `status` + a `page_token`, **Then** every page honours the same `after` + `status` filter (the token preserves the filter context).
3. **Given** `page_size` above the maximum (200), **Then** a clear invalid-argument result is returned.
4. **Given** an empty corpus (or no matches), **Then** the response is an empty document list with an empty `next_page_token` (never an error).

---

### User Story 3 - The same listing from any transport (Priority: P3)

A document listing over gRPC, REST, MCP, or CLI must be byte-for-byte identical — the same documents in the same order, the same `next_page_token`, the same per-document metadata. go-rag serves four transports over one engine, and the constitution requires every operation available across all of them with identical results. `ListDocuments` is one operation on the shared engine, projected onto all four transports.

**Why this priority**: Parity is a universal invariant (Constitution Principle V). It is the lowest-priority story because the filtering/pagination logic (Stories 1–2) is the hard part; projecting it is mechanical.

**Independent Test**: Call `ListDocuments` with the same `after`+`status`+`page_size` over gRPC, REST, MCP, and CLI; assert all four return identical document-id lists (in order), the same `next_page_token`, and the same per-document metadata.

**Acceptance Scenarios**:

1. **Given** the same `(after, status, page_size, page_token)`, **When** listed over gRPC, REST, MCP, and CLI, **Then** all four return the same ordered document list and the same `next_page_token`.
2. **Given** an invalid request (`page_size > 200`, malformed `after`, bad `status`), **When** submitted over any transport, **Then** each surfaces an equivalent invalid-argument result in its native form.

---

### Edge Cases

- **Empty corpus / no matches:** empty `documents` + empty `next_page_token`; never an error.
- **`after` far in the future:** empty result (no document has `ingested_at > after`).
- **`after` empty/omitted:** all documents (the cursor is unbounded below).
- **`status=""` (omitted):** all documents regardless of state (no status filter).
- **Invalid `status` value (not embedded/pending/error/""):** invalid-argument.
- **Malformed `after` (not RFC3339):** invalid-argument.
- **`page_size` ≤ 0 or > 200:** invalid-argument (default 50 when omitted).
- **`page_token` from a different filter context (client changes `after`/`status` mid-pagination):** the new filter takes precedence — the token's resume position is applied within the new filter set (the token encodes the last-returned `(ingested_at, document_id)`, not the filter). The plan pins the exact semantics.
- **Two documents with identical `ingested_at`:** tie-broken by document id (so the ordering is total + the cursor is stable).
- **`ingested_at` reliability (affirmed, not new work):** every document has a non-empty `ingested_at` (set at ingest); re-ingesting changed content mints a new document record with a fresh `ingested_at` (content-addressed identity). The plan verifies this holds for all records; if any pre-existing record lacks it, the plan decides a backfill (default: none expected — no migration).

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `ListDocuments(page_size, page_token, after, status)` operation that returns a page of documents matching the filters, plus an opaque `next_page_token`.
- **FR-002**: The returned documents MUST be in ascending `ingested_at` order, tie-broken by document id (a total, stable ordering).
- **FR-003**: `after` (RFC3339) MUST filter to documents with `ingested_at` strictly greater than `after`; omitted/empty `after` means unbounded below (all documents).
- **FR-004**: `status` MUST filter to the given state (`embedded` | `pending` | `error`); omitted/empty `status` means no status filter. `after` and `status` MUST combine with AND semantics.
- **FR-005**: `page_size` MUST default to 50 when omitted; a value `< 1` or `> 200` MUST return an invalid-argument result.
- **FR-006**: `page_token` MUST be opaque; an empty token means the first page. The response's `next_page_token` MUST be empty when the returned page is the last.
- **FR-007**: Pagination MUST compose with `after` and `status` — every page of a paged, filtered, cursor-limited listing returns the correct slice, with each matching document appearing exactly once across the full result.
- **FR-008**: Every returned document MUST carry its complete current metadata — the same `DocumentMeta` projection `GetChunk`/`GetChunkContext`/`BatchGetChunks` return (source path, file path/type, content hash, status, `ingested_at`, enrichment, chunk count).
- **FR-009**: Every document record in the store MUST have a non-empty `ingested_at` (affirmed — already true by construction at ingest; the plan verifies across all records).
- **FR-010**: Re-ingesting a document whose content changed MUST yield a record whose `ingested_at` reflects the re-ingest time (affirmed — content-addressed identity mints a new record on changed content; the plan verifies).
- **FR-011**: `ListDocuments` MUST be exposed over all four transports — gRPC, REST, MCP, and CLI — returning identical results for the same `(page_size, page_token, after, status)` (cross-transport parity). *(Constitution Principle V.)*
- **FR-012**: The REST endpoint MUST be `GET /v1/documents?page_size=&page_token=&after=&status=` (the `/v1/<resource>` convention; no per-vault URL segment, no `/api/` base).
- **FR-013**: The listing MUST be a single logical read that returns a consistent view — iterate the document records, filter by `after` + `status`, order by `ingested_at`, then paginate — not N independent caller round-trips.
- **FR-014**: `ListDocuments` MUST be a pure read: no on-disk key-space layout change, no schema migration, `migrate.ExpectedVersion` unchanged (pending the plan's `ingested_at` verification; default no migration). *(Constitution — Storage discipline / schema-version compliance.)*
- **FR-015**: `ListDocuments` MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies. *(Constitution Principle III.)*
- **FR-016**: The request MUST NOT carry a `vault` field — the engine is single-vault-per-process (spec 035 convention); vault is a connection-time concern.

### Key Entities *(include if feature involves data)*

- **Document**: the ingested unit, identified by its content-addressed id, carrying `IngestedAt`, `Status`, source/file metadata, and enrichment. Read as-is by the listing; the listing returns the `DocumentMeta` projection (unchanged, reused from spec 035).
- **DocumentMeta**: the transport projection of a document (ids, paths, type, status, `ingested_at`, content hash, chunk count, enrichment). The same projection `GetChunk` returns (spec 035). Unchanged by this feature.
- **Page token**: an opaque cursor encoding the last-returned `(ingested_at, document_id)` so the next page resumes strictly after it under the total ordering. Read-only; the plan pins the encoding. A read-only artefact — no new stored structure.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Any client — gRPC, REST, MCP, or CLI — can list documents ingested after a cursor, filtered by status, paginated, with identical results across all four transports.
- **SC-002**: The bridge's change-poll + crash-recovery path can be implemented using `ListDocuments` alone — no client-side "load everything and filter"; an incremental poll returns only documents ingested since the last cursor.
- **SC-003**: A listing over a corpus larger than one page returns every matching document exactly once, in order, across the pages, with `after` + `status` honoured on every page.
- **SC-004**: Unbounded or malformed input — `page_size > 200`, a non-RFC3339 `after`, an unknown `status` — is rejected with a clear invalid-argument result 100% of the time.
- **SC-005**: The feature lands as a pure read over existing stored data — no on-disk schema change, no migration (default), no new runtime dependency, no change to any document record.

---

## Assumptions

- **Pure read, no schema change (default).** `ListDocuments` reads document records go-rag already stores (prefix `0x02`) via `storage.PrefixScan`. It creates no new stored state and changes no on-disk layout, so no migration is expected and `migrate.ExpectedVersion` is unchanged. (The plan verifies `ingested_at` is populated on every record; if a gap is found, the plan decides whether a backfill migration is warranted — default: none.)
- **`ingested_at` is reliable by construction.** `processFile` sets `IngestedAt = now` at every ingest; content-addressed identity (Constitution II) mints a new document record (new id, fresh `IngestedAt`) when changed content is re-ingested. So `ingested_at`-based cursors are monotonic and complete. The spec affirms this; the plan verifies.
- **Reuses spec 035.** The `DocumentMeta` proto message + every transport's `toDocumentMeta*` projection are established by spec 035 (BL-001). `ListDocuments` reuses them; it does not reinvent them.
- **Single-vault-per-process.** No `vault` field; the listing is over this vault's documents only.
- **Pagination is new to go-rag.** No existing `page_token` scheme; this spec introduces one. The plan pins the encoding (recommended: opaque token over `(ingested_at, document_id)`). `page_size` default 50, max 200 mirrors the backlog draft.
- **Filtering is in-memory.** Documents are prefix-scanned, then filtered by `after` + `status` in memory, ordered by `ingested_at`, then paginated. The plan confirms this is adequate for v1 corpus sizes (no secondary index required).

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

- **Verify `ingested_at` reliability.** Confirm every document record has a non-empty `ingested_at` and that re-ingest of changed content mints a fresh record (content-addressed). Grounding this session: `internal/pipeline/pipeline.go` `processFile` sets `IngestedAt = now`. If verification finds any record lacking it, decide a backfill migration (Constitution schema-evolution) — default expectation: none needed.
- **Pagination encoding (NEW pattern).** go-rag has no existing pagination. Decide the `page_token` encoding — recommend an opaque, URL-safe token encoding the last-returned `(ingested_at, document_id)` so the next page resumes strictly after it under `ORDER BY ingested_at ASC, id ASC`. Confirm the token is stable across a paginated read (no torn pages) given eventual-consistency concurrent writes.
- **Iteration mechanism.** `storage.PrefixScan(PrefixDocument)` iterates document records; filter (`after` + `status`) + sort (`ingested_at`) + paginate in-memory. Confirm this satisfies FR-013 (single logical read) and v1 scale; no secondary `ingested_at` index needed for v1 (decide in plan).
- **Reuse the spec-035 `DocumentMeta` message + projection.** `ListDocumentsResponse` returns `repeated DocumentMeta` (the spec-035 message) + `string next_page_token`. Confirm no new proto message beyond the request/response wrappers.
- **REST route.** `GET /v1/documents?page_size=&page_token=&after=&status=` — register in the REST router (beside `/v1/files`, `/v1/dirs`); parse + validate; add to `openapi.yaml` (the openapi parity test asserts the two match). 200 with `{ documents: […], next_page_token }`; 400 for invalid-argument.
- **Cross-transport.** Engine `ListDocuments(req ListDocumentsRequest) (*ListDocumentsResult, error)`; project to gRPC, REST, MCP tool (`go_rag_list_documents`), and CLI (`go-rag documents list [--page-size N] [--after T] [--status embedded]`). Extend `parity_test.go`. Bump the MCP tool-count tests (22 → 23).
- **Errors.** `ErrInvalid` (page_size out of [1,200], malformed `after`, unknown `status`), mirroring the GetChunk family. An empty result is NOT an error (empty `documents` + empty `next_page_token`).
- **Constitution compliance to assert in the plan:** Principles II (no identity change — read-only), III (pure Go), V (all transports, one engine); Storage discipline (no new key prefix); Schema evolution (no migration, `ExpectedVersion` unchanged — pending `ingested_at` verification); Performance standards (bounded page; the plan confirms scan cost is acceptable for v1).
