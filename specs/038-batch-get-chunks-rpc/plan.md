# Implementation Plan: BatchGetChunks (BL-003)

**Branch**: `038-batch-get-chunks-rpc` *(single-author repo — work commits to `main`; slug identifies the spec, not a git branch)* | **Date**: 2026-07-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/038-batch-get-chunks-rpc/spec.md` (bridge backlog item BL-003).

## Summary

`BatchGetChunks(chunk_ids)` — the spec-035/spec-037 sibling that resolves **up to 100 chunks by content-addressed ID in one call**, returning one result per requested ID **in request order**. It is the third member of the GetChunk family (get-one → get-window → get-batch) and the direct enabler of the MuninnDB bridge's bulk-sync path (`BatchRemember`), collapsing N `GetChunk` round-trips into one.

The key design difference from `GetChunk`/`GetChunkContext` is the **per-id error model (partial success)**: a missing or cross-vault ID yields a result entry with an empty chunk and `error = "not found"` — the call itself never fails for one bad ID. Only structurally invalid requests (oversized, empty, or containing an empty/whitespace element) fail at the call level with `ErrInvalid`. Mechanically it is a loop of `lookupChunk` point-Gets over prefix `0x03` — at most 100 sub-millisecond Pebble point-Gets in one caller round-trip. Chunks are immutable once written (content-addressed), so the N point-Gets yield a consistent snapshot with no need for a snapshot/transaction primitive.

The operation mirrors `GetChunk` (spec 035) end-to-end: a new `Engine.BatchGetChunks` returning a `BatchResult` (ordered `BatchItem` slice), a new `rpc BatchGetChunks` + request/response/result messages (reusing the spec-035 `Chunk` + `DocumentMeta`), projected to all four transports, with `parity_test.go` extended.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra, pebble, grpc-go, protobuf. No new dependencies.

**Storage**: Pebble KV; chunk records under prefix `0x03`, keyed by content-addressed `chunkID`. Read-only access — no new key, no new prefix, no migration.

**Testing**: `go test -race -cover ./...`. Extends the spec-035/037 tests + `internal/engine/parity_test.go`.

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine.

**Performance Goals**: ≤ 100 Pebble point-Gets (one per requested ID), each sub-millisecond; latency independent of corpus size. One caller round-trip.

**Constraints**: pure Go; no schema migration; request cap 100 IDs (inclusive); per-id error model (partial success — never a call-level NOT_FOUND for a missing ID).

**Scale/Scope**: **size S** — one new engine method + one proto RPC + four transport projections + tests, all mirroring the shipped spec-035/037 GetChunk family. Smaller than spec 037 (no linked-list walk; just N independent point-Gets).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Local Pebble point-reads. No cloud, no network egress. |
| **II. Content-Addressed Identity** | ✅ PASS | Read-only — resolves existing `chunk_id`s; introduces no new identity, no new stored state. |
| **III. Pure Go — No CGo** | ✅ PASS | Reuses `lookupChunk`/`lookupDoc`; no new deps, no CGo. |
| **IV. Async-After-ACK Writes** | ✅ PASS (N/A) | Pure read — no write path, no ACK budget impact. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Surfaced on all four transports (`Engine.BatchGetChunks` → REST/gRPC/MCP/CLI). Parity asserted by `parity_test.go`. |
| **Storage discipline / Schema evolution** | ✅ PASS | No new/retired prefix, no key-construction change, no migration, `migrate.ExpectedVersion` unchanged. Additive proto RPC + messages only. |

No violations. No Complexity Tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/038-batch-get-chunks-rpc/
├── plan.md              # this file
├── research.md          # Phase 0 — read mechanism + per-id-error + DocumentMeta shape
├── data-model.md        # Phase 1 — BatchResult, validation, resolution algorithm
├── quickstart.md        # Phase 1 — runnable validation
├── contracts/
│   └── api.md           # Phase 1 — wire contract (proto + REST + CLI + MCP)
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root — files touched)

```text
internal/engine/batch_get_chunks.go (new)   # Engine.BatchGetChunks + BatchResult/BatchItem; reuses lookupChunk/lookupDoc
internal/engine/errors.go                    # (no change — reuse ErrInvalid; no call-level ErrNotFound for batch)
internal/grpc/batch_get_chunks.go (new)      # BatchGetChunks handler + response projection (reuses toChunkPB/toDocumentMetaPB)
proto/gorag.proto                            # + rpc BatchGetChunks; + BatchGetChunksRequest/Result/Response messages
proto/gen/                                   # regenerated (protoc)
internal/rest/batch_get_chunks.go (new)      # POST /v1/chunks/batch + DTO
internal/rest/server.go                      # register the route (+ openapi parity test)
internal/rest/openapi.yaml                   # + /v1/chunks/batch path
internal/cli/chunk.go                        # `go-rag chunk batch <id> [<id>...]`
internal/mcp/server.go                       # go_rag_batch_get_chunks tool (+ dispatch + toolDefs; count tests 21→22)
internal/engine/batch_get_chunks_test.go (new) # order/missing/duplicates/cap/validation/orphan
internal/engine/parity_test.go               # cross-transport parity for BatchGetChunks
```

**Structure Decision**: every edit lands in the PRD-mapped directory for its subsystem, mirroring where spec 035 (GetChunk) and spec 037 (GetChunkContext) placed their files. The proto change is additive (one RPC + three messages). No new packages, no new key prefix.

## Phase Status

- **Phase 0 (Research)** — ✅ complete → [research.md](./research.md). The two Research-Note questions are resolved: read mechanism = N `lookupChunk` point-Gets (no `MultiGet` exists; chunks are immutable → consistent without a transaction); `DocumentMeta` shape = per-result (mirrors `GetChunk` 1:1 for parity). No `NEEDS CLARIFICATION` remains.
- **Phase 1 (Design & Contracts)** — ✅ complete → [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md).
- **Phase 2 (Tasks)** — ⏭ next: `/speckit-tasks`.
