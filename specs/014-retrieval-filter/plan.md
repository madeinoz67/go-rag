# Implementation Plan: Metadata Filtering at Retrieval (H14)

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main`) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/014-retrieval-filter/spec.md` (audit backlog item **H14**, P1).

## Summary

Add an optional **Filter** (source path glob, file type, tags) to the query operation. The filter is a predicate over a chunk's **document attributes** (`FilePath`, `FileType`, `Metadata`), applied to the FTS and Vector candidate lists **before RRF fusion** — so a query can be scoped to a document subset without retrieving, fusing, and ranking non-matching chunks. The engine builds the predicate from `QueryRequest.Filter` + the existing `docOf`/`lookupDoc` resolvers (chunk→document→attributes). The filter is opt-in (absent = today's behavior, byte-identical). Exposed on all four transports (CLI `--source`/`--type`/`--tags` wired, REST/gRPC/MCP request fields); the CLI's dead `--source` flag gets wired.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib (`path/filepath` for glob, `strings`); existing `internal/index`, `internal/engine`, `internal/model`. Proto regen for gRPC (protoc command known from spec 009). **No new dependencies.**

**Storage**: Pebble KV — **N/A**. The filter is request-state + retrieval logic. No new persisted data; the filter evaluates existing `Document` attributes (`FilePath`, `FileType`, `Metadata`) resolved via the existing chunk→doc lookup. Principle II intact.

**Testing**: `go test -race -cover ./...` (`make test`); H02 eval gate (SC-004 — default queries carry no filter → no regression). New tests: `internal/index` (Filter type + predicate), `internal/engine` (filtered query scoping + unfiltered-identical + cross-transport parity).

**Target Platform**: Local single binary; query-path only.

**Project Type**: CLI + multi-transport server. This feature touches the retrieval core (`internal/index`), the engine (`internal/engine`), and all four transport adapters (CLI/REST/gRPC/MCP) + proto.

**Performance Goals**: Filtered query no slower than unfiltered (the filter check is O(candidates), negligible vs BM25/cosine). For very selective filters, the candidate pool may thin (poolSize=60 gives headroom; oversampling is a future optimization, documented).

**Constraints**: Pure Go; filter is opt-in (absent = no-op); `FTS.Search`/`Vector.Query` signatures MAY gain an optional predicate or the filter is applied at the `Retrieval` layer (design decision in research); cross-transport parity (FR-008); no storage change.

**Scale/Scope**: Per-query, O(candidates) filter checks; no corpus-wide work.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | In-process filter; no network, no new binary. |
| II | Content-Addressed Identity | ✅ Pass | No change to identity/hashes/records. The filter reads existing `Document` attributes; nothing persisted. |
| III | Pure Go — No CGo | ✅ Pass | stdlib only (path/filepath, strings); no new dependency. Proto regen is build-time. |
| IV | Async-After-ACK Writes | ✅ Pass | Query path only; write-ACK untouched. |
| V  | Extension by Interface, MCP-First | ✅ Pass | The filter is an optional predicate on the retrieval path; exposed on all four transports (CLI/REST/gRPC/MCP) for parity. The `Filter` type is a clean, self-contained struct; the retrieval layer applies it without restructuring `FTS`/`Vector` internals. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/014-retrieval-filter/
├── plan.md              # This file
├── research.md          # Phase 0 — filter application site, predicate vs struct, glob semantics, tag storage
├── data-model.md        # Phase 1 — Filter entity + Document-attribute match targets
├── contracts/
│   └── query-filter.md  # Phase 1 — the cross-transport filter contract
├── quickstart.md        # Phase 1 — filtered-query validation + eval gate
└── tasks.md             # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/index/
├── filter.go             # Filter type {Source, Type, Tags} + Matches(source, ftype, tags) bool + Empty()
├── retrieval.go          # Search/SearchWithRerank: apply a keep(chunkID) predicate pre-fusion
└── filter_test.go        # Filter.Matches unit tests (glob, type, tags, conjunction, empty)
internal/engine/
├── types.go              # QueryRequest gains Filter field
└── query.go              # build keep predicate from Filter + docOf/lookupDoc; pass to SearchWithRerank
internal/cli/query.go     # wire --source (existing, dead) + add --type, --tags; pass to QueryRequest.Filter
internal/rest/{types,engine_adapter}.go   # filter fields in queryRequest + map to QueryRequest
internal/grpc/engine_adapter.go           # map proto filter fields → QueryRequest.Filter
internal/mcp/server.go                    # filter in go_rag_query inputSchema + renderQuery
proto/gorag.proto + proto/gen/            # QueryRequest gains filter fields (source, type, tags); regen
```

**Structure Decision**: The filter is a self-contained `Filter` struct in `internal/index/filter.go` (the retrieval layer), with a `Matches` predicate. The engine builds a `keep(chunkID) bool` closure from `QueryRequest.Filter` + the existing `docOf`/`lookupDoc` resolvers, and passes it to `Retrieval.Search`/`SearchWithRerank`, which applies it to the FTS and Vector candidate lists **before RRF fusion** (pre-fusion pruning). `FTS.Search`/`Vector.Query` signatures are unchanged — the filter is applied at the `Retrieval` layer, not inside the index primitives (avoids restructuring FTS/Vector to know about document attributes). Transport exposure mirrors H08 (`rrf_k`): CLI flags + REST DTO + gRPC proto fields + MCP schema.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles.)*
