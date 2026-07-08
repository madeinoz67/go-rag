# Implementation Plan: go-rag Management Console — Documents View (Slice 1)

**Branch**: `main` (single-author repo; commits straight to `main`) | **Date**: 2026-07-08 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/047-ui-documents-view/spec.md`

## Summary

Slice 1 of the go-rag management console: replace the **Documents** placeholder
(view 2 of the spec 046 shell) with the first real corpus-management surface — a
read-only **browse → inspect → find** flow over the document corpus. The view lists
documents (paginated, with embedding/enrichment status and tags), drills into a
document's metadata + enrichment summary + chunks (each with section context), and
searches the corpus by name/path or by chunk content.

The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine
`goragApp` root, and spec 045 Bearer auth **unchanged**. It calls the engine
**in-process** (like the Dashboard calls `engine.Status()`), introducing **no new
transport, no new storage, no new auth, and no Node build chain**.

One new read-only engine capability is required: **`Engine.ListChunks`** — paginated
chunk listing for a document (the detail view's enabling accessor). It ships with
full cross-transport parity (engine + REST + gRPC + MCP + CLI + proto), mirroring how
spec 039 shipped `ListDocuments`. The tag filter is an additive optional param on the
existing `ListDocumentsRequest`. Content search reuses `Engine.Query`.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); browser-side vendored Alpine.js 3.14
(already embedded from spec 046). No Node/Vite/Tailwind.

**Primary Dependencies**:
- stdlib `net/http` (the UI transport, unchanged from spec 046)
- `internal/engine` — `Engine.ListDocuments` (spec 039), **new** `Engine.ListChunks`
  (this slice), `Engine.GetChunk` (035), `Engine.GetChunkContext` (037), `Engine.Query`
- `internal/model` — `Document` / `Chunk` / `EnrichInfo` (read projections only)
- `internal/auth` — spec 045 `auth.Validate` guard (unchanged)
- Vendored Alpine.js (already embedded)

**Storage**: None new. Read-only projections over existing Pebble prefixes
(0x02 documents, 0x03 chunks). No new prefix, no migration, no `ExpectedVersion` bump.

**Testing**: `go test -race`; `curl -i` smoke for the new UI routes (loopback bypass +
Bearer regimes); cross-transport parity test for `ListChunks` (pattern of
`internal/engine/parity_test.go::TestCrossTransport_ListDocumentsParity`); Interceptor
browser verify of browse/inspect/search render. No JS test runner (vendored SPA).

**Target Platform**: Loopback HTTP (`127.0.0.1:7881`, the spec 046 UI transport), modern
browser SPA. Single-operator.

**Project Type**: Additive view on an existing web-service transport + one new read-only
engine accessor with cross-transport projections.

**Performance Goals**:
- Document list first page ≤ existing `GET /v1/documents` latency (same engine call).
- Detail chunk page ≤ `BatchGetChunks`-class latency (prefix scan over 0x03).
- Content search ≤ existing `Engine.Query` latency.

**Constraints** (hard):
- Read-only — no add/remove/reingest/re-embed (spec FR-009).
- No Node/build chain (CLAUDE.md; single-binary).
- No new storage / no migration (constitution Storage Discipline).
- Cross-transport parity for the new `ListChunks` accessor (constitution V).
- UI calls the engine in-process; it is a 4th adapter, not a REST proxy.

**Scale/Scope**: ~6–10 new/edited files: `internal/engine/list_chunks.go` (+test),
`internal/rest/list_chunks.go`, `internal/grpc/list_chunks.go`, `internal/mcp` edit,
`internal/cli/chunk.go` edit, `proto/gorag.proto` (+regen), `internal/ui/documents.go`
(+test), and `internal/ui/web/{templates/index.html,static/js/app.js}` edits.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The go-rag constitution (`.specify/memory/constitution.md`, v1.1.0) defines five Core
Principles. Slice 1 is evaluated against each:

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback view in the existing binary; new accessor is pure Go in the same binary; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | Read-only. No document/chunk identity, content hash, or ingest path is touched. The new `tag` filter param and `ListChunks` scan read existing keys only. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | stdlib + engine + model. No new Go dependency; vendored Alpine already embedded. |
| IV | Async-After-ACK Writes | **PASS** | Strictly read-only slice — no write path, so the <10ms write-ACK budget is unaffected. |
| V | Extension by Interface, MCP-First | **PASS** | The new `ListChunks` accessor ships across **all** transports (engine + REST + gRPC + MCP + CLI) + proto, mirroring spec 035/037/038/039 — no parity debt. The `tag` filter is an additive optional param on an existing request (backward-compatible). |

**Gate verdict: PASS.** No Complexity Tracking entry required (no principle violated;
no storage change).

## Project Structure

### Documentation (this feature)

```text
specs/047-ui-documents-view/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1–R8 decisions)
├── data-model.md        # Phase 1 output (DTOs + route table)
├── quickstart.md        # Phase 1 output (validation guide)
├── contracts/           # Phase 1 output (HTTP transport contract)
│   └── ui-documents.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
internal/engine/
  list_chunks.go          # NEW (R1) — Engine.ListChunks: paginated chunk scan over 0x03
  list_chunks_test.go     # NEW — pagination + filter + parity-shape tests
  list_documents.go       # EDIT (R3) — add optional Tags []string filter to ListDocumentsRequest + filter pass
internal/rest/
  list_chunks.go          # NEW — GET /v1/documents/{id}/chunks projection (parity)
  list_documents.go       # EDIT — bind ?tag= (additive, optional)
internal/grpc/
  list_chunks.go          # NEW — Adapter.ListChunks (parity)
internal/mcp/server.go    # EDIT — renderListChunks + renderListDocuments tag arg (parity)
internal/cli/chunk.go     # EDIT — `go-rag chunk list <docID>` subcommand (parity → MCP)
proto/gorag.proto         # EDIT — ListChunksRequest/Response messages + ListChunks rpc; tag field on ListDocumentsRequest
proto/gen/                # REGEN — protoc-gen-go(-grpc)

internal/ui/
  documents.go            # NEW — Documents view handlers: list / detail / chunks / search → engine, in-process
  ui_test.go              # EDIT — view tests + ListChunks UI parity
  web/static/js/app.js    # EDIT — Documents view (Alpine): list, detail, chunk pager, search/filter
  web/templates/index.html# EDIT — Documents view template; sidebar active state; replaces the placeholder
```

**Structure decision**: No new package. The view is a new file (`internal/ui/documents.go`)
inside the existing UI transport, plus its Alpine/template edits — exactly how the
Dashboard lives in `internal/ui/dashboard.go`. The one new engine capability
(`ListChunks`) follows the established cross-transport pattern (engine + REST + gRPC +
MCP + CLI + proto), mirroring spec 039. The `tag` filter is an additive optional param
on the existing `ListDocumentsRequest`.

## Complexity Tracking

> None — Constitution Check is PASS with no violations. (No storage change, no
> migration, no principle bent.) The new `ListChunks` accessor is additive read-only
> surface, not a complexity-tracked deviation.
