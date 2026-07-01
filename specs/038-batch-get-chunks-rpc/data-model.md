# Data Model — BatchGetChunks (BL-003)

> Phase 1 output for `/speckit-plan`. No new persisted entities — the feature is a read-only projection over existing chunks. See [research.md](./research.md) (R1–R4) for the read mechanism + the per-id-error + DocumentMeta-shape decisions, and [contracts/api.md](./contracts/api.md) for the wire shape.

## Entity: `BatchResult` / `BatchItem` (engine projection, new — not persisted)

`internal/engine/` — the engine-level result of `BatchGetChunks`, mirroring spec-035's `ChunkResult` per entry, ordered 1:1 with the request.

```go
// BatchItem is one positional entry in a BatchGetChunks result: the requested
// chunk_id, its resolved chunk (nil when not found), the parent document/source
// (zero-valued when absent — orphan-tolerant, mirroring GetChunk), and a non-
// empty Err when this id failed (currently only "not found"). Mirrors ChunkResult
// (spec 035) per entry, so the projection reuses the GetChunk helpers verbatim.
type BatchItem struct {
	ChunkID  string
	Chunk    *model.Chunk // nil if not found
	Document model.Document
	Source   model.Source
	Err      string // "" or "not found"
}

// BatchResult is the ordered result of BatchGetChunks — one BatchItem per
// requested chunk_id, in request order (positional, 1:1, including duplicates
// and missing ids).
type BatchResult struct {
	Results []BatchItem
}
```

**Batch invariants**

| Property | Rule |
|----------|------|
| Ordering | `Results[i]` corresponds to `request[i]` — request order, positional, 1:1 |
| Length | `len(Results) == len(request)` (always — including all-missing / all-duplicate requests) |
| Missing id | `Results[i] = { ChunkID: request[i], Chunk: nil, Err: "not found" }`; call does NOT error |
| Cross-vault id | identical to "not found" (single-vault store — the id simply isn't present) |
| Duplicate id | each position resolved independently; no de-duplication (`["a","a"]` → two identical entries) |
| Cap | `len(request) > 100` → `ErrInvalid` before any lookup |
| Empty list | `len(request) == 0` → `ErrInvalid` |
| Empty/whitespace element | any element `""`/whitespace → `ErrInvalid` before any lookup |
| Orphan chunk | `Document`/`Source` zero-valued for that entry; chunk still returned; never an error |

## Resolution algorithm (engine, read-only)

```
validate request length: 0 → ErrInvalid; > 100 → ErrInvalid
for each id in request:
    if strings.TrimSpace(id) == "" → ErrInvalid          // before any lookup
results := make([]BatchItem, 0, len(request))
for each id in request:
    c, ok := lookupChunk(db, id)
    if !ok:
        results = append(results, BatchItem{ChunkID: id, Err: "not found"})
        continue
    item := BatchItem{ChunkID: id, Chunk: &c}
    if d, ok := lookupDoc(db, c.DocumentID); ok {         // tolerant — orphan → zero document
        item.Document = d
        if raw, ok, _ := db.GetWithPrefix(PrefixSource, []byte(d.SourceID)); ok {
            _ = json.Unmarshal(raw, &item.Source)
        }
    }
    results = append(results, item)
return &BatchResult{Results: results}, nil
```

**Cost**: exactly `len(request)` `lookupChunk` point-Gets (≤ 100) + one `lookupDoc`+source-Get per *found* chunk. Each is a Pebble point-Get over prefix `0x03`/`0x02`/source prefix (sub-millisecond). No scan. One caller round-trip.

**Consistency**: chunks are immutable once written (content-addressed, Constitution II), so the N point-Gets observe a consistent view with no snapshot/transaction (see research.md R1).

## Reused entities (unchanged)

- **Chunk** (`internal/model`) — carries `ChunkIndex`/`TotalChunks`, `SectionContext`, `Wikilinks`, `Poisoning`, and all sidecars. Read as-is by each `lookupChunk`; the batch returns full chunks identical to `GetChunk`. Unchanged by this feature.
- **Document / Source** — resolved via `lookupDoc` for the parent metadata; zero-valued when absent (orphan-tolerant). Same projection `GetChunk` returns (spec 035).

## Identity & storage invariants (constitution Principle II)

- **No new stored state.** `BatchGetChunks` is a pure read over existing prefixes (`0x03` chunks, `0x02` documents, source prefix). It introduces no new key, no new prefix, no new persisted struct.
- **On-disk layout**: unchanged. No migration; `migrate.ExpectedVersion` unchanged.
- **Identity**: read-only — no `chunk_id` is created or changed. Duplicates are resolved by reading the same key twice (Pebble caches the block; effectively free).

## Validation rules (map to FRs)

- **FR-001/FR-002**: `BatchGetChunks(chunk_ids)` resolves up to 100 ids; one result per id in request order.
- **FR-003**: missing/cross-vault id → `{empty chunk, error="not found"}`; no call-level error (partial success).
- **FR-004**: `len > 100` → `ErrInvalid` (INVALID_ARGUMENT).
- **FR-005**: empty list → `ErrInvalid`.
- **FR-006**: empty/whitespace element → `ErrInvalid` (no lookup).
- **FR-007**: duplicates resolved positionally (no dedup).
- **FR-008**: every found chunk carries full metadata (reuses GetChunk projection).
- **FR-009**: single logical read — N point-Gets in one caller round-trip.
- **FR-010/FR-011/FR-015**: all four transports identical; REST `POST /v1/chunks/batch`; no `vault` field.
- **FR-012/FR-013/FR-014**: reuses `lookupChunk` + spec-035 `Chunk`/`DocumentMeta`; pure read, no migration; pure Go, no new deps.
