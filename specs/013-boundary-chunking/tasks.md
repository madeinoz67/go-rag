---

description: "Task list for H10 — Boundary-aware chunking cascade"
---

# Tasks: Boundary-Aware Chunking Cascade (H10)

**Input**: Design documents from `/specs/013-boundary-chunking/` — plan.md, spec.md, research.md, data-model.md, quickstart.md

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md

**Tests**: The constitution mandates `go test ./...` green on every change, and H10's value (boundary-aware chunks) is only provable through tests; the H02 eval harness is the no-regression gate (SC-003). Tests included per FR/acceptance scenario.

**Organization**: Tasks grouped by user story. Foundational phase builds the cascade (sentence/paragraph helpers + the `Split` rewrite + corrected doc); the stories verify it (US1=boundaries, US2=doc-accuracy, US3=no-regression).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story (US1, US2, US3) — present only on user-story-phase tasks
- Exact file paths in every description

## Path Conventions

Go single-module repo. This feature lives in `internal/chunk/`. Tests alongside source. Paths are repository-relative.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm a green starting point.

- [x] T001 Confirm green starting state: run `make build && make vet && make test` from repo root (capture any pre-existing failures so regressions are unambiguous)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The cascade — sentence/paragraph boundary helpers + the `Split` rewrite + the corrected package doc. Every user story depends on this.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `make test` is green.

- [x] T002 [P] Create `internal/chunk/sentences.go`: `splitParagraphs(text string) []string` (split on blank-line `\n\n`, preserve order) and `splitSentences(text string) []sentenceSpan` where `sentenceSpan` carries `text`, `start`, `end` char offsets — a single linear scan splitting at sentence terminators (ASCII `. ! ?` and CJK `。！？`) followed by whitespace/end, NO regex backtracking (O(text), safe on the sync ACK path). No NLP dependency. Independent of other tasks
- [x] T003 Rewrite `func (s *Splitter) Split` in `internal/chunk/chunk.go` to the cascade: iterate paragraphs → sentences; greedily fill whole sentences into a chunk up to the `Size` token budget; flush at a sentence boundary when the next would overflow; flush at a paragraph boundary when the current chunk is non-trivially filled (don't span paragraphs that fit); for a single sentence exceeding the budget, fall back to the existing word-window (`tokenizeWords`) on that sentence; carry last sentence(s) within the `Overlap` budget into the next chunk (sentence-granularity overlap); preserve the sub-`MinTokens` final-tail merge. Emit `Segment`s with accurate `StartCharIdx`/`EndCharIdx` into the source and `TokenCount` via `EstimateTokens`. ALSO rewrite the `internal/chunk` package doc (top of `chunk.go`) to truthfully describe the cascade (paragraph→sentence→word greedy-fill, ~Size tokens, sentence-granularity overlap, MinTokens tail-merge, 1.3× token heuristic) — FR-005. `Splitter`/`Segment`/`NewSplitter`/`EstimateTokens` signatures unchanged; pipeline + CLI callers untouched. Depends on T002

**Checkpoint**: Foundation ready — `Split` produces boundary-aware chunks; the package doc is accurate; existing tests still compile/pass where behavior overlaps. `make build && make vet && make test` green.

---

## Phase 3: User Story 1 — Chunks respect boundaries (Priority: P1) 🎯 MVP

**Goal**: Chunks end at sentence boundaries, paragraphs are honored, over-long sentences word-fallback, CJK + no-terminator cases degrade gracefully. (spec US1, FR-002/003/004, SC-001/002)

**Independent Test**: Structured prose → chunks align to sentence/paragraph boundaries; over-long sentence → word-split; no-terminator/CJK handled.

### Tests for User Story 1

- [x] T004 [US1] Add boundary tests to `internal/chunk/chunk_test.go`: (a) multi-sentence prose → every chunk ends at a sentence terminator (or is final); (b) two paragraphs each fitting the budget → no chunk spans the `\n\n`; (c) a single sentence > size budget → split at word boundaries into ≥2 budget-sized chunks (FR-004); (d) no-terminator text (a list/log) → degrades to word-window, no giant chunk; (e) CJK prose with `。！？` → chunks end at CJK terminators, characters preserved. Depends on T003

**Checkpoint**: User Story 1 functional — boundary-aware chunking proven.

---

## Phase 4: User Story 2 — Package doc is accurate (Priority: P1)

**Goal**: Every behavioral claim in the `internal/chunk` package doc is implemented and test-covered — no false claims (FR-005, US2, SC-004).

**Independent Test**: Read the doc; confirm each claim matches the implementation + a test.

### Implementation for User Story 2

- [x] T005 [US2] Verify `internal/chunk` package-doc accuracy (the rewrite from T003): confirm each claim — cascade levels (paragraph→sentence→word), ~Size budget, sentence-granularity overlap, MinTokens tail-merge, 1.3× token heuristic — is implemented in `Split`/`sentences.go` AND covered by a test in `chunk_test.go`. Tighten wording if any claim overstates (e.g. the old false "cascade" claim must not linger). Depends on T003, T004

**Checkpoint**: User Story 2 functional — the doc tells the truth.

---

## Phase 5: User Story 3 — No retrieval regression (Priority: P2)

**Goal**: The chunking change does not regress retrieval quality. (spec US3, FR-006, SC-003)

**Independent Test**: `make test-eval` shows recall@10/MRR no worse than the word-window baseline.

### Tests for User Story 3

- [x] T006 [US3] Run the safety gate: `make build && make vet && make test && make test-eval` — recall@10/MRR no worse than baseline (SC-003). Because chunk boundaries shift, this is the gate that proves the cascade helps or is neutral; if it regresses, tune the cascade (paragraph-flush threshold / overlap granularity) rather than reverting. Depends on T003

**Checkpoint**: All user stories independently functional; the change is safe to ship.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Merge gate + backlog hygiene.

- [x] T007 Run `make build && make vet && make test && make test-eval`; run `golangci-lint run` and `govulncheck ./...` (note: both may be unavailable in this env — `golangci-lint` config-version-incompatible, `govulncheck` uninstalled — record as pre-existing env issues, not code regressions, as on prior specs)
- [x] T008 Run the hermetic `quickstart.md` scenarios 1–8 (unit + eval; no real Ollama); scenario 9 (real ingest) is optional
- [x] T009 Mark **H10** complete in `RAG_BOOK_AUDIT_BACKLOG.md`: change the H10 row `- [ ]` → `- [x]` and append a `✅ COMPLETE (spec 013): …` annotation summarizing the cascade (paragraph→sentence→word, pure-Go sentence detection, eval-gated) + the scope decision (clarify Q1 → Option A: implemented the cascade, not the doc-only fix) + the gates (matching prior rows' style). Depends on T004–T006 green and T007 passing
- [x] T010 Commit to `main` with Conventional Commits (e.g. `feat(chunk): boundary-aware paragraph→sentence→word cascade (H10)`) including the backlog update, and push

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: T002 (independent) → T003 (depends T002). **BLOCKS all user stories.**
- **US1 (Phase 3)**: T004 depends on T003.
- **US2 (Phase 4)**: T005 depends on T003 + T004.
- **US3 (Phase 5)**: T006 depends on T003.
- **Polish (Phase 6)**: T007/T008 depend on all stories; T009 depends on green stories + gate; T010 is last.

### Critical path

`T001 → T002 → T003 → (US1/US3) → T006 → T009 → T010`. US2 (T005) rides on US1's tests.

### Parallel Opportunities

- Phase 2: T002 (`sentences.go`) is fully independent → starts immediately; T003 (`chunk.go`) follows (same package, sequential).
- Phases 3 & 5: T004 (boundary tests) and T006 (eval gate) are logically independent once T003 lands (different verification surfaces).

---

## Parallel Example: verification (Phases 3 & 5)

```bash
# Once T003 lands, the two verifications are independent:
Task: "boundary tests in internal/chunk/chunk_test.go"   # T004 (US1)
Task: "eval gate — make test-eval no-regression"         # T006 (US3)
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (Setup) — green baseline.
2. Phase 2 (Foundational) — cascade + corrected doc; existing tests green.
3. Phase 3 (US1) — boundary-aware chunking proven.
4. **STOP and VALIDATE** — run the boundary tests; run quickstart scenarios 1–5.
5. US1 delivers the headline fix (coherent chunks); US2 (doc truth) + US3 (no regression) must land before merge.

### Incremental Delivery

1. Setup + Foundational → cascade works, doc accurate; `Split` contract preserved.
2. + US1 → boundary-aware chunks proven (MVP).
3. + US2 → doc verified truthful.
4. + US3 → eval gate green (no regression).
5. Polish → gate green, backlog marked, committed to `main`.

---

## Notes

- Same-file tasks are never marked `[P]` (T003 in `chunk.go` after T002 in `sentences.go`; T004/T005 in `chunk_test.go`/doc).
- **`Splitter.Split` signature is unchanged** — `pipeline.processFile:206` and the 4 CLI `NewSplitter` sites are untouched. Any edit to a caller is scope drift.
- **Chunking is on the sync ACK path** (pre-Pebble-Sync) — the cascade MUST stay O(text) (linear scan in `splitSentences`, no catastrophic regex) so the < 10ms write-ACK budget is unaffected (Principle IV).
- **Re-chunking changes chunk IDs**: existing vaults keep old chunking until `Reprocess`/`migrate` (idempotent, no dupes — Principle II). Documented operational note, not a migration the feature runs.
- **Abbreviations** ("Dr.", "e.g.") split early (safe, dependency-free); an abbreviation blocklist is explicitly out of scope.
- `golangci-lint`/`govulncheck` may be unavailable in this env (same as specs 006–012) — record as an env note, do not block. `go vet` + `-race` are the active gates.
