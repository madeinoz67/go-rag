# Implementation Plan: Swappable Vector Index

**Branch**: `027-vector-index-interface` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/027-vector-index-interface/spec.md` (audit finding **H27**, Phase 7 §1.3). Promotes the brute-force vector store's implicit contract into an explicit, implementation-neutral interface so the nearest-neighbour backend becomes substitutable — landed while one implementation exists and can validate the contract.

## Summary

Today the vector store is a single concrete type — `*index.Vector`, an in-memory
brute-force linear-scan cosine search (`internal/index/vector.go`) — wired directly
into retrieval: `Retrieval.vec *Vector` and `NewRetrieval(fts, vec, ...)`
(`internal/index/retrieval.go`). There is no seam at which a backend can be
substituted. Worse, three correctness behaviours are incidental to the brute-force
implementation rather than guarantees: (a) the H03 anti-silent-corruption guard
— `Query` **skips** vectors whose dimensionality ≠ the query's instead of
garbage-scoring them; (b) goroutine-safety under concurrent ingest writes + query
reads (`sync.Mutex`, mutated by pipeline workers); (c) deterministic ranking with
stable `ChunkID` tie-breaking. A naive substitute (a typical ANN library) violates
all three — silently re-introducing wrong scores on a mixed corpus, racing, and
returning non-deterministic order.

The fix is a **structural, behaviour-preserving extraction** — interface-only, no
second backend shipped:

1. **Define `VectorIndex`** (`Add/Delete/Query → []Hit`) in `internal/index` over
   the existing `Hit`/`[]float32` types, with the three invariants stated as
   contract (R1). Mirrors the existing `Reranker`/`EmbedFunc` interface precedent
   in the same package.
2. **Retrieval depends on the contract, not the concrete type**: `Retrieval.vec`
   becomes `VectorIndex`; `NewRetrieval` takes the interface. `*Vector` satisfies
   it structurally and remains the reference implementation (R2).
3. **Pin the contract with a conformance test** (the "second implementation" of
   SC-001): a test-double `VectorIndex` plus assertions that the reference
   `*Vector` honours the three invariants — so a future backend is provably
   held to them, or wrapped/rejected (FR-009).
4. **Zero behavioural change**: `engine.idxVec` keeps holding concrete `*Vector`;
   persistence (`Save`/`Load`, vestigial post-H01/H16 — R3) stays off the
   contract; no transport, config, or on-disk change (FR-006/SC-002/003/005).

Full design rationale: [research.md](./research.md) (R1–R6). Contract detail:
[data-model.md](./data-model.md). Interface contract + conformance rules:
[contracts/vector-index.md](./contracts/vector-index.md). Validation runbook:
[quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: none added. The interface and conformance test are pure
Go stdlib. The eventual ANN backend (not shipped here) is constrained to a
pure-Go library (Constitution III; chromem-go is the anticipated shape — R5).

**Storage**: unchanged — single Pebble instance. The vector store is seeded from
Pebble prefix `0x04` (embeddings) by `pipeline.LoadIndex` (`internal/pipeline/load.go`),
**not** from `Vector.Load`; the JSON `Save`/`Load` methods are vestigial and stay
out of the contract (R3).

**Testing**: `go test -race -cover ./...` (`make test`). This feature adds a
conformance test (`internal/index/vector_contract_test.go`) and a seam test
(`internal/index/retrieval_test.go` — Retrieval wired to a fake `VectorIndex`),
and must leave `internal/engine/parity_test.go` + the spec-004 retrieval-eval
harness byte-identical (SC-002/003).

**Target Platform**: single statically-linked binary, local-first (Constitution I/III). No platform change.

**Project Type**: single-binary local RAG database + multi-transport daemon (CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution, unchanged): query <500 ms hybrid / <100 ms
vector. The extraction is a pointer/interface swap — zero added work on the query
path; the brute-force `Query` body is untouched, so latency is identical (FR-006).

**Constraints**: behaviour-preserving (FR-006 — the hard gate), Pure-Go/No-CGo,
the three invariants non-negotiable under any backend (FR-002/003/004), string
chunk-IDs preserved (FR-008), no shipped second backend (FR-005).

**Scale/Scope**: one new interface + doc-stated invariants in `internal/index`,
one type change on `Retrieval.vec` + `NewRetrieval`, one conformance test, one
seam test. Touches `internal/index` (primary) and `internal/engine` (the
`indexes()`/`idxVec` holder — passes concrete `*Vector` where the interface is
expected; no field-type change required). No transport/proto/config/on-disk
change. Targets local <10K-doc corpora (brute-force remains adequate — audit §1.3).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All
five PASS — no violations, so the Complexity Tracking table is empty. This
finding **directly serves** Principle V (Extension by Interface).

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | Pure-Go interface + stdlib conformance test; no LLM, no network, no cloud. Single `CGO_ENABLED=0` binary, no new runtime dep. The future backend stays local (R5). |
| II | Content-Addressed Identity | ✅ PASS | The interface operates over existing string chunk-IDs and `Hit`; it touches neither chunk `cid` nor document `GenerateID`. Identity/content-hash dedup is untouched → re-add stays a no-op. |
| III | Pure Go — No CGo | ✅ PASS | Interface + conformance test are stdlib. No second backend is shipped, so no new dependency at all; the *future* backend is constrained to pure-Go (FR/Assumptions), preserving the CGo ban. |
| IV | Async-After-ACK Writes | ✅ PASS | Pure structural refactor — no ACK-path reordering, no new I/O. The pipeline's `Add`/`Delete` call sites keep their existing (async-after-ACK) timing; only the *static type* of the receiver at the Retrieval boundary changes. <10 ms ACK preserved. |
| V | Extension by Interface, MCP-First | ✅ PASS | **This is the principle the feature enacts.** It extends the existing `FileReader`/`Embedder`/`Reranker` interface-extension pattern to the vector store — closing the core while the backend set stays open. No MCP surface change (the seam is internal), consistent with MCP-first (existing tools unchanged). |

**Post-design re-check** (after `data-model.md` / `contracts/vector-index.md`):
no new persisted entity, no new Pebble prefix, no on-disk shape change, no
transport/proto field, no identity change, no ACK-path reordering. The interface
adds a seam; `*Vector` remains the sole shipped implementation. The five verdicts
are unchanged. **Gate: PASS.**

## Project Structure

### Documentation (this feature)

```text
specs/027-vector-index-interface/
├── spec.md                   # Feature spec (/speckit-specify)
├── plan.md                   # This file (/speckit-plan)
├── research.md               # Phase 0 — design decisions R1–R6
├── data-model.md             # Phase 1 — VectorIndex contract + Vector entity + lifecycle
├── quickstart.md             # Phase 1 — validation runbook (SC-001..005)
├── contracts/
│   └── vector-index.md       # Phase 1 — internal interface contract + invariants + conformance
└── tasks.md                  # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

A surgical change to `internal/index` (the interface + tests) and one type
adjust at the Retrieval boundary; the engine holder passes the concrete type
through. No new package, no new `main`:

```text
internal/
├── index/
│   ├── vector.go             # body UNCHANGED; doc comment updated to cite the new contract (R1)
│   ├── index.go              # VectorIndex interface { Add; Delete; Query→[]Hit } + invariant docs (R1)
│   ├── retrieval.go          # Retrieval.vec: *Vector → VectorIndex; NewRetrieval param → VectorIndex (R2)
│   ├── vector_contract_test.go   # NEW: conformance — *Vector honours the 3 invariants (R4, SC-001/004)
│   └── retrieval_test.go     # EXTEND: Retrieval wired to a fake VectorIndex returns identical results (SC-001)
└── engine/
    └── engine.go             # indexes() still returns *index.Vector (concrete); *Vector satisfies VectorIndex where passed to NewRetrieval — NO field-type change required (R2)
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem (per
`CLAUDE.md`). The feature is the smallest possible seam: the interface lives in
`internal/index` alongside its existing interface siblings (`Reranker`,
`EmbedFunc`), `Retrieval` is the single consumer that switches from concrete to
contract, and the engine keeps holding the concrete reference-implementation
`*Vector` (passed structurally where the interface is expected — no storage
field type change, no lifecycle change). No new package, no new `main`, single
binary entrypoint (`cmd/go-rag`) untouched. This minimal blast radius is itself a
requirement (FR-006 — the change must be invisible).

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles. This
table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
