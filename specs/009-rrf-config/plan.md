# Implementation Plan: Configurable Reciprocal Rank Fusion (RRF) Constant

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main` per project convention) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-rrf-config/spec.md` (audit backlog item **H08**).

## Summary

Collapse go-rag's asymmetric per-list RRF (`kVec=40` / `kFTS=60`, formula
`1/(k+rank+1)` per list) into a single symmetric constant `k` (default 60) using
the retrieval book's standard `score(d) = Σ 1/(k + rank)` (rank 1-based). Make
that constant configurable (`.go-rag/config.json` → `rrf_k`), overridable per
query (`--rrf-k` CLI flag; `rrf_k` on the REST / gRPC / MCP request), and
document the formula once. The override flows through `Engine.Query` → the
per-query `Retrieval` (constructed fresh each call, same pattern as
`EnableRerankRetry`) → `reciprocalRankFusion`. No storage change; cross-transport
parity is preserved (spec 003). The default-ranking shift is measured on the H02
eval harness before merge.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, built `CGO_ENABLED=0`.

**Primary Dependencies** (relevant subset): `spf13/cobra` (CLI flags);
`internal/config`, `internal/index`, `internal/engine` (core path); `grpc-go` +
`google.golang.org/protobuf` (gRPC transport); stdlib `net/http` + JSON-RPC (MCP
transport). Pebble (KV) and chromem-go (vector) are **untouched** by this feature.

**Storage**: Pebble KV — **N/A**. `rrf_k` is configuration + per-request state,
not persisted per-document data. No new key-space prefix, no migration
(Constitution Principle II intact).

**Testing**: `go test -race -cover ./...` (`make test`); plus the H02 retrieval
gate `make test-eval` → `go-rag eval --embedder offline --baseline
testdata/golden/baseline.json --tolerance 2.0`. Optional real-corpus measurement
via `go-rag eval --benchmark {scifact,msmarco}` (`internal/eval/beir`).

**Target Platform**: Local single binary; Linux / macOS / Windows (static
cross-compile). No network egress for the fusion path.

**Project Type**: CLI + multi-transport server (MCP `:7878` / REST `:7879` / gRPC
`:7880`) over one `internal/engine.Engine`.

**Performance Goals**: Hybrid query < 500ms top-5 (constitution). The fusion is
O(pool) over ≤ `2 × poolSize` (=120) hits — negligible; the change must not
regress query latency.

**Constraints**: Pure Go (no CGo); single Pebble instance; cross-transport parity
(spec 003 FR-002/003); zero breakage for existing configs that omit `rrf_k`
(absent key → default 60); write-ACK < 10ms untouched (query path only).

**Scale/Scope**: Local vaults < 100K chunks; `poolSize = 60`, so fusion runs over
≤ 120 candidates regardless of corpus size.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | `rrf_k` is in-process config/request state. No new binary, no cloud, no egress. |
| II | Content-Addressed Identity | ✅ Pass | No per-document persistence; `rrf_k` is not part of document identity. Identity/change hashes untouched. |
| III | Pure Go — No CGo | ✅ Pass | No new runtime dependency. Proto regen (if needed for gRPC field 6) is a **build-time** codegen step; the shipped binary stays pure-Go static. |
| IV | Async-After-ACK Writes | ✅ Pass | Query path only; write/ingest ACK budget untouched. |
| V  | Extension by Interface, MCP-First | ✅ Pass (core alignment) | The override is exposed on **every** transport incl. MCP (FR-003). `Retrieval.SetRRFK` mirrors the existing `EnableRerankRetry` setter — same interface-consistent extension seam. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/009-rrf-config/
├── plan.md              # This file
├── research.md          # Phase 0 — formula, 0-as-unset, proto-regen, eval re-baseline
├── data-model.md        # Phase 1 — QueryRequest/Config/Retrieval entities
├── quickstart.md        # Phase 1 — end-to-end validation scenarios
├── contracts/
│   └── query-rrf-k.md   # Phase 1 — cross-transport query contract for rrf_k
└── tasks.md             # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── index/
│   └── retrieval.go         # reciprocalRankFusion(vec,fts,k); Retrieval.rrfK + SetRRFK
├── engine/
│   ├── types.go             # QueryRequest.RRFK int  (0 = unset → config/default)
│   └── query.go             # resolve effective k; r.SetRRFK(effective) per query
├── config/
│   └── config.go            # Config.RRFK + EffectiveRRFK(); Validate; Get("rrf_k")
├── cli/
│   └── query.go             # --rrf-k flag (reject explicit ≤ 0)
├── rest/
│   ├── types.go             # queryRequest.RRFK json:"rrf_k,omitempty"
│   └── engine_adapter.go    # map req.RRFK → engine.QueryRequest.RRFK
├── grpc/
│   └── engine_adapter.go    # map req.GetRrfK() → engine.QueryRequest.RRFK
└── mcp/
    └── server.go            # inputSchema "rrf_k"; renderQuery reads args["rrf_k"]
proto/
├── gorag.proto              # QueryRequest: int32 rrf_k = 6;
└── gen/gorag.pb.go          # regenerated (see research.md §4 — regen command TBD)
testdata/golden/
└── baseline.json            # re-captured post default-ranking change (H02 gate)
```

**Structure Decision**: This feature touches **no new packages** — it threads one
scalar (`rrf_k`) through the existing query path and the four transport adapters
that already project `engine.QueryRequest`. Every change is at an existing seam;
the layout above is the exhaustive file list. The only generated artifact is
`proto/gen/gorag.pb.go` (regenerated, not hand-edited).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles. No
violations to justify.)*
