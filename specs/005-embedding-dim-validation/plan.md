# Implementation Plan: Embedding Model/Dimension Mismatch Validation

**Branch**: `005-embedding-dim-validation` | **Date**: 2026-06-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-embedding-dim-validation/spec.md`
(audit item H03 — a P0 silent-killer per `RAG_BOOK_AUDIT.md` §1.2; the next backlog item).

## Summary

Today go-rag computes retrieval similarity over a query vector and stored vectors
with **no dimensionality or model check**: `cosine()` silently truncates to
`min(len(a), len(b))`, so a query embedded under a different model/dimensionality
than the corpus produces **plausible-but-wrong rankings with no error** (or, for a
gross mismatch, an index panic). This plan adds a validation + refusal guard: the
engine derives the corpus's embedding profile (majority model + dimensionality
from stored records), compares it to the active query embedder's model/dim before
scoring, **refuses with a clear error on mismatch** (US1), makes `Vector.Query`
**skip+count** length-mismatched stored vectors instead of garbage-scoring them
(US3 graceful mid-migration), and surfaces model/dim **consistency + drift** in
status (US2). The guard is local, pure-Go, O(1) on the happy path, and consistent
across every query transport.

## Technical Context

**Language/Version**: Go 1.22+, `CGO_ENABLED=0` pure Go.

**Primary Dependencies**: existing only — `internal/engine`, `internal/index`
(`Vector`, `Retrieval`, `cosine`), `internal/embed` (`Embedder`:
`Embed`/`Dimensions`/`Model`), `internal/storage` (Pebble prefix 0x04 Embedding
records), `internal/model` (`Embedding.Model`/`.Dimensions`). **No new deps.**

**Storage**: read-only use of the existing Pebble Embedding records (prefix 0x04)
to derive the corpus profile; no schema change, no new prefix. The profile is
computed during the existing per-query index load (which already scans Embedding
records), so it adds **no new O(N) scan** — and is cached for free once H01 lands.

**Testing**: `go test -race -cover ./...`; new tests in `internal/index` (cosine
length-guard, Vector.Query skip+count) and `internal/engine` (refuse on model
mismatch, refuse on dim mismatch, graceful skip of minority, status drift flag).
The spec-004 evaluation harness can additionally exercise a mismatch scenario
end-to-end (SC-005) — extended as a verification, not duplicated.

**Target Platform**: same single static binary, all Go targets.

**Project Type**: library + CLI/MCP/REST/gRPC (guard lives in the shared engine +
index, surfaced uniformly by every transport).

**Performance Goals**: the happy-path check is O(1) — a length comparison
(`len(queryVec)` vs `Vector.dims`) plus a model-string compare. The corpus
*model* is derived from records during the load the query already performs, so no
new per-query cost. Existing latency budgets (<500ms hybrid, <50ms keyword)
preserved (FR-006/SC-004).

**Constraints**:
- **Never garbage-score** — a length mismatch must never reach `cosine()`
  truncation; mismatched vectors are skipped, mismatched queries are refused.
- **Never panic** — a corrupted/odd-length stored entry is skipped+logged, not a
  crash (edge case).
- **Pure + local** — no network, no external service (FR-006).
- **Cross-transport parity** — the refuse error is identical whether the query
  originates from CLI/REST/gRPC/MCP (FR-007, Principle V).
- **Narrower than H11** — no drift baselines, version-pinning, or auto-reindex;
  this is the refusal/safety guard only.

**Scale/Scope**: one new `CorpusProfile` derivation helper (shared by engine.Query
and Status), a length guard in `Vector.Query`/`cosine`, a refuse check in the
engine query path, status drift fields, and tests. Small, surgical change to
existing files; no new package required.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Check is pure Go, no network/egress; uses only local Pebble records + in-memory vectors. Still one `CGO_ENABLED=0` binary. |
| **II. Content-Addressed Identity** | ✅ PASS | Unchanged — embeddings keep their chunk-addressed identity; the guard reads stored `Embedding.Model`/`.Dimensions` provenance, introduces no new identity scheme. |
| **III. Pure Go — No CGo** | ✅ PASS | Length comparison + string compare + a Pebble scan that already happens; no new deps. `CGO_ENABLED=0 go build ./...` stays green. |
| **IV. Async-After-ACK Writes** | ✅ PASS (read-side) | The guard is on the **query/read** path; no write path or <10ms ACK budget is touched. Ingest (which sets provenance) is unchanged. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Uses the existing `embed.Embedder` `Model()`/`Dimensions()` methods; the refuse error surfaces through every transport (CLI/MCP/REST/gRPC) identically. |

**Performance & Reliability Standards**: happy-path check is O(1) (length +
model compare); the corpus model derivation rides the existing per-query load
scan (no new O(N)); latency budgets preserved. Single-writer/concurrency: the
guard is read-only; `Vector.Query` already holds the vector mutex.

**Development & Quality**: build/vet/test stay green; change is surgical to
existing `internal/engine` + `internal/index`; Conventional Commits. No new
package; respects the 1:1 PRD-subsystem directory mapping.

**No violations.** Complexity Tracking table left empty.

## Project Structure

### Documentation (this feature)

```text
specs/005-embedding-dim-validation/
├── plan.md              # This file
├── research.md          # Phase 0 — check location, profile source, cosine fix, refuse/skip semantics
├── data-model.md        # Phase 1 — CorpusProfile, MismatchVerdict, QueryEmbedding provenance
├── quickstart.md        # Phase 1 — mismatch refuse / status drift / graceful-skip validation
├── contracts/           # Phase 1 — refuse-error + status-drift contract across transports
│   └── mismatch-guard.md
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/index/
└── vector.go             # length guard in Query (skip+count mismatched stored vecs); cosine no longer silently truncates to garbage

internal/engine/
├── query.go              # derive corpus profile; refuse (clear error) when query model/dim != corpus majority
├── status.go             # report STORED majority model+dim + per-model/dim counts + drift flag (US2)
└── embedding_profile.go  # NEW helper: CorpusProfile from Embedding records (majority model/dim + counts) — shared by query + status

# No new package; no storage schema change (reads existing Embedding.Model/.Dimensions).
```

**Structure Decision**: Surgical change to two existing packages (`internal/index`
for the per-vector length guard; `internal/engine` for the corpus-profile refuse
check and status drift). One new helper file (`embedding_profile.go`) keeps the
profile logic in one place, shared by the query path and status. No new package,
no new prefix, no schema migration — the guard reads provenance that is already
stored.

## Complexity Tracking

> None — Constitution Check passes with no justified violations.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| _(none)_ | | |
