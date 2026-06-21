# Implementation Plan: Reranker Error Surfacing (H09)

**Branch**: `006-rerank-error-surfacing` | **Date**: 2026-06-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/006-rerank-error-surfacing/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Stop swallowing reranker failures. Today `Retrieval.SearchWithRerank`
(`internal/index/retrieval.go:99`) discards **two** errors: the candidate-retrieval
error (`hits, _ := r.Search(...)` at line 110) and the reranker error / score-length
mismatch (lines 129–133), returning fallback-ordered hits as a silent success. This
plan makes both failures observable and distinct:

- A **retrieval-stage** failure on the rerank path **propagates as a query error**
  (FR-009) — failed retrieval means no valid candidates, so it cannot degrade
  gracefully.
- A **reranker** failure **degrades gracefully** to fallback-ordered results and sets
  a single boolean `RerankFailed` flag on `engine.QueryResult` (FR-001/002), surfaced
  identically across MCP, REST, and gRPC (FR-004).
- Each rerank failure is **logged** via the existing stdlib `log` (FR-003), mirroring
  the established "embedding drift" log line at `query.go:84` — error cause + metadata
  only, never query text.
- An **optional, off-by-default** retry-with-larger-pool path (FR-006) is included as
  the P3 slice.

**Technical approach:** change `SearchWithRerank` to `([]Hit, bool, error)`
(hits, `rerankFailed`, retrievalErr); add `RerankFailed bool` to `engine.QueryResult`;
thread it to the REST DTO, the proto `QueryResponse` (additive field, non-breaking),
the MCP text render, and a CLI stderr warning. `config.Config` gains
`RerankRetryOnFailure` (default false). **No new dependencies; read-path only.**

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4); `CGO_ENABLED=0`, pure Go.

**Primary Dependencies**: cobra (CLI), Pebble (KV), chromem-go (vectors), grpc-go
(gRPC), stdlib `net/http` (REST), fsnotify (watcher). **No new dependencies
introduced** — the change reuses stdlib `log` already imported by `engine/query.go`.

**Storage**: Pebble KV (prefix-partitioned). **No storage changes** — this is a
read-path/query behavior change; no new key prefixes.

**Testing**: `go test -race -cover ./...`. `internal/engine/parity_test.go` is the
cross-transport parity anchor (extended here); `internal/rerank/rerank_test.go`
covers the adapter; spec-004 eval harness (`make test-eval`) is the retrieval-quality
regression gate (SC-005).

**Target Platform**: Single static binary, all Go targets, loopback-only by default.

**Project Type**: single-binary local service + CLI; multi-transport
(MCP `:7878` / REST `:7879` / gRPC `:7880`) over one `engine.Engine`.

**Performance Goals**: **Zero latency added to the default (success) path.** The retry
(FR-006), when explicitly enabled, adds at most one extra retrieval+rerank round-trip,
and only on a rerank *failure*.

**Constraints**: Pure Go / no CGo (Constitution III); single Pebble writer (N/A — read
path); cross-transport parity (Constitution V / spec 003 FR-002/003); loopback default.

**Scale/Scope**: Local, single-user, <10K docs. Per-query behavior; no scale concerns.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|---|---|---|
| I. Local-First, Single-Binary | ✅ Pass | No new deps, no network egress, single binary unchanged. |
| II. Content-Addressed Identity | ✅ Pass (N/A) | Read path only; no identity / hash changes. |
| III. Pure Go — No CGo | ✅ Pass | Reuses stdlib `log` already in `engine/query.go:84`; no new imports beyond existing project packages. |
| IV. Async-After-ACK Writes | ✅ Pass (N/A) | Query / read path only; no writes, no ACK / drain semantics touched. |
| V. Extension by Interface, MCP-First | ✅ Pass | `Reranker` interface unchanged; the new flag is surfaced on **all** transports including MCP (the primary interface). |

**Gate result: PASS — no violations.** Complexity Tracking table is therefore not
required. Re-checked after Phase 1 design (see research.md D6): the design adds one
additive proto field and one boolean through the existing adapter pipeline — still
no violation.

## Project Structure

### Documentation (this feature)

```text
specs/006-rerank-error-surfacing/
├── plan.md              # this file
├── spec.md              # /speckit-specify output
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── query-response.md
└── tasks.md             # /speckit-tasks output (NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── index/
│   ├── retrieval.go        # SearchWithRerank → ([]Hit, bool, error): surface retrieval err + rerankFailed; optional retry
│   └── retrieval_test.go   # rerank-fail→fallback+flag; retrieval-err→propagated; retry success/2nd-fail
├── engine/
│   ├── types.go            # QueryResult += RerankFailed bool
│   ├── query.go            # thread rerankFailed into QueryResult; log failure (FR-003); gate retry on cfg
│   └── parity_test.go      # extend: assert RerankFailed parity across MCP/REST/gRPC + CLI
├── config/
│   └── config.go           # Config += RerankRetryOnFailure bool (default false)
├── rest/
│   ├── types.go            # queryResponse += RerankFailed bool `json:"rerank_failed"`
│   └── engine_adapter.go   # handleQuery maps res.RerankFailed → DTO
├── grpc/
│   └── engine_adapter.go   # Query maps res.RerankFailed → proto field
├── mcp/
│   └── server.go           # renderQuery prepends a warning line when RerankFailed
├── cli/
│   └── query.go            # emit stderr warning line when res.RerankFailed
└── rerank/
    └── rerank.go           # UNCHANGED — Reranker.Score already returns ([]float64, error)

proto/
├── gorag.proto             # QueryResponse += bool rerank_failed = 2;
└── gen/                    # regenerated via the project's protoc/buf step
```

**Structure Decision**: No new packages. The change threads one boolean through the
existing `engine → adapter` pipeline that spec 003 established, plus a signature change
on the single retrieval entry point. This honors the constitution's "extension by
interface" principle and the project's 1:1 directory-to-subsystem map (CLAUDE.md
architecture table). `internal/rerank` is intentionally untouched — the `Reranker`
interface already returns the error we need; the bug is purely in the caller that
discards it.

## Complexity Tracking

> **Not applicable.** Constitution Check passes with no violations to justify.
