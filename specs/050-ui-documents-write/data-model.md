# Data Model — Documents Write-Actions (Slice 4)

**Feature**: specs/050-ui-documents-write | **Date**: 2026-07-12

Phase 1 output. The slice introduces **no new persistent data** — writes flow through the
existing engine write path (`Engine.Add` / `Reprocess` / the new `DeleteDoc` wrapper over
`Pipeline.DeleteDoc`). This document defines the three write routes and their request/response
DTOs.

---

## Route table (new — UI transport)

| Method | Path | Auth | Body | Returns | Maps to |
|--------|------|------|------|---------|---------|
| POST | `/api/documents` | `Server.guard` | `addRequestDTO` | `ingestSummaryDTO` (200) | `Engine.Add` (in-process) |
| DELETE | `/api/documents/{id}` | `Server.guard` | — | 204 No Content | `Engine.DeleteDoc` (in-process) |
| POST | `/api/documents/{id}/reingest` | `Server.guard` | — | `ingestSummaryDTO` (200) | resolve source path → `Engine.Reprocess` |

All three are guarded by the spec 045 Bearer session via `Server.guard`. Confirmation for the
two destructive actions (DELETE, reingest) is a **client-side UX gate** (R7) — the server
executes the authenticated mutation on receipt.

The new **delete** operation ALSO ships on the other transports (constitution V, R3):
`go-rag delete <id>` (CLI), `DELETE /v1/documents/{id}` (REST), `DeleteDocument` (gRPC),
`go_rag_delete_document` (MCP), + proto. Add/reingest need no other-transport work (already
fully cross-transport).

---

## Request DTOs

**addRequestDTO** (POST `/api/documents`):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `path` | string | — (required) | Server-side path (file or directory) the daemon reads. Empty → 400 `path required`. |
| `glob` | string | `""` (engine uses `*`) | Optional file-glob filter for directory adds. |

(No `tags` field — `Engine.Add` has no tags parameter; tags come from enrichment, spec 029.)

Reingest (POST `/api/documents/{id}/reingest`) and remove (DELETE) take **no body** — the doc
ID is in the URL path.

---

## Response DTOs

**ingestSummaryDTO** (add + reingest, 200) — projects `engine.IngestSummary`:

| Field | Type | Notes |
|-------|------|-------|
| `new` | int | Documents newly ingested. |
| `skipped` | int | Documents skipped (e.g. duplicate content on add). |
| `errors` | int | Documents that errored. |
| `path` | string | The path operated on (echoed). |

(Additional `IngestSummary` fields surfaced only if present — e.g. a per-file error list. The
plan confirms the exact `IngestSummary` shape; the DTO projects it field-parallel.)

**Remove** returns **204 No Content** (no body) — the document is gone.

---

## State transitions

- **Add**: path → (engine ACK <10ms) → document appears (pending embed) → (async embed/index)
  → embedded. The pending→embedded transition surfaces in Operations (spec 049) and the
  Documents list without operator action beyond refresh.
- **Reingest**: existing doc → (delete-then-re-add by source path, bypassing dedup) → chunks/
  embeddings re-derived → list reflects the updated state.
- **Remove**: existing doc → (synchronous delete: Pebble + live index) → gone from list, status,
  and query results. Source file on disk unchanged.

All three are **atomic at ACK** (add/reingest ACK-or-not; remove completes fully or errors).

---

## Validation rules (enforced at the UI handler)

- `path` empty/whitespace on add → 400 `path required`.
- Unknown doc ID (remove/reingest) → 404 `not found`.
- Reingest of a doc whose source file no longer exists → 404/409 `source not found` (distinct
  from a successful empty reingest).
- Malformed JSON body → 400 `invalid request body`.
- Engine errors → `writeEngineErr` (existing helper, same package) → plain operator-actionable
  messages; no silent failures.

---

## Sub-DTO: source-path resolution (reingest)

Reingest resolves the doc's source path server-side: `POST /api/documents/{id}/reingest` →
`Engine.GetDocument(id)` (or the document store) → `sourcePath` → `Engine.Reprocess(sourcePath)`.
The operator never types the path; the handler derives it from the ID. If the doc has no
resolvable source path (synthetic / caption chunks), reingest returns a clear error.
