# Feature Specification: GetChunkContext — Fetch a Chunk with Its Surrounding Window

**Feature Branch**: `037-get-chunk-context-rpc` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-06-30 · **Status**: Draft

**Input**: Second item (BL-002) of the go-rag ↔ MuninnDB bridge integration backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`), Phase 1. *The bridge's `ActivateWithRAG` pattern needs the document context around a retrieved chunk — not just the chunk itself, but its N neighbours on each side — so MuninnDB can promote (or a consumer can display) the chunk with its surrounding prose. `Chunk` already carries `PreviousChunkID` / `NextChunkID` forming a linked list through the document (plus `ChunkIndex` / `TotalChunks`). `GetChunkContext` (BL-001 `GetChunk`'s direct sibling) traverses that and returns a window of up to N chunks either side in a single call, with the requested chunk at a known index. Without it the bridge chains N individual `GetChunk` calls.*

> **Grounded like spec 035 (BL-001).** Two deltas from the backlog's draft, per the engine's actual conventions:
> 1. **No `vault` field** on the request — the engine is single-vault-per-process; `GetChunk` (spec 035) takes only `chunk_id`, and `GetChunkContext` mirrors it. The bridge connects to the daemon bound to the target vault (`--vault`/`--db-path`); vault is a connection-time concern, not a per-call field.
> 2. **REST path is `GET /v1/chunks/{id}/context?window=N`** — the existing REST surface uses `/v1/<resource>` with `{id}` path params (spec 035: `/v1/chunks/{id}`); there is no `/api/` base and no per-vault URL segment.

**Why this item next:** BL-001 (`GetChunk`, spec 035) and BL-004 (`Wikilinks`, spec 036) are shipped. The post-review map (`bridge-map-post-review.md` §4) names **context expansion (`ActivateWithRAG`)** as a headline unblocked pattern needing `Read` (shipped by MuninnDB) + BL-001/002/003 (ours); BL-002 is the direct enabler. (BL-005/BL-006 were triaged out: BL-005 is ~80% redundant with `SectionContext`; BL-006 is marginal without an OCR pipeline.) BL-002 depends only on BL-001 (done), is size-S, and reuses the existing linked list + the spec-035 `Chunk` message.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Resolve a chunk plus its surrounding window in one call (Priority: P1)

A client — the go-rag-muninn bridge's `ActivateWithRAG`, an integration, an agent, or a developer — holds a `chunk_id` and needs the chunk **and its neighbours** to read it in context. Today the only way to get neighbours is to chain N `GetChunk` calls, following `PreviousChunkID`/`NextChunkID` one hop at a time — N round-trips with N chances to surface a partial or inconsistent view. `GetChunkContext` collapses that to one call: the requested chunk plus up to `window` chunks on each side, returned in document order with the requested chunk at a known `target_index`. One request, one consistent snapshot, one paginated result.

**Why this priority**: This is the primitive the `ActivateWithRAG` pattern (the bridge's other headline use, alongside the wikilink→`Link` pipeline) cannot be built without. Delivering this story alone is a viable MVP — a client can fetch any chunk with as much surrounding context as it needs in one round-trip.

**Independent Test**: Ingest a multi-chunk document, obtain an interior chunk ID, call `GetChunkContext` with `window=2`; the response contains 5 chunks in document order (2 before, target, 2 after), with `target_index=2`.

**Acceptance Scenarios**:

1. **Given** an interior chunk of a document with enough neighbours on both sides, **When** a client calls `GetChunkContext(chunk_id, window=2)`, **Then** the response contains exactly `[2 before, target, 2 after]` in document order, and `target_index` is `2`.
2. **Given** a valid `chunk_id`, **When** the client calls `GetChunkContext`, **Then** every returned chunk carries its full current metadata (content, ordinal position, source path, page, `SectionContext`, `Wikilinks` per spec 036, and any other sidecars).
3. **Given** a `chunk_id` that does not exist in the vault, **When** the client calls `GetChunkContext`, **Then** a clear not-found result is returned (not an empty window).
4. **Given** a `chunk_id` that exists but belongs to a different vault, **When** called against this single-vault store, **Then** not-found is returned — the other vault's chunk is never disclosed (no cross-vault leakage).

---

### User Story 2 - Correct windowing at boundaries and edge values (Priority: P2)

A context fetch must behave predictably at the document's edges and for unusual window values: the first chunk has no predecessors, the last has no successors, `window=0` means "just the target," and an excessive window is rejected rather than silently truncated to an arbitrary large slice. `GetChunkContext` returns as many neighbours as exist at each boundary (never an error), treats `window=0` as equivalent to a single-chunk fetch, caps the window at 10, and rejects anything larger with a clear invalid-argument result.

**Why this priority**: Correctness of the windowing contract. The MVP (Story 1 — "the window is returned") is the unblocker; this story pins the boundary and clamp behaviour that makes the window trustworthy. Independently testable: exercise the first chunk, the last chunk, `window=0`, and `window>10` directly.

**Independent Test**: Fetch the document's first chunk with `window=5` → the response has the target at `target_index=0` and up to 5 successors (no predecessors). Fetch with `window=0` → exactly one chunk (`target_index=0`). Fetch with `window=11` → invalid-argument.

**Acceptance Scenarios**:

1. **Given** the first chunk of a document, **When** fetched with `window=N`, **Then** the response contains the target at `target_index=0` followed by up to N successors and zero predecessors (no error).
2. **Given** the last chunk of a document, **When** fetched with `window=N`, **Then** the target sits at the last index with up to N predecessors and zero successors.
3. **Given** `window=0`, **When** a client fetches any valid chunk, **Then** the response contains exactly one chunk — the target — with `target_index=0` (equivalent to `GetChunk`).
4. **Given** `window` greater than 10, **When** a client calls `GetChunkContext`, **Then** a clear invalid-argument result is returned (the window is capped at 10, never silently truncated above it).
5. **Given** a negative `window`, **When** a client calls `GetChunkContext`, **Then** a clear invalid-argument result is returned.

---

### User Story 3 - The same context fetch from any transport (Priority: P3)

A context window fetched over gRPC, REST, MCP, or CLI must be byte-for-byte identical — the same chunks in the same order, the same `target_index`, the same parent document metadata. go-rag serves four transports over one engine, and the constitution requires every operation available across all of them with identical results. `GetChunkContext` is one operation on the shared engine, projected onto all four transports.

**Why this priority**: Parity is a universal invariant (Constitution Principle V), not unique to this feature — but for `GetChunkContext` it is what makes the window usable by the bridge (gRPC), by agents (MCP), by scripts (CLI), and by HTTP clients (REST). It is the lowest-priority story because the windowing logic (Stories 1–2) is the hard part; projecting it is mechanical.

**Independent Test**: Fetch the same `chunk_id` with the same `window` over gRPC, REST, MCP, and CLI; assert all four return identical chunk lists, `target_index`, and document metadata.

**Acceptance Scenarios**:

1. **Given** a valid `chunk_id` and `window`, **When** fetched over gRPC, REST, MCP, and CLI, **Then** all four return the same ordered chunks, the same `target_index`, and the same parent document metadata.
2. **Given** a missing `chunk_id`, **When** fetched over any transport, **Then** each surfaces an equivalent not-found result in its native form.

---

### Edge Cases

- **Single-chunk document:** `GetChunkContext(any id, window=N)` returns exactly one chunk at `target_index=0` (no neighbours exist).
- **Window larger than the document:** returns the whole document's chunks with the target at its real index; never an error, never padding.
- **Stale `chunk_id` after re-ingest:** if a document was re-chunked, an old `chunk_id` no longer resolves → not-found (content-addressed identity means an unchanged chunk keeps its ID; a changed chunk gets a new one).
- **Orphan chunk (parent document removed):** `GetChunkContext` still returns the window of chunk neighbours (the linked list is among chunks, not via the document); the parent `DocumentMeta` is zero-valued / omitted — tolerant, not an error (mirrors `GetChunk`'s orphan handling, spec 035).
- **Linked-list break (a neighbour ID that does not resolve):** return as many neighbours as the unbroken run allows up to the requested window; do not error. (Defensive — the linked list is written atomically at ingest, but a partial read should degrade gracefully.)
- **Quarantined / poisoned chunks in the window:** returned as stored (a context fetch is a deterministic point access, not a ranked query); poisoning is a query-time signal, consistent with `GetChunk` (spec 035).
- **Empty / whitespace `chunk_id`:** invalid-argument, no lookup.
- **`window` omitted (default):** treated as `window=2`.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `GetChunkContext(chunk_id, window)` operation that resolves a content-addressed `chunk_id` and returns the chunk plus up to `window` predecessor chunks and up to `window` successor chunks, in a single call.
- **FR-002**: The response MUST return the chunks in document order and MUST include a `target_index` that identifies the requested chunk's position within the returned slice.
- **FR-003**: `window` MUST default to `2` when omitted, and `window=0` MUST return exactly the target chunk alone (equivalent to `GetChunk`).
- **FR-004**: `window` MUST be capped at `10`; a request with `window > 10` MUST return an invalid-argument result. A negative `window` MUST also return invalid-argument.
- **FR-005**: At document boundaries (first/last chunk) or for short documents, the system MUST return as many neighbours as exist — it MUST NOT error when fewer than `window` neighbours are available on a side.
- **FR-006**: `GetChunkContext` MUST return a clear not-found result when the `chunk_id` does not exist in the vault, and not-found (never the chunk) when it exists in a different vault — cross-vault disclosure is forbidden (mirrors `GetChunk`, spec 035 FR-003).
- **FR-007**: `GetChunkContext` MUST return a clear invalid-argument result for an empty/whitespace `chunk_id`, without performing a lookup.
- **FR-008**: Every returned chunk MUST carry its complete current metadata — content, ordinal position, source path, page where applicable, `SectionContext` (spec 025), `Wikilinks` (spec 036), and any other sidecars — identical to what `GetChunk` returns for that chunk.
- **FR-009**: The response MUST include the parent document's metadata (the same `DocumentMeta` projection `GetChunk` returns), so a context fetch needs exactly one round-trip. Tolerant of an orphan chunk (zero-valued document, not an error).
- **FR-010**: `GetChunkContext` MUST be exposed over all four transports — gRPC, REST, MCP, and CLI — returning identical results for the same `(chunk_id, window)` (cross-transport parity). *(Constitution Principle V — Extension by Interface, MCP-First.)*
- **FR-011**: The REST endpoint MUST be `GET /v1/chunks/{id}/context?window=N` (mirrors spec 035's `/v1/chunks/{id}`; no per-vault URL segment, no `/api/` base).
- **FR-012**: The fetch MUST be a single logical read returning a consistent snapshot of the chunk and its neighbours — not N independent caller round-trips. *(Constitution — Performance & Reliability Standards; the plan confirms the mechanism: linked-list traversal within one Pebble read, or equivalent.)*
- **FR-013**: `GetChunkContext` MUST reuse the existing `PreviousChunkID` / `NextChunkID` linked list (and/or `ChunkIndex`/`TotalChunks`) and the spec-035 `Chunk` proto message. It introduces no new identity scheme and no new stored state. *(Constitution Principle II; spec 035 reuse.)*
- **FR-014**: `GetChunkContext` MUST be a pure read: no on-disk key-space layout change, no schema migration, `migrate.ExpectedVersion` unchanged. *(Constitution — Storage discipline / schema-version compliance.)*
- **FR-015**: `GetChunkContext` MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies. *(Constitution Principle III.)*
- **FR-016**: The request MUST NOT carry a `vault` field — the engine is single-vault-per-process (spec 035 convention); vault is a connection-time concern.

### Key Entities *(include if feature involves data)*

- **Chunk**: the unit of retrieved text, identified by its content-addressed `chunk_id`, carrying the linked-list pointers (`PreviousChunkID` / `NextChunkID`) and ordinal position (`ChunkIndex` / `TotalChunks`) that `GetChunkContext` traverses. Unchanged by this feature.
- **Context window**: the ordered slice of up to `2*window+1` chunks centred on the target (fewer at boundaries), with `target_index` marking the requested chunk. A read-only projection — no new stored structure.
- **DocumentMeta**: the parent document's metadata projection (source path, type, ingestion/enrichment status), returned alongside the window so the caller has full context in one call. The same projection `GetChunk` returns (spec 035).

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Any client — gRPC, REST, MCP, or CLI — can resolve a valid `chunk_id` plus up to `window` neighbours on each side and the parent document metadata in a single call, with identical results across all four transports.
- **SC-002**: The bridge's `ActivateWithRAG` pattern can be implemented using `GetChunkContext` alone — no client-side chaining of N `GetChunk` calls, no document scan — context expansion reduces to one round-trip per activation.
- **SC-003**: Windowing is correct at every boundary — first chunk, last chunk, single-chunk document, `window=0`, `window>document` — 100% of the time, and `window>10` / negative `window` yield a clear invalid-argument result.
- **SC-004**: The feature lands as a pure read over existing stored data — no on-disk schema change, no migration, no new runtime dependency, no change to any `chunk_id`.

---

## Assumptions

- **Pure read, no schema change.** `GetChunkContext` traverses data go-rag already stores (the linked list + chunk records). It creates no new stored state and changes no on-disk layout, so no migration is required and `migrate.ExpectedVersion` is unchanged.
- **Reuses spec 035.** The `Chunk` proto message, the `DocumentMeta` projection, the `lookupChunk` / `lookupDoc` engine helpers, and the cross-transport projection pattern are all established by spec 035 (BL-001). `GetChunkContext` composes them; it does not reinvent them.
- **Single-vault-per-process.** No `vault` field on the request; not-found is the cross-vault-isolation path (the other vault's chunk simply isn't in this store), consistent with `GetChunk` (spec 035 FR-003).
- **Consistent snapshot.** "Single logical read" means the window reflects one consistent view of the document's chunks (one Pebble read transaction or equivalent), even though the linked list is followed hop-by-hop. The plan confirms the exact mechanism (N point-Gets within one read transaction vs an index/range fetch).
- **Linked list is authoritative.** `PreviousChunkID`/`NextChunkID` are written atomically at ingest (spec 015); `GetChunkContext` follows them. `ChunkIndex`/`TotalChunks` are available as a cross-check / fallback if a linked-list hop ever fails to resolve (defensive graceful degradation).
- **Window default 2, cap 10.** Mirrors the backlog draft; the cap bounds the response size and read cost.

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

- **Confirm the read mechanism.** Decide between (a) following `PreviousChunkID`/`NextChunkID` for up to `window` hops each direction within ONE Pebble read transaction (consistent snapshot, N point-Gets, no write lock), or (b) fetching by `(DocumentID, ChunkIndex±window)` if a suitable key/index exists. Either must satisfy FR-012 (one logical read, consistent). Verify `lookupChunk` is reusable and whether a batch/multi-get helper exists.
- **Reuse the spec-035 `Chunk` message.** `GetChunkContextResponse` should return `repeated Chunk` (the spec-035 message, which already carries `Wikilinks` per spec 036) + `int32 target_index` + `DocumentMeta`. Confirm no new proto message is needed beyond the request/response wrappers.
- **Proto field tags.** `GetChunkContextRequest { string chunk_id = 1; int32 window = 2; }` (no vault). `GetChunkContextResponse { repeated Chunk chunks = 1; int32 target_index = 2; DocumentMeta document = 3; }`. Add the `rpc GetChunkContext(...)` to the `Gorag` service after `GetChunk`.
- **REST route.** `GET /v1/chunks/{id}/context?window=N` — register in the REST router (beside `/v1/chunks/{id}`); parse `window` with the default-2 / cap-10 / invalid-argument rules.
- **Cross-transport.** Engine `GetChunkContext(id, window) (*ContextResult, error)` (mirrors `GetChunk`'s `ChunkResult`); project to gRPC, REST, MCP tool (`go_rag_get_chunk_context`), and CLI (`go-rag chunk context <id> [--window N]`). Extend `parity_test.go`.
- **Errors.** `ErrInvalid` (empty id, window>10, negative window), `ErrNotFound` (missing/cross-vault id), mirroring `GetChunk`.
- **Constitution compliance to assert in the plan:** Principles II (no identity change — read-only), III (pure Go), V (all transports, one engine); Storage discipline (no new key prefix); Schema evolution (no migration, `ExpectedVersion` unchanged); Performance standards (sub-millisecond point lookups; bounded window keeps latency independent of corpus size).
