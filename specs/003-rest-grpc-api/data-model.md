# Data Model: Multi-Transport Server APIs

**Feature**: 003-rest-grpc-api | **Date**: 2026-06-20

This feature adds **no new persisted entities** — all storage remains the existing
Pebble key-space (Source/Document/Chunk/Embedding, PRD §6.7). The data model here
defines the **`internal/engine` facade's structured result types** (the shared
"unified engine" surface that all three adapters serialize) and the **daemon
address book** extension. These types replace the pre-formatted strings that
`internal/mcp/server.go` and `internal/cli/*.go` currently produce independently.

---

## 1. Engine facade — operation result types

All live in `internal/engine/types.go`. Each adapter (REST/gRPC/MCP) serializes
these verbatim — they are the contract that guarantees cross-transport parity
(FR-002/003, research R6).

### QueryHit / QueryResult

Returned by `Engine.Query(ctx, QueryRequest) (*QueryResult, error)`.

| Field | Type | Notes |
|---|---|---|
| `QueryResult.Hits` | `[]QueryHit` | ranked, top-K, post-threshold |
| `QueryHit.ChunkID` | `string` | existing `model.Chunk.ID` |
| `QueryHit.DocumentID` | `string` | collapsed parent doc (1 hit/doc) |
| `QueryHit.Score` | `float64` | RRF score, or reranker score if enabled |
| `QueryHit.Content` | `string` | chunk text (full, not preview-truncated) |
| `QueryHit.FilePath` | `string` | source file (from `model.Document`) |
| `QueryHit.Page` | `int` | page number for paginated sources; 0 if N/A |
| `QueryHit.Preview` | `string` | convenience truncated preview for text renders |

`QueryRequest`: `{ Query string; K int; Mode string; NoRerank bool; Threshold float64 }`.
`Mode ∈ {"hybrid","semantic","keyword"}` → `index.ParseMode`.

**Validation**: `K` clamped to `[1,100]`; `Query` non-empty; unknown `Mode` →
`hybrid` (mirrors `index.ParseMode` default). Threshold filters post-rank.

### StatusInfo

Returned by `Engine.Status(ctx) (*StatusInfo, error)`.

| Field | Type | Notes |
|---|---|---|
| `Documents` | `int` | count, `PrefixDocument` |
| `Chunks` | `int` | `PrefixChunk` |
| `Embeddings` | `int` | `PrefixEmbedding` |
| `Dimensions` | `int` | from a sample embedding vector |
| `EmbeddingModel` | `string` | `cfg.EmbeddingModel` |
| `Reranker` | `string` | `cfg.RerankModel`, or `"disabled"` |
| `OllamaURL` | `string` | `cfg.OllamaURL` |
| `EmbeddingsComplete` | `bool` | `embeddings == documents` (approx.) |

### IngestSummary

Returned by `Engine.Add`, `.Scan`, `.Reprocess`, `.Migrate`.

| Field | Type | Notes |
|---|---|---|
| `New` | `int` | newly ingested |
| `Skipped` | `int` | unchanged (idempotent no-op) |
| `Modified` | `int` | scan only |
| `Deleted` | `int` | scan only |
| `Errors` | `int` | per-file failures |

*(Mirrors existing `pipeline.Result` fields — facade exposes them as a stable
struct rather than a `fmt.Sprintf("new=%d skipped=%d …")`.)*

### FileEntry / DirEntry

Returned by `Engine.Files` / `Engine.Dirs`.

- `FileEntry{ FilePath, FileType, Status string; ChunkCount int }`
- `DirEntry{ Dir string; Files, Chunks int }`

### VaultInfo

Returned by `Engine.ListVaults` (no DB required).

- `VaultEntry{ Name string; Documents int }`

---

## 2. Engine facade — interface

`internal/engine/engine.go`. The facade holds `(cfg config.Config, db *storage.DB)`
and reuses existing packages — it adds **no** new storage/index/embed logic, only
orchestration and structured results. Sketch (illustrative, not final code):

```go
type Engine struct{ cfg config.Config; db *storage.DB }

func NewWithDB(cfg config.Config, db *storage.DB) *Engine

func (e *Engine) Query(ctx, req QueryRequest) (*QueryResult, error)   // wraps index.NewRetrieval + rerank
func (e *Engine) Status(ctx) (*StatusInfo, error)
func (e *Engine) Add(ctx, path string) (*IngestSummary, error)        // wraps pipeline.New + Ingest
func (e *Engine) Scan(ctx) (*IngestSummary, error)                    // wraps watcher.ScanOnce
func (e *Engine) Reprocess(ctx, path string) (*IngestSummary, error)
func (e *Engine) Migrate(ctx) (*IngestSummary, error)
func (e *Engine) Files(ctx) ([]FileEntry, error)
func (e *Engine) Dirs(ctx) ([]DirEntry, error)
func (e *Engine) GetConfig(ctx) (config.Config, error)
func (e *Engine) SetConfig(ctx, key, val string) error
func (e *Engine) ListVaults(ctx) ([]VaultEntry, error)
```

**Source of truth**: each method is the *single* implementation of that operation.
`internal/mcp`, `internal/rest`, `internal/grpc`, and (optionally) `internal/cli`
all call it. This is what makes go-rag's "unified engine" real and removes today's
duplication of `openDB`/`docOf`/`lookupChunk`/`countPrefix` across `cli/wire.go`
and `mcp/server.go`.

---

## 3. Daemon address book (extension of existing struct)

`internal/daemon/pid.go:Addrs` is extended (backward-compatible — new fields
default empty):

```go
type Addrs struct {
    MCPAddr  string `json:"mcp_addr"`
    RESTAddr string `json:"rest_addr"`  // NEW
    GRPCAddr string `json:"grpc_addr"`  // NEW
}
```

Persisted to `daemon.addrs` by an extended `daemon.Start`; read by `Status` and
the stop/health machinery so each transport's address is discoverable.

---

## 4. Unchanged persisted entities (for reference)

No changes to the Pebble key-space. Existing entities the facade reads/writes:

- **Source** — ingested file provenance (path, hash, timestamps)
- **Document** — `model.Document` (FilePath, FileType, Status, ChunkCount, …)
- **Chunk** — `model.Chunk` (ID, DocumentID, Content, page)
- **Embedding** — vector + model tag

Identity stays content-addressed (Principle II): re-adding an unchanged file over
any transport is a no-op because `pipeline.Ingest` derives the same SHA-256 ID.

---

## 5. Adapter DTOs (transport-specific, derived from the above)

Each adapter owns thin DTOs that map 1:1 to the facade types — they carry **no
independent logic**, only serialization:

- **gRPC**: `proto/gorag.proto` messages (`QueryRequest`, `QueryResponse`,
  `StatusResponse`, …) — see [contracts/gorag.proto](contracts/gorag.proto).
- **REST**: JSON DTOs in `internal/rest/types.go`, mirroring the proto — see
  [contracts/rest-openapi.yaml](contracts/rest-openapi.yaml).
- **MCP**: existing tool-call text rendering, now produced from the structured
  types instead of inline `fmt.Sprintf`.

A change to a facade type flows identically to all three DTO sets — the parity
test (research R6) fails if they drift.
