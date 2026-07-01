# Contract — `BatchGetChunks` (spec 038, BL-003)

> Phase 1 output for `/speckit-plan`. One new RPC, surfaced identically on every transport, mirroring spec 035 (`GetChunk`) and spec 037 (`GetChunkContext`). The canonical type lives on `engine.BatchResult` (`internal/engine/`). See [data-model.md](../data-model.md) for the resolution rules and [research.md](../research.md) (R1–R4) for the decisions.

## Operation contract

| Property | Value |
|----------|-------|
| Name | `BatchGetChunks` |
| Inputs | `chunk_ids` (repeated content-addressed string, max 100) |
| Output | one `BatchGetChunksResult` per requested id, in request order |
| Ordering | `results[i]` ↔ `request[i]` (positional, 1:1, including duplicates + missing) |
| Cap | 100 ids inclusive — `len > 100` → `INVALID_ARGUMENT` |
| Empty list | `len == 0` → `INVALID_ARGUMENT` |
| Empty/whitespace element | any element `""`/whitespace → `INVALID_ARGUMENT` (no lookup) |
| Missing / cross-vault id | result with empty `chunk` + `error = "not found"`; **call does NOT error** (partial success) |
| Duplicates | resolved independently per position (no de-dup) |
| Orphan chunk | chunk returned; `document` zero-valued/omitted — never an error |
| Parity | identical result across CLI, REST, gRPC, MCP for the same `chunk_ids` |
| Cost | ≤ 100 Pebble point-Gets (one per id); no scan |

The operation is **additive** — a new RPC alongside `GetChunk`/`GetChunkContext`. No existing operation, field, or value changes. The per-id-error model (partial success) is the key delta: `GetChunk`/`GetChunkContext` fail the whole call on a missing id (`NOT_FOUND`); `BatchGetChunks` never does.

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

Add the RPC to the `Gorag` service (after `GetChunkContext`, before the closing `}`):

```proto
service Gorag {
  // ...existing RPCs...
  rpc GetChunkContext(GetChunkContextRequest) returns (GetChunkContextResponse);
  // spec 038 (BL-003): resolve up to 100 chunks by id in one call. Per-id error
  // (partial success) — a missing id yields an empty chunk + error, not a call
  // failure. → engine.BatchGetChunks (also REST POST /v1/chunks/batch,
  // MCP go_rag_batch_get_chunks, CLI `go-rag chunk batch`).
  rpc BatchGetChunks(BatchGetChunksRequest) returns (BatchGetChunksResponse);
}
```

New messages (reuse the spec-035 `Chunk` + `DocumentMeta`):

```proto
message BatchGetChunksRequest {
  repeated string chunk_ids = 1; // max 100, no vault field
}

// One positional result entry — the requested id, its chunk (zero-value if not
// found), a non-empty error when this id failed, and the parent document.
message BatchGetChunksResult {
  string      chunk_id = 1;
  Chunk       chunk    = 2; // zero-value if not found
  string      error    = 3; // non-empty iff this chunk_id failed ("not found")
  DocumentMeta document = 4; // parent document; zero-value if orphan/not-found
}

// Ordered 1:1 with the request chunk_ids (same length, including duplicates/missing).
message BatchGetChunksResponse {
  repeated BatchGetChunksResult results = 1;
}
```

Regenerate `proto/gen` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). Wire the handler + response projection in a new `internal/grpc/batch_get_chunks.go` (reuse the existing `toChunkPB` + `toDocumentMetaPB` used by GetChunk/GetChunkContext).

### REST — `internal/rest/`

`POST /v1/chunks/batch` — register beside the other `/v1/chunks/*` routes (`internal/rest/server.go`, both the `routes` table and the `handlerFor` switch; **also** add the path to `openapi.yaml` — the openapi parity test asserts the two match).

Request body:
```json
{ "chunk_ids": ["id1", "id2", "id3"] }
```

Response (200 — even when some ids are missing):
```json
{
  "results": [
    { "chunk_id": "id1", "chunk": { /* chunkDTO — same shape as GET /v1/chunks/{id}'s chunk */ }, "document": { /* documentMetaDTO */ } },
    { "chunk_id": "id2", "error": "not found" },
    { "chunk_id": "id3", "chunk": { /* … */ }, "document": { /* … */ } }
  ]
}
```

Errors: **400** for structural invalid-argument (empty list, > 100 ids, empty/whitespace element). **Never 404** — missing ids are in-band `error` fields on a 200. (Mirrors the per-id-error model; contrasts with `GET /v1/chunks/{id}` which returns 404 for a missing id.)

### Engine (canonical) — `internal/engine/`

```go
func (e *Engine) BatchGetChunks(chunkIDs []string) (*BatchResult, error)
// BatchResult{ Results []BatchItem }
// BatchItem{ ChunkID string; Chunk *model.Chunk; Document model.Document; Source model.Source; Err string }
```

Errors: `ErrInvalid` (empty list, > 100, empty/whitespace element) — the ONLY call-level error. Missing ids are NOT an error (they surface as `BatchItem.Err = "not found"`).

### CLI — `internal/cli/`

`go-rag chunk batch <chunk_id> [<chunk_id>…]` (beside `chunk get` / `chunk context`). The positional args are the chunk-id list. `--format json|text` (default json). Reject `> 100` args with a non-zero exit + clear message. JSON envelope matches the proto/REST shape (cross-transport parity).

### MCP — `internal/mcp/server.go`

`go_rag_batch_get_chunks` tool (beside `go_rag_get_chunk` / `go_rag_get_chunk_context`). Args: `{ "chunk_ids": ["…", "…"] }` (a JSON array; numbers aren't involved, but arrays arrive as `[]any` of `string` — parse accordingly). Render one line per result: the chunk_id, `ok`/`not found`, and the document line. Add to `dispatchDB` + `toolDefs()`; bump the tool-count tests (21 → 22).

---

## Parity & determinism

- **Parity (FR-010):** `internal/engine/parity_test.go` is extended with `TestCrossTransport_BatchGetChunksParity` — `BatchGetChunks` returns identical per-position results (chunk-id, error string, document id/file_path) across CLI, REST, gRPC, and MCP for the same `chunk_ids`, over a batch mixing live + missing + duplicate ids (mirrors the GetChunk/GetChunkContext parity coverage).
- **Determinism:** the result is a pure function of `chunk_ids` and the stored chunks — fully deterministic; re-fetching yields byte-identical results. Order is the request order.
- **Boundaries (FR-004..FR-007):** parity + engine tests cover > 100, empty list, empty/whitespace element, duplicates, all-missing, and single-id batches.

## Backward compatibility

- Pure-additive RPC + messages. No existing field's tag, type, or value changes.
- No on-disk key-space layout change; no migration; `migrate.ExpectedVersion` unchanged.
- Old clients are unaffected — they simply do not call the new RPC.
