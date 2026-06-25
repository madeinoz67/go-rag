# Tasks: Swappable Vector Index

**Input**: Design documents from `/specs/027-vector-index-interface/` (audit finding **H27**, Phase 7 §1.3).

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/vector-index.md, quickstart.md — all present.

**Tests**: INCLUDED — this feature's definition-of-done *is* verification. The conformance test enforces the contract (FR-009 / SC-004) and the seam test proves Retrieval depends on the interface (SC-001); the zero-regression gate (SC-002/003/005) is the hard correctness check for a behaviour-preserving refactor. Tests are not optional add-ons here.

**Organization**: Tasks grouped by user story. US1 (the seam) is the MVP; US2 (the enforced contract) is what makes the seam safe; US3 (zero-change verification) is the gate.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- File paths are project-relative (Go module `github.com/madeinoz67/go-rag`); `internal/…` is repo-root-relative.

## Path Conventions (Go)

- Single binary, single entrypoint `cmd/go-rag` (untouched by this feature).
- All work is in `internal/index` (the interface + tests) and one type change at the `Retrieval` boundary. No `main`, no transport, no `proto/`, no config, no on-disk change.

## ⚠️ Build-order note (priority vs dependency)

This feature is a **structural refactor with a hard non-regression gate**, so
priority and dependency are unusually tightly coupled:

- The **interface (Phase 2)** must exist before any story work.
- **US1 (the seam)** and **US2 (the contract/conformance)** are mechanically
  independent after Phase 2 (different files) but conceptually one unit: US2
  defines *what* the seam must carry. They may be done in parallel, but both must
  land before US3.
- **US3 (zero behavioural change)** is pure verification and MUST run last — it
  is the gate that proves the refactor changed nothing observable. It cannot
  "fail" into a partial ship; a US3 failure means an earlier task was done wrong.

---

## Phase 1: Setup (Baseline)

**Purpose**: Record the pre-feature green baseline so US3's "identical before/after" claims are grounded.

- [x] T001 Run `make build vet test` (or `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`) on `main`; confirm green and record as the pre-feature baseline. No code changes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The contract that every user story depends on. No story work can begin until the `VectorIndex` interface exists.

**⚠️ CRITICAL**: Blocks US1 and US2.

- [x] T002 [P] Define the `VectorIndex` interface in `internal/index/index.go` — `Add(id string, vec []float32)`, `Delete(id string)`, `Query(vec []float32, k int) []Hit` over the existing `Hit`/`[]float32` types — with a doc comment stating it is the implementation-neutral vector-store contract and listing the three invariants as obligations (dimensionality-skip, determinism + stable chunkID tie-break, concurrency-safety). Mirror the existing `Reranker`/`EmbedFunc` interface style in the package. Research R1; contract: contracts/vector-index.md.
- [x] T003 [P] Confirm `Vector.Save`/`Vector.Load` (`internal/index/vector.go`) are vestigial — verify no production caller remains (only `LoadIndex` seeds the store, from Pebble `0x04`). Record the finding; this empirically grounds FR-007 (persistence stays off the contract). No code change unless a live caller is found (then surface it before proceeding). Research R3.

**Checkpoint**: Interface defined and the persistence decision empirically confirmed — story work can begin.

---

## Phase 3: User Story 1 — The vector backend is freely replaceable (Priority: P1) 🎯 MVP

**Goal**: Retrieval depends on the `VectorIndex` contract, not on the concrete `*Vector` — the escape hatch exists.

**Independent Test**: Wire `Retrieval` to a fake `VectorIndex` holding the same vectors as a real `*Vector`; the same query returns identical ranked `[]Hit` from both (SC-001).

### Implementation for User Story 1

- [x] T004 [US1] Change `Retrieval.vec` from `*index.Vector` to `VectorIndex` in `internal/index/retrieval.go`, and update the `NewRetrieval(fts, vec, embed)` parameter type to `VectorIndex`. This is the single consumer switch (R2); `*Vector` satisfies the interface structurally. Depends T002.
- [x] T005 [US1] Verify the engine holder needs no change: confirm `engine.indexes()` (`internal/engine/engine.go`) still returns concrete `*index.Vector` and that `query.go`'s `NewRetrieval(fts, vec, …)` call compiles unchanged (`*Vector` satisfies `VectorIndex`). Build the package; only edit if a call site does not satisfy structurally (none expected — R2). Depends T004.
- [x] T006 [US1] Add the seam test in `internal/index/retrieval_test.go`: build a real `*Vector` and a fake `VectorIndex` seeded with the same `(id→vec)` pairs; run the same query through `Retrieval` wired to each and assert byte-identical `[]Hit` (order + scores). Proves Retrieval depends on the contract, not the implementation (SC-001). Depends T004.

**Checkpoint**: The seam exists and is proven — Retrieval no longer names the concrete store.

---

## Phase 4: User Story 2 — The store's invariants are explicit guarantees, not accidents (Priority: P2)

**Goal**: The three correctness behaviours (H03 dimensionality-skip, determinism, concurrency) are pinned by an executable conformance test, so any future backend is held to them or rejected/wrapped.

**Independent Test**: The conformance test passes against the reference `*Vector` for all three invariants over fixture corpora (SC-004).

### Implementation for User Story 2

- [x] T007 [US2] Create the conformance test `internal/index/vector_contract_test.go` asserting the reference `*Vector` honours all three invariants: (1) a mixed-dimensionality corpus queried with one dimensionality returns only same-dimensionality hits — mismatched vectors skipped, never garbage-scored (FR-002 / Invariant 1); (2) repeated identical queries return byte-identical order, and a crafted equal-score pair resolves by ascending chunkID (FR-003 / Invariant 2); (3) concurrent `Add`+`Query` under `go test -race` reports no race (FR-004 / Invariant 3). This test is the bar any future backend must pass (FR-009). Research R4; contract: contracts/vector-index.md §Conformance. Depends T002.
- [x] T008 [US2] Update the `Vector` doc comment in `internal/index/vector.go` to state it is the **reference implementation** of `VectorIndex` and cite the three invariants (replacing the stale "mirrors a chromem-go backend… swapped in later" note). Retire the deferred-"task T027" TODO in `internal/index/index.go` (the interface now exists). Research R6. Depends T002.

**Checkpoint**: The contract is executable — the invariants survive any backend swap.

---

## Phase 5: User Story 3 — Zero behavioural change for every existing consumer (Priority: P3)

**Goal**: Prove the structural refactor changed nothing observable — query results, retrieval quality, cross-transport parity, and the shipped surface are identical.

**Independent Test**: The full existing suite + eval harness + parity tests are byte-identical before/after; no transport/proto/config/on-disk file was touched.

### Implementation for User Story 3

- [x] T009 [US3] Run `make test-eval` (spec 004 retrieval-eval harness) and assert recall is **identical** to the T001 pre-feature baseline — the structural change is quality-neutral (SC-002). No code.
- [x] T010 [US3] Run `go test -race ./internal/engine/` (incl. `parity_test.go`) and assert cross-transport parity (CLI/REST/gRPC/MCP) is unchanged (SC-003). No code.
- [x] T011 [US3] Confirm the blast radius: `git diff --stat main -- internal/rest internal/grpc internal/mcp internal/cli proto/ internal/config` is empty, and `git grep 'VectorIndex' -- '*.go'` shows only the interface definition + the `Retrieval.vec` field (SC-005). No code. Depends T004, T007, T008.

**Checkpoint**: The refactor is proven invisible. The feature ships only if all three pass.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final gate and audit-bookkeeping.

- [x] T012 [P] Run the full gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III); `go mod tidy` clean (no new dependency expected).
- [x] T013 Update audit tracking: mark finding **H27** done in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 7 §1.3) with a one-line completion note (spec 027 — interface-only, brute-force remains reference impl).
- [x] T014 Final gate: commit to `main` with Conventional Commits (e.g. `refactor(index): swappable vector index interface (H27)`) and push (single-author repo — straight to `main`, per `CLAUDE.md`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately; produces the baseline US3 measures against.
- **Foundational (Phase 2)**: After Setup. **BLOCKS** US1 and US2 (the interface must exist first).
- **US1 (Phase 3) & US2 (Phase 4)**: Both depend only on Phase 2. Mechanically independent (different files: `retrieval.go`/`retrieval_test.go` vs `vector_contract_test.go`/`vector.go`) → **can run in parallel**.
- **US3 (Phase 5)**: Depends on US1 + US2 both complete — it verifies their combined result. MUST run last.
- **Polish (Phase 6)**: Depends on US3 passing.

### User Story Dependencies

- **US1 (P1)**: Starts after Phase 2. No dependency on US2/US3.
- **US2 (P2)**: Starts after Phase 2. No dependency on US1/US3. (Conceptually paired with US1 — the contract US2 defines is what US1's seam carries — but they touch disjoint files.)
- **US3 (P3)**: Depends on US1 + US2. Pure verification; the gate.

### Within Each User Story

- Interface (Phase 2) before any consumer switch.
- Implementation before its test where the test asserts the implementation (T004 → T006; T002 → T007).
- US3 (verification) is the final gate — never skip.

### Parallel Opportunities

- **Phase 2**: T002 (interface def) and T003 (Save/Load vestigial check) — different files, both `[P]`.
- **Phase 3 vs Phase 4**: Once Phase 2 lands, US1 (T004–T006) and US2 (T007–T008) proceed in parallel — disjoint files (`retrieval.go`/`retrieval_test.go` vs `vector_contract_test.go`/`vector.go`).
- Within US1: T006 (seam test) follows T004 (not parallel — test depends on the type change).

---

## Parallel Example: After Phase 2

```bash
# US1 and US2 advance concurrently on disjoint files:
Task: "T004 [US1] switch Retrieval.vec → VectorIndex in internal/index/retrieval.go"
Task: "T007 [US2] conformance test in internal/index/vector_contract_test.go"
Task: "T008 [US2] update Vector doc comment in internal/index/vector.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (baseline) → Phase 2 (interface).
2. Complete Phase 3 (US1 — the seam).
3. **STOP and VALIDATE**: the seam test (T006) proves Retrieval depends on the contract.
4. At this point the escape hatch *exists*; US2/US3 make it *safe* and *proven*.

### Incremental Delivery

1. Setup + Foundational → interface defined.
2. US1 (seam) → test independently → the backend is substitutable.
3. US2 (contract) → test independently → the invariants are enforced.
4. US3 (verification) → the gate; recall + parity + blast-radius identical.
5. Polish → audit marked, committed to `main`.

### Solo-Author Note

This is a single-author repo committing to `main` directly (per `CLAUDE.md`). The
parallel structure above exists for clarity and for `/loop`/agent fan-out, not for
a team. In practice: Phase 1 → 2 → (3 ∥ 4) → 5 → 6, sequentially or with US1/US2
fanned out.

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] label maps a task to its user story for traceability.
- This feature changes **no transport, proto, CLI, config, or on-disk shape** —
  if any task implies such a change, stop (it contradicts FR-006 / US3 / SC-005).
- The feature is done only when US3 (the zero-change gate) passes — a structural
  refactor that regresses anything is a failed refactor.
- Commit after each task or logical group; Conventional Commits to `main`.
