# Implementation Plan: go-rag Management Console — Query View (Slice 2)

**Branch**: `main` (single-author repo; commits straight to `main`) | **Date**: 2026-07-11 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/048-ui-query-view/spec.md`

## Summary

Slice 2 of the go-rag management console: replace the **Query** placeholder (view 3 of
the spec 046 shell) with the retrieval surface — a read-only **query → inspect → tune** flow
that surfaces go-rag's hybrid retrieval in the browser. An operator enters a natural-language
query and sees ranked chunk hits with score, citation, and section context; opens a hit for
full text, sibling context, and provenance; and controls retrieval (mode, top-k, threshold,
rerank, filters, context expansion, dedup, cache bypass) while the view reports what the
engine actually used (effective mode/k/pool, rerank-failed banner).

The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
root, and spec 045 Bearer auth **unchanged**. It calls `Engine.Query` **in-process** (like the
Dashboard calls `engine.Status()` and the Documents view calls `engine.ListDocuments`),
introducing **no new transport, no new storage, no new auth, no new engine capability, and no
Node build chain**.

Unlike Slice 1 (spec 047, which had to add `Engine.ListChunks` across all five transports),
**Slice 048 requires zero engine/RPC work**: `Engine.Query` already exists and is already
exposed identically over CLI / MCP / REST / gRPC (cross-transport parity is already proven).
The UI is purely a new view file (`internal/ui/query.go`) plus its Alpine/template edits — the
simplest console slice so far.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); browser-side vendored Alpine.js 3.14
(already embedded from spec 046). No Node/Vite/Tailwind.

**Primary Dependencies**:
- stdlib `net/http` (the UI transport, unchanged from spec 046) — Go 1.22 pattern mux
- `internal/engine` — `Engine.Query` (existing; the one retrieval path shared by every
  transport), `QueryRequest` / `QueryHit` / `QueryResult` types
- `internal/model` — `PoisonVerdict` / `NearDupInfo` (read projections on hits)
- `internal/auth` — spec 045 `auth.Validate` guard (unchanged, via `Server.guard`)
- Vendored Alpine.js (already embedded)

**Storage**: None new. Retrieval reads existing Pebble prefixes via the engine. No new prefix,
no migration, no `ExpectedVersion` bump.

**Testing**: `go test -race`; `curl -i` smoke for the new `/api/query` route (loopback bypass +
Bearer regimes); cross-transport parity test confirming `/api/query` returns the same hits /
order / scores as `go-rag query` and REST `POST /v1/query` for identical input (pattern of the
Documents view's parity test); Interceptor browser verify of query/inspect/tune render. No JS
test runner (vendored SPA).

**Target Platform**: Loopback HTTP (`127.0.0.1:7881`, the spec 046 UI transport), modern
browser SPA. Single-operator.

**Project Type**: Additive view on an existing web-service transport. No new engine surface.

**Performance Goals**:
- Query round-trip ≤ existing `Engine.Query` latency (the UI adds only JSON marshal + loopback
  HTTP; constitution budget: < 500ms hybrid top-5).
- Hit detail open is client-side (no extra round-trip — the hit payload already carries full
  text + context + provenance).

**Constraints** (hard):
- Read-only — no add/remove/reingest/re-embed (spec FR-009).
- No Node/build chain (CLAUDE.md; single-binary).
- No new storage / no migration (constitution Storage Discipline).
- Quarantine-by-default — `IncludeQuarantined=false` unless the operator opts in per query
  (spec FR-007; engine default preserved).
- UI calls the engine in-process; it is a 4th adapter, not a REST proxy.
- No per-stage score breakdown — the engine returns one fused `Score`; this slice surfaces the
  fused score + effective mode/k/pool + rerank status only (spec Open Question OQ1, resolved).

**Scale/Scope**: ~4–5 edited/new files: `internal/ui/query.go` (NEW +test), `internal/ui/ui.go`
(EDIT — register route), `internal/ui/web/static/js/app.js` (EDIT), and
`internal/ui/web/templates/index.html` (EDIT). No engine / REST / gRPC / MCP / CLI / proto
changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The go-rag constitution (`.specify/memory/constitution.md`, v1.1.0) defines five Core
Principles. Slice 2 is evaluated against each:

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback view in the existing binary; in-process engine call; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | Strictly read-only retrieval. No document/chunk identity, content hash, or ingest path is touched. Filters and params are read-only inputs to an existing query path. |
| III | Pure Go — No CGo, No External Runtime | **PASS** | stdlib + engine + model. No new Go dependency; vendored Alpine already embedded. |
| IV | Async-After-ACK Writes | **PASS** | Strictly read-only slice — no write path, so the <10ms write-ACK budget is unaffected. |
| V | Extension by Interface, MCP-First | **PASS** | `Engine.Query` is already exposed across **all** transports (engine + REST + gRPC + MCP + CLI) with proven parity. The UI is an additive 4th adapter over the existing method — no new accessor, no parity debt. |

**Storage discipline**: no on-disk layout change — no prefix added/retired, no value encoding
change, no key construction change. **No migration, no `ExpectedVersion` bump.** Affirmed: this
slice has zero schema-version impact.

**Gate verdict: PASS.** No Complexity Tracking entry required (no principle violated; no
storage change; no new engine surface).

## Project Structure

### Documentation (this feature)

```text
specs/048-ui-query-view/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1–R12 decisions)
├── data-model.md        # Phase 1 output (DTOs + route table)
├── quickstart.md        # Phase 1 output (validation guide)
├── contracts/           # Phase 1 output (HTTP transport contract)
│   └── ui-query.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT this command)
```

### Source Code (repository root)

```text
internal/ui/
  query.go               # NEW (R1,R3,R4) — Query view handlers: POST /api/query → engine.Query, in-process
  query_test.go          # NEW — handler tests (auth, empty-query 400, happy path) + cross-transport parity
  ui.go                  # EDIT (R3) — register POST /api/query route, guarded by Server.guard
  web/static/js/app.js   # EDIT — Query view (Alpine): input, controls, results list, hit detail,
                         #         effective-state indicators, rerank-failed banner, quarantine opt-in
  web/templates/index.html # EDIT — Query view template; sidebar active state; replaces the placeholder
```

**Structure decision**: No new package, no new engine file, no RPC/proto work. The view is a
new file (`internal/ui/query.go`) inside the existing UI transport, plus its Alpine/template
edits — exactly how the Dashboard lives in `internal/ui/dashboard.go` and the Documents view in
`internal/ui/documents.go`. The DTOs are local to `query.go`, field-parallel to the REST
`queryHit` / `queryRequest` contract (`internal/rest/engine_adapter.go`) so the two adapters
stay consistent without one importing the other.

## Complexity Tracking

> None — Constitution Check is PASS with no violations. (No storage change, no migration, no
> new engine surface, no principle bent.) The view is additive read-only presentation over an
> existing engine method.
