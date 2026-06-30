# Feature Specification: GetChunk — Fetch a Single Chunk by Content-Addressed ID

**Feature Branch**: `035-get-chunk-rpc` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-06-29

**Status**: Draft

**Input**: First item of the go-rag ↔ MuninnDB bridge integration backlog (`go-rag-bridge-backlog.md`, BL-001): *"`GetChunk` RPC — fetch a single chunk by its content-addressed ID."* The bridge's `ActivateWithRAG` pattern receives a MuninnDB engram carrying `metadata["chunk_id"]` and needs to resolve that ID back to the chunk in go-rag. There is currently no way to fetch a chunk by ID — only by listing all chunks on a document. `GetChunk` is the missing primitive that makes `chunk_id` a usable foreign key from MuninnDB (and any client) back into go-rag.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Resolve a chunk_id to its full chunk (Priority: P1)

A client — the go-rag-muninn bridge, another integration, an AI agent, or a developer — holds a `chunk_id` (returned from an earlier query, stored as a reference inside a MuninnDB engram, or captured from `GetDocument`). It needs to resolve that ID back to the actual chunk: its text, its position in the source document, and enough context to act on it (verify it still exists, re-promote it, display it, or link it). Today the only way to reach a specific chunk is to list every chunk on its parent document and search that list — which requires already knowing the document and pays a full scan. `GetChunk` turns `chunk_id` into a direct, single-call foreign-key lookup.

**Why this priority**: This is the primitive every other bridge integration pattern depends on. The bridge's `ActivateWithRAG` (pattern D1) cannot be implemented without it, and idempotency recovery after state-store loss is impossible if a stored `chunk_id` cannot be resolved back to data. It is the single item that unblocks the rest of Phase 1. Delivering this story alone is a viable MVP — a client can resolve any `chunk_id` it holds.

**Independent Test**: Ingest a document, obtain one of its chunk IDs, and fetch it via `GetChunk`; the returned content matches the chunk produced at ingestion. A `chunk_id` that was never ingested returns a clear not-found.

**Acceptance Scenarios**:

1. **Given** a chunk exists in a vault, **When** a client calls `GetChunk` with that chunk's ID, **Then** the full chunk (text, ordinal position, source path, page if paginated, section context, and all current metadata) is returned.
2. **Given** a chunk_id that does not exist in the vault, **When** a client calls `GetChunk`, **Then** a clear not-found result is returned (not an empty chunk, not an ambiguous error).
3. **Given** a chunk_id that exists but belongs to a different vault than the one named in the request, **When** a client calls `GetChunk`, **Then** a not-found result is returned — the chunk from the other vault is never disclosed (no cross-vault leakage).
4. **Given** a malformed or empty chunk_id, **When** a client calls `GetChunk`, **Then** a clear invalid-input result is returned without crashing the server.

---

### User Story 2 - Get the parent document's metadata in the same call (Priority: P2)

When the bridge resolves a chunk, it almost always needs to know which document the chunk came from — its source path, type, and ingestion/enrichment status — to decide how to promote it into MuninnDB. Forcing a second round-trip to fetch the document for every chunk fetch doubles the call count and adds latency to a hot path. `GetChunk` returns the parent document's metadata alongside the chunk, so a single call gives the caller everything needed to act.

**Why this priority**: Pure latency and call-count efficiency for the bridge's hot path. Valuable but secondary — Story 1 alone delivers the resolution primitive; this story removes the follow-up call. It is independently testable: assert the document metadata is present and correct in the same response.

**Independent Test**: Fetch a chunk by ID and assert the response also carries its parent document's metadata (source path, type, status), matching the document ingested — no second call required.

**Acceptance Scenarios**:

1. **Given** a chunk that belongs to an ingested document, **When** a client fetches it via `GetChunk`, **Then** the response includes the parent document's metadata as well as the chunk.
2. **Given** a chunk whose document has been re-ingested or enriched since the chunk_id was stored, **When** a client fetches it, **Then** the returned document metadata reflects the document's current state.

---

### User Story 3 - The same fetch from any transport (Priority: P3)

go-rag serves four transports over one engine (gRPC, REST, MCP, CLI) and the constitution requires every operation to be available across all of them with identical results. A chunk fetched by an MCP agent, a gRPC client, a REST caller, and a CLI user must be byte-for-byte the same chunk for the same ID — so `chunk_id` is a transport-independent reference. `GetChunk` is delivered as one primitive on the shared engine, projected onto all four transports, not as four separate fetches.

**Why this priority**: Parity is a universal invariant (Constitution Principle V), not unique to this feature — but for `GetChunk` specifically it is what makes the primitive usable by the bridge (gRPC), by AI agents (MCP), by scripts (CLI), and by HTTP clients (REST). It is the lowest-priority story because the resolution logic (Stories 1–2) is the hard part; projecting it onto the remaining transports is mechanical once the engine method exists.

**Independent Test**: Fetch the same chunk_id over gRPC, REST, MCP, and CLI and assert all four return identical chunk content and document metadata.

**Acceptance Scenarios**:

1. **Given** a valid chunk_id, **When** it is fetched over gRPC, REST, MCP, and CLI, **Then** all four return the same chunk and document metadata.
2. **Given** a missing chunk_id, **When** it is fetched over any transport, **Then** each transport surfaces an equivalent not-found result in its native form.

---

### Edge Cases

- **Stale chunk_id after re-ingest:** if a document's content changed and was re-chunked, the old chunk_id may no longer resolve — `GetChunk` returns not-found rather than silently returning a different chunk. (Content-addressed identity means an unchanged chunk keeps its ID; a changed chunk gets a new one.)
- **Quarantined / poisoned chunks:** `GetChunk` is a direct fetch-by-ID, not a ranked query. It returns the addressed chunk as stored regardless of its poisoning/quarantine verdict — poisoning is a query-time signal that affects `Query`, not deterministic point access. (The poisoning verdict, if present on the chunk, travels with it as metadata.)
- **Concurrent re-chunking during the fetch:** under the single-writer model, `GetChunk` returns the chunk as of its read; reads during writes are eventual-consistent.
- **Chunk belonging to a re-ingested document whose old chunks were replaced:** old IDs cease to resolve (not-found); new IDs resolve to the new chunks.
- **Empty or whitespace-only chunk_id:** invalid input, clear error, no server-side scan.
- **Empty vault in the request:** resolves to the default vault (see Assumptions); behaviour matches the daemon's existing vault-binding model.
- **Very large chunk:** returned in full, no truncation (it is the stored chunk, fetched verbatim).

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `GetChunk` operation that resolves a content-addressed `chunk_id` to the full stored chunk in a single call.
- **FR-002**: `GetChunk` MUST return a clear not-found result when the `chunk_id` does not exist in the requested vault.
- **FR-003**: `GetChunk` MUST return a not-found result — never the chunk — when the `chunk_id` exists but belongs to a different vault than the one in the request. Cross-vault disclosure is forbidden.
- **FR-004**: The `GetChunk` response MUST include the chunk's complete current metadata (text, ordinal position within the source document, source file path, page where applicable, section context, and any extensible metadata fields the chunk carries).
- **FR-005**: The `GetChunk` response MUST include the parent document's metadata in the same response, so resolving a chunk requires exactly one round-trip.
- **FR-006**: `GetChunk` MUST be exposed over all four transports — gRPC, REST, MCP, and CLI — returning identical results for the same `chunk_id` (cross-transport parity). *(Constitution Principle V — Extension by Interface, MCP-First.)*
- **FR-007**: `GetChunk` MUST be a constant-time point lookup whose latency is independent of corpus size (it must not scan documents or chunk lists). *(Constitution — Performance & Reliability Standards.)*
- **FR-008**: `GetChunk` MUST reuse the existing content-addressed chunk identity (SHA-256-derived). It introduces no new identity scheme and creates no new stored state. *(Constitution Principle II — Content-Addressed Identity.)*
- **FR-009**: `GetChunk` MUST surface a clear invalid-input result for a malformed or empty `chunk_id`, without performing a scan or crashing.
- **FR-010**: `GetChunk` MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies. *(Constitution Principle III.)*
- **FR-011**: `GetChunk` MUST NOT change the on-disk key-space layout — it is a read over existing stored data, so it triggers no schema migration and leaves `migrate.ExpectedVersion` unchanged. *(Constitution — Storage discipline / schema-version compliance: no on-disk layout change.)*

### Key Entities *(include if feature involves data)*

- **Chunk**: the unit of retrieved text — identified by its content-addressed `chunk_id`; carries the chunk text, its ordinal position in the source document, the source file path, page (for paginated formats), section context, and an extensible metadata map. `GetChunk` exposes the chunk already stored by ingestion; it computes nothing new.
- **Document Metadata (DocumentMeta)**: the parent document's metadata — its identity, source path, file type, ingestion status, and any document-level enrichment. Returned alongside the chunk so a caller has full context in one call.
- **chunk_id (foreign key)**: the content-addressed identifier that MuninnDB (or any external system) stores as a durable reference back into go-rag. `GetChunk` is what makes this ID a resolvable foreign key rather than an opaque string.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Any client — gRPC, REST, MCP, or CLI — can resolve a valid `chunk_id` to its full chunk plus parent document metadata in a single call, with identical results across all four transports.
- **SC-002**: A `chunk_id` that is missing, stale, or belongs to another vault yields a clear not-found result 100% of the time — never the wrong chunk, never an ambiguous error — so `chunk_id` is a trustworthy foreign key for MuninnDB and any integration.
- **SC-003**: `GetChunk` latency is independent of corpus size — a constant-time point lookup completing in single-digit milliseconds on a local store, whether the vault holds ten chunks or ten million.
- **SC-004**: The bridge's `ActivateWithRAG` pattern can be implemented using `GetChunk` alone, with no document listing or chunk scanning — chunk verification/promotion reduces to one round-trip per chunk.

---

## Assumptions

- **Direct fetch, not a query.** `GetChunk` is a deterministic point-fetch by content-addressed ID. It does not rank, filter, dedup, or apply query-time signals (poisoning/quarantine). It returns the addressed chunk as stored; poisoning remains a query-time concern that affects `Query`, not direct access.
- **Vault is optional; empty means default.** The request may name a vault; an empty vault resolves to the daemon's default vault, consistent with existing RPCs that do not take a vault parameter. The planner reconciles this with the daemon's actual vault-binding model (see Research Note).
- **Read-only, no schema change.** `GetChunk` exposes data go-rag already stores. It creates no new stored state and changes no on-disk layout, so no migration is required and `migrate.ExpectedVersion` is unchanged.
- **Response shapes are projections of existing internal models.** The `Chunk` and document-metadata response shapes are the transport projection of go-rag's existing internal chunk and document models. Their exact field set is finalized in the plan phase.
- **Metadata fields expand downstream.** The metadata fields reserved for backlog items BL-004 (wikilinks), BL-005 (section heading), and BL-006 (extraction quality) are additive; `GetChunk` returns the chunk's complete current metadata, and those fields appear as their respective items land.
- **Identity unchanged.** `chunk_id` is the existing SHA-256-derived content-addressed identity; `GetChunk` introduces no new identity scheme and no new runtime dependencies (pure Go, `CGO_ENABLED=0`).

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

> This spec specifies the WHAT and the WHY. The HOW — message shapes, vault contract, read path — is finalized in `/speckit-plan`, grounded in the go-rag codebase and the bridge backlog.

The planner MUST answer, grounded in `internal/engine`, `internal/storage`, `proto/gorag.proto`, and `go-rag-bridge-backlog.md`:

1. **Vault-binding model.** How do the existing RPCs that take no `vault` parameter (e.g., `Add`, `Query`, `ReleaseChunk`, `ResetChunk`) scope their operation — a single daemon-bound vault, or all vaults? How does `ListVaults` relate? From that, finalize `GetChunk`'s vault contract (required vs. optional-with-default) and decide whether the existing chunk-scoped RPCs should gain a `vault` parameter for consistency. *(The backlog's `GetChunkRequest` includes `vault`; the existing `ReleaseChunk`/`ResetChunk` do not — this mismatch must be resolved.)*
2. **Chunk & DocumentMeta transport messages.** Define the `Chunk` and `DocumentMeta` response messages as the projection of the existing internal `model.Chunk` and document-metadata structures. Confirm the field set, including how `page`, `section_context`, poisoning verdict, and enrichment status map onto the response (the existing `QueryHit` is the closest analogue).
3. **Read path & latency.** Confirm `GetChunk` resolves to a single Pebble point read (no scan), sits on the engine's read path, and that its latency is genuinely corpus-size-independent and within the read budget.
4. **Not-found mapping per transport.** Confirm the not-found mapping: gRPC `NOT_FOUND`, REST `404`, MCP structured not-found, CLI non-zero exit with a clear message — and the invalid-input mapping for malformed IDs.
5. **Scope boundary.** Confirm `GetChunk` depends on nothing else in Phase 1 (BL-002 context-window and BL-003 batch-fetch build *on top of* it but are separate specs). Confirm it requires no key-space change and therefore no migration.
