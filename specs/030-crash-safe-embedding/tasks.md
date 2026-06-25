# Tasks: Crash-Safe Background Embedder

**Input**: Design documents from `/specs/030-crash-safe-embedding/` (the MuninnDB crash-safe background embedder).

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/embedder.md, quickstart.md — all present.

**Tests**: INCLUDED — the crash-safety guarantee (FR-001), idempotency (FR-006), and output-identity (FR-008) are all assertable and load-bearing. A crash that silently loses retrievability is the exact failure this feature exists to prevent — tests prove it doesn't.

**Organization**: US1 (crash-safe embedding — the durable queue + embedder + startup recovery) is the MVP; US2 (decoupled + cross-doc batched + circuit-breaker) is the throughput/resilience layer; US3 (compatible + observable) is the gate. The queue + processor skeleton (Phase 2) underpins all three.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- File paths are project-relative (Go module `github.com/madeinoz67/go-rag`).

## Path Conventions (Go)

- Single binary, single entrypoint `cmd/go-rag` (untouched).
- New `internal/embedproc` package (the background embedder); new `0x14` prefix (the durable pending-embed queue); the processFile/processJob split; daemon + CLI wiring; status fields across 4 transports. No new main.

## ⚠️ Build-order note (priority vs dependency)

This is the **biggest core-path refactor** in the audit — it moves the embed step off
the per-ingest `processJob` to a decoupled background processor. The risk is real:

- The **queue + processor skeleton (Phase 2)** must exist before US1.
- **US1 Phase A** (crash-safety) is the critical path: atomic write → embedder loop →
  processJob loses embed role → wiring → tests. **Do not run US2 until US1 is green.**
- **US2** (batching + circuit breaker) layers ON the embedder loop from US1.
- **US3** (no-regression) is the final gate — the eval recall must be byte-identical
  (FR-008). If it regresses, the embedder's output (0x04 shape, vec.Add, epoch bump)
  diverged from the old processJob path — a bug.
- The **processJob embed removal (T007)** is the high-risk task — it must hand off ALL
  embed responsibilities (prefixing, 0x04 shape, epoch bump, H03 guard) to the embedder
  verbatim, or queries break silently.

---

## Phase 1: Setup (Baseline)

**Purpose**: Record the pre-feature green baseline (the "outputs unchanged" claim SC-005 is measured against it).

- [x] T001 Run `make build vet test` and `make test-eval` on `main`; confirm green and record recall@10 as the pre-feature baseline. No code changes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The durable pending-embed queue (0x14) + the background embedder skeleton + the circuit breaker primitive. Blocks every story.

**⚠️ CRITICAL**: Blocks US1, US2, US3.

- [x] T002 [P] Add `PrefixEmbedQueue byte = 0x14` to `internal/storage/storage.go` (next free prefix after `PrefixNearDup 0x13`) and `Put/Get/ScanEmbedQueue` helpers mirroring the quarantine helpers in `internal/storage/poison.go`. The queue record value is `{Model, Status, Attempts}` (JSON). research R1, data-model §1.
- [x] T003 [P] Create `internal/embedproc/processor.go`: the `Processor` struct (holds the shared `*index.FTS`, `*index.Vector`, embedder, prefixer, OnChange epoch hook) + `Start(ctx)` (initial scan + loop on Notify chan + 3s poll) + `Stop()` (drain) + `Notify()` (buffered(1), non-blocking). research R3, data-model §2.
- [x] T004 [P] Extract the circuit breaker from `internal/enrich/circuit.go` to a shared `internal/circuit` package (or copy into `internal/embedproc`), so both the enricher (spec 029) and the embedder reuse the same primitive (5 fails / 30s defaults). research R4.

**Checkpoint**: Queue prefix + processor skeleton + circuit breaker exist; ready for the US1 hand-off.

---

## Phase 3: User Story 1 — Embeddings are crash-safe (Priority: P1) 🎯 MVP

**Goal**: A crash mid-embed no longer orphans documents — the embedder recovers pending 0x14 records on restart.

**Independent Test**: Ingest + kill before embed + restart → doc retrievable by semantic search, no manual re-ingest (SC-001).

### Implementation for User Story 1

- [x] T005 [US1] Modify `processFile` in `internal/pipeline/pipeline.go`: write the chunk (0x03) + the pending-embed record (0x14) in **one atomic Pebble batch**, then ACK. Notify the embedder on ACK (if bound). The <10ms ACK budget is preserved (the 0x14 write is a small durable KV write alongside the chunk). research R1, data-model §3. Depends T002.
- [x] T006 [US1] Implement the embedder's embed loop in `internal/embedproc/processor.go`: drain 0x14 → for each pending chunkID, read chunk text from 0x03, apply the document-role prefix (H07 prefixer), call `Embedder.Embed`, write 0x04 `{model, convention, vector}`, `vec.Add`, bump the index epoch (`OnChange`), remove the 0x14 record. Absorb ALL embed responsibilities verbatim from the current `processJob` (prefixing, 0x04 shape, epoch bump, H03 guard). research R3/R5, data-model §2. Depends T002, T003.
- [x] T007 [US1] Remove the embed role from `processJob` in `internal/pipeline/workers.go`: processJob no longer calls Embed or writes 0x04 — it keeps FTS indexing, near-dup clustering, enrichment (spec 029), and status. The embedder (T006) is now the sole writer of 0x04. **High-risk: ensure every embed responsibility moved to T006.** research R2. Depends T006.
- [x] T008 [US1] Wire the embedder: the daemon (`internal/daemon`) constructs + `Start(ctx)`s it over the shared DB + index handles, for the daemon's lifetime, draining on shutdown; the CLI one-shot commands (`add`/`reprocess`/`scan`) get a short-lived embedder that runs + drains before the process exits (so `go-rag add` still embeds before returning). research R3. Depends T006.
- [ ] T009 [US1] Add crash-recovery + hand-off tests (`internal/embedproc/processor_test.go`): (a) write a chunk + 0x14 (no embedding), run `Start` (initial scan), assert 0x04 written + vec.Add + 0x14 removed (SC-001 recovery); (b) the kill-restart scenario simulated without the daemon (seed 0x14, start embedder, verify recovery); (c) idempotency — re-running on already-embedded is a no-op (FR-006). SC-001. Depends T006, T008.

**Checkpoint**: Embedding is crash-safe — a crash between ACK and embed recovers on restart.

---

## Phase 4: User Story 2 — Decoupled + batched + circuit-breaker (Priority: P2)

**Goal**: The embedder micro-batches across documents (bulk-ingest throughput) and fast-fails a failing backend (no stall, no errored docs).

**Independent Test**: N pending chunks → ~N/32 Embed calls; backend down → circuit opens, no stall (SC-003/004).

### Implementation for User Story 2

- [x] T010 [US2] Add cross-document micro-batching to the embedder loop: accumulate pending chunk texts up to MaxBatchSize (H12's 32), issue ONE `Embedder.Embed` call per micro-batch, scatter the vectors back to their chunkIDs (write 0x04 + vec.Add + remove 0x14 per chunk). research R4. Depends T006.
- [x] T011 [US2] Integrate the circuit breaker into the embedder: `Allow()` before the Embed call; `ok()` on success, `fail()` on error; an open breaker fast-fails (the chunk's 0x14 stays pending for retry); a permanent failure (bad model/config) marks the 0x14 record `status=failed` (terminal — not retried indefinitely). research R4, data-model §2. Depends T004, T010.
- [ ] T012 [US2] Add batch + circuit tests (`internal/embedproc/processor_test.go`): (a) N=100 pending chunks → assert ⌈100/32⌉ = 4 Embed calls (cross-doc batching, SC-004); (b) embedder that always errors → circuit opens after 5 fails; pending chunks stay pending (not failed); no infinite retry (SC-003). Depends T010, T011.

**Checkpoint**: The embedder is throughput-optimized and resilient.

---

## Phase 5: User Story 3 — Compatible + observable (Priority: P3)

**Goal**: The refactor is output-identical (eval recall unchanged); the embedder backlog is visible in status.

**Independent Test**: `make test-eval` recall identical to baseline; status shows embed_pending/embed_failed (SC-002/005).

### Implementation for User Story 3

- [x] T013 [US3] Run `make test-eval` (spec 004 harness) and assert recall@10 is **identical** to the T001 baseline — the refactor is structural (the 0x04 shape + vec.Add + epoch are unchanged). If recall regresses, the embedder's output diverged from processJob (a bug in T006/T007 — fix before proceeding). SC-005. Depends T007.
- [ ] T014 [P] [US3] Add `EmbedPending int` + `EmbedFailed int` to `engine.StatusInfo` (`internal/engine/types.go`), compute in `internal/engine/status.go` (count 0x14 records with status=pending / status=failed), and project on the 4 transports (REST/gRPC/MCP/CLI status). research R6, contracts/embedder.md. Depends T002.
- [ ] T015 [US3] Add status-surfacing tests (`internal/engine/status_test.go` or parity_test): with pending 0x14 records, `status` reports `embed_pending > 0`; with a failed record, `embed_failed > 0`; both surface identically across transports. SC-002. Depends T014.

**Checkpoint**: Output-identical + the backlog is visible.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final gate + ship.

- [ ] T016 [P] Run the full gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III); `go mod tidy` clean (no new dependency — the circuit breaker is extracted internally).
- [ ] T017 Final gate: commit to `main` with Conventional Commits (e.g. `refactor(embed): crash-safe background embedder — MuninnDB approach (spec 030)`) and push.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — produces the baseline.
- **Foundational (Phase 2)**: After Setup. **BLOCKS** every story.
- **US1 (Phase 3)**: Depends on Phase 2. MVP — the critical crash-safety path. **Must be green before US2.**
- **US2 (Phase 4)**: Depends on US1 (the embedder loop from T006).
- **US3 (Phase 5)**: Depends on US1's processJob split (T007) for the no-regression gate; T014 (status) depends on Phase 2 (0x14).
- **Polish (Phase 6)**: Depends on all stories complete + green.

### User Story Dependencies

- **US1 (P1)**: After Phase 2. MVP. No dependency on US2/US3.
- **US2 (P2)**: After US1 (T006 embedder loop). Layers batching + circuit breaker on the loop.
- **US3 (P3)**: After US1 (T007 — the no-regression test needs the embedder fully in place). T014 (status) is independent (Phase 2 only).

### Within Each User Story

- Queue (T002) before the embedder (T006) and processFile (T005).
- Embedder loop (T006) before the processJob removal (T007) — the embedder must be functional before processJob stops embedding.
- Embedder loop (T006) before batching (T010) and circuit breaker (T011).

### Parallel Opportunities

- **Phase 2**: T002 (queue prefix), T003 (processor skeleton), T004 (circuit breaker) — different files/packages, all `[P]`.
- **Phase 5**: T014 (status fields) depends only on Phase 2 (0x14) — can run parallel to US1/US2.
- After Phase 2, US3's T014 (status) can advance alongside US1.

---

## Parallel Example: After Phase 2

```bash
# US1 core + US3 status can advance concurrently on disjoint files:
Task: "T005 [US1] processFile atomic 0x03+0x14 write in internal/pipeline/pipeline.go"
Task: "T006 [US1] embedder loop in internal/embedproc/processor.go"
Task: "T014 [US3] status fields in internal/engine/ + 4 transports"
```

---

## Implementation Strategy

### MVP First (User Story 1 only — crash-safety)

1. Phase 1 (baseline) → Phase 2 (queue + processor + circuit breaker).
2. Phase 3 (US1 — atomic write + embedder loop + processJob hand-off + wiring + tests).
3. **STOP and VALIDATE**: ingest + kill + restart → doc retrievable (SC-001); eval recall identical (SC-005 spot-check). **The crash gap is closed.**
4. At this point, embedding is crash-safe. US2/US3 add throughput + observability.

### Incremental Delivery

1. Setup + Foundational → queue + processor exist.
2. US1 (crash-safety) → test → kill-restart recovers.
3. US2 (batching + circuit breaker) → test → bulk-ingest throughput + backend-down resilience.
4. US3 (no-regression + status) → eval identical + backlog visible.
5. Polish → gate green, committed to `main`.

### Solo-Author Note

Single-author repo, commits to `main` directly (per `CLAUDE.md`). The parallel structure
is for clarity and agent fan-out, not a team. In practice: Phase 1 → 2 → 3 → 4 → 5 → 6,
sequentially. **US1 is the critical path — do not skip to US2 before US1 is green.**

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] label maps a task to its user story for traceability.
- **T007 (processJob embed removal) is the highest-risk task** — it hands off embed
  responsibilities to T006. If any responsibility is missed (prefixing, 0x04 shape, epoch
  bump, H03 guard), queries break silently. The eval no-regression test (T013/SC-005)
  catches this — run it immediately after T007.
- **The 0x14 queue is the source of truth for "pending"** — if a chunk is in 0x03 but not
  0x04, it MUST have a 0x14 record (or it's a pre-feature orphan that the startup scan
  detects via a one-time 0x03-without-0x04 check, or is simply re-ingested).
- **Migrate (spec 017) and this feature are orthogonal** — Migrate re-embeds stale-model;
  this recovers missing. If Migrate writes 0x14 records (instead of embedding directly),
  the embedder processes them — but that's a plan-level choice for the implementer.
- Commit after each task or logical group; Conventional Commits to `main`.
