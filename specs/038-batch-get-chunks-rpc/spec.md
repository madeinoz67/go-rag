# Feature Specification: BatchGetChunks — Fetch Many Chunks by ID in One Call

**Feature Branch**: `038-batch-get-chunks-rpc` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-07-01 · **Status**: Draft

**Input**: Third item (BL-003) of the go-rag ↔ MuninnDB bridge integration backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`), Phase 1. *The bridge sync worker calls `GetDocument` to retrieve a document's chunk-ID list, then needs the content of all those chunks to build `RememberItem` structs for MuninnDB's `BatchRemember`. With no batch fetch endpoint, the worker serialises one read per chunk — 50 round-trips for a 50-chunk document. `BatchGetChunks` resolves up to 100 chunk IDs in a single call, returning each chunk (or a per-id error) in request order.*

> **Grounded like spec 035 (BL-001) and spec 037 (BL-002).** Two deltas from the backlog's BL-003 draft, per the engine's actual conventions (the same two deltas GetChunk/GetChunkContext made):
> 1. **No `vault` field** on the request — the engine is single-vault-per-process; vault is a connection-time concern (`--vault`/`--db-path`), not a per-call field. A `chunk_id` that belongs to a different vault is simply "not found" in this single-vault store (no cross-vault leakage).
> 2. **REST path is `POST /v1/chunks/batch`** with a JSON body `{ "chunk_ids": […] }` — the existing `/v1/<resource>` convention (no `/api/` base, no per-vault segment). POST (not GET) because the chunk-ID list is a request body; a GET with up to 100 IDs in a query string is unbounded.

**Why this item next:** BL-001 (`GetChunk`, spec 035), BL-004 (`Wikilinks`, spec 036), and BL-002 (`GetChunkContext`, spec 037) are shipped. `BatchGetChunks` completes the GetChunk family — **get-one → get-window → get-batch** — and is the direct enabler of the bridge's bulk-sync path (the last P1 chunk-access primitive the MuninnDB `BatchRemember` consumer needs). It depends only on BL-001 (the single-chunk read is the inner loop), is size-S, and reuses `lookupChunk` + the spec-035 `Chunk` projection verbatim.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Resolve many chunks by ID in one call, with per-id tolerance (Priority: P1)

A client — the go-rag↔MuninnDB bridge sync worker, an integration, an agent, or a developer — holds a list of chunk IDs (e.g. a document's full chunk-ID list) and needs all their content in one round-trip. Today the only option is to loop `GetChunk` once per ID — N round-trips, N chances for a partial view, and no way to distinguish "done" from "mid-batch." `BatchGetChunks` takes up to 100 IDs and returns one result per requested ID, **in the same order**, reusing the exact chunk projection `GetChunk` returns. Crucially, a missing ID does **not** fail the call: it yields a result entry with an empty chunk and a clear per-id error, so a caller requesting a stale or cross-vault ID alongside live ones still gets the live ones.

**Why this priority**: This is the primitive the bridge's `BatchRemember` sync path cannot be built without — and the per-id-error model (partial success) is the part `GetChunk`/`GetChunkContext` cannot provide (they fail the whole call on a missing ID). Delivering this story alone is a viable MVP: a client can fetch any batch of chunks in one round-trip and handle missing IDs positionally.

**Independent Test**: Ingest a multi-chunk document, obtain its chunk IDs, and call `BatchGetChunks` with that list plus one fabricated (missing) ID; the response has one result per requested ID in request order, the live IDs carry their full chunks, and the fabricated ID carries an empty chunk with `error = "not found"` — the call itself succeeds.

**Acceptance Scenarios**:

1. **Given** a list of valid chunk IDs that exist in the vault, **When** a client calls `BatchGetChunks(chunk_ids)`, **Then** the response contains exactly one result per requested ID, in the same order, and each carries its full chunk (content, ordinal position, source path, page, `SectionContext`, `Wikilinks` per spec 036 — identical to `GetChunk`).
2. **Given** a request list containing a `chunk_id` that does not exist (or belongs to a different vault), **When** a client calls `BatchGetChunks`, **Then** that position returns a result with an empty chunk and a non-empty `error` ("not found"), every other position returns its chunk normally, and the call itself does **not** error (partial success).
3. **Given** a `chunk_id` that exists in a different vault, **When** called against this single-vault store, **Then** "not found" is returned for it — the other vault's chunk is never disclosed (no cross-vault leakage).

---

### User Story 2 - Bounded, validated batches — correct at every edge (Priority: P2)

A batch fetch must reject unbounded or malformed input rather than silently truncate or do unbounded work, and must honour the documented cap. `BatchGetChunks` caps the list at 100 IDs; a request with more than 100, an empty list, or any empty/whitespace-only ID is rejected with a clear invalid-argument result before any lookup. Duplicate IDs in the request are resolved independently and positionally (no de-duplication — the response is 1:1 with the request). The fetch is one logical read (a Pebble batch / N point-gets in a single read), not N independent caller round-trips.

**Why this priority**: Correctness of the batch contract — the cap, the empty-list rejection, the per-id-error shape, and the single-read guarantee. The MVP (Story 1 — "the batch is returned") is the unblocker; this story pins the boundaries that make the batch trustworthy and bounded. Independently testable: exercise >100 IDs, the empty list, an empty/whitespace ID, and duplicates directly.

**Independent Test**: Call `BatchGetChunks` with 101 IDs → invalid-argument. With `[]` → invalid-argument. With `["valid-id", "  ", "valid-id"]` → invalid-argument. With `["a","b","a"]` (duplicate) → three results, positions 0 and 2 both carrying chunk "a".

**Acceptance Scenarios**:

1. **Given** a request with more than 100 chunk IDs, **When** a client calls `BatchGetChunks`, **Then** a clear invalid-argument result is returned (the list is capped at 100, never silently truncated above it), with no lookup performed.
2. **Given** an empty `chunk_ids` list, **When** a client calls `BatchGetChunks`, **Then** a clear invalid-argument result is returned.
3. **Given** a list containing an empty or whitespace-only `chunk_id`, **When** a client calls `BatchGetChunks`, **Then** a clear invalid-argument result is returned (no lookup).
4. **Given** a list with duplicate chunk IDs, **When** a client calls `BatchGetChunks`, **Then** the response has one result per requested position (no de-duplication); each duplicate position carries the same resolved chunk.
5. **Given** a valid bounded batch, **When** a client calls `BatchGetChunks`, **Then** the fetch is one logical read returning a consistent snapshot — not N independent caller round-trips.

---

### User Story 3 - The same batch fetch from any transport (Priority: P3)

A batch fetched over gRPC, REST, MCP, or CLI must be byte-for-byte identical — the same results in the same order, the same per-id errors, the same chunk projection. go-rag serves four transports over one engine, and the constitution requires every operation available across all of them with identical results. `BatchGetChunks` is one operation on the shared engine, projected onto all four transports.

**Why this priority**: Parity is a universal invariant (Constitution Principle V), not unique to this feature — but for `BatchGetChunks` it is what makes the batch usable by the bridge (gRPC), by agents (MCP), by scripts (CLI), and by HTTP clients (REST). It is the lowest-priority story because the batch logic (Stories 1–2) is the hard part; projecting it is mechanical.

**Independent Test**: Fetch the same `chunk_ids` list (including one missing ID) over gRPC, REST, MCP, and CLI; assert all four return identical result lists (same per-position chunk IDs, same per-position error string).

**Acceptance Scenarios**:

1. **Given** a list of chunk IDs (including a missing one), **When** fetched over gRPC, REST, MCP, and CLI, **Then** all four return the same per-position results — same chunks for live IDs, same "not found" error for the missing one.
2. **Given** an invalid request (>100 IDs / empty list / empty ID), **When** submitted over any transport, **Then** each surfaces an equivalent invalid-argument result in its native form.

---

### Edge Cases

- **All IDs missing:** every position returns `error = "not found"` with an empty chunk; the call still succeeds (no top-level error).
- **Duplicate IDs:** resolved positionally — `["a","a"]` returns two identical chunk results; no de-duplication, no collapse.
- **Single ID:** equivalent to `GetChunk` but wrapped in the batch result shape (one result entry); a valid single-id batch succeeds.
- **100 IDs (the cap):** accepted; 101 rejected. The cap is inclusive of 100.
- **Stale ID after re-ingest:** an old `chunk_id` whose chunk content changed no longer resolves → "not found" (content-addressed identity means an unchanged chunk keeps its ID; a changed chunk gets a new one).
- **Orphan chunk (parent document removed):** the chunk itself still resolves (it lives under prefix `0x03`); the per-result `DocumentMeta` is omitted/zero-valued — tolerant, not an error (mirrors `GetChunk`, spec 035). *(If the batch shape proves per-result, a document may be returned once per chunk; see Research Note.)*
- **Quarantined / poisoned chunks:** returned as stored (a batch fetch is a deterministic point access, not a ranked query); poisoning is a query-time signal, consistent with `GetChunk`/`GetChunkContext`.
- **Order preservation under concurrency:** results are 1:1 with the request order regardless of internal read ordering.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `BatchGetChunks(chunk_ids)` operation that resolves up to 100 content-addressed `chunk_id`s and returns one result per requested ID in a single call.
- **FR-002**: The response MUST contain exactly one result per requested `chunk_id`, **in the same order** as the request (positional, 1:1) — including for duplicates and missing IDs.
- **FR-003**: A missing or cross-vault `chunk_id` MUST yield a result with an empty chunk and a non-empty `error` ("not found"); the call itself MUST NOT error (partial success). This per-id-error model is the key delta from `GetChunk`/`GetChunkContext` (which return a top-level NOT_FOUND).
- **FR-004**: A request with more than 100 `chunk_ids` MUST return an invalid-argument result (the cap is 100 inclusive), with no lookup performed.
- **FR-005**: An empty `chunk_ids` list MUST return an invalid-argument result.
- **FR-006**: A `chunk_ids` list containing any empty or whitespace-only ID MUST return an invalid-argument result, without performing a lookup.
- **FR-007**: Duplicate `chunk_id`s in the request MUST each be resolved independently and positionally (no de-duplication); the response is 1:1 with the request.
- **FR-008**: Every resolved chunk MUST carry its complete current metadata — content, ordinal position, source path, page where applicable, `SectionContext` (spec 025), `Wikilinks` (spec 036), and any other sidecars — identical to what `GetChunk` returns for that chunk.
- **FR-009**: The fetch MUST be a single logical read returning a consistent snapshot of the requested chunks — not N independent caller round-trips. *(Constitution — Performance & Reliability Standards.)*
- **FR-010**: `BatchGetChunks` MUST be exposed over all four transports — gRPC, REST, MCP, and CLI — returning identical results for the same `chunk_ids` (cross-transport parity). *(Constitution Principle V — Extension by Interface, MCP-First.)*
- **FR-011**: The REST endpoint MUST be `POST /v1/chunks/batch` with a JSON body `{ "chunk_ids": ["…","…"] }` (mirrors the `/v1/<resource>` convention; no per-vault URL segment, no `/api/` base).
- **FR-012**: `BatchGetChunks` MUST reuse the existing `lookupChunk` helper and the spec-035 `Chunk` (and `DocumentMeta`) projection. It introduces no new identity scheme and no new stored state. *(Constitution Principle II; spec 035 reuse.)*
- **FR-013**: `BatchGetChunks` MUST be a pure read: no on-disk key-space layout change, no schema migration, `migrate.ExpectedVersion` unchanged. *(Constitution — Storage discipline / schema-version compliance.)*
- **FR-014**: `BatchGetChunks` MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies. *(Constitution Principle III.)*
- **FR-015**: The request MUST NOT carry a `vault` field — the engine is single-vault-per-process (spec 035 convention); vault is a connection-time concern.

### Key Entities *(include if feature involves data)*

- **Chunk**: the unit of retrieved text, identified by its content-addressed `chunk_id`. Read as-is by each `lookupChunk` in the batch; the batch returns full chunks identical to `GetChunk`. Unchanged by this feature.
- **BatchGetChunksResult**: the per-id positional entry — the requested `chunk_id`, the resolved `Chunk` (zero-value when not found), and a non-empty `error` string when that ID failed. A read-only projection — no new stored structure.
- **DocumentMeta**: the parent document's metadata projection. The same projection `GetChunk` returns (spec 035); whether it is attached per-chunk or returned once per distinct document is settled in the plan (see Research Note).

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Any client — gRPC, REST, MCP, or CLI — can resolve up to 100 chunk IDs in a single call, with identical results across all four transports, and one result per requested ID in request order.
- **SC-002**: The bridge's bulk-sync path (`BatchRemember`) can be implemented using `BatchGetChunks` alone — no client-side loop of N `GetChunk` calls — reducing a 50-chunk document sync from 50 round-trips to 1.
- **SC-003**: A batch containing any mix of live, missing, duplicate, and cross-vault IDs returns correctly for every position (live IDs resolve, missing/cross-vault IDs report "not found"), and the call never fails the whole batch for one bad ID.
- **SC-004**: Unbounded/malformed input — >100 IDs, an empty list, an empty/whitespace ID — is rejected with a clear invalid-argument result 100% of the time, before any lookup.
- **SC-005**: The feature lands as a pure read over existing stored data — no on-disk schema change, no migration, no new runtime dependency, no change to any `chunk_id`.

---

## Assumptions

- **Pure read, no schema change.** `BatchGetChunks` reads chunk records go-rag already stores (prefix `0x03`). It creates no new stored state and changes no on-disk layout, so no migration is required and `migrate.ExpectedVersion` is unchanged.
- **Reuses spec 035.** The `Chunk`/`DocumentMeta` proto messages, the `lookupChunk` engine helper, and the cross-transport projection pattern are all established by spec 035 (BL-001). `BatchGetChunks` composes them; it does not reinvent them.
- **Single-vault-per-process.** No `vault` field on the request; "not found" is the cross-vault-isolation path (the other vault's chunk simply isn't in this store), consistent with `GetChunk` (spec 035 FR-003).
- **Cap 100 inclusive.** Mirrors the backlog draft; bounds response size and read cost. 100 accepted, 101 rejected.
- **Per-id error, not top-level error.** The batch never fails the whole call for a missing/invalid individual ID (that is the point of a batch); only structurally invalid requests (oversized, empty, empty-element) fail at the call level.
- **Order-preserving, 1:1, no de-dup.** The response is positional to the request; duplicates are resolved independently. This makes the result trivially correlatable for the caller (result[i] ↔ request[i]).
- **Single logical read.** "One call" means one caller round-trip returning a consistent view; internally it may be N point-`Get`s within one Pebble read (the plan confirms the mechanism — Pebble `NewBatch`/`Get` loop, or a `MultiGet` if available).

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

- **Confirm the read mechanism.** Decide between (a) N `lookupChunk` point-`Get`s in a loop within one logical read, or (b) a Pebble `MultiGet`/batch read if the storage wrapper exposes one. Either MUST satisfy FR-009 (one logical read, consistent snapshot). Verify whether `storage.DB` exposes a multi-get/batch helper or whether the loop-of-`GetWithPrefix` is the idiomatic path (the wrapper currently exposes `GetWithPrefix` point-gets).
- **Reuse the spec-035 `Chunk` message + projection.** `BatchGetChunksResult` carries the spec-035 `Chunk` (which already includes `Wikilinks` per spec 036). Confirm no new proto message is needed beyond the request/response/result wrappers.
- **DocumentMeta shape — open question for the plan.** Two options: (a) attach `DocumentMeta` per-chunk in each `BatchGetChunksResult` (simple, mirrors `GetChunk` 1:1, but repeats the same document for sibling chunks); (b) return a separate `map<chunk_id, DocumentMeta>` or a deduped document list (compact, but diverges from `GetChunk`'s shape and complicates parity). Recommendation: **(a) per-result `DocumentMeta`** for parity simplicity and `GetChunk`-shape consistency — the plan decides. *(If (a), add a `DocumentMeta document = 4` field to `BatchGetChunksResult`.)*
- **Proto field tags.** `BatchGetChunksRequest { repeated string chunk_ids = 1; }` (no vault). `BatchGetChunksResult { string chunk_id = 1; Chunk chunk = 2; string error = 3; }` (+ optional `DocumentMeta document = 4` per the shape decision). `BatchGetChunksResponse { repeated BatchGetChunksResult results = 1; }`. Add `rpc BatchGetChunks(...)` to the `Gorag` service after `GetChunkContext`.
- **REST route.** `POST /v1/chunks/batch` with body `{ "chunk_ids": […] }` — register in the REST router; parse + validate (count ≤100, no empty/whitespace elements, non-empty list) before lookup. Returns 200 with the results array (per-id errors in-band), 400 for structural invalid-argument.
- **Cross-transport.** Engine `BatchGetChunks(ids []string) (*BatchResult, error)` — returns a `BatchResult{ Results []BatchItem }` where each `BatchItem{ ChunkID, Chunk, Err string }` (and optionally Document). Project to gRPC, REST, MCP tool (`go_rag_batch_get_chunks`), and CLI (`go-rag chunk batch <id> [<id>…]`). Extend `parity_test.go`.
- **Errors.** Call-level: `ErrInvalid` (empty list, >100, empty/whitespace element). Per-id: in-band `error` string ("not found") — NOT a top-level `ErrNotFound` (the call succeeds). Confirm the REST handler returns 200 (not 404) when some IDs are missing — 404 is reserved for call-level failure, which this operation never raises for missing IDs.
- **Constitution compliance to assert in the plan:** Principles II (no identity change — read-only), III (pure Go), V (all transports, one engine); Storage discipline (no new key prefix); Schema evolution (no migration, `ExpectedVersion` unchanged); Performance standards (≤100 sub-millisecond point-`Get`s; latency independent of corpus size).
