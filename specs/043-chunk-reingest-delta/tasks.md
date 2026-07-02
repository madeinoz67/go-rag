---
description: "Task list for spec 043 — Chunk Change Deltas on Re-Ingest (BL-010)"
---

# Tasks: Chunk Change Deltas on Re-Ingest (BL-010)

**Input**: Design documents from `/specs/043-chunk-reingest-delta/` — `spec.md` (3 user stories, FR-001..010, SC-001..005), `plan.md`, `research.md` (R1–R6), `data-model.md`, `contracts/api.md`, `quickstart.md`. Technical design: [`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md) (B-simple, red-team-validated).

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present + consistent. The Constitution Check gate passed (plan.md).

**Tests**: INCLUDED — the go-rag constitution mandates `go build`/`go vet`/`go test` green on every change, AND this is concurrency-heavy code (the re-ingest reorder + the embed-skip preservation + the `PrefixEmbedding` copy are the surfaces the red teams flagged) — the `-race` tests are load-bearing.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push. **The concurrency-heavy embed-skip + reorder MUST get an adversarial Forge/Cato review (races/torn-write/<10ms-budget) before merge — T023.**

**Organization**: grouped by user story (US1 P1 = the delta event MVP; US2 P2 = the embed-skip; US3 P3 = delta correctness under repeats/moves). Foundational phase holds the `ContentHash` sidecar + migration + the proto wire.

**Load-bearing invariants** (every implementation task must preserve):
- **`cid` stays globally unique + unchanged** — `ContentHash` is a non-identity sidecar; it never enters `GenerateID` (Constitution II). No stored ID changes as a side-effect.
- **The <10ms ACK budget** — the diff computation + the `PrefixEmbedding` copy MUST NOT breach the write-ACK budget (Constitution IV). Verify (T019); move post-ACK if profiling shows pressure.
- **`RE_INGESTED` replaces (not accompanies) `INGESTED`+`DELETED`** for a re-ingest — exactly one event, no double-count.
- **The embed-skip gate** — an `UNCHANGED` chunk's vector is reused ONLY when the embedding baseline is unchanged; a drift → re-embed (stale vectors never reused).
- **The `PrefixEmbedding` copy is synchronous** (in `processFile`, before the async worker) — no torn-rewire race. FTS + NearDup are recomputed normally (no inverted-index rewiring).

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new deps (the diff is stdlib `crypto/sha256` + maps; the event extends the existing grpc-go stream).

- [x] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the `ContentHash` sidecar + its backfill + the proto wire — the substrate every user story needs. **No story work begins until this phase is green.**

- [x] T002 [P] Add the `ContentHash` non-identity sidecar to `model.Chunk` (`internal/model/model.go`) — `ContentHash string \`json:"content_hash,omitempty"\``, doc comment matching the sidecar idiom (Poisoning/SectionContext); add a JSON round-trip + omitempty test. [Constitution II; data-model.md]
- [x] T003 Add the v2 migration `internal/storage/migrate/v2_content_hash.go` — scan `PrefixChunk`, unmarshal each chunk, set `ContentHash = model.ContentHash([]byte(c.Content))`, re-marshal, write back; register `{Version: 2, ...}`; bump `migrate.ExpectedVersion` 1→2; idempotent (unconditional re-write). Test idempotency + the nil-sidecar round-trip (a chunk whose sidecar was nil MUST round-trip to nil, not a zero-value drift — RedTeam caveat). (depends T002) [Constitution: schema evolution; research R5]
- [x] T004 Compute `ContentHash` in `processFile` next to `cid` (`internal/pipeline/pipeline.go`) — `ContentHash: model.ContentHash([]byte(s.Text))` on each chunk (the redacted text). (depends T002) [research R1; data-model.md]
- [x] T005 [P] Add the proto wire (`proto/gorag.proto`): the `ChunkDelta` message (`change_type` enum ADDED/REMOVED/UNCHANGED; `chunk_id`; `prev_chunk_id`) + `repeated ChunkDelta chunk_deltas = 7` on `DocumentEvent`; regenerate `proto/gen`. (`DocumentEventType_RE_INGESTED = 2` is already reserved.) [contracts/api.md]

**Checkpoint**: the sidecar + migration + computation + wire exist + compile green. Story work can begin.

---

## Phase 3: User Story 1 — The delta event (Priority: P1) 🎯 MVP

**Goal**: a re-ingest emits exactly one `RE_INGESTED` event carrying the per-chunk delta (`ADDED`/`REMOVED`/`UNCHANGED`) + the old→new chunk-ID map, replacing the `INGESTED`+`DELETED` pair.

**Independent Test**: ingest a multi-paragraph doc; open a `WatchDocuments` stream; edit one paragraph; re-ingest → assert ONE `RE_INGESTED` with the edited chunk `ADDED`, the rest `UNCHANGED`, + a populated old→new cid map; assert no accompanying `INGESTED`/`DELETED`. (`quickstart.md` Scenarios 1+2; FR-001..005.)

### Implementation for User Story 1

- [x] T006 [P] [US1] Implement the multiset diff in a new `internal/pipeline/delta.go` — pure function `diffChunks(old, new []model.Chunk) (deltas []ChunkDelta, remap map[oldCid]newCid)`: key on `ContentHash`; `min(N_old, N_new)` `UNCHANGED`, surplus-old `REMOVED`, surplus-new `ADDED`; pair `UNCHANGED` old→new by stable position-within-bucket. [research R2; data-model.md]
- [x] T007 [P] [US1] Factor read-only `chunksOfDoc(docID) []model.Chunk` + `embedsOfDoc(docID)` helpers out of `DeleteDoc`'s existing scan (`internal/pipeline/delete.go`) — for the capture-before-delete. `DeleteDoc`'s behavior is unchanged.
- [x] T008 [US1] Re-ingest reorder (`internal/pipeline/reprocess.go` `Reprocess`/`ReprocessAll`): capture `old []model.Chunk` (+ each old chunk's `PrefixEmbedding` record) via T007's helpers BEFORE `DeleteDoc`; thread `oldChunks` (+ embeddings) into `processFile` as a new optional parameter. (depends T007)
- [x] T009 [US1] In `processFile` (`internal/pipeline/pipeline.go`): when `oldChunks` is non-nil (a re-ingest), compute the diff (T006); emit a `DocumentEvent{Type: EventReingested, ...}` carrying the delta + the cid map (via the existing `OnEvent`/bus) INSTEAD of the `INGESTED`+`DELETED` a re-ingest would otherwise surface. When `oldChunks` is nil → first ingest → today's `INGESTED` path unchanged. (depends T006, T008) [FR-001..005]
- [x] T010 [US1] Carry `ChunkDeltas` on the internal `events.DocumentEvent` + project them in `toEventProto` (`internal/grpc/watch_documents.go`). [contracts/api.md]
- [x] T011 [US1] Wire the watcher MODIFIED path (`internal/watcher/watcher.go`) through the `oldChunks`-threaded re-ingest so file-change-triggered re-ingests also emit `RE_INGESTED`. (depends T008/T009) [plan §4]
- [x] T012 [US1] Streaming test (`internal/grpc/watch_documents_test.go` or a new `reingest_test.go`): ingest → edit → re-ingest → assert exactly one `RE_INGESTED` with the correct `ADDED`/`UNCHANGED`/`REMOVED` delta + a populated old→new cid map; assert NO accompanying `INGESTED`/`DELETED`. Under `-race`. (depends T009, T010) [quickstart Scenarios 1+2]

**Checkpoint**: US1 MVP — a re-ingest delivers the delta event. Validate under `-race`.

---

## Phase 4: User Story 2 — Embed-skip (Priority: P2)

**Goal**: `UNCHANGED` chunks skip embedding generation when the embedding baseline is unchanged; ALL chunks re-embed when the baseline drifts (stale vectors never reused).

**Independent Test**: re-ingest with an unchanged baseline → `UNCHANGED` chunks not re-embedded (their `PrefixEmbedding` was copied); change the model → ALL chunks re-embedded. (`quickstart.md` Scenarios 3+4; FR-006/007.)

### Implementation for User Story 2

- [x] T013 [US2] The embed-skip gate: reuse the `CorpusBaseline` drift verdict (`internal/engine/baseline.go`) to decide, per re-ingest, whether the embedding config (model/dim/convention) is unchanged since the old chunks were embedded; expose the verdict to `processFile`/the worker. (depends US1's diff — needs to know which chunks are `UNCHANGED`) [research R3; FR-006/007]
- [x] T014 [US2] Preserve the embedding for `UNCHANGED`+baseline-unchanged chunks: copy the old `PrefixEmbedding` record to the new `cid` — a single direct-key KV write, synchronous in `processFile` BEFORE the async worker dequeues the job (no race); mark the job "embedding present — skip Ollama". (depends T013) [research R3 — closes the red-team "async race" hole]
- [x] T015 [US2] The async worker (`internal/pipeline/workers.go` `processJob`) skips the Ollama embed call when the embedding is already present for a chunk; it STILL re-indexes FTS + re-clusters NearDup normally under the new `cid` (no inverted-index rewiring). (depends T014) [FR-006; "recompute FTS + NearDup normally"]
- [x] T016 [US2] Tests (`internal/pipeline/*_test.go`): (a) re-ingest with unchanged baseline → `UNCHANGED` chunks' embeddings copied (not re-embedded), `ADDED` chunks embedded; (b) baseline drift → ALL chunks re-embedded (stale vectors not reused). Under `-race`. (depends T015) [quickstart Scenarios 3+4; FR-007]

**Checkpoint**: US2 — the embed-skip optimization, gated + race-free.

---

## Phase 5: User Story 3 — Delta correctness under repeated/moved text (Priority: P3)

**Goal**: the diff is correct for real-world document structures — repeated text as a multiset, moved paragraphs as `UNCHANGED`.

**Independent Test**: ingest a doc with a paragraph repeated 3× + a moved paragraph; edit (change the repeat count + move the paragraph); assert the multiset/content-identity delta. (`quickstart.md` Scenario 5; FR-003/008.)

### Tests for User Story 3

- [x] T017 [US3] Delta-correctness tests (`internal/pipeline/delta_test.go`): (a) a paragraph repeated 3×→2× yields 2 `UNCHANGED` + 1 `REMOVED` (multiset, not set); (b) a paragraph moved (same text, new position) = `UNCHANGED` (content identity, not positional); (c) preamble/no-change edge cases; (d) the cid-map stability (the `UNCHANGED` old→new pairing). Harden `diffChunks` (T006) if any test surfaces a bug. [research R2; FR-003/008; quickstart Scenario 5]

**Checkpoint**: the diff is correct under edge cases.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T018 Run `make lint` (golangci-lint — the `ci.yml` gate) + resolve every finding; run `go test -race ./...` full green.
- [ ] T019 Verify the <10ms ACK budget (Constitution IV): benchmark/profile the re-ingest path with the diff + `PrefixEmbedding` copy in place; confirm the ACK stays <10ms — or move the diff/copy post-ACK if profiling shows pressure. [plan Constitution Check IV; SC-005]
- [ ] T020 The UNCHANGED-ratio measurement (`research.md` R6): instrument a re-ingest over a representative vault (or fixture corpus); record the `UNCHANGED`/`ADDED`/`REMOVED` distribution + the `PrefixEmbedding`-copy vs Ollama-call cost. **Exit criterion**: if the real ratio ≪80%, re-scope/defer the embed-skip (the delta event ships regardless). [SC-001; research R6]
- [ ] T021 Run `quickstart.md` Scenarios 1–5 end-to-end on an isolated DB (non-default `--db-path`/ports per project CLAUDE.md).
- [ ] T022 Mark BL-010 resolved in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`; affirm constitution compliance in the commit (ContentHash non-identity sidecar; v2 migration ExpectedVersion 1→2, no new prefix; `RE_INGESTED` gRPC-only justified; pure Go, no new deps).
- [ ] T023 Schedule the **mandatory adversarial review** (Forge/Cato) for the concurrency-heavy code — the re-ingest reorder (T008) + the `PrefixEmbedding` copy (T014) + the worker skip (T015): races, torn-write, the synchronous-before-worker invariant, the <10ms budget. Do NOT merge before this review. [mirrors spec 040's mandatory review; the red teams' focus area]

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002–T005)**: T003/T004 depend on T002; T005 independent. **Blocks all stories.**
- **US1 (T006–T012)**: T006/T007 parallel; T008 needs T007; T009 needs T006+T008; T010/T011 after T009; T012 after T009+T010.
- **US2 (T013–T016)**: depends on US1's diff (T006) — needs to know which chunks are `UNCHANGED`. T013→T014→T015→T016.
- **US3 (T017)**: tests `diffChunks` (T006); can run alongside US2 (different file).
- **Polish (T018–T023)**: after all stories; T023 (adversarial review) before merge.

### Story completion order (single-author, sequential)
Foundational → US1 (MVP, stop + validate under `-race`) → US2 → US3 → Polish.

### Parallel opportunities (within phase, different files)
- Foundational: T002 (`model.go`) + T005 (`proto`) are parallel; T003/T004 after T002.
- US1: T006 (`delta.go`) + T007 (`delete.go`) parallel; then T008 → T009 → T010/T011.
- US3 (T017, `delta_test.go`) can run alongside US2.

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T005 (sidecar + migration + compute + wire).
3. US1 T006–T012 (diff + reorder + `RE_INGESTED` + the streaming test).
4. **STOP & VALIDATE**: `quickstart.md` Scenarios 1+2 under `-race`.

### Incremental delivery
- + US2 (T013–T016): the embed-skip optimization (gated + race-free).
- + US3 (T017): the multiset/move correctness tests.
- Polish (T018–T023): lint, the <10ms verification, the measurement, quickstart, BL-010 resolved, the mandatory adversarial review.

### Commit cadence
Conventional Commits to `main` after each checkpoint (`feat(spec043): ...`); `make build && vet && test && lint` green before every push. **The embed-skip + reorder (T008/T014/T015) MUST get the adversarial Forge/Cato review (T023) before merge — it is the red teams' focus area (races, torn-write, the <10ms budget).**

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 RE_INGESTED on re-ingest | T009, T012 |
| FR-002 per-chunk ADDED/REMOVED/UNCHANGED delta | T006, T009, T012 |
| FR-003 content-identity (not position) | T006, T017 |
| FR-004 old→new chunk-ID map | T006, T009, T010, T012 |
| FR-005 RE_INGESTED replaces INGESTED+DELETED | T009, T012 |
| FR-006 skip embed for UNCHANGED (baseline unchanged) | T013, T014, T015, T016 |
| FR-007 re-embed all on baseline drift | T013, T016 |
| FR-008 multiset diff (repeated text) | T006, T017 |
| FR-009 capture old chunks before delete | T007, T008 |
| FR-010 cid identity unchanged (ContentHash non-identity) | T002, T003 |
| SC-001 ≥80% UNCHANGED (target) | T020 (measurement) |
| SC-005 <10ms ACK preserved | T019 (verify) |

Every spec acceptance scenario (US1 #1–3, US2 #1–2, US3 #1–2) is covered by T012, T016, T017 respectively.

## Notes
- This is a **size-L** feature (a sidecar + migration + a diff + a re-ingest reorder + the embed-skip gate + the `PrefixEmbedding` preservation + the proto extension). The hardest pieces are the embed-skip preservation (T014 — gated, synchronous, race-free) + the re-ingest path reorder across `Reprocess`/`ReprocessAll`/the watcher (T008/T011).
- The embed-skip + reorder is concurrency-critical — the `-race` tests (T012/T016) + the adversarial review (T023) are load-bearing.
- The UNCHANGED-ratio measurement (T020) is the gate on the embed-skip's value (research R6); the delta event (US1) ships regardless.
