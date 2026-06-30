# Implementation Plan: GetChunkContext (BL-002)

**Branch**: `037-get-chunk-context-rpc` *(single-author repo — work commits to `main`; slug identifies the spec, not a git branch)* | **Date**: 2026-06-30 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/037-get-chunk-context-rpc/spec.md` (bridge backlog item BL-002).

## Summary

`GetChunkContext(chunk_id, window)` — the spec-035 `GetChunk` sibling that returns a chunk **plus up to `window` neighbours on each side** in one call, so the bridge's `ActivateWithRAG` context-expansion pattern stops chaining N `GetChunk` calls. Phase 0 verified the read mechanism: chunks are keyed `0x03 + chunkID`, the storage wrapper exposes point-gets (`GetWithPrefix`) but no multi-get/snapshot, and `Chunk` already carries the `PreviousChunkID`/`NextChunkID` linked list written atomically at ingest (spec 015). So the window is fetched by **following the linked list** — `lookupChunk(target)`, then up to `window` hops back via `PreviousChunkID` and forward via `NextChunkID`, each a Pebble point-get. That is ≤ `1 + 2·window` (max 21) sub-millisecond point-gets in one caller round-trip — a consistent-enough snapshot (the list is atomic at ingest; reads-during-writes are eventual per the constitution, acceptable for a point-access read of an existing document).

The operation mirrors `GetChunk` (spec 035) end-to-end: a new `Engine.GetChunkContext` returning a `ContextResult` (the `ChunkResult` shape plus an ordered chunk slice and `target_index`), a new `rpc GetChunkContext` + request/response messages (reusing the spec-035 `Chunk` message, which already carries `Wikilinks`), projected to all four transports, with `parity_test.go` extended.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra, pebble, grpc-go, protobuf. No new dependencies.

**Storage**: Pebble KV; chunk records under prefix `0x03`, keyed by content-addressed `chunkID`. Read-only access — no new key, no new prefix, no migration.

**Testing**: `go test -race -cover ./...`. Extends the spec-035 GetChunk tests + `internal/engine/parity_test.go`.

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine.

**Performance Goals**: bounded point-lookups — ≤ `1 + 2·window` Pebble point-gets (max 21 at `window=10`), each sub-millisecond, latency independent of corpus size.

**Constraints**: `<10ms` (read path, well within budget); pure Go; no schema migration; window capped at 10 (INVALID_ARGUMENT above), default 2.

**Scale/Scope**: **size S** — one new engine method + one proto RPC + four transport projections + tests, all mirroring the shipped spec-035 GetChunk. Smaller than spec 036.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Local Pebble point-reads. No cloud, no network egress. |
| **II. Content-Addressed Identity** | ✅ PASS | Read-only — resolves existing `chunk_id`s; introduces no new identity, no new stored state. |
| **III. Pure Go — No CGo** | ✅ PASS | Reuses `lookupChunk`/`lookupDoc`; no new deps, no CGo. |
| **IV. Async-After-ACK Writes** | ✅ PASS (N/A) | Pure read — no write path, no ACK budget impact. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Surfaced on all four transports (`Engine.GetChunkContext` → REST/gRPC/MCP/CLI). Parity asserted by `parity_test.go`. |
| **Storage discipline / Schema evolution** | ✅ PASS | No new/retired prefix, no key-construction change, no migration, `migrate.ExpectedVersion` unchanged. Additive proto RPC + messages only. |

No violations. No Complexity Tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/037-get-chunk-context-rpc/
├── plan.md              # this file
├── research.md          # Phase 0 — read-mechanism verification
├── data-model.md        # Phase 1 — ContextResult, windowing rules
├── quickstart.md        # Phase 1 — runnable validation
├── contracts/
│   └── api.md           # Phase 1 — wire contract (proto + REST + CLI + MCP)
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root — files touched)

```text
internal/engine/get_chunk.go (or new get_chunk_context.go)  # Engine.GetChunkContext + ContextResult; reuses lookupChunk/lookupDoc
internal/engine/errors.go        # (no change — reuse ErrInvalid/ErrNotFound)
internal/grpc/engine_adapter.go  # GetChunkContext handler + response projection (reuses Chunk projection)
proto/gorag.proto                # + rpc GetChunkContext; + GetChunkContextRequest/Response messages
proto/gen/                       # regenerated (protoc)
internal/rest/get_chunk.go (or new)  # GET /v1/chunks/{id}/context?window=N + DTO
internal/rest/server.go          # register the route
internal/cli/chunk.go            # `go-rag chunk context <id> [--window N]`
internal/mcp/server.go           # go_rag_get_chunk_context tool
internal/engine/get_chunk_context_test.go  # windowing: interior/boundary/single/window=0/window>10
internal/engine/parity_test.go   # cross-transport parity for GetChunkContext
```

**Structure Decision**: every edit lands in the PRD-mapped directory for its subsystem, mirroring where spec 035 (GetChunk) placed its files. The proto change is additive (one RPC + two messages). No new packages, no new key prefix.

## Phase Status

- **Phase 0 (Research)** — ✅ complete → [research.md](./research.md). The one open mechanic (read mechanism) is resolved: linked-list traversal via point-gets. No `NEEDS CLARIFICATION` remains.
- **Phase 1 (Design & Contracts)** — ✅ complete → [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md).
- **Phase 2 (Tasks)** — ⏭ next: `/speckit-tasks`.
