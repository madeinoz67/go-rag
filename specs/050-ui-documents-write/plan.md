# Implementation Plan: go-rag Management Console — Documents Write-Actions (Slice 4)

**Branch**: `main` (single-author repo; commits straight to `main`) | **Date**: 2026-07-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/050-ui-documents-write/spec.md`

## Summary

Slice 4 is the console's **first write surface** — it makes the Documents view (spec 047)
actionable. An operator can **add** a document by server-side path, **remove** a document, and
**reingest** a document, all from the browser, behind the existing spec 045 Bearer guard and
with confirmation on the destructive actions.

Two of the three operations are cheap: **add** and **reingest** are already exposed on **every**
transport (CLI / MCP / REST / gRPC) via `Engine.Add` and `Engine.Reprocess`. The UI simply
becomes a 5th adapter over them — no new engine method, cross-transport parity inherited. The
third — **remove** — is a **new operation**: `Pipeline.DeleteDoc(docID)` exists at the pipeline
level but is exposed on **no transport** today. Per the spec 047 precedent and constitution
Principle V, remove ships **cross-transport**: a new thin `Engine.DeleteDoc` wrapper plus CLI +
REST + gRPC + MCP + proto projections, mirroring how spec 047 shipped `ListChunks` everywhere.

Net: the slice is the UI write handlers for all three actions (dialogs, confirmation, async
ACK) PLUS the full cross-transport surface for the one new operation (delete). No new storage,
no migration, no Node chain.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); browser-side vendored Alpine.js 3.14
(already embedded). No Node/Vite/Tailwind.

**Primary Dependencies**:
- stdlib `net/http` (the UI transport) — Go 1.22 pattern mux
- `internal/engine` — `Engine.Add(ctx, path, glob)` (existing), `Engine.Reprocess(ctx, path)`
  (existing), **new** `Engine.DeleteDoc(ctx, docID)` (this slice, wraps `Pipeline.DeleteDoc`)
- `internal/pipeline` — `Pipeline.DeleteDoc(docID)` (existing; serializes via the spec 044
  per-doc lock, deletes from Pebble + the live FTS/Vector index)
- `internal/cli`, `internal/rest`, `internal/grpc`, `internal/mcp`, `proto/` — for the new
  delete operation's cross-transport projections (spec 047 pattern)
- `internal/auth` — spec 045 `auth.Validate` guard (unchanged, via `Server.guard`)
- Vendored Alpine.js (already embedded)

**Storage**: None new. Remove deletes keys within the **existing** document (0x02) and chunk
(0x03) prefixes and updates the live in-memory index — no new/retired prefix, no value-encoding
change, no key-construction change. **No migration, no `ExpectedVersion` bump.**

**Testing**: `go test -race`; cross-transport parity tests (delete ≡ across engine/CLI/REST/
gRPC/MCP; add/reingest ≡ their CLI); `curl -i` smoke for the new UI write routes; Interceptor
browser verify of add/remove/reingest render + confirmation. No JS test runner.

**Target Platform**: Loopback HTTP (`127.0.0.1:7881`), modern browser SPA. Single-operator.

**Project Type**: UI write adapter over two existing cross-transport operations + one new
operation shipped cross-transport.

**Performance Goals**:
- Add/reingest ACK ≤ the existing `Engine.Add`/`Reprocess` ACK (async-after-ACK, <10ms commit;
  embedding/indexing async — surfaces in Operations, spec 049).
- Remove completes in a bounded single-doc deletion (synchronous: K chunk deletes + live-index
  update); fast for typical documents.

**Constraints** (hard):
- Confirmation on destructive actions (remove, reingest) — client-side UX gate.
- Writes gated by the existing spec 045 Bearer guard — no write unauthenticated.
- Remove is **index-only** — never delete or modify the source file on disk.
- No file upload (multipart), no bulk/batch, no re-embed-only, no scan-trigger, no cross-vault,
  no undo/soft-delete, no tags-on-add (`Engine.Add` has no tags parameter).
- Pure Go, single binary, no Node/build chain.
- No storage layout change (remove mutates within existing prefixes).

**Scale/Scope**: ~11–13 files. New: `internal/engine/delete.go` (+test), `internal/cli/delete.go`,
`internal/rest/delete_document.go`, `internal/grpc/delete_document.go`, MCP edit, `proto/gorag.proto`
(+regen `proto/gen`), `internal/ui/documents_write.go` (+test). Edits: `internal/ui/ui.go`
(routes), `internal/ui/web/{static/js/app.js, static/css/components.css, templates/index.html}`.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback writes in the existing binary; in-process engine calls; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | Writes use the existing content-addressed engine — add dedups by SHA-256, reingest re-derives by source path, remove targets the content-addressed doc ID. The new `Engine.DeleteDoc` wrapper changes no identity rule. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | stdlib + engine + pipeline + transports. No new dependency; vendored Alpine already embedded. |
| IV | Async-After-ACK Writes | **PASS** | Add/reingest reuse the existing async-after-ACK path (<10ms commit, embed/index async). Remove is a **synchronous** bounded single-doc deletion (not an ingest) — it does not run through the async pipeline, which is correct: deletion is a read-modify-write of the live index, not an ACK-then-embed operation. Principle IV governs ingest ACKs; delete is a different op class. |
| V | Extension by Interface, MCP-First | **PASS** | Add/reingest are already on **all** transports (CLI/MCP/REST/gRPC); the UI joins as a 5th adapter — no parity debt. Remove is a **new** operation: it ships cross-transport here (engine + CLI + REST + gRPC + MCP + proto), mirroring the spec 047 / 035–039 precedent. No operation is left transport-asymmetric. |

**Storage discipline**: remove mutates keys within the existing 0x02/0x03 prefixes and the
live in-memory index — **no new/retired prefix, no value-encoding change, no key-construction
change**. **No migration, no `ExpectedVersion` bump.** Affirmed: zero schema-version impact.

**Gate verdict: PASS.** No Complexity Tracking entry required (no principle violated; no
storage change; the new operation ships with full cross-transport parity).

## Project Structure

### Documentation (this feature)

```text
specs/050-ui-documents-write/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1–R10 decisions)
├── data-model.md        # Phase 1 output (write route table + DTOs)
├── quickstart.md        # Phase 1 output (validation guide)
├── contracts/           # Phase 1 output (HTTP write contract)
│   └── ui-documents-write.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
# New operation (remove) shipped cross-transport (spec 047 pattern):
internal/engine/
  delete.go               # NEW (R3) — Engine.DeleteDoc(ctx, docID): thin wrapper over
                          #   Pipeline.DeleteDoc (resolves the pipeline, delegates). Read-write.
  delete_test.go          # NEW — delete by ID; unknown ID; live-index cleared (no phantom hits)
internal/cli/
  delete.go               # NEW — `go-rag delete <docID>` subcommand (parity → MCP)
internal/rest/
  delete_document.go      # NEW — DELETE /v1/documents/{id} projection (parity)
internal/grpc/
  delete_document.go      # NEW — Adapter.DeleteDocument (parity; depends on proto regen)
internal/mcp/server.go    # EDIT — renderDeleteDocument + go_rag_delete_document tool
proto/gorag.proto         # EDIT — DeleteDocumentRequest/Response + DeleteDocument rpc
proto/gen/                # REGEN — protoc-gen-go(-grpc)

# UI write adapter (all three actions):
internal/ui/
  documents_write.go      # NEW (R1,R4,R5,R6) — handleDocumentAdd (Engine.Add),
                          #   handleDocumentRemove (Engine.DeleteDoc), handleDocumentReingest
                          #   (resolve source path -> Engine.Reprocess); local request/summary DTOs
  documents_write_test.go # NEW — add/remove/reingest happy-path + parity + guard + errors
                          #   (empty path, unknown ID, vanished source) + cross-transport delete parity
  ui.go                   # EDIT — register POST /api/documents, DELETE /api/documents/{id},
                          #   POST /api/documents/{id}/reingest (all guarded)
  web/static/js/app.js    # EDIT — add dialog (path + optional glob), remove confirm, reingest
                          #   confirm; disable-on-submit; refresh list on ACK; pending badge
  web/static/css/components.css # EDIT — dialog/confirm primitives (reuse if present)
  web/templates/index.html # EDIT — Add button + dialog; per-row Remove/Reingest actions
```

**Structure decision**: No new package. The new operation (delete) follows the established
cross-transport pattern (engine wrapper + CLI + REST + gRPC + MCP + proto), exactly as spec 047
shipped `ListChunks`. The UI write surface is a new file (`internal/ui/documents_write.go`)
inside the existing UI transport — siblings to `documents.go` (the read view) — plus its
Alpine/template/CSS edits. Add/reingest need no engine change (they reuse the cross-transport
`Engine.Add`/`Reprocess`).

## Complexity Tracking

> None — Constitution Check is PASS with no violations. (No storage change; the new operation
> ships with full cross-transport parity; no principle bent.) The slice is larger than
> 048/049 because remove is a genuinely new operation, not a complexity-tracked deviation.
