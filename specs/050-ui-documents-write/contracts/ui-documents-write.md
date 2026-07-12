# Contract: UI Documents Write-Actions

**Feature**: specs/050-ui-documents-write | **Date**: 2026-07-12

The HTTP contracts for the three write routes on the UI transport (`127.0.0.1:7881`), guarded
by the spec 045 Bearer session via `Server.guard`. The UI calls the engine in-process (5th
adapter). Field-level definitions live in [data-model.md](../data-model.md).

---

## `POST /api/documents` — add by server-side path

**Auth**: `Authorization: Bearer <gorags_ session token>`.

**Request** — `application/json`:
```json
{ "path": "/home/user/notes/spec.md", "glob": "" }
```
`path` required (empty → 400 `path required`); `glob` optional (default `""` → engine uses `*`).
No `tags` field (`Engine.Add` has none; tags come from enrichment, spec 029).

**Response 200** — `application/json` (acks fast; embedding continues async):
```json
{ "new": 1, "skipped": 0, "errors": 0, "path": "/home/user/notes/spec.md" }
```

**Response 400** — `{"error":"path required"}` (empty/whitespace path) or
`{"error":"invalid request body"}`.
**Response 401** — `{"error":"unauthorized"}`.
**Response 404/500** — engine error via `writeEngineErr` (e.g. path not readable,
permission denied).

---

## `DELETE /api/documents/{id}` — remove a document (index-only)

**Auth**: `Authorization: Bearer <gorags_ session token>`.

**Path**: `{id}` = the content-addressed document ID.

**Response 204** — No Content. The document + its chunks are deleted from the index; the source
file on disk is **untouched**.

**Response 404** — `{"error":"not found"}` (unknown / already-removed ID).
**Response 401** — `{"error":"unauthorized"}`.

(Client-side: a confirmation dialog MUST precede this request — R7.)

---

## `POST /api/documents/{id}/reingest` — reingest a document

**Auth**: `Authorization: Bearer <gorags_ session token>`.

**Path**: `{id}` = the document ID. The handler resolves the doc's source path, then calls
`Engine.Reprocess(sourcePath)` — bypassing dedup, re-deriving chunks/embeddings.

**Response 200** — `application/json` (same `ingestSummaryDTO` shape as add):
```json
{ "new": 1, "skipped": 0, "errors": 0, "path": "/home/user/notes/spec.md" }
```

**Response 404** — `{"error":"not found"}` (unknown doc ID) or `{"error":"source not found"}`
(the source file no longer exists).
**Response 401** — `{"error":"unauthorized"}`.

(Client-side: a confirmation dialog MUST precede this request — R7.)

---

## Cross-transport delete (constitution V — the new operation)

Remove also ships on every transport (not just the UI), mirroring spec 047:

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag delete <docID>` |
| REST | `DELETE /v1/documents/{id}` → 204 / 404 |
| gRPC | `DeleteDocument(DeleteDocumentRequest{doc_id}) → DeleteDocumentResponse` |
| MCP | `go_rag_delete_document` tool |
| proto | `message DeleteDocumentRequest{string doc_id=1;} message DeleteDocumentResponse{}` + `rpc DeleteDocument(DeleteDocumentRequest) returns (DeleteDocumentResponse)` |

Add (`go-rag add` / `POST /v1/add` / `go_rag_add`) and reingest (`go-rag reprocess` /
`POST /v1/reprocess` / `go_rag_reprocess`) are **already** cross-transport — no new work there.

---

## Non-goals of this contract

- No file upload (multipart) — add is path-based.
- No bulk/batch — one add/remove/reingest per request.
- No re-embed-only, no scan-trigger (Operations' domain), no source-file deletion, no cross-vault
  writes, no undo/soft-delete.
- No server-side confirmation token — confirmation is client-side UX (R7).
