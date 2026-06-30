# Implementation Plan: GetChunk — Fetch a Single Chunk by Content-Addressed ID

**Branch**: `035-get-chunk-rpc` *(single-author repo — work commits to `main`)* | **Date**: 2026-06-30 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from [`spec.md`](spec.md) — first item (`BL-001`) of the [go-rag ↔ MuninnDB bridge backlog](../../docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md).

**Note**: This template is filled in by the `/speckit-plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

`GetChunk` resolves a content-addressed `chunk_id` to its full chunk **plus** its parent document's metadata in a single call, exposed identically over gRPC / REST / MCP / CLI. It is the missing primitive that makes `chunk_id` a usable foreign key from MuninnDB (and any client) back into go-rag, unblocking the bridge's `ActivateWithRAG` pattern.

**Technical approach** (grounded in the codebase by the Phase-0 workflow — see [`research.md`](research.md)):

- **Engine.** One new method `GetChunk(chunkID string) (*ChunkResult, error)` — arity-identical to `ReleaseChunk`/`ResetChunk`. It composes the **existing** point-read helpers `lookupChunk` (prefix `0x03`) + `lookupDoc` (prefix `0x02`, keyed by the inline `chunk.DocumentID`). Two Pebble point Gets, no scan, corpus-size-independent.
- **Vault contract.** **No `vault` field** — the engine is single-vault-per-process, so every chunk-scoped RPC takes only `chunk_id`. The backlog's `string vault = 2` is dropped; the backlog's REST path `/api/vaults/{vault}/chunks/{chunk_id}` is corrected to `GET /v1/chunks/{id}` (matches the `/v1/poison/{id}/...` convention).
- **Messages.** New `Chunk` + `DocumentMeta` response messages (projections of `model.Chunk` / `model.Document`); **reuse** the existing `Poisoning` / `NearDup` / `PoisoningSignals` proto messages. `QueryHit` is the field template.
- **Not-found gap (the one real new surface).** Introduce `engine.ErrNotFound` and teach each transport's error mapper to surface it natively (`codes.NotFound` / `404` / MCP `-32001` / non-zero CLI exit). Today's bare `fmt.Errorf("chunk not found")` mis-maps to `Internal`/`500`; back-filling `ReleaseChunk`/`ResetChunk` is recommended.
- **Constitution gate holds.** No new prefix, value encoding, or key construction → no migration → `migrate.ExpectedVersion` stays at 1.

## Technical Context

> Filled by `/speckit-plan` for spec 035 (GetChunk). Values grounded in `PRD_RAG_Database.md` + constitution v1.1.0. The no-migration affirmation rests on Phase-0 research (see `research.md`).

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: cobra (CLI), Pebble (KV), chromem-go (vector index), grpc-go + `google.golang.org/protobuf` (gRPC), stdlib `net/http` (REST), fsnotify (watcher); optional local Ollama (embeddings). **GetChunk adds no new dependency (FR-010).**

**Storage**: ONE Pebble KV instance, prefix-partitioned single-byte key space (PRD §6.7). `GetChunk` is a read over the existing chunk + document prefixes — no second database, no sidecar files.

**Testing**: `go test -race -cover ./...` (`make test`); `make vet` + `make lint` (golangci-lint) are the push gates. Cross-transport parity test: the same `chunk_id` fetched over gRPC / REST / MCP / CLI returns byte-identical chunk + document metadata.

**Target Platform**: Single statically-linked binary, cross-compiled with `CGO_ENABLED=0` to linux/darwin/windows × amd64/arm64. Local-first, air-gapped.

**Project Type**: single-binary CLI + multi-transport daemon (MCP `:7878` / REST `:7879` / gRPC `:7880`) over one `internal/engine.Engine`.

**Performance Goals**: `GetChunk` = constant-time point lookup, single-digit milliseconds, **independent of corpus size** (SC-003, FR-007). Engine-wide budgets (write ACK <10ms, hybrid query <500ms) are unaffected — this read sits below all of them.

**Constraints**: pure Go / `CGO_ENABLED=0`; **no new stored state, no schema migration** (FR-010, FR-011); four-transport parity with identical results for the same `chunk_id` (FR-006); cross-vault isolation — a chunk from any vault other than the one named in the request is never disclosed (FR-003); binary <25 MB; single-writer Pebble (concurrent reads during writes are eventual-consistent).

**Scale/Scope**: latency flat from 10 chunks to 10M; one new engine method projected onto four transports; response surface = projection of existing `model.Chunk` + document-metadata structs (no new entities, no new identity scheme).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Constitution v1.1.0** — evaluated against all five Core Principles + Performance/Reliability + Storage discipline + Schema evolution. Result: **PASS, no violations** (Complexity Tracking table left empty).

| Principle | Gate | Evidence |
|---|---|---|
| **I. Local-First, Single-Binary** | ✅ PASS | `GetChunk` is a read over the local Pebble store; no cloud egress, no account, no network dependency. Ships inside the one binary. |
| **II. Content-Addressed Identity** | ✅ PASS | Reuses the existing SHA-256-derived `chunk_id`; introduces no new identity scheme and no new stored state (FR-008). |
| **III. Pure Go — No CGo** | ✅ PASS | Adds no dependency and no CGo (FR-010); reads over existing protobuf/Pebble. `CGO_ENABLED=0 go build ./...` still succeeds. |
| **IV. Async-After-ACK Writes** | ✅ N/A | `GetChunk` is a read — there is no write path and the <10ms write-ACK budget is untouched. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Delivered as one engine method projected onto **all four transports** — gRPC, REST, MCP, CLI — with identical results (FR-006). |
| **Performance & Reliability** | ✅ PASS | Constant-time point lookup (FR-007, SC-003); reads are below every existing latency budget; cold-start budget unaffected (no index load). |
| **Storage discipline / Schema evolution** | ✅ PASS | Reads existing chunk + document prefixes only — **no new prefix, value encoding, or key construction → no migration → `migrate.ExpectedVersion` unchanged** (FR-011). Exact `ExpectedVersion` value and the no-on-disk-layout-change affirmation are grounded in `internal/storage/migrate` — see `research.md`. |

**Compliance statement for the PR**: *No on-disk layout change. `GetChunk` adds no key-space prefix, value encoding, or key construction; `migrate.ExpectedVersion` is unchanged; no migration is added.*

## Project Structure

### Documentation (this feature)

```text
specs/035-get-chunk-rpc/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)
> go-rag maps 1:1 to PRD subsystems (see repo `CLAUDE.md`). Only the read path
> + the four transport adapters change; `internal/model`, `internal/storage`,
> and `internal/storage/migrate` are **unchanged** (read-only).

```text
cmd/go-rag/              # single binary entrypoint                    (unchanged)
proto/                   # gorag.proto: GetChunk RPC + Chunk/DocumentMeta msgs; regenerate proto/gen

internal/engine/         # NEW GetChunk(chunkID) method + ErrNotFound sentinel (errors.go)
internal/grpc/           # GetChunk adapter + toStatusErr NotFound branch      (engine_adapter.go)
internal/rest/           # GET /v1/chunks/{id}: routes + handlerFor + handleGetChunk + DTOs + openapi.yaml
internal/mcp/            # go_rag_get_chunk tool + dispatch + renderGetChunk    (server.go)
internal/cli/            # NEW chunk.go: `go-rag chunk get <chunk_id>`           (register newChunkCmd in root.go)

internal/model/          # UNCHANGED — read-only projection source (model.Chunk / model.Document / verdict.go)
internal/storage/        # UNCHANGED — read-only over PrefixChunk 0x03 + PrefixDocument 0x02 (db.GetWithPrefix)
internal/storage/migrate/# UNCHANGED — ExpectedVersion stays 1; no migration added

# Tests (cross-transport parity is the headline invariant, FR-006 / SC-001)
internal/engine/         # GetChunk unit test: found, not-found (ErrNotFound), invalid (ErrInvalid), orphan-chunk
internal/rest/           # extend parity test T035 (routes == openapi.yaml) for the new route
# Cross-transport parity test: same chunk_id over gRPC/REST/MCP/CLI → byte-identical chunk + document
```

**Structure Decision**: Single-binary Go project (the existing repo layout — no new top-level dirs). `GetChunk` is a read+project: it adds **one engine method**, **one CLI command**, and **one handler each** in the four transport adapters, plus proto messages. `internal/model`, `internal/storage`, and `internal/storage/migrate` are **unchanged** (read-only — no storage edit, no migration). The exact touch-points per transport are enumerated in [`contracts/get-chunk.md`](contracts/get-chunk.md); field projections in [`data-model.md`](data-model.md).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
