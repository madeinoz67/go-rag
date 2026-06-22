# Implementation Plan: Query Transformation Seam + Normalization (H05)

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main`) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/012-query-transform/spec.md` (audit backlog item **H05**, P0).

## Summary

Add a query-transformation step that runs once at the top of `Engine.Query`, before
the query reaches either retrieval path. The step is a pluggable `QueryTransformer`
interface (defined in `internal/index`, mirroring the `Reranker` pattern — the
"behind `internal/index`, no Ollama coupling" requirement), with a pure-Go default
normalizer (Unicode-aware trim + whitespace-collapse + case-fold). The default is
always on; a custom transformer can be injected (tests today; future HyDE/multi-query
later). The interface returns one-or-more queries so multi-query can land behind the
same seam without re-architecting. The change is gated by the H02 eval harness
(no regression); for the harness's already-clean queries normalization is a no-op.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib only — `strings`, `unicode`, `context`, `fmt` (all already in use). **No new dependencies.** The interface reuses the existing `internal/index` + `internal/engine` packages.

**Storage**: Pebble KV — **N/A**. Pure query-path change; no stored data, no record shape change (Principle II intact).

**Testing**: `go test -race -cover ./...` (`make test`); the H02 eval harness (`make test-eval`, spec 004) is the no-regression gate (SC-002). New tests in `internal/index` (normalization correctness, idempotency, Unicode, empty-after-normalize) and `internal/engine` (custom-transformer-honored, parity unchanged).

**Target Platform**: Local single binary; query-path only.

**Project Type**: CLI + multi-transport server. This feature is internal to the engine/index core — **no transport adapter, CLI, config, or proto change**.

**Performance Goals**: Normalization is O(len(query)) — negligible vs the < 500ms query budget. No measurable latency impact.

**Constraints**: Pure Go; the default normalizer carries no external dependency (no Ollama); the seam lives in `internal/index` (Principle V); results identical across transports (the transform runs in the shared engine path); no retrieval-quality regression (SC-002).

**Scale/Scope**: Per-query, O(query-length); no corpus-wide work.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | Pure in-process normalization; no network, no new binary. |
| II | Content-Addressed Identity | ✅ Pass | Query-side only; no change to stored chunks/embeddings/identity. |
| III | Pure Go — No CGo | ✅ Pass | stdlib `strings`/`unicode` only; no new dependency. |
| IV | Async-After-ACK Writes | ✅ Pass | Query path only; the write-ACK budget is untouched. |
| V  | Extension by Interface, MCP-First | ✅ Pass (core alignment) | The `QueryTransformer` interface is defined in `internal/index`, the default normalizer is pure, and a future HyDE/multi-query transform implements the same interface in a separate adapter package — `internal/index` stays free of Ollama. Exactly the `Reranker` pattern (interface in `internal/index`, Ollama adapter in `internal/rerank`, engine wires it). No new transport surface. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/012-query-transform/
├── plan.md                  # This file
├── research.md              # Phase 0 — interface shape, injection point, normalization ops, asymmetry gating
├── data-model.md            # Phase 1 — QueryTransformer entity + normalized-query contract
├── quickstart.md            # Phase 1 — normalization parity, custom-transformer-honored, eval gate
├── contracts/
│   └── query-transform.md   # Phase 1 — the QueryTransformer interface contract (for future implementers)
└── tasks.md                 # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/index/
├── transform.go             # QueryTransformer interface + default NormalizingTransformer + normalizeQuery
└── transform_test.go        # case/whitespace/Unicode/idempotent/empty + custom-transformer unit tests
internal/engine/
├── engine.go                # qTransformer field; default normalizer in NewWithDB/NewWithEmbedder
└── query.go                 # apply e.qTransformer at the top of Query (replaces the bare empty-check)
```

**Structure Decision**: The interface + default live in `internal/index` (the audit's "behind `internal/index`"), mirroring `Reranker`. The engine holds a `qTransformer` (default normalizer) and applies it once at the top of `Engine.Query` — the single injection point that reaches the mismatch guard, the H07 query-prefix embed, and `SearchWithRerank` (both FTS and vector). **No transport/CLI/config/proto change**; the query contract (input → ranked hits) is unchanged.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles. No violations to justify.)*
