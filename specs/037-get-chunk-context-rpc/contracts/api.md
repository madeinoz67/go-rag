# Contract — `GetChunkContext` (spec 037, BL-002)

> Phase 1 output for `/speckit-plan`. One new RPC, surfaced identically on every transport, mirroring spec 035 (`GetChunk`). The canonical type lives on `engine.ContextResult` (`internal/engine/`). See [data-model.md](./data-model.md) for the windowing rules and [research.md](./research.md) (R1–R4) for the read mechanism.

## Operation contract

| Property | Value |
|----------|-------|
| Name | `GetChunkContext` |
| Inputs | `chunk_id` (content-addressed), `window` (int32, default 2, range 0–10) |
| Output | ordered `[predecessors…][target][successors…]` chunk list + `target_index` + parent `DocumentMeta` |
| Ordering | document order; `target_index == len(predecessors)` |
| Window default / cap | 2 / 10 (`>10` or `<0` → `INVALID_ARGUMENT`) |
| `window=0` | exactly the target chunk, `target_index=0` (≡ `GetChunk`) |
| Boundaries | as many neighbours as exist — never an error |
| Absent target | `NOT_FOUND` (missing or cross-vault — single-vault store) |
| Invalid input | empty/whitespace `chunk_id`, `window` out of range → `INVALID_ARGUMENT` |
| Orphan chunk | window returned; `document` zero-valued — never an error |
| Parity | identical result across CLI, REST, gRPC, MCP for the same `(chunk_id, window)` |
| Cost | ≤ `1 + 2·window` point-gets (max 21); no scan |

The operation is **additive** — a new RPC alongside `GetChunk`. No existing operation, field, or value changes. Mirrors how `GetChunk` (spec 035) was added.

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

Add the RPC to the `Gorag` service (after `GetChunk`, before the closing `}`):

```proto
service Gorag {
  // ...existing RPCs...
  rpc GetChunk(GetChunkRequest) returns (GetChunkResponse);
  // spec 037 (BL-002): a chunk plus up to N neighbours on each side, in one
  // call. → engine.GetChunkContext (also REST GET /v1/chunks/{id}/context,
  // MCP go_rag_get_chunk_context, CLI `go-rag chunk context`).
  rpc GetChunkContext(GetChunkContextRequest) returns (GetChunkContextResponse);
}
```

New messages (reuse the spec-035 `Chunk` message, which carries `Wikilinks` per spec 036):

```proto
message GetChunkContextRequest {
  string chunk_id = 1;
  int32  window   = 2; // default 2, clamped [0,10]; >10 → INVALID_ARGUMENT
}

message GetChunkContextResponse {
  repeated Chunk chunks       = 1; // ordered [predecessors][target][successors]
  int32          target_index = 2; // index of the requested chunk within chunks[]
  DocumentMeta   document     = 3; // parent document (zero-valued if orphan)
}
```

Regenerate `proto/gen` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). Wire the handler + response projection in `internal/grpc/engine_adapter.go` (reuse the existing `Chunk` projection helper used by `GetChunk`).

### REST — `internal/rest/`

`GET /v1/chunks/{id}/context?window=N` — register beside `GET /v1/chunks/{id}` (`internal/rest/server.go`). Parse `window` with: default 2 when absent; `>10` or `<0` → 400 (invalid argument); otherwise clamp to `[0,10]`. Response JSON:

```json
{
  "chunks": [ /* ordered chunkDTO[] — same shape as GET /v1/chunks/{id}'s chunk */ ],
  "target_index": 2,
  "document": { /* documentMetaDTO — same as GetChunk */ }
}
```

Errors: 404 for a missing/cross-vault `chunk_id`; 400 for empty/whitespace `id` or out-of-range `window` (mirrors `GetChunk`'s error mapping).

### Engine (canonical) — `internal/engine/`

```go
func (e *Engine) GetChunkContext(chunkID string, window int) (*ContextResult, error)
// ContextResult{ Chunks []model.Chunk; TargetIndex int; Document model.Document; Source model.Source }
```

Errors: `ErrInvalid` (empty id / window out of range), `ErrNotFound` (missing/cross-vault id), wrapped with context at the call site — the same convention as `GetChunk` (`fmt.Errorf("%w: chunk %s", ErrNotFound, chunkID)`).

### CLI — `internal/cli/`

`go-rag chunk context <id> [--window N]` (beside `go-rag chunk get`). Render the ordered chunks with a marker on the target (`>>>`) and its `target_index`, plus the parent document line. Default `--window 2`; reject `>10` with a non-zero exit + clear message.

### MCP — `internal/mcp/server.go`

`go_rag_get_chunk_context` tool (beside `go_rag_get_chunk`). Render the window as a numbered list with the target marked, plus the document line — consistent with the existing GetChunk text render.

---

## Parity & determinism

- **Parity (FR-010):** `internal/engine/parity_test.go` is extended to assert `GetChunkContext` returns identical `chunks` / `target_index` / `document` across CLI, REST, gRPC, and MCP for the same `(chunk_id, window)` (mirrors the existing `GetChunk` parity coverage).
- **Determinism:** the window is a pure function of `(chunk_id, window)` and the stored linked list — fully deterministic; re-fetching yields byte-identical results.
- **Boundaries (FR-005):** parity tests cover the first chunk, last chunk, single-chunk document, `window=0`, and `window > document size`.

## Backward compatibility

- Pure-additive RPC + messages. No existing field's tag, type, or value changes.
- No on-disk key-space layout change; no migration; `migrate.ExpectedVersion` unchanged.
- Old clients are unaffected — they simply do not call the new RPC.
