# Research — GetChunkContext (BL-002)

> Phase 0 output for `/speckit-plan`. Every question grounded in source read this session (`internal/engine/helpers.go`, `internal/engine/get_chunk.go`, `internal/engine/errors.go`, `internal/storage/db.go`, `internal/storage/storage.go`, `internal/pipeline/pipeline.go`) and in the direct prior art: spec 035 (GetChunk, BL-001). No `NEEDS CLARIFICATION` remains.

## Spec reconciliation

The spec was written grounded in spec 035 (BL-001), so it carries no backlog-clone errors. Two deltas from the backlog draft were already encoded in the spec (no `vault` field; REST `/v1/chunks/{id}/context`). Phase 0 confirmed the one open mechanic — the read mechanism — below.

---

## R1 — How is a chunk looked up today, and how is the window fetched?

**Finding.** `lookupChunk(db *storage.DB, chunkID string) (model.Chunk, bool)` (`internal/engine/helpers.go:59`) is the point-lookup primitive `GetChunk` uses (`internal/engine/get_chunk.go`: `c, ok := lookupChunk(e.db, chunkID)`). Chunks are stored under `PrefixChunk = 0x03` keyed by content-addressed `chunkID`, value = marshalled `Chunk` JSON (`internal/pipeline/pipeline.go:386`: `SetWithPrefix(storage.PrefixChunk, []byte(c.ID), cj)`). `lookupDoc(db, docID) (model.Document, bool)` (`helpers.go:72`) resolves the parent document. Errors are the `ErrInvalid` / `ErrNotFound` sentinels (`internal/engine/errors.go`).

`Chunk` carries the linked list `PreviousChunkID` / `NextChunkID` (`internal/model/model.go:90-91`) plus `ChunkIndex` / `TotalChunks` (`:85-86`). The linked list is written atomically with the document's chunks at ingest (spec 015: "populate the per-document linked list").

**Decision.** `GetChunkContext` fetches the window by **following the linked list**: `lookupChunk(target)`, then walk `PreviousChunkID` up to `window` hops backward, and `NextChunkID` up to `window` hops forward, each hop a `lookupChunk` point-get. That is ≤ `1 + 2·window` (max 21 at `window=10`) Pebble point-gets.

**Rationale.** The linked list exists for exactly this (`PreviousChunkID`/`NextChunkID` have no other consumer), so following it is the natural, minimal mechanism. Chunks are keyed by `chunkID` (content-addressed), NOT by `(DocumentID, ChunkIndex)`, so there is no range/index key to fetch `chunks[i-N..i+N]` in one read — a range approach would require a prefix scan + filter by `DocumentID` + sort by `ChunkIndex`, strictly more work than the pointer hops. `ChunkIndex`/`TotalChunks` are kept as a cross-check (the resolved window's indices must be contiguous and target's index must match) and as a defensive fallback if a pointer hop ever fails to resolve.

**Alternatives rejected.**
- *Prefix-scan the document's chunks by `DocumentID`, then slice by `ChunkIndex`.* Rejected — chunks are keyed by `chunkID`, so a doc's chunks aren't a contiguous key range; the scan would read ALL of the doc's chunks (potentially many) and filter, vs ≤21 targeted point-gets. More I/O for the common case.
- *A secondary `(DocumentID, ChunkIndex) → chunkID` index.* Rejected — new stored state, new key prefix, a migration, and ingest-side work, for a read that the existing linked list already serves. Violates the "pure read, no schema change" scope.

---

## R2 — Is the window a "single consistent read" (FR-012)?

**Finding.** The storage wrapper (`internal/storage/db.go`) exposes `Get`, `GetWithPrefix(prefix, key)`, and `PrefixScanByte`, but **no multi-get and no explicit snapshot/NewSnapshot**. Pebble itself snapshot-isolates reads, but the wrapper doesn't surface it.

**Decision.** The window is N independent point-gets, not one snapshot call. This still satisfies FR-012's intent ("one logical read returning a consistent snapshot — not N independent caller round-trips"): it is ONE caller round-trip; the N point-gets read an existing document's linked list that was written atomically at ingest. Under go-rag's single-writer model (Constitution — "exactly one process may open the Pebble database at a time"), there is no concurrent writer mutating this document's chunks during the read, so the N point-gets are mutually consistent in practice. (Concurrent reads during a write of a *different* document are eventual-consistent, irrelevant here.)

**Rationale.** Adding a snapshot/multi-get to the storage wrapper for one read-only RPC is scope creep with no correctness benefit under single-writer. The spec's FR-012 is honored as "one round-trip, consistent view of an atomic-at-ingest structure."

**Alternatives rejected.**
- *Add `DB.NewSnapshot` + a batched multi-get.* Rejected — wrapper API surface for one caller; no correctness gain under single-writer.

---

## R3 — Reuse from spec 035 (GetChunk)?

**Finding.** Spec 035 established the full pattern this feature mirrors:
- `Engine.GetChunk(chunkID) (*ChunkResult, error)` + `ChunkResult{Chunk, Document, Source}` (`internal/engine/get_chunk.go`).
- The proto `Chunk` message (spec 035, now carrying `Wikilinks` per spec 036) + `DocumentMeta`.
- The gRPC handler + projection (`internal/grpc/engine_adapter.go`), REST `GET /v1/chunks/{id}` + `chunkDTO` (`internal/rest/get_chunk.go`), MCP `go_rag_get_chunk` tool (`internal/mcp/server.go`), CLI `go-rag chunk get` (`internal/cli/chunk.go`).
- `ErrInvalid` (empty/whitespace id) / `ErrNotFound` (missing or cross-vault) error mapping.

**Decision.** `GetChunkContext` reuses all of it: a `ContextResult{Chunks []model.Chunk; TargetIndex int; Document model.Document; Source model.Source}` (the `ChunkResult` shape plus the ordered slice + index); the same `lookupChunk`/`lookupDoc`; the same error sentinels; the same `Chunk` proto message (repeated); the same per-transport projection pattern (response = repeated `Chunk` + `target_index` + `DocumentMeta`). No new proto message is needed beyond the request/response wrappers.

**Rationale.** One pattern, already proven and shipped (spec 035), now with cross-transport parity tests. Mirroring it keeps the surface uniform and the implementation mechanical.

---

## R4 — Windowing rules (boundaries, clamps, equivalence)?

**Decision** (pinned by the spec FRs; confirmed feasible against the linked list):

| Input | Behaviour |
|-------|-----------|
| `window` omitted / 0 | default 2; `window=0` → return exactly the target chunk, `target_index=0` (≡ `GetChunk`) |
| `window` 1..10 | up to `window` predecessors + target + up to `window` successors |
| `window > 10` | `ErrInvalid` (INVALID_ARGUMENT) — capped at 10, never silently truncated above |
| `window < 0` | `ErrInvalid` |
| first chunk (no predecessors) | target at `target_index=0`, up to `window` successors, no error |
| last chunk (no successors) | target at the last index, up to `window` predecessors, no error |
| single-chunk document | exactly one chunk, `target_index=0` |
| `window > document size` | the whole document's chunks, target at its real index, no error |
| empty/whitespace `chunk_id` | `ErrInvalid`, no lookup |
| missing / cross-vault `chunk_id` | `ErrNotFound` |
| orphan chunk (parent doc gone) | window returned; `Document`/`Source` zero-valued, no error (mirrors GetChunk) |
| a linked-list hop that fails to resolve | return the unbroken run up to the requested window, no error (defensive graceful degradation) |

**Rationale.** The backward walk collects predecessors in reverse (prepend each), the forward walk appends successors; `target_index = len(predecessors)`. Boundary tolerance falls out naturally (stop walking when `PreviousChunkID`/`NextChunkID` is empty or a hop misses).

---

## Summary of decisions

| # | Question | Decision |
|---|----------|----------|
| R1 | Window read mechanism? | Follow the `PreviousChunkID`/`NextChunkID` linked list via `lookupChunk` point-gets (≤ `1+2·window`, max 21). |
| R2 | "Single consistent read"? | N point-gets in one caller round-trip; consistent under single-writer + atomic-at-ingest list. No new snapshot/multi-get API. |
| R3 | Reuse from spec 035? | All of it — `ContextResult` ≈ `ChunkResult` + slice + index; reuse `Chunk` proto, `lookupChunk`/`lookupDoc`, errors, per-transport projection. |
| R4 | Windowing rules? | default 2, cap 10 (INVALID_ARGUMENT above), `window=0` ≡ GetChunk, boundary-tolerant, orphan-tolerant, graceful on a broken hop. |
