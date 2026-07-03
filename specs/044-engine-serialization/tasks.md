---
description: "Task list for spec 044 — Engine Write-Operation Serialization"
---

# Tasks: Engine Write-Operation Serialization

**Input**: Design documents from `/specs/044-engine-serialization/` — `spec.md` (2 user stories, FR-001..008, SC-001..004), `plan.md`, `research.md` (R1..R4), `data-model.md`, `quickstart.md`. Technical design: [`docs/design/engine-serialization.md`](../../docs/design/engine-serialization.md) (per-doc lock + non-blocking push, 12-agent RedTeam-validated).

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present. The Constitution Check gate passed (plan.md).

**Tests**: INCLUDED — the constitution mandates `go test -race`, and this is concurrency-fixing code where the race detector + the concurrent-same-docID test are load-bearing.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = the per-document lock — the race fix; US2 P2 = the non-blocking push — the stall fix). **The two stories are coupled**: US2 MUST precede US1 (the RedTeam proved the non-blocking push is required for the doc-lock to work on ReingestPath without re-introducing the stall). The phases below reflect this dependency.

**Load-bearing invariants** (every implementation task must preserve these):
- **Lock ordering `docLock → p.mu`** — acyclic. No path acquires `p.mu` then a `docLock`.
- **Close does NOT take doc-locks** — only `pipeMu`. The per-doc lock must never be acquired by a path that `Close` waits on.
- **No job lost** — the detached sender delivers the job (it blocks on the send until a worker drains). The embedder reads `0x14` independently.
- **Distinct-docID concurrency preserved** — the existing `TestConcurrent_AddQuery_NoCorruption` must still pass.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new deps (stdlib `sync` only).

- [x] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the `docLocks` map + the `docLock` helper that both stories depend on. **No story work begins until this phase is green.**

- [x] T002 Add the `docLocks sync.Map` field to the Pipeline struct + a `docLock(docID string) func()` helper that `LoadOrStore`s a `*sync.Mutex`, locks it, and returns an unlock closure (`internal/pipeline/pipeline.go`). [Constitution: pure stdlib, no CGo]

**Checkpoint**: the docLock primitive exists + compiles. Story work can begin.

---

## Phase 3: User Story 2 — No forward-progress stall (Priority: P2)

**Goal**: `processFile`'s queue push (`p.queue <- job`) never blocks the calling goroutine when the queue is full — a detached goroutine parks on the send instead. This MUST precede US1 because the doc-lock (US1) is held across `Ingest → processFile → p.queue <-` in `ReingestPath`; without the non-blocking push, the lock-holder stalls (the per-docID variant of the opMu deadlock).

**Independent Test**: fill the job queue to capacity, call `processFile`; assert it returns immediately (the job is delivered via a detached goroutine, not blocked).

### Implementation for User Story 2

- [x] T003 [US2] Replace the bare `p.queue <- job{...}` at the end of `processFile` with `select { case p.queue <- job{...}: default: go func(j job) { p.queue <- j }(job{...}) }` (`internal/pipeline/pipeline.go`). The detached goroutine parks on the blocking send; the caller returns immediately. [research R3; FR-002, FR-007]

### Tests for User Story 2

- [x] T004 [US2] Test: fill the cap-64 queue (ingest 65+ docs without workers draining, or pre-fill the channel), then call `processFile`; assert it returns within the normal ACK latency (<10ms, Principle IV). The job lands in the queue via the detached goroutine. Run under `-race`. (`internal/pipeline/serialization_test.go`) [quickstart Scenario 2; SC-002]

**Checkpoint**: `processFile` never blocks on a full queue. US1 can proceed safely.

---

## Phase 4: User Story 1 — No double events on concurrent re-ingest (Priority: P1) 🎯 MVP

**Goal**: two concurrent operations touching the same docID produce exactly one lifecycle event (no DELETED + RE_INGESTED double event). The per-document lock serializes same-docID operations so the `reingestDocs` suppression flag can't be consumed by the wrong `DeleteDoc`.

**Independent Test**: run two goroutines on the same docID (one `Reprocess`, one bare `DeleteDoc`); assert exactly one event fires.

### Implementation for User Story 1

- [x] T005 [P] [US1] Acquire the docLock in `DeleteDoc` for the entire body — so any concurrent reingest's `DeleteDoc` waits (`internal/pipeline/delete.go`). Lock ordering: `docLock → p.mu` (DeleteDoc takes docLock first, then its existing `p.mu.Lock()`). [FR-001, FR-004, FR-008]
- [x] T006 [US1] Acquire the docLock in `ReingestPath` across the full critical section (`captureReingest + DeleteDoc + Ingest`) (`internal/pipeline/reingest.go`). The non-blocking push (T003) ensures `Ingest → processFile` never stalls under the lock. [FR-003]
- [x] T007 [US1] Acquire the docLock in `Reprocess`/`ReprocessAll` around the `captureReingest + DeleteDoc` pair inside the scan loop (`internal/pipeline/reprocess.go`). The lock covers capture+delete per-docID; the Ingest is batched after the loop. [FR-001]

### Tests for User Story 1

- [x] T008 [US1] Test: two concurrent goroutines on the same docID (one `Reprocess`, one `DeleteDoc`) — assert exactly one event fires (no DELETED + RE_INGESTED double). Also: two concurrent `Reprocess` calls on the same doc — one emits RE_INGESTED, the other no-ops or INGESTED. Run under `-race`. (`internal/pipeline/serialization_test.go`) [quickstart Scenario 1; SC-001]

**Checkpoint**: the concurrent-DeleteDoc race is closed; distinct-docID concurrency preserved.

---

## Phase 5: Polish & Cross-Cutting

- [x] T009 Run `make lint` (golangci-lint) + `go test -race ./...` full repo green; confirm the existing `TestConcurrent_AddQuery_NoCorruption` still passes (distinct-doc concurrency preserved, SC-004); affirm constitution compliance in the commit (pure stdlib, no migration, no proto, lock ordering acyclic, Close never contends).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002)**: T002 is standalone. **Blocks all stories.**
- **US2 (T003–T004)**: depends on T002 (docLock helper exists, though US2 itself doesn't use it). T003 must precede US1 (the doc-lock requires the non-blocking push). T004 depends on T003.
- **US1 (T005–T008)**: depends on T002 + T003. T005/T006/T007 are parallel (different files); T008 depends on T005+T006+T007.
- **Polish (T009)**: after all stories.

### Story completion order (single-author, sequential)
Foundational → US2 (the non-blocking push — the RedTeam proved it's required, not separate) → US1 (the per-document lock) → Polish.

### Within Each User Story
- The non-blocking push (T003) before its test (T004).
- The lock acquisitions (T005/T006/T007) can be parallel (different files); the test (T008) after all three.

---

## Implementation Strategy

### MVP (both stories — they're inseparable per the RedTeam)
1. T001 baseline green.
2. Foundational T002 (docLocks field + helper).
3. US2 T003–T004 (the non-blocking queue push + its test).
4. US1 T005–T008 (the per-doc lock acquisitions + the concurrent test).
5. Polish T009 (lint + full race suite).

### Commit cadence
Conventional Commits to `main` after the US2 checkpoint and after the US1 checkpoint. `make build && vet && test && lint` green before every push. **No adversarial review required** — this is a size-S internal fix (no proto, no transport, no migration); the RedTeam already validated the design.

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 no double events | T005, T006, T007, T008 |
| FR-002 processFile never blocks | T003, T004 |
| FR-003 ReingestPath lock covers full critical section | T006 |
| FR-004 DeleteDoc acquires docLock | T005 |
| FR-005 distinct-docID concurrency | T009 (existing concurrency test) |
| FR-006 Close does not contend | T005 (DeleteDoc's docLock is never on Close's path) |
| FR-007 no job lost | T003, T004 |
| FR-008 lock ordering acyclic | T005 (docLock → p.mu) |
| SC-001 one event per concurrent same-docID | T008 |
| SC-002 processFile <10ms with full queue | T004 |
| SC-003 -race clean on reingestDocs/docLock | T008 |
| SC-004 existing concurrency test passes | T009 |
