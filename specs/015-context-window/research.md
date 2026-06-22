# Phase 0 — Research: Context Window (H15)

> Grounded in: `internal/pipeline/pipeline.go:206-222` (chunk construction — **confirmed
> PreviousChunkID/NextChunkID are NOT set**), `internal/model/model.go:80-81` (the fields exist on
> Chunk), `internal/engine/query.go` (result-building loop — where context expansion goes).

## 1. Linked list is unpopulated — populate in processFile

**Decision**: After the chunk-construction loop in `pipeline.processFile` (which builds `chunks []model.Chunk` from segments), add a second loop that sets `chunks[i].PreviousChunkID = chunks[i-1].ID` (for i>0) and `chunks[i].NextChunkID = chunks[i+1].ID` (for i<len-1). This populates the linked list before `storeDocument` persists the chunks.

**Rationale**: The fields exist on the struct and in the JSON record shape (they're just always empty today). Populating them is a 3-line addition to the existing loop — no schema change, no migration, no new key. The linked list is per-document (within one `processFile` call), so the ordering is known (chunk index i).

**Alternatives**: *Populate at query time* (resolve siblings by ChunkIndex + DocumentID scan) — heavier (a prefix scan per hit); the linked list is cheaper (direct ID lookup). Rejected.

## 2. Context expansion in Engine.Query — post-ranking, per-hit

**Decision**: After the result-building loop in `Engine.Query` (which produces `[]QueryHit`), if `req.ContextWindow > 0`, iterate each hit and follow its chunk's `PreviousChunkID`/`NextChunkID` chain up to N steps each way, fetching sibling chunk text via `lookupChunk(e.db, id)`. Attach the siblings to a new `QueryHit.Context` field.

**Rationale**: Context expansion is purely additive (after ranking/rerank) — it doesn't affect the candidate pool, fusion, collapse, or ranking. The linked-list chain gives O(N) Pebble lookups per hit (negligible for small N). `lookupChunk` already exists in the engine helpers.

**Alternatives**: *Expand at the Retrieval layer* (inside Search/SearchWithRerank) — couples retrieval to context expansion and the DB; the engine is the right layer (it owns the DB + the result shape). Rejected.

## 3. Result shape: Context on QueryHit, clearly distinguishable

**Decision**: Add a `Context []ContextChunk` field to `QueryHit` (engine/types.go), where `ContextChunk { ChunkID, Content, Direction }` carries the sibling text and whether it's "previous" or "next" relative to the hit. The primary hit's Content stays as-is; context is separate. `Context` is nil when ContextWindow=0 (zero serialization overhead).

**Rationale**: US2 requires context be distinguishable from the hit (not flattened into ranked results). A separate field on QueryHit is the clean shape. Transports serialize it optionally (`omitempty`).

## 4. ContextWindow semantics: N siblings each side

**Decision**: `ContextWindow=N` fetches up to N previous + N next siblings per hit (total up to 2N context chunks). N=0 = off (default). The chain follows the linked list from the hit outward (previous N, next N independently). Missing siblings (first/last chunk, or empty IDs) are simply absent — no error.

**Rationale**: Symmetric expansion (N each side) is the intuitive semantic ("show me N chunks of context around each hit"). Independent previous/next handles boundary chunks gracefully.

## 5. Transport exposure: mirror H14 (filter) — request + response fields

**Decision**: CLI `--context-window N`; REST `context_window` request field + `context` response field; gRPC proto `int32 context_window` on QueryRequest + `repeated ContextChunk context` on QueryHit; MCP `context_window` in schema + context in the result rendering. Proto regen needed.

**Rationale**: Cross-transport parity (spec 003, Principle V). The response carries context alongside each hit; transports serialize it identically.

## 6. Re-ingestion needed for existing vaults

**Decision**: Since PreviousChunkID/NextChunkID are currently empty in persisted chunks, existing vaults need re-ingestion (Reprocess/migrate) to populate the linked list. Context expansion on un-populated chunks gracefully returns no context (empty IDs → no siblings). This is documented (same pattern as H10's re-chunk note).

**Rationale**: The linked-list values are computed at chunk-creation time; old chunks lack them. The expansion code handles empty IDs gracefully (no crash), so the feature degrades correctly on old vaults. Re-ingestion populates them.
