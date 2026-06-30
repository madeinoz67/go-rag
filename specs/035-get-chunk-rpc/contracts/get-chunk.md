# Contract — GetChunk RPC (spec 035)

> Phase 1 output of `/speckit-plan`. The public contract for `GetChunk` across
> all four transports. All four are thin projections of one new engine method,
> so cross-transport parity (Constitution V) holds by construction. Grounded in
> the `ReleaseChunk` / `ResetChunk` precedent (`proto/gorag.proto:130-135`,
> `internal/engine/poison.go:60,79`) — see `research.md` for evidence.

## Engine method (the single source all four transports project)

```go
// internal/engine — new method on *Engine
func (e *Engine) GetChunk(chunkID string) (*ChunkResult, error)

type ChunkResult struct {
    Chunk    model.Chunk
    Document model.Document  // zero value if parent absent (orphan-chunk edge)
    Source   model.Source    // populated only if source_path is included (open decision)
}
```

- **Input:** `chunkID string` — arity-identical to `ReleaseChunk(chunkID string)` / `ResetChunk(chunkID string)`.
- **No `vault` parameter** — the engine is single-vault-per-process (`research.md` R1).
- **Read path:** two Pebble point reads via the existing `lookupChunk` + `lookupDoc`
  helpers (`internal/engine/helpers.go:56-79`); optional 3rd read for `Source`.
- **Errors:**
  - `engine.ErrInvalid` — malformed/empty `chunkID` (wraps the existing sentinel).
  - `engine.ErrNotFound` — **new sentinel**; the lookup site wraps it
    `fmt.Errorf("%w: chunk %s", engine.ErrNotFound, chunkID)`. See Not-found contract below.

---

## gRPC / protobuf contract

Add to `proto/gorag.proto` (in the `Gorag` service block, beside
`ReleaseChunk`/`ResetChunk`), then regenerate `proto/gen`
(package `github.com/madeinoz67/go-rag/proto/gen;goragpb`):

```protobuf
service Gorag {
  // ...existing RPCs...
  rpc GetChunk(GetChunkRequest) returns (GetChunkResponse);
}

message GetChunkRequest {
  string chunk_id = 1;   // NO vault field — matches ReleaseChunkRequest/ResetChunkRequest
}

message GetChunkResponse {
  Chunk        chunk    = 1;
  DocumentMeta document = 2;
}

message Chunk {
  string          chunk_id         = 1;
  string          document_id      = 2;
  string          content          = 3;
  int32           chunk_index      = 4;
  int32           total_chunks     = 5;
  int32           page_number      = 6;   // 0 = not paginated
  int32           start_char       = 7;
  int32           end_char         = 8;
  int32           token_count      = 9;
  string          previous_chunk_id = 10;
  string          next_chunk_id     = 11;
  Poisoning       poisoning        = 12;  // reuse existing message
  repeated string section_context  = 13;
  NearDup         near_dup         = 14;  // reuse existing message
  string          kind             = 15;
  string          created_at       = 16;  // RFC3339
}

message DocumentMeta {
  string          id                = 1;   // identity hash
  string          content_hash      = 2;   // change-detection hash (distinct from id)
  string          source_id         = 3;
  string          source_path       = 4;   // optional 3rd read; see data-model.md
  string          file_path         = 5;   // relative
  string          file_name         = 6;
  string          file_type         = 7;
  string          mime_type         = 8;
  int32           chunk_count       = 9;
  int64           file_size         = 10;
  string          status            = 11;  // pending|embedded|error
  string          ingested_at       = 12;  // RFC3339
  string          updated_at        = 13;  // RFC3339
  repeated string tags              = 14;
  string          summary           = 15;
  string          enrichment_status = 16;  // enriched|failed|nothing-to-enrich
  string          enrichment_model  = 17;
  string          enrichment_at     = 18;  // RFC3339
}
```

**Adapter** (`internal/grpc/engine_adapter.go`): add
`func (a *Adapter) GetChunk(ctx, req)` calling `a.eng.GetChunk(req.GetChunkId())`
with `toChunkPB` / `toDocumentMetaPB` mappers in the style of `toPoisoningPB`.
Extend `toStatusErr` (`engine_adapter.go:14-19`):
`errors.Is(err, engine.ErrNotFound) → codes.NotFound`.

---

## REST contract

**Route:** `GET /v1/chunks/{id}` — matches the `/v1/<resource>` + `{id}`
path-param convention (`/v1/poison/{id}/release`, `/v1/poison/{id}/reset`).
> **Rejected:** the bridge backlog's `GET /api/vaults/{vault}/chunks/{chunk_id}`
> breaks convention (wrong base path `/api/` vs `/v1/`; vault segment no route
> uses). See `research.md` R1.D.

| Method | Path | Path param | Auth |
|---|---|---|---|
| `GET` | `/v1/chunks/{id}` | `id` = `chunk_id` | same as siblings (`true`) |

**Response 200** — JSON body (snake_case, parity with `queryHit` DTO at
`internal/rest/types.go:24`):
```json
{
  "chunk":    { "chunk_id": "…", "document_id": "…", "content": "…", "page_number": 0, "poisoning": null, "section_context": [], … },
  "document": { "id": "…", "content_hash": "…", "file_path": "…", "status": "embedded", "summary": "…", … }
}
```
**Errors:** `404` (`ErrNotFound`), `400` (`ErrInvalid` — empty/whitespace `id`),
`500` (anything else). Extend `writeEngineErr` (`internal/rest/server.go:182-187`).

**Touch-points** (`internal/rest/`):
1. `server.go` `routes` — add `{"GET", "/v1/chunks/{id}", true}` (`server.go:43-65`).
2. `server.go` `handlerFor` — add `case "GET /v1/chunks/{id}": return s.handleGetChunk`.
3. `engine_adapter.go` — implement `handleGetChunk` reading `r.PathValue("id")` (pattern: `handlePoisonRelease` `server.go:323-330`).
4. `types.go` — add `chunkDTO`, `documentMetaDTO`, `getChunkResponse` wrapper.
5. `openapi.yaml` — add the `GET /v1/chunks/{id}` path with `{id}` param + `200`/`404` responses (mirror `/v1/poison/{id}/release`).

> **CI invariant:** the parity test `T035` (`server.go:39-42`) asserts `routes`
> matches `openapi.yaml` exactly — add the route to **both** in the same commit.

---

## MCP contract

Add tool `go_rag_get_chunk` to `internal/mcp/server.go`, mirroring
`go_rag_poison_release` (`server.go:807-813`):

```jsonc
{
  "name": "go_rag_get_chunk",
  "inputSchema": {
    "type": "object",
    "properties": { "chunk_id": { "type": "string" } },
    "required": ["chunk_id"]
  }
}
```
- **Dispatch:** add `case "go_rag_get_chunk":` calling `engine.GetChunk` + a
  `renderGetChunk` formatter (mirror `renderQuery` `server.go:231`).
- **Not-found:** MCP has no HTTP status. Map `ErrNotFound` to JSON-RPC `-32001`
  ("chunk not found") — within the reserved `-32000..-32099` server-error range.
  Do **not** let it collapse into the existing `-32603` Internal bucket
  (`server.go:104,689`).

---

## CLI contract

Add `internal/cli/chunk.go` with `go-rag chunk get <chunk_id>`
(`RunE` pattern from `newPoisonReleaseCmd` `poison.go:88-103`):

```
go-rag chunk get <chunk_id> [--db-path <path> | --vault <name>] [--json]
```
- `openDB(dbPath)` → `engine.NewWithDB(cfg, db).GetChunk(args[0])` → print JSON
  (default for scripting/bridge) or a human-readable block.
- Register via `newChunkCmd()` in `root.go` (beside `newVaultCmd()` `root.go:81`).
- **Not-found:** `RunE` returns the error → cobra prints `chunk not found: <id>`
  to stderr and exits non-zero (existing convention).

---

## Not-found contract (cross-transport)

`GetChunk` introduces **`engine.ErrNotFound`** (`internal/engine/errors.go` has
only `ErrInvalid` today). The lookup site wraps it. Each transport surfaces it natively:

| Outcome | gRPC | REST | MCP | CLI |
|---|---|---|---|---|
| valid `chunk_id`, found | `GetChunkResponse` | `200` + body | result | `0`, printed chunk |
| missing / stale / other-vault `chunk_id` | `codes.NotFound` | `404` | `-32001` "chunk not found" | non-zero + stderr |
| malformed / empty `chunk_id` | `codes.InvalidArgument` | `400` | `-32602` "invalid params" | non-zero + stderr |
| internal error | `codes.Internal` | `500` | `-32603` | non-zero + stderr |

**Cross-vault isolation (FR-003):** because the engine is single-vault-per-process,
a `chunk_id` from another vault simply does not exist in the bound store — it
resolves to the same not-found path above. The chunk from another vault is never
disclosed. No separate check is needed.

**Recommended back-fill:** wrap `ReleaseChunk` / `ResetChunk`'s bare
`fmt.Errorf("chunk not found: %s", chunkID)` (`poison.go:60,79`) in `ErrNotFound`
too — fixes the same latent 500-instead-of-404 bug on those RPCs and keeps the
chunk-scoped family consistent. Minimum scope is `GetChunk`.
