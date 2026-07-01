# Research — BatchGetChunks (BL-003)

> Phase 0 output for `/speckit-plan`. Resolves the two open questions the spec's Research Note raised, plus confirms the conventions every transport depends on. Grounded in a direct read of `internal/storage/db.go`, `internal/engine/helpers.go`, and the shipped spec-035/037 implementations.

## R1 — Read mechanism: N point-Gets, not a MultiGet

**Decision**: `BatchGetChunks` resolves each requested ID with an independent `lookupChunk` point-Get (Pebble `Get` over prefix `0x03`), looped in the engine. There is **no** Pebble batch/multi-get primitive on the storage wrapper, and none is needed.

**Rationale**: `internal/storage/db.go` exposes, for chunk records, only:
- `GetWithPrefix(prefix, key)` — the point-get `lookupChunk` (internal/engine/helpers.go) already uses (`storage.PrefixChunk` = `0x03`).
- plus `Set`/`SetWithPrefix`/`Delete`/`DeleteWithPrefix`/`PrefixScan`/`PrefixScanByte` — none a multi-get.

So the "single logical read" (spec FR-009) is **a loop of N `lookupChunk` point-Gets in one caller round-trip**, exactly the pattern `GetChunkContext` (spec 037) uses for its linked-list walk (≤ `1+2*window` point-Gets). For a batch the bound is simply N (≤ 100). Each Pebble point-Get is sub-millisecond; 100 of them is a few milliseconds total, latency independent of corpus size.

**Consistency without a transaction**: a Pebble snapshot/transaction is unnecessary here. Chunks are **immutable once written** — content-addressed identity (Constitution II) means a given `chunk_id` always maps to the same bytes for the life of that chunk (a re-ingest that changes content mints a *new* `chunk_id`; the old one either still resolves to its old bytes or is gone). So N point-Gets, even without a single transaction, observe a consistent view: there is no mid-batch mutation of a chunk's content. (A document re-ingest during a batch could, in principle, retire some old IDs mid-loop — those then resolve to "not found", which is the correct, tolerant outcome. No torn read of a single chunk is possible.)

**Alternatives considered**:
- *Pebble `NewBatch`/`MultiGet`*: Pebble's `Batch` is a write primitive; reads use `NewSnapshot()` + `Get`. A snapshot would give a transactionally-consistent read but adds an allocation + closes a snapshot for no correctness benefit (chunks are immutable). Rejected as premature complexity.
- *Prefix scan over `0x03` + filter by ID set*: a scan reads ALL chunks then filters — O(corpus) not O(N). Strictly worse than N point-Gets for N ≤ 100. Rejected.
- *Add a `MultiGet` to the storage wrapper*: YAGNI — the only batch-read caller is this feature; a loop of the existing point-get is idiomatic and sufficient. Rejected (can be added later if a second caller wants it).

## R2 — `DocumentMeta` shape: per-result (mirrors GetChunk 1:1)

**Decision**: each `BatchGetChunksResult` carries its own `DocumentMeta document = 4` (the parent document of that result's chunk), exactly as `GetChunk`'s `ChunkResult{Chunk, Document, Source}` does. The same document repeats across sibling chunks of the same document.

**Rationale**:
- **Parity simplicity** — byte-for-byte parity with `GetChunk`: every result entry IS a `GetChunk` resolution (chunk + document + source). The projection reuses `toChunkPB` + `toDocumentMetaPB` (gRPC), `toChunkDTO` + `toDocumentMetaDTO` (REST), `toChunkOut` + `toDocumentOut` (CLI) verbatim. No new projection code, no DTO drift surface.
- **Positional correlation** — the bridge consumer correlates `result[i] ↔ request[i]`; a per-result document keeps each entry self-contained (no secondary lookup into a document map).
- **Cost is negligible** — `DocumentMeta` is small metadata (ids, paths, status, enrichment summary). Duplicating it across ≤ 100 results is at most a few KB. The caller asked for full chunks; returning each chunk's document inline is the expected shape.
- **Orphan tolerance** — a chunk whose document was removed yields a zero-valued/omitted `document` for that entry only (mirrors `GetChunk`'s orphan handling), not a call-level condition.

**Alternatives considered**:
- *Deduped side-structure* (`map<chunk_id, DocumentMeta>` or a separate `repeated DocumentMeta documents` + index): more compact for single-document batches, but diverges from `GetChunk`'s shape, complicates cross-transport parity (every transport must agree on the dedup encoding), and forces the caller to re-correlate. Rejected — parity and simplicity outweigh the bytes saved.

## R3 — Per-id error model: partial success (no call-level NOT_FOUND)

**Decision**: a missing or cross-vault `chunk_id` produces a result entry with `chunk` zero-valued and `error = "not found"`. The call itself returns no error for missing IDs. Only structurally invalid requests (empty list, > 100 IDs, any empty/whitespace element) raise a call-level `ErrInvalid` → `INVALID_ARGUMENT` / 400 / non-zero exit.

**Rationale**: this is the entire reason a batch primitive exists distinct from looping `GetChunk`. A bridge syncing a document's chunk list may hold stale IDs (post-re-ingest) alongside live ones; failing the whole batch for one stale ID would force the caller back into per-id retry loops — exactly the N-round-trip pattern `BatchGetChunks` exists to eliminate. Per-id tolerance lets the caller act on the live results immediately and treat "not found" positions as stale/removed.

**Error-surface mapping (per transport)**:
- **gRPC**: call-level `ErrInvalid` → `status.InvalidArgument` (via `toStatusErr`). Missing IDs are **never** a gRPC status — they are in-band `result.error`. (Contrast: `GetChunk`/`GetChunkContext` return `status.NotFound` for a missing ID; `BatchGetChunks` does NOT.)
- **REST**: 200 with the results array (per-id errors in-band); 400 only for structural invalid-argument. **Never 404** for a missing ID.
- **MCP**: call succeeds (text lists each result); the `error` field per id. (`callTool` maps only a returned `error` to JSON-RPC — but the engine returns *no error* for missing IDs, so MCP returns the full result text.)
- **CLI**: exit 0 with the results; non-zero exit only for structural invalid-argument.

**Cross-vault**: a `chunk_id` from another vault simply isn't in this single-vault store → "not found" (no leakage), identical to `GetChunk` (spec 035 FR-003) and `GetChunkContext` (spec 037).

## R4 — Cap, validation, and duplicate semantics

**Decision**:
- **Cap**: `len(chunk_ids) > 100` → `ErrInvalid` (100 inclusive). Bounds response size and read cost.
- **Empty list**: `len == 0` → `ErrInvalid` (a no-op batch is a client error, not an empty success).
- **Empty/whitespace element**: any `chunk_id` that is `""` or whitespace-only → `ErrInvalid` (validated before any lookup, so no partial work).
- **Duplicates**: resolved independently and positionally — `["a","a"]` returns two identical result entries. No de-duplication. The response is strictly 1:1 with the request.

**Rationale**: mirrors the validation posture of `GetChunk`/`GetChunkContext` (reject bad input before lookup) while extending it to the batch shape. Duplicate resolution is positional because the caller's contract is `result[i] ↔ request[i]`; de-dup would break that correspondence and save negligible work (a repeated point-Get of an already-cached Pebble block is free).

**Order preservation**: results are appended in request order; internal read ordering is irrelevant. This makes the batch trivially correlatable and is the basis of the parity test.
