# HTTP Transport Contract — `internal/ui` Documents View (Slice 1)

**Spec**: [../spec.md](../spec.md) · **Data model**: [../data-model.md](../data-model.md)

The Documents view adds five read-only routes to the existing `internal/ui` mux
(spec 046). They are guarded by the same spec 045 Bearer middleware as the Dashboard
(`/api/dashboard/stats`); the shell `/`, `/static/*`, and `POST /login` stay public and
unchanged. The handlers call `internal/engine.Engine` **in-process** (R4) — they are not a
REST proxy.

## Auth model

Identical to spec 046's `/api/*` routes (see `contracts/ui-transport.md`):

- **Credential:** `Authorization: Bearer gorags_…` header only. **No cookies, ever.**
- **Guard:** `auth.Store.Validate(r)`, bypass-enabled. Failure → 401
  `{"error":"unauthorized"}` + `audit.Log`. All auth failures collapse to an identical
  401 (no status/body oracle).
- **Loopback bypass:** fires only on a bare **pre-init** vault (no admin). `init` creates
  an admin, so an initialized vault requires a Bearer.
- The view makes **no independent auth decision** — it funnels through `internal/auth`.

## Routes

### `GET /api/documents` — paginated document list (US1)

Query parameters (all optional):

| Param | Values | Default | Meaning |
|-------|--------|---------|---------|
| `page_size` | 1–200 | 50 | page size (`0` → default) |
| `page_token` | opaque cursor | (first page) | resume cursor from a prior `next_page_token` |
| `after` | RFC3339 | (all) | lower bound on `ingested_at` |
| `status` | `embedded`\|`pending`\|`error` | (all) | embedding-state filter |
| `tag` | repeatable string | (all) | tag filter, **match-any** (R3) |

- **Auth:** guard.
- **Response 200:** `application/json` —
  `{ "documents": [documentDTO…], "next_page_token": "<opaque>" }`. `next_page_token`
  empty on the last page. Empty result = empty array (never an error).
- **Response 400:** `{"error":"invalid"}` — bad `page_size`, non-RFC3339 `after`, unknown
  `status`, malformed `page_token`.
- **Parity:** the `documentDTO` field set is byte-identical to `GET /v1/documents` (REST)
  and `go_rag_list_documents` (MCP); `documents` count matches `go-rag status` (FR-013).
- **Note:** list rows omit `source_path` (the listing skips per-document source
  resolution for performance — consistent across all transports).

### `GET /api/documents/{id}` — document detail header (US2)

- **Auth:** guard.
- **Path param:** `id` = document identity hash.
- **Response 200:** `application/json` — `documentDTO` with `source_path` **resolved**.
- **Response 404:** `{"error":"not found"}` — unknown id.
- **Response 400:** `{"error":"invalid"}` — empty/whitespace id.

### `GET /api/documents/{id}/chunks` — paginated chunk list (US2)

Query parameters:

| Param | Values | Default | Meaning |
|-------|--------|---------|---------|
| `page_size` | 1–200 | 50 | page size |
| `page_token` | opaque cursor | (first page) | resume cursor |

- **Auth:** guard.
- **Response 200:** `application/json` —
  `{ "chunks": [chunkDTO…], "next_page_token": "<opaque>" }`, ordered by `chunk_index`
  ASC.
- **Response 404:** `{"error":"not found"}` — unknown document id.
- **Response 400:** `{"error":"invalid"}` — bad `page_size` / malformed `page_token`.
- **Engine:** the new `Engine.ListChunks` (R1). Parity-pinned against REST
  `GET /v1/documents/{id}/chunks` (shipped together).

### `GET /api/documents/{id}/chunks/{chunkID}/context?window=N` — chunk context (US2)

- **Auth:** guard.
- **Query param:** `window` 0–10 (default 2). `0` returns exactly the target (≡ GetChunk).
- **Response 200:** `application/json` —
  `{ "chunks": [chunkDTO…], "target_index": <int>, "document": documentDTO }`.
- **Response 404:** `{"error":"not found"}` — unknown chunk/document.
- **Response 400:** `{"error":"invalid"}` — `window` out of range, empty id.
- **Engine:** `Engine.GetChunkContext(chunkID, window)` (spec 037). Same shape as REST
  `GET /v1/chunks/{id}/context`.

### `GET /api/documents/search?q=<text>&limit=N` — content search (US3)

- **Auth:** guard.
- **Query params:** `q` (required, non-empty); `limit` 1–100 (default 20).
- **Response 200:** `application/json` —
  `{ "query": "<text>", "documents": [documentDTO…] }` — distinct parent documents of
  matching chunks, ranked by the retrieval order (R2).
- **Response 400:** `{"error":"invalid"}` — empty/missing `q`, bad `limit`.
- **Engine:** `Engine.Query` (hybrid BM25+vector+rerank) projected to distinct documents.
  Name/path matching is folded in client-side over the result; the server search is
  content-based.

## Error contract (all routes)

Identical to spec 046's `/api/*` contract:

- **400:** `{"error":"invalid"}` — bad argument.
- **401:** `{"error":"unauthorized"}` — identical body for any credential failure. No `Set-Cookie`.
- **404:** `{"error":"not found"}` — unknown document/chunk id.
- **500:** `{"error":"internal"}` — engine failure; no detail leakage.

No route accepts a body; every route is `GET`. No route mutates state (FR-009).

## Cross-transport parity

- The `documentDTO` shape matches REST `GET /v1/documents` / MCP `go_rag_list_documents`
  byte-for-byte (pinned by `TestCrossTransport_ListDocumentsParity` + a new UI parity case).
- The new `ListChunks` ships across engine + REST + gRPC + MCP + CLI + UI + proto (R1),
  so `GET /api/documents/{id}/chunks` matches REST `GET /v1/documents/{id}/chunks`.
- Document/chunk **counts** shown in the view match `go-rag status` (FR-013).

## Non-goals this slice

No write routes (add/remove/reingest/re-embed). No live streaming (watch is deferred,
R5). No "source changed" staleness indicator (deferred to the write-actions slice, R6).
No TLS (reverse proxy terminates). No multi-user/RBAC.
