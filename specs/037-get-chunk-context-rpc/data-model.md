# Data Model — GetChunkContext (BL-002)

> Phase 1 output for `/speckit-plan`. No new persisted entities — the feature is a read-only projection over existing chunks. See [research.md](./research.md) (R1–R4) for the read mechanism and [contracts/api.md](./contracts/api.md) for the wire shape.

## Entity: `ContextResult` (engine projection, new — not persisted)

`internal/engine/` — the engine-level result of `GetChunkContext`, mirroring spec-035's `ChunkResult` plus the ordered window and target index.

```go
// ContextResult is the engine projection returned by GetChunkContext (spec 037,
// BL-002): the ordered context window centred on the requested chunk, the
// requested chunk's index within that window, and the parent document (+
// optional source). Mirrors ChunkResult (spec 035). Document/Source are
// zero-valued when absent — GetChunkContext tolerates an orphan chunk.
type ContextResult struct {
	Chunks      []model.Chunk // ordered [predecessors…] [target] [successors…]
	TargetIndex int           // index of the requested chunk within Chunks
	Document    model.Document
	Source      model.Source
}
```

**Windowing invariants**

| Property | Rule |
|----------|------|
| Ordering | document order — predecessors in ascending order, then target, then successors in ascending order |
| `TargetIndex` | `== len(predecessors)` (0 when the target is the first chunk / `window=0`) |
| Length | `min(window, #predecessors) + 1 + min(window, #successors)`; ≤ `2·window+1` |
| `window` default | 2 (when omitted) |
| `window=0` | exactly `[target]`, `TargetIndex=0` (≡ `GetChunk`) |
| `window` cap | 10 — `>10` or `<0` → `ErrInvalid` before any lookup |
| Boundaries | return as many neighbours as exist; never an error |
| Orphan chunk | `Document`/`Source` zero-valued; window still returned; never an error |
| Broken hop | return the unbroken run; never an error (defensive) |

## Resolution algorithm (engine, read-only)

```
validate chunkID (non-empty) else ErrInvalid
validate window (0..10) else ErrInvalid        // default 2 when omitted
target, ok := lookupChunk(db, chunkID)
if !ok return ErrNotFound                       // missing or cross-vault (single-vault store)

// Walk backward via PreviousChunkID.
var predecessors []model.Chunk
cur := target
for i := 0; i < window && cur.PreviousChunkID != ""; i++ {
    p, ok := lookupChunk(db, cur.PreviousChunkID)
    if !ok { break }                            // defensive — broken linked list
    predecessors = append([]model.Chunk{p}, predecessors...) // prepend → ascending order
    cur = p
}
// Walk forward via NextChunkID.
var successors []model.Chunk
cur = target
for i := 0; i < window && cur.NextChunkID != ""; i++ {
    nx, ok := lookupChunk(db, cur.NextChunkID)
    if !ok { break }
    successors = append(successors, nx)
    cur = nx
}

chunks := append(append(predecessors, target), successors...)
targetIndex := len(predecessors)

document, _ := lookupDoc(db, target.DocumentID) // tolerant — zero value if orphan
// optional source read (mirrors GetChunk)

return &ContextResult{Chunks: chunks, TargetIndex: targetIndex, Document: document, ...}
```

**Cost:** ≤ `1 + 2·window` `lookupChunk` point-gets (max 21) + 1 `lookupDoc`. Each is a Pebble point-get over prefix `0x03` (sub-millisecond). No scan, no write.

## Reused entities (unchanged)

- **Chunk** (`internal/model/model.go:80`) — carries `PreviousChunkID` / `NextChunkID` (the linked list), `ChunkIndex` / `TotalChunks`, and all sidecars (`SectionContext`, `Wikilinks`, `Poisoning`, …). Read as-is by every `lookupChunk` hop; the window returns full chunks identical to what `GetChunk` returns.
- **Document / Source** — resolved via `lookupDoc` for the parent metadata; zero-valued when absent (orphan-tolerant).

## Identity & storage invariants (constitution Principle II)

- **No new stored state.** `GetChunkContext` is a pure read over prefix `0x03`. It introduces no new key, no new prefix, no new struct that is persisted.
- **On-disk layout**: unchanged. No migration; `migrate.ExpectedVersion` unchanged.
- **Identity**: read-only — no `chunk_id` is created or changed.

## Validation rules (map to FRs)

- **FR-001/FR-002**: returns `[predecessors][target][successors]` in document order with `target_index`.
- **FR-003**: `window` defaults to 2; `window=0` → `[target]`, `target_index=0`.
- **FR-004**: `window>10` or `<0` → `ErrInvalid` (INVALID_ARGUMENT).
- **FR-005**: boundaries return as many neighbours as exist, no error.
- **FR-006**: missing/cross-vault `chunk_id` → `ErrNotFound`.
- **FR-007**: empty/whitespace `chunk_id` → `ErrInvalid`, no lookup.
- **FR-008/FR-009**: every returned chunk carries full metadata; parent `DocumentMeta` included (orphan-tolerant).
- **FR-010/FR-011/FR-016**: all four transports identical; REST `/v1/chunks/{id}/context?window=N`; no `vault` field.
- **FR-012/FR-013/FR-014/FR-015**: one logical read (≤21 point-gets); reuses linked list + spec-035 `Chunk`; no migration; pure Go.
