# Implementation Plan: Adaptive Retrieval Depth & Pool-Size Tuning (H22)

**Branch**: `main` (single-author Spec Kit work lands on `main` directly per `CLAUDE.md`; no feature branch) | **Date**: 2026-06-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/024-adaptive-retrieval/spec.md` — audit `RAG_BOOK_AUDIT_BACKLOG.md` H22 (§1.4): *No adaptive retrieval depth / pool-size tuning.*

## Summary

Make the two retrieval-cost knobs — reranker candidate-pool size and retrieval depth (`k`) — adaptive and observable, without changing default behavior. US1 promotes the currently **hardcoded** candidate pool (`poolSize = 60` in `internal/index/retrieval.go`) to a first-class config key (`pool_size`, default 60) overridable per query across all four transports (CLI/REST/gRPC/MCP), with aggregate pool-utilization surfaced in `status`. US2 adds a pluggable, in-process, rule-based `QueryClassifier` (the same extension-by-interface pattern as `QueryTransformer` and `Reranker`) that recommends a retrieval depth `k` — **never** mode — when the caller has not set one; the effective candidate pool then shrinks with the recommended `k` (`k` + small slack, floored and ceilinged) so reduced depth actually reduces search cost (FR-011). US3 makes every knob observable: effective depth/mode/pool per query in the response, and pool-size/classifier-enablement/utilization in `status`. The whole feature is default-OFF: with the classifier disabled and no per-query overrides, results are byte-identical to pre-H22 (FR-007/SC-005), and `make test-eval` recall@10 cannot regress (FR-010/SC-003).

**Technical approach**: extend the existing seams rather than fork them. The classifier mirrors `internal/index/transform.go` (interface + pure-Go default in the `index` package, future model-based impl in an adapter). Pool plumbing mirrors the RRF-k plumbing from spec 009 (`SetRRFK` → `SetPoolSize`; config `rrf_k`/`EffectiveRRFK()` → config `pool_size`/`EffectivePoolSize()`). Effective-k and effective-pool resolution happens once at the top of `Engine.Query`, feeds both the cache key (which must newly fold pool now that it varies) and the retrieval layer.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4), pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: existing only — cobra (CLI), Pebble (KV), chromem-go (vectors), grpc-go (gRPC), stdlib `net/http` (REST). **No new dependencies.** The rule-based classifier is pure stdlib (`strings`/unicode heuristics) — it introduces no third-party import (FR-008, Constitution III).

**Storage**: the single Pebble instance (PRD §6.7). **No storage change.** Classification is stateless and computed per query; pool-utilization is an in-memory aggregate (like the existing `CacheStats`), not persisted.

**Testing**: `go test -race -cover ./...` (unit) + `make test-eval` (retrieval-quality regression gate, offline deterministic embedder — `./bin/go-rag eval --embedder offline --baseline testdata/golden/baseline.json --tolerance 2.0`). The eval harness is the no-regression gate for FR-010/SC-003 and the tuning oracle for SC-001/SC-002.

**Target Platform**: cross-platform single binary (Linux/macOS/Windows), loopback-only transports.

**Project Type**: single-binary local service with four transports (CLI/MCP/REST/gRPC) over one `internal/engine.Engine` facade.

**Performance Goals**: no new SLA invented (clarification Q2 = Option A). SC-001 anchors to the constitution's existing query-latency budgets: a classifier-reduced factoid query meets (or approaches) the **<50ms keyword-only** budget, and **no query regresses past the <500ms hybrid budget** (Performance & Reliability Standards). Shrinking the candidate pool with `k` (FR-011) is what makes the keyword-side target reachable, because `poolSize` drives the actual FTS/vector fetch cost and the rerank candidate count.

**Constraints**:
- **Byte-identical default** (FR-007/SC-005): classifier disabled and no overrides ⇒ same passages, same order as pre-H22. The `pool_size` default MUST be 60 (today's hardcoded `poolSize`) and the classifier MUST be off by default.
- **Cross-transport parity** (FR-001/FR-009): the per-query `pool_size` override round-trips identically on CLI/REST/gRPC/MCP — same resolution, same results.
- **Explicit > recommended > default** (FR-006): an explicit `k` always wins; the classifier only acts when `k` is unset.
- **In-process only** (FR-008): the classifier never touches Ollama or any network service — the `index` package stays embedder-free.
- **Cache correctness**: once pool varies it MUST enter the result-cache key (it currently does not, because it was constant) — see research R5.
- **Pure-Go build gate**: `CGO_ENABLED=0 go build ./...` stays green (Constitution III).

**Scale/Scope**: S-effort (per audit H22 sizing). Touches 6 internal packages (`index`, `engine`, `config`, `cli`, `rest`, `grpc`, `mcp`) plus `proto/gorag.proto`, all沿 existing seams. No new subsystems, no storage migration, no new dependency.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluated against `.specify/memory/constitution.md` v1.0.0 (ratified 2026-06-19).

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | **Local-First, Single-Binary** | ✅ PASS | Classifier is in-process and rule-based (FR-008); no cloud, no network egress, no account. Pool/classifier state is in-process memory. Still one `CGO_ENABLED=0` binary. |
| II | **Content-Addressed Identity** | ✅ N/A | No new document/chunk identity; no storage schema change. Classification is derived per query, not persisted. |
| III | **Pure Go — No CGo** | ✅ PASS | Rule-based classifier is stdlib-only heuristics (`strings`, unicode). No new third-party import; existing deps unchanged. Build gate stays green. |
| IV | **Async-After-ACK Writes** | ✅ N/A | Query-path feature; the <10ms write-ACK budget is untouched. Query-latency budgets are *honored* (SC-001), not invented. |
| V | **Extension by Interface, MCP-First** | ✅ PASS (core exercise) | The classifier is a self-registering-style interface with a default impl (FR-004/005), exactly the `QueryTransformer`/`Reranker`/`FileReader` pattern. The new pool knob is exposed on MCP (and REST/gRPC/CLI) from day one (FR-001). |

**Performance & Reliability Standards**: the query-latency budgets (<500ms hybrid, <50ms keyword-only, <100ms vector top-60) are the SC-001 anchor, not a new SLA. H22 *reduces* cost on factoid queries (smaller pool) and MUST NOT push any query past the hybrid budget. Verified via the eval harness + a latency spot-check (quickstart).

**Development & Quality Workflow**: `go build ./...`, `go vet ./...`, `go test ./...` green on every change; `make test-eval` is the recall@10 gate. Conventional Commits to `main` (single-author). tokensave-indexed.

**GATE RESULT**: ✅ **PASS — no violations.** Every principle is either satisfied or unaffected. No Complexity Tracking entries required. The feature is a textbook application of Principle V on the query path.

## Project Structure

### Documentation (this feature)

```text
specs/024-adaptive-retrieval/
├── plan.md              # This file (/speckit-plan output)
├── research.md          # Phase 0 — R1–R7 decisions (pool default, dead config, classifier, math, cache key, transports, latency)
├── data-model.md        # Phase 1 — QueryClassification / PoolSize / PoolUtilization entities
├── quickstart.md        # Phase 1 — runnable validation (parity, adaptivity, observability, no-regression)
├── contracts/
│   ├── query-pool-knob.md   # the per-query pool_size surface across CLI/REST/gRPC/MCP
│   ├── classifier-interface.md  # the QueryClassifier extension point + rule-based default
│   ├── config-keys.md         # new pool_size + adaptive_depth_enabled config keys
│   └── status-and-cache.md    # status additions + the result-cache key change
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── index/
│   ├── retrieval.go        # MODIFY: add SetPoolSize (mirror SetRRFK); accept resolved effective pool
│   ├── classify.go         # NEW: QueryClassifier interface + RuleBasedClassifier default (mirror transform.go)
│   └── classify_test.go    # NEW: classifier unit tests (shape→k, empty-query, disable)
├── engine/
│   ├── types.go            # MODIFY: QueryRequest.PoolSize; QueryResult effective depth/pool/mode; StatusInfo pool+classifier+utilization
│   ├── query.go            # MODIFY: resolve effective k (explicit>recommended>default) + effective pool (FR-011); apply SetPoolSize; fold into cache key
│   ├── status.go           # MODIFY: surface pool size, classifier enablement, aggregate utilization
│   ├── cache.go            # MODIFY: add EffPool (+ EffK) to cacheKey + hash
│   └── engine.go           # MODIFY: hold classifier; construct default RuleBasedClassifier when adaptive_depth_enabled
├── config/config.go        # MODIFY: PoolSize + AdaptiveDepthEnabled fields, defaults, EffectivePoolSize(), Validate(), Get/Set
├── cli/
│   ├── query.go            # MODIFY: --pool-size flag
│   └── config_cli.go       # MODIFY: list pool_size + adaptive_depth_enabled
├── rest/
│   ├── server.go           # MODIFY: pool_size field on the query request struct
│   └── engine_adapter.go   # MODIFY: plumb req.PoolSize → QueryRequest
├── grpc/                   # MODIFY: map proto pool_size field → QueryRequest (generated from proto)
└── mcp/server.go           # MODIFY: go_rag_query tool gains pool_size param; go_rag_status surfaces new fields
proto/gorag.proto           # MODIFY: QueryRequest.pool_size (field 13); optional QueryResponse effective-depth/pool fields
```

**Structure Decision**: pure extension along existing seams — no new top-level package, no new `main` package, no storage migration. The only new file is `internal/index/classify.go` (+ its test), placed in `index` alongside `transform.go` because both are pre-retrieval seams owned by the index layer and both MUST stay embedder-free (Constitution V / FR-008). Every other change modifies an existing file that already owns that concern (per the directory→PRD map in `CLAUDE.md`).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No violations. Table intentionally empty — the feature satisfies all five principles without exception.
