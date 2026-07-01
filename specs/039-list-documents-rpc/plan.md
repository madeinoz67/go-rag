# Implementation Plan: ListDocuments (BL-007)

**Branch**: `039-list-documents-rpc` *(single-author repo — work commits to `main`; slug identifies the spec, not a git branch)* | **Date**: 2026-07-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/039-list-documents-rpc/spec.md` (bridge backlog item BL-007).

## Summary

`ListDocuments(page_size, page_token, after, status)` — a NEW document-listing operation (go-rag has `Files` = a flat file listing, but no paginated document list) returning `DocumentMeta[]` (the spec-035 projection) in ascending `ingested_at` order, plus an opaque `next_page_token`. It is the bridge's reliable incremental-listing primitive: fetch only documents ingested since an `after` cursor, filtered by `status`, paginated — for crash-recovery and the polling fallback until BL-008's push stream lands.

The headline design surface is **pagination**, which is new to go-rag: an opaque `page_token` encodes the last-returned `(ingested_at, document_id)` so the next page resumes strictly after it under `ORDER BY ingested_at ASC, id ASC`. Filtering (`after` + `status`, AND) and ordering happen in-memory over a `storage.PrefixScan` of the document prefix (`0x02`); no secondary index is needed for v1. The `ingested_at`-reliability half is already satisfied by construction (`processFile` sets `IngestedAt = now`; content-addressed identity mints a fresh record on changed-content re-ingest) — verified, no migration.

The operation mirrors the GetChunk family (spec 035/037/038): a new `Engine.ListDocuments` returning a `ListDocumentsResult{Documents, NextPageToken}`, a new `rpc ListDocuments` + request/response messages (reusing `DocumentMeta`), projected to all four transports, with `parity_test.go` extended.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra, pebble, grpc-go, protobuf. No new dependencies.

**Storage**: Pebble KV; document records under prefix `0x02`, keyed by content-addressed document ID. Read-only access via `storage.PrefixScan` — no new key, no new prefix, no migration.

**Testing**: `go test -race -cover ./...`. Extends the GetChunk-family tests + `internal/engine/parity_test.go`.

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine.

**Performance Goals**: one `PrefixScan` over the document prefix + in-memory filter/sort/paginate. Cost is O(documents) per call (acceptable for v1 vault sizes); the `after` cursor bounds the working set for incremental polls. Bounded page size (≤200).

**Constraints**: pure Go; no schema migration (default — `ingested_at` verified reliable); `page_size` ∈ [1,200]; opaque `page_token`; no `vault` field.

**Scale/Scope**: **size S-to-M** — one new engine method + one proto RPC + four transport projections + tests + a new pagination primitive. The pagination is the only new pattern vs the shipped GetChunk family (spec 035/037/038).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Local Pebble prefix-scan. No cloud, no network egress. |
| **II. Content-Addressed Identity** | ✅ PASS | Read-only — lists existing documents; introduces no new identity, no new stored state. (Content-addressing is also why `ingested_at` is reliable: changed content → new id → fresh record.) |
| **III. Pure Go — No CGo** | ✅ PASS | Reuses `PrefixScan` + the `DocumentMeta` projection; no new deps, no CGo. |
| **IV. Async-After-ACK Writes** | ✅ PASS (N/A) | Pure read — no write path, no ACK budget impact. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Surfaced on all four transports (`Engine.ListDocuments` → REST/gRPC/MCP/CLI). Parity asserted by `parity_test.go`. |
| **Storage discipline / Schema evolution** | ✅ PASS | No new/retired prefix, no key-construction change, no migration, `migrate.ExpectedVersion` unchanged. Additive proto RPC + messages only. The `page_token` is an in-memory artefact, never persisted. |

No violations. No Complexity Tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/039-list-documents-rpc/
├── plan.md              # this file
├── research.md          # Phase 0 — pagination encoding + ingested_at verification + iteration
├── data-model.md        # Phase 1 — ListDocumentsResult, filters, pagination rules
├── quickstart.md        # Phase 1 — runnable validation
├── contracts/
│   └── api.md           # Phase 1 — wire contract (proto + REST + CLI + MCP)
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root — files touched)

```text
internal/engine/list_documents.go (new)     # Engine.ListDocuments + ListDocumentsResult + page-token codec
internal/engine/errors.go                    # (no change — reuse ErrInvalid)
internal/grpc/list_documents.go (new)        # ListDocuments handler + response projection (reuses toDocumentMetaPB)
proto/gorag.proto                            # + rpc ListDocuments; + ListDocumentsRequest/Response messages
proto/gen/                                   # regenerated (protoc)
internal/rest/list_documents.go (new)        # GET /v1/documents + DTO
internal/rest/server.go                      # register the route (+ openapi parity test)
internal/rest/openapi.yaml                   # + /v1/documents path
internal/cli/documents.go (new)              # `go-rag documents list` (new parent cmd `documents`)
internal/mcp/server.go                       # go_rag_list_documents tool (+ dispatch + toolDefs; count tests 22→23)
internal/engine/list_documents_test.go (new) # after/status/pagination/order/edge validation
internal/engine/parity_test.go               # cross-transport parity for ListDocuments
```

**Structure Decision**: every edit lands in the PRD-mapped directory for its subsystem, mirroring where spec 035/037/038 placed their files. The proto change is additive (one RPC + two messages). The CLI gains a new `documents` parent command (`go-rag documents list`) — `files`/`dirs` remain under root. No new packages, no new key prefix. The page-token codec lives in the engine package (pure function; no persistence).

## Phase Status

- **Phase 0 (Research)** — ✅ complete → [research.md](./research.md). The two Research-Note questions are resolved: `page_token` = opaque base64 of `(ingested_at, document_id)`; `ingested_at` reliability verified (set at ingest; re-ingest mints fresh record) — no migration. No `NEEDS CLARIFICATION` remains.
- **Phase 1 (Design & Contracts)** — ✅ complete → [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md).
- **Phase 2 (Tasks)** — ⏭ next: `/speckit-tasks`.
