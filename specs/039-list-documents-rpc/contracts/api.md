# Contract — `ListDocuments` (spec 039, BL-007)

> Phase 1 output for `/speckit-plan`. One new RPC, surfaced identically on every transport, mirroring spec 035 / 037 / 038. The canonical logic lives on `engine.ListDocuments` (`internal/engine/`). See [data-model.md](../data-model.md) for the filter/sort/paginate rules and [research.md](../research.md) (R1–R4) for the decisions.

## Operation contract

| Property | Value |
|----------|-------|
| Name | `ListDocuments` |
| Inputs | `page_size` (int32, default 50, max 200), `page_token` (opaque string), `after` (RFC3339 string), `status` (`embedded`\|`pending`\|`error`\|`""`) |
| Output | a page of `DocumentMeta` in `(ingested_at ASC, id ASC)` order + `next_page_token` |
| Ordering | `(ingested_at ASC, document_id ASC)` — total order |
| `after` | keep only documents with `ingested_at > after`; empty → all |
| `status` | exact match on document status; empty → all; AND with `after` |
| `page_size` | default 50; `<1` or `>200` → `INVALID_ARGUMENT` |
| `page_token` | opaque; empty → first page; malformed → `INVALID_ARGUMENT`; carries only the resume point |
| `next_page_token` | non-empty iff more matching docs remain; empty ⇒ last page |
| Empty result | empty `documents` + empty `next_page_token`; never an error |
| Parity | identical result across CLI, REST, gRPC, MCP for the same `(page_size, page_token, after, status)` |
| Cost | one prefix-scan + in-memory filter/sort/paginate; no index |

The operation is **additive** — a new RPC alongside `Files` (which stays as-is). No existing operation, field, or value changes. `Document.Status` already uses exactly `pending`\|`embedded`\|`error`, so the `status` filter maps 1:1.

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

Add the RPC to the `Gorag` service (after `BatchGetChunks`, before the closing `}`):

```proto
service Gorag {
  // ...existing RPCs...
  rpc BatchGetChunks(BatchGetChunksRequest) returns (BatchGetChunksResponse);
  // spec 039 (BL-007): list documents — reliable ingested_at cursor + status
  // filter + page_token pagination. → engine.ListDocuments (also REST
  // GET /v1/documents, MCP go_rag_list_documents, CLI `go-rag documents list`).
  rpc ListDocuments(ListDocumentsRequest) returns (ListDocumentsResponse);
}
```

New messages (reuse the spec-035 `DocumentMeta`):

```proto
message ListDocumentsRequest {
  int32  page_size  = 1; // default 50, max 200; <1 or >200 → INVALID_ARGUMENT
  string page_token = 2; // opaque; empty → first page
  string after      = 3; // RFC3339; only docs with ingested_at > after; "" → all
  string status     = 4; // embedded|pending|error|"" (all); AND with after
}

message ListDocumentsResponse {
  repeated DocumentMeta documents     = 1; // ordered (ingested_at ASC, id ASC)
  string                next_page_token = 2; // empty ⇒ last page
}
```

Regenerate `proto/gen`. Wire the handler + response projection in a new `internal/grpc/list_documents.go` (reuse `toDocumentMetaPB`).

### REST — `internal/rest/`

`GET /v1/documents?page_size=&page_token=&after=&status=` — register beside `/v1/files` and `/v1/dirs` (`internal/rest/server.go`: both the `routes` table and the `handlerFor` switch; **also** add the path to `openapi.yaml` — the openapi parity test asserts the two match).

Response (200):
```json
{
  "documents": [ { /* documentMetaDTO — same shape as GetChunk's document */ } ],
  "next_page_token": "..."   // omitted/empty when last page
}
```

Errors: **400** for invalid-argument (`page_size` out of [1,200], non-RFC3339 `after`, unknown `status`, malformed `page_token`). Empty result is **200** with an empty `documents` array (never 404; never an error).

### Engine (canonical) — `internal/engine/`

```go
func (e *Engine) ListDocuments(req ListDocumentsRequest) (*ListDocumentsResult, error)
// ListDocumentsRequest{ PageSize, PageToken, After, Status }
// ListDocumentsResult{ Documents []model.Document; NextPageToken string }
```

Errors: `ErrInvalid` (page_size out of range, malformed `after`/`status`/`page_token`). An empty result is NOT an error. The page-token codec (`encodePageToken`/`decodePageToken`) lives in this package (pure functions; data-model.md).

### CLI — `internal/cli/`

`go-rag documents list [--page-size N] [--after T] [--status embedded] [--page-token TOK] [--format json|text]` (a new `documents` parent command beside `files`/`dirs`). JSON envelope `{ documents: […], next_page_token }` matches the proto/REST shape. Reject `--page-size > 200` with a non-zero exit.

### MCP — `internal/mcp/server.go`

`go_rag_list_documents` tool (beside the file/dir tools). Args: `{ page_size?, page_token?, after?, status? }`. Renders the document list (one line per doc: file path, status, ingested_at) + the next_page_token line. Add to `dispatchDB` + `toolDefs()`; bump the tool-count tests (22 → 23).

---

## Parity & determinism

- **Parity (FR-011):** `internal/engine/parity_test.go` is extended with `TestCrossTransport_ListDocumentsParity` — `ListDocuments` returns identical document-id lists (in order), the same `next_page_token`, and the same per-document metadata across CLI, REST, gRPC, and MCP for the same `(page_size, page_token, after, status)`; covers a multi-page corpus with `after` + `status` set (mirrors the GetChunk-family parity coverage).
- **Determinism:** the result is a pure function of `(page_size, page_token, after, status)` and the stored documents — fully deterministic for a fixed document set; re-fetching a page yields byte-identical results. Ordering is total `(ingested_at, id)`.
- **Pagination (FR-006/FR-007):** parity + engine tests cover multi-page iteration (every matching doc exactly once, in order, empty `next_page_token` on the last page) composing with `after` + `status`.

## Backward compatibility

- Pure-additive RPC + messages. No existing field's tag, type, or value changes.
- No on-disk key-space layout change; no migration; `migrate.ExpectedVersion` unchanged (`ingested_at` verified reliable — research.md R2).
- `Files` and `Dirs` are unchanged — `ListDocuments` is a sibling, not a replacement.
- Old clients are unaffected — they simply do not call the new RPC.
