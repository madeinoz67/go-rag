# Data Model: Chunk Change Deltas on Re-Ingest (BL-010)

**Phase 1 output for `/speckit-plan`.** See [spec.md](./spec.md) (WHAT/WHY) + [research.md](./research.md) (decisions) + [`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md) (full technical design).

## Entities

### `ContentHash` (new non-identity sidecar on `model.Chunk`)

- **Type**: `string` (SHA-256 hex of the chunk's redacted text), `json:"content_hash,omitempty"`.
- **Semantics**: a **comparison key** for re-ingest diffing — NOT a storage key, NOT part of chunk identity. `GenerateID` folds `text+mime+{doc,idx}` only; `ContentHash` never enters the identity hash. Cross-document `ContentHash` collisions are harmless (the diff is per-doc).
- **Input**: the *redacted* chunk text (`s.Text` post-redaction, `pipeline.go`) — the same input space `cid` hashes, minus `{doc, idx}`. Consistent with identity; the config-drift gate (R3) handles redactor changes.
- **Lifecycle**: computed in `processFile` next to `cid`; persisted by `storeDocument` (auto, whole-struct marshal); backfilled for existing chunks by the v2 migration.
- **Precedent**: `Document.ContentHash` (model.go:32, document-granularity) — same concept, same name, different scope.

### `ChunkDelta` (new — the per-chunk change classification)

```
ChunkDelta {
    change_type   ∈ {ADDED, REMOVED, UNCHANGED}
    chunk_id      string   // the NEW chunk's cid (present for ADDED + UNCHANGED)
    prev_chunk_id string   // the OLD chunk's cid (present for UNCHANGED + REMOVED)
}
```

- **ADDED**: `chunk_id` set, `prev_chunk_id` empty — a chunk in the new version with no content-match in the old.
- **REMOVED**: `prev_chunk_id` set, `chunk_id` empty — a chunk in the old version with no content-match in the new.
- **UNCHANGED**: both set — content-match; `prev_chunk_id → chunk_id` is the old→new cid remap a consumer uses to preserve stored references.

### `RE_INGESTED` event (extends the existing `DocumentEvent` vocabulary)

- **Type**: `DocumentEventType = 2` (`EventReingested`, already reserved in `events/bus.go` + proto enum 2).
- **Carries**: the new `DocumentID` + `SourcePath`; `repeated ChunkDelta chunk_deltas` (the full delta for the re-ingested document); timestamp + cursor (seq) as the other WatchDocuments events.
- **Ordering**: **replaces** the `INGESTED(new)` + `DELETED(old)` pair a re-ingest surfaces today (spec 040) — a re-ingest emits exactly one `RE_INGESTED`, no `INGESTED`/`DELETED` for that document.

### Embedding config — `CorpusBaseline` (existing — the embed-skip gate)

- **Existing**: `internal/engine/baseline.go` — the model / dimension / convention under which chunks were embedded (spec 017).
- **Use here**: the gate for FR-006/FR-007 — an `UNCHANGED` chunk's existing vector is reused only if the baseline is unchanged since it was embedded; otherwise re-embed.

## State / computation

### The re-ingest delta computation (pure function, `internal/pipeline/delta.go`)

- **Input**: `old []Chunk` (captured before delete) + `new []Chunk` (the re-ingested version), for the SAME source path.
- **Algorithm**: multiset diff on `ContentHash` — `countOld[h]`, `countNew[h]`; for each hash `h`: `u = min(countOld[h], countNew[h])` → `u × UNCHANGED`; surplus-old → `REMOVED`; surplus-new → `ADDED`. Pair UNCHANGED old→new by stable position-within-bucket.
- **Output**: `[]ChunkDelta` + the `map[prevCid]newCid` remap.
- **Cost**: O(N) build + O(unique-hashes) join — sub-millisecond for typical docs; does not breach the <10ms ACK budget (verify in implementation).

### The re-ingest path reorder

- **Today**: `Reprocess`/`ReprocessAll` → `DeleteDoc` (wipes chunks + 6 indexes) → `Ingest` (re-build). Old data gone before new exists.
- **Reorder**: capture `old []Chunk` (+ each old chunk's `PrefixEmbedding` record) via read-only `chunksOfDoc(docID)` + `embedsOfDoc(docID)` helpers (factored from `DeleteDoc`'s scan, `delete.go:23-30`) **BEFORE** `DeleteDoc`; thread `oldChunks` (+ embedding records) into `processFile`; compute the diff there; for `UNCHANGED`+baseline-unchanged, copy the old `PrefixEmbedding` to the new `cid` (synchronous, before the async worker) + mark the job "skip embed"; always recompute FTS + NearDup normally (the worker re-indexes/re-clusters under the new `cid`; old postings deleted by `DeleteDoc`).
- **Coverage**: `Reprocess`, `ReprocessAll`, AND the watcher's MODIFIED path (`watcher.go`) — all route through the `oldChunks`-threaded `processFile`.
