# Implementation Plan: Retrieval-Quality Evaluation Harness

**Branch**: `004-retrieval-eval-harness` | **Date**: 2026-06-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-retrieval-eval-harness/spec.md`
(audit item H02 — go-rag's #1 risk per `RAG_BOOK_AUDIT.md` §6).

## Summary

go-rag tests retrieval *mechanics* (ordering, collapse, parity) but has no way to
measure retrieval *quality*. This plan adds a retrieval-quality evaluation harness
that computes **recall@5, recall@10, precision@5, MRR, NDCG@10** over a committed,
hand-labeled **golden dataset** of query→relevant-chunk pairs, drives the real
shared `engine.Query` path, exposes itself as both a `go-rag eval` CLI command and
a `go_rag_eval` MCP tool (Principle V), runs **offline and reproducibly** by
injecting a deterministic pure-Go embedder, and ships a CI regression gate
(`make test-eval`) that fails when recall@10 regresses beyond a tolerance. The
one architectural enabler is an **additive, optional embedder injection** on
`engine.Engine` so the eval harness can drive the canonical query path with a
deterministic embedder while every existing caller is unaffected.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4), `CGO_ENABLED=0` pure Go.

**Primary Dependencies**: existing only — `spf13/cobra` (CLI), the `internal/engine`
facade, `internal/index` (retrieval), `internal/embed` (`Embedder` interface),
`internal/storage` (Pebble). **No new third-party dependencies.** IR metrics
(recall/precision/MRR/NDCG) are hand-rolled pure Go (see research.md — pulling a
metrics library risks a CGo/transitive-C dependency, violating Principle III).

**Storage**: read-only access to a go-rag Pebble vault via the existing engine; a
throwaway Pebble vault (the established vault-dir abstraction, not a second DB
type) is built when the harness ingests the golden corpus for a self-contained
run. The golden dataset itself is a committed JSONL file under `testdata/golden/`
(git, not Pebble — test fixture, not core state).

**Testing**: `go test -race -cover ./...`; new `internal/eval/*_test.go` with
table-driven metric-correctness tests (hand-computed expected values) and an
end-to-end harness test over a tiny committed golden set using the deterministic
embedder. CI gate via a new `make test-eval` target.

**Target Platform**: same as go-rag — single static binary, darwin/linux/amd64/arm64.

**Project Type**: CLI tool + library (new `internal/eval` package surfaced through
the existing CLI/MCP adapters).

**Performance Goals**: offline deterministic eval of the MVP golden set (~30–50
queries) completes in **well under a minute** (each query skips the Ollama
round-trip → ≈ keyword-only latency); real-Ollama baseline run is a one-shot
measurement, not a hot path (≈ golden-size × <500ms hybrid).

**Constraints**:
- **Read-only to the user's live vault** (FR-006) — eval MUST NOT add/modify/delete
  the user's documents or indexes.
- **Offline-reproducible by default** (FR-004/SC-004) — no network calls in the
  default/CI path; identical metric values run-to-run on a clean machine.
- **Hand-rolled metrics** — no metrics library that could drag in CGo/C (Principle III).
- **Single shared retrieval path** (FR-007) — eval drives `engine.Query`, not a
  parallel retrieval implementation.

**Scale/Scope**: one new `internal/eval` package (~metrics + dataset + runner +
deterministic embedder, ≈ a few hundred LOC), one additive engine hook (embedder
injection), one CLI command, one MCP tool, one committed golden JSONL + small
corpus, one Make target, and a CI gate. Audit estimates <500 LOC for the MVP.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Pure-Go, no cloud/egress; deterministic offline embedder means eval runs with no Ollama; still one `CGO_ENABLED=0` binary; golden dataset committed locally. |
| **II. Content-Addressed Identity** | ✅ PASS | Golden labels ARE `chunk_id` (SHA-256 content-addressed). Ingesting the same golden corpus into a throwaway vault yields identical chunk_ids (idempotent by content hash) — labels are portable. No new identity scheme introduced. |
| **III. Pure Go — No CGo** | ✅ PASS | `internal/eval` is pure Go; IR metrics hand-rolled (no metrics library); deterministic embedder is pure Go. `CGO_ENABLED=0 go build ./...` stays green. |
| **IV. Async-After-ACK Writes** | ✅ PASS (read-only) | Eval is **read-only** w.r.t. the user's vault (FR-006). Throwaway-vault ingestion uses the normal async-after-ACK pipeline unchanged. No core write path or <10ms budget is affected. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Exposed as BOTH `go-rag eval` (CLI) and `go_rag_eval` (MCP tool) — FR-003. Embedder injection uses the existing `embed.Embedder` interface; no new abstraction. |

**Performance & Reliability Standards**: query budget <500ms hybrid respected
(eval just runs N such queries); cold-start <1s (one vault open); memory/binary
budgets unaffected (no new deps). Single-writer/concurrency: eval opens its vault
read-only or a throwaway vault; it does not contend for the user's live `LOCK`.

**Development & Quality**: build/vet/test stay green; new code under `internal/`
mapping to the PRD's package discipline; Conventional Commits. **Known follow-on
test edit:** `internal/mcp/server_test.go::TestMCP_ToolsListHas12` asserts exactly
12 tools — adding `go_rag_eval` makes 13, so that test is updated (count + name)
in the tasks phase.

**No violations.** Complexity Tracking table left empty.

## Project Structure

### Documentation (this feature)

```text
specs/004-retrieval-eval-harness/
├── plan.md              # This file
├── research.md          # Phase 0 — embedder-mode decision, metric formulas, golden schema
├── data-model.md        # Phase 1 — Golden Query / Relevance Judgment / Evaluation Run
├── quickstart.md        # Phase 1 — end-to-end validation guide
├── contracts/           # Phase 1 — eval CLI + eval MCP tool + golden-file format
│   └── eval.md
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/eval/                   # NEW package — pure-Go, no third-party deps
├── metrics.go                   # recall@k, precision@k, MRR, NDCG@k (hand-rolled)
├── dataset.go                   # load/validate the committed golden JSONL
├── embedder.go                  # DeterministicEmbedder: pure-Go hashing vectorizer
├── run.go                       # EvalRunner: drives engine.Query, joins on chunk_id, aggregates
└── *_test.go                    # metric-correctness + end-to-end harness tests

internal/engine/
├── engine.go                    # ADDITIVE: optional injectable embedder + NewWithEmbedder
└── query.go                     # use injected embedder if present, else NewOllama (unchanged)

internal/cli/
└── eval.go                      # NEW cobra command: go-rag eval [--golden …] [--mode …] [--baseline …]

internal/mcp/
└── server.go                    # ADD: go_rag_eval tool def + renderEval + guide entry

testdata/golden/
├── v1.jsonl                     # NEW committed golden dataset (query → relevant chunk_ids)
└── corpus/                      # NEW small source corpus the labels refer to (+ reuse testdata/*.md)

Makefile                         # ADD: test-eval target (offline deterministic gate)
.github/workflows/ci.yml         # ADD: run make test-eval on chunk/index/rerank/hybrid changes
```

**Structure Decision**: New work is a single new `internal/eval` package (PRD
package-discipline: one dir per subsystem) wired into the existing adapters —
no new top-level layout. The only cross-cutting change is the additive embedder
injection on `internal/engine`, required so eval can drive the canonical query
path offline. Golden data lives under `testdata/` (test fixture convention), not
in `internal/` or in Pebble.

## Complexity Tracking

> None — Constitution Check passes with no justified violations.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| _(none)_ | | |
