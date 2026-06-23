# Tasks: Pebble-backed Async FTS Index (H16, pivoted)

**Input**: Design documents from `/specs/018-fts-snapshot/` — [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/fts-pebble-contract.md](contracts/fts-pebble-contract.md), [quickstart.md](quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: INCLUDED — go-rag is test-gated (constitution: `go test -race -cover` + `make test-eval`). The FTS rewrite touches many existing tests (parity, H06 cache, H14 filter, H15 context-window) — all must pass.

**Organization**: Phase 2 is the **FTS rewrite** (foundational — every story uses it). Phases 3–6 wire it into the pipeline (LoadIndex, storeDocument/processJob, DeleteDoc, migration/eval). Phase 7 is polish + backlog closure.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US4 (user-story phases only)
- Exact file paths in every description

---

## Phase 1: Setup

**Purpose**: Confirm a clean baseline.

- [ ] T001 Record clean baseline: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` green; `make test-eval` recall@10 baseline (must not regress).

---

## Phase 2: Foundational — The FTS Rewrite (Blocking)

**Purpose**: Rewrite the BM25 FTS from an in-memory `map[string]map[string]float64` to a durable Pebble-backed inverted index. This is the core of the pivot — every user story uses the rewritten FTS.

**⚠️ CRITICAL**: The BM25 math (k1=1.2, b=0.75, field weights concept=3.0/tags=2.0/body=1.0) MUST be identical to the current in-memory FTS (transparency, FR-008). The rewrite changes the DATA SOURCE (map → Pebble prefix scans), not the MATH.

- [ ] T002 [P] Assign prefix roles in `internal/storage/storage.go`: `PrefixFTSPosting = 0x05`, `PrefixFTSTermStat = 0x06`, `PrefixFTSGlobalStat = 0x08` (within the reserved `0x05–0x08` BM25 FTS range). Add key-constructor helpers (posting key, term-stat key, global-stat key) or document the layout.
- [ ] T003 Rewrite the FTS struct in `internal/index/fts.go`: replace `postings map[string]map[string]float64` + `docLen` + `totalLen` + `N` with a thin Pebble-backed adapter `{db *pebble.DB, mu sync.RWMutex, idfCache map[string]float64}`. `NewFTS(db *pebble.DB) *FTS` (signature change — gains the db param). Keep `Tokenize`/`Trigrams`/`fieldWeight` unchanged. Remove the in-memory posting map entirely.
- [ ] T004 Implement `FTS.Index` in `internal/index/fts.go`: tokenize each field → build one atomic Pebble batch (posting keys `0x05|term|sep|field|chunkID` with 7-byte values + DF updates `0x06|term` + global-stats `0x08|"stats"`) → commit `pebble.NoSync`. Same field-weight semantics as today. Invalidate the IDF cache for the touched terms.
- [ ] T005 Implement `FTS.Search` in `internal/index/fts.go`: tokenize query → for each term, prefix-scan `0x05|term|0x00|*` via `db.NewIter` (LowerBound/UpperBound per term) → decode 7-byte posting values → accumulate BM25 (k1=1.2, b=0.75, field weights, IDF from cached DF `0x06|term`) → top-k `[]Hit`. Read N + avgdl from `0x08|"stats"`. **Math unchanged from the current Search.** IDF computed lazily + cached (pattern: MuninnDB `getIDF`).
- [ ] T006 Implement `FTS.Delete` in `internal/index/fts.go`: **signature change** `(chunkID, content string)` — re-tokenize content → for each term, batch-delete `0x05|term|sep|field|chunkID` for all fields + invalidate IDF cache. Same delete semantics as today (remove the chunk's postings).
- [ ] T007 Tests in `internal/index/fts_test.go`: Index/Search round-trip (index chunks, search, assert correct hits + BM25 order); Delete (index, delete, assert gone); prefix-scan query for a multi-term query; field weighting (concept > tags > body); stats correctness (N, avgdl, DF). Use a temp Pebble DB.

**Checkpoint**: The Pebble-backed FTS works standalone — Index writes postings, Search reads them via prefix scans, Delete removes them. BM25 math is identical. Ready to wire into the pipeline.

---

## Phase 3: User Story 1 — Cold start has no FTS rebuild (Priority: P1) 🎯 MVP

**Goal**: `LoadIndex` stops re-tokenizing chunks; the FTS is read from Pebble in place.

**Independent Test**: quickstart Scenario 1 — cold-start a fresh engine and keyword-query; no re-tokenization; fast.

### Implementation for User Story 1

- [ ] T008 [US1] Simplify `LoadIndex` in `internal/pipeline/load.go`: replace the `PrefixScan(PrefixChunk)` FTS rebuild with `fts := index.NewFTS(db)` (O(1) adapter). Keep the vector reload from `PrefixEmbedding` (unchanged). Return `(fts, vec, err)`.
- [ ] T009 [US1] Add the one-time migration backfill in `internal/pipeline/load.go`: if the `0x08|"stats"` key is absent (pre-pivot vault), scan `PrefixChunk`, tokenize each, write postings + DF + stats (the same logic as the old LoadIndex, now writing to Pebble). Gated by the stats-key check so it runs exactly once.
- [ ] T010 [US1] Test in `internal/pipeline/load_test.go`: cold start with a Pebble-backed FTS (stats key present) does NOT scan PrefixChunk for FTS (only vectors); cold start with a pre-pivot vault (no stats key) triggers the migration backfill (writes postings, subsequent starts skip it).

**Checkpoint**: US1 — cold start has no FTS rebuild; the FTS is durable in Pebble.

---

## Phase 4: User Story 2 — FTS indexing is async, off the ACK path (Priority: P1)

**Goal**: `storeDocument` drops its sync `fts.Index` call; BM25 indexing happens entirely async in `processJob` (Principle IV compliance).

**Independent Test**: ACK time unchanged (no FTS work on the ACK path); keyword visibility after `waitEmbedded`.

### Implementation for User Story 2

- [ ] T011 [US2] Remove the sync `fts.Index` call from `storeDocument` in `internal/pipeline/pipeline.go` (the FTS is now async in `processJob`). Leave the durable Pebble writes (doc/chunks/path/contenthash) — the ACK path is now only durable writes.
- [ ] T012 [US2] Verify `processJob` in `internal/pipeline/workers.go` still calls `fts.Index(c.ID, fields)` — the call site is unchanged, but the backing is now Pebble (the FTS.Index from T004 writes posting keys). Confirm the field map `{"body": c.Content}` matches what Search expects.
- [ ] T013 [US2] Test in `internal/pipeline/workers_test.go`: ingest a chunk; immediately query (before async drain) — the chunk is NOT keyword-visible (async pending); `waitEmbedded`; query — now visible. Assert ACK time is unchanged vs pre-pivot.

**Checkpoint**: US1 + US2 — cold start is fast + FTS indexing is async (Principle IV).

---

## Phase 5: User Story 3 — Postings stay current + survive restart (Priority: P1)

**Goal**: Ingest/delete reflect in the durable postings; no marker, no snapshot — always current.

**Independent Test**: ingest → cold start → find; delete → cold start → gone.

### Implementation for User Story 3

- [ ] T014 [US3] Update `DeleteDoc` in `internal/pipeline/delete.go`: pass the chunk content to `fts.Delete(chunkID, content)` (the new signature from T006). DeleteDoc already reads chunk records (has `c.Content`). Remove the old `fts.Delete(cid)` call (no content) and replace with `fts.Delete(cid, c.Content)`.
- [ ] T015 [US3] Test in `internal/pipeline/delete_test.go`: ingest doc A + `waitEmbedded`; cold-start + query → A found. Delete A; cold-start + query → A gone. Ingest doc B; cold-start + query → B found, A still gone.

**Checkpoint**: US1–US3 — the Pebble-backed FTS is durable, current, and async.

---

## Phase 6: User Story 4 — Backward compat + no quality regression (Priority: P2)

**Goal**: Existing vaults migrate; retrieval quality is unchanged; the full suite (parity, H06, H14, H15) passes with the rewritten FTS.

**Independent Test**: `make test-eval` recall@10 unchanged; all existing tests pass.

### Implementation for User Story 4

- [ ] T016 [US4] Transparency test in `internal/index/fts_test.go` (or a dedicated `fts_transparency_test.go`): build a Pebble-backed FTS and (for comparison) the old in-memory FTS over the same corpus; run identical queries; assert byte-identical hits (chunkIDs + BM25 scores within epsilon). This is the FR-008 gate.
- [ ] T017 [US4] Run `make test-eval` — recall@10 must be unchanged from the T001 baseline (the BM25 math is identical; only the backing changed).
- [ ] T018 [US4] Run the full test suite `go test -race ./...` — all existing tests pass with the rewritten FTS. Fix any breakage in tests that assumed the old in-memory FTS (e.g., tests that checked `fts.postings` directly, or assumed sync keyword visibility without `waitEmbedded`).

**Checkpoint**: US1–US4 — the pivoted FTS is transparent, backward-compatible, and quality-neutral.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: End-to-end validation, docs, gates, and backlog closure.

- [ ] T019 [P] Update `README.md` if it documents FTS internals (e.g., the architecture section mentions the in-memory BM25 — update to reflect the Pebble-backed design).
- [ ] T020 Run quickstart.md scenarios 1–7 on an isolated DB; capture results (cold-start time improvement, transparency, currency, migration, eval).
- [ ] T021 Final gates all green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race -cover ./...`, `make test-eval`. Record coverage.
- [ ] T022 Mark H16 complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H16 line checkbox `[ ] → [x]` and append a `✅ COMPLETE (spec 018, pivoted)` note — following the format of the neighbouring H06/H11 entries — summarising what shipped (Pebble-backed FTS: postings as keys under 0x05, per-term prefix-scan BM25, async indexing in processJob, cold start with no rebuild, one-time migration backfill), the benchmark (0.3ms worst-case query, 6.7MB store), the gates passed, and the design pivot from snapshot to Pebble-backed (grounded in MuninnDB research). This is the explicit "make the backlog item complete when finished" task requested for this spec.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps.
- **Foundational (Phase 2)**: depends on Phase 1 — **BLOCKS all user stories**. The FTS rewrite is the core.
- **US1 (Phase 3)**: depends on Phase 2 (the rewritten FTS + LoadIndex).
- **US2 (Phase 4)**: depends on Phase 2 + US1 (storeDocument drops sync FTS once LoadIndex is simplified).
- **US3 (Phase 5)**: depends on Phase 2 + US2 (DeleteDoc signature change after the FTS is wired).
- **US4 (Phase 6)**: depends on Phases 2–5 (transparency + eval against the fully-wired FTS).
- **Polish (Phase 7)**: depends on all stories complete.

### Within Each Phase

- The FTS rewrite (T003–T006) is sequential (struct → Index → Search → Delete — same file, each builds on the prior).
- Pipeline edits (T008/T009, T011/T012, T014) are small, one-file-per-task.
- T007 (FTS unit tests) can be written alongside T003–T006 (TDD) or after.

### Parallel Opportunities

- **Phase 2**: T002 (storage prefixes) is independent of T003–T006 (different file) → can run ahead.
- **Phase 7**: T019 (README) is independent.

---

## Implementation Strategy

### MVP First (Foundation + US1)

1. Phase 1 (baseline) → Phase 2 (FTS rewrite: the adapter, Index, Search, Delete, unit tests).
2. Phase 3 (US1: LoadIndex simplification + migration).
3. **STOP and VALIDATE**: quickstart Scenario 1 — cold start is fast (no FTS rebuild); keyword queries return correct results.
4. The headline win (cold-start rebuild eliminated) is live.

### Incremental Delivery

1. Foundation → US1 (MVP: no-rebuild cold start) → validate.
2. + US2 (async FTS off the ACK path — Principle IV compliance) → validate.
3. + US3 (delete signature + currency) → validate.
4. + US4 (transparency + eval + full-suite) → validate.
5. Polish → T019–T021 green → T022 closes H16 in the backlog.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[USx]` labels map tasks to spec user stories for traceability.
- Every story is independently testable at its checkpoint.
- Commit after each task or logical group (Conventional Commits, straight to `main`).
- **Highest-risk item**: **T003–T005 (the FTS rewrite)**. The BM25 math MUST be identical (FR-008/SC-001). The transparency test (T016) is the gate — it builds both FTses and diffs. Any BM25 score divergence is a bug in the prefix-scan or the stats management.
- **Second-risk**: **T012 + T013 (async visibility window)**. Tests that assumed keyword visibility immediately after `add` must now `waitEmbedded`. The H01 "keyword query right after ACK" guarantee is intentionally changed (async, symmetric with vectors) — document the behavior change.
- **Third-risk**: **T009 (migration backfill)**. A pre-pivot vault must migrate cleanly (one-time, gated by stats key) without re-ingestion. The migration is the OLD LoadIndex behavior (scan + tokenize) redirected to Pebble — verify it runs exactly once.
