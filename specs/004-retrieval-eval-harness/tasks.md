# Tasks: Retrieval-Quality Evaluation Harness

**Input**: Design documents from `/specs/004-retrieval-eval-harness/`

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅, quickstart.md ✅, constitution.md ✅

**Tests**: INCLUDED. This feature IS a measurement/quality harness — FR-006
(read-only), FR-008 (skip-zero-relevant), SC-002 (gate detects regression), and
SC-004 (offline/no-network) are *verifiable guarantees* that require tests, and
the constitution mandates `go test ./...` green. Metric correctness needs
hand-computed unit tests. Tests are intrinsic here, not optional.

**Organization**: Grouped by user story (US1/US2/US3 from spec.md) so each story
is independently implementable, testable, and shippable as an increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story (US1/US2/US3); setup/foundational/polish have NO story label
- All paths are project-relative; new code is pure Go, `CGO_ENABLED=0` (Principle III)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Stand up the new `internal/eval` package.

- [x] T001 Create `internal/eval/` package with `doc.go` describing the package purpose (retrieval-quality evaluation: recall/precision/MRR/NDCG over a golden dataset), pure-Go, no third-party deps — in `internal/eval/doc.go`

**Checkpoint**: Empty package compiles; `CGO_ENABLED=0 go build ./...` green.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The architectural enabler (engine embedder injection) + the three
leaf modules the EvalRunner composes. No user-story work can begin until this
phase is complete.

**⚠️ CRITICAL**: Blocks all user stories.

- [x] T002 Add additive, optional embedder injection to the engine: add an unexported `embedder embed.Embedder` field to `Engine`, a `NewWithEmbedder(cfg config.Config, db *storage.DB, em embed.Embedder) *Engine` constructor, and an `embedderOrOllama()` helper that returns the injected embedder if set else `embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel)` (zero behavior change for existing callers) — in `internal/engine/engine.go`
- [x] T003 Wire the injected embedder into the canonical query path: replace the hardcoded `embed.NewOllama(...)` in `Engine.Query` with `e.embedderOrOllama()`, and use the same helper in the lazy ingest `pipeline()` so ingest+query share one embedder — in `internal/engine/query.go` and `internal/engine/engine.go`
- [x] T004 [P] Implement the deterministic offline embedder: a pure-Go feature-hashing vectorizer (lowercase word tokenize → hash into fixed N dims, e.g. 256 → +1 per bucket → L2-normalize) satisfying `embed.Embedder` (`Embed`/`Dimensions`/`Model`), no HTTP — in `internal/eval/embedder.go`
- [x] T005 [P] Implement IR metrics — `RecallAt(retrieved, relevant, k)`, `PrecisionAt(retrieved, relevant, k)`, `MRR(retrieved, relevant)`, `NDCGAt(retrieved, relevant, k)` (binary relevance; graded-capable formula), hand-rolled pure Go with deterministic tie-break (chunk_id lexicographic) — in `internal/eval/metrics.go`
- [x] T006 [P] Implement the golden-dataset loader/validator: parse JSONL `{id,query,relevant[,notes]}` via stdlib `encoding/json`+`bufio.Scanner`, enforce unique non-empty `id`, non-empty `query`, valid `relevant` chunk_id list; return `[]GoldenQuery` with validation errors — in `internal/eval/dataset.go`
- [x] T007 Foundational unit tests: (a) metric correctness with hand-computed expected values (incl. relevant-item-missed-from-top-k and the zero-relevant edge), (b) deterministic embedder is pure (no HTTP, byte-identical vectors for identical input, deterministic across runs), (c) dataset loader accepts valid JSONL and rejects malformed/duplicate-id records — in `internal/eval/metrics_test.go`, `internal/eval/embedder_test.go`, `internal/eval/dataset_test.go`

**Checkpoint**: Engine accepts an injectable embedder (existing callers unaffected); metrics/dataset/embedder modules are correct and independently tested.

---

## Phase 3: User Story 1 — Measure Retrieval Quality (Priority: P1) 🎯 MVP

**Goal**: `go-rag eval` computes recall@5/10, precision@5, MRR, NDCG@10 over a
committed golden dataset, drives the shared `engine.Query` path, runs offline and
reproducibly, and is also exposed as the `go_rag_eval` MCP tool.

**Independent Test**: `./bin/go-rag eval --embedder offline` runs, prints all five
metrics, and produces byte-identical numbers on a second run (SC-004); the
measured vault's document/chunk/embedding counts are unchanged (FR-006); zero-
relevant queries are skipped not fatal (FR-008).

### Implementation for User Story 1

- [x] T008 [US1] Implement the `EvalRunner` in `internal/eval/run.go`: build the engine over the measured/throwaway vault with the chosen embedder (offline deterministic by default), run `engine.Query` per golden query, join retrieved `QueryHit.ChunkID` against `relevant`, aggregate the `MetricSet` (averaged), record per-query results, and skip (not crash) zero-relevant and stale-label queries per FR-008
- [x] T009 [US1] Implement the `go-rag eval` cobra command in `internal/cli/eval.go` with flags `--golden`, `--corpus`, `--db-path`, `--mode`, `--k`, `--embedder auto|offline|ollama`, `--no-rerank`, `--format text|json`; render text (per contracts/eval.md) and JSON (Evaluation Run); exit codes 0/1/2; register it on the root command (depends on T008)
- [x] T010 [US1] Add the `go_rag_eval` MCP tool in `internal/mcp/server.go`: append the tool definition to `toolDefs()`, add a `case "go_rag_eval"` to `dispatchDB` + a `renderEval` that returns the same metric numbers as the CLI text output, and add a `go_rag_eval` bullet to the `guide()` tool list (Principle V parity) (depends on T008)
- [x] T011 [P] [US1] Author the committed golden dataset + corpus: add `testdata/golden/v1.jsonl` (~30–50 hand-labeled `{id,query,relevant}` records) and a small `testdata/golden/corpus/` (reuse `testdata/sample.md`,`testdata/sample.txt` + a few new docs); derive `relevant` chunk_ids by ingesting the corpus and recording the real content-addressed `ChunkID`s (Principle II portability)
- [x] T012 [US1] US1 tests: end-to-end harness test (offline deterministic → reproducible numbers), read-only guarantee (snapshot vault document/chunk/embedding counts before vs after — must be equal), FR-008 skip behavior (synthetic zero-relevant query present in `queries_skipped`); update `internal/mcp/server_test.go::TestMCP_ToolsListHas12` → assert 13 tools incl. `go_rag_eval` — in `internal/eval/run_test.go`, `internal/mcp/server_test.go`
- [x] T013 [US1] Validate US1 via quickstart Scenario A (offline eval) and Scenario F (MCP parity) in `specs/004-retrieval-eval-harness/quickstart.md`; confirm `make build && make vet && make test` green

**Checkpoint**: User Story 1 fully functional and independently testable — go-rag can measure retrieval quality offline and reproducibly, via CLI and MCP. This is the shippable MVP.

---

## Phase 4: User Story 2 — Regression Gate (Priority: P2)

**Goal**: A committed baseline + tolerance-based gate (`make test-eval` in CI) that
fails when recall@10 regresses beyond tolerance and passes on unrelated changes.

**Independent Test**: An unrelated change (e.g. doc-comment edit) leaves the gate
passing; a change that degrades retrieval fails it with a clear message naming the
metric and delta (US2 acceptance #1 & #2).

### Implementation for User Story 2

- [x] T014 [US2] Add baseline compare/record to `internal/eval/run.go`: load a `Baseline` (`{mode,recorded_at,metrics}`) from `testdata/golden/baseline.json`, compare the run's `MetricSet` against it with a tolerance (primary: recall@10; secondary: MRR, NDCG@10), and produce a `verdict pass|fail` with per-metric deltas; `--record-baseline` writes the file (depends on T008)
- [x] T015 [US2] Wire `--baseline`, `--tolerance` (default 2.0 points), and `--record-baseline` flags in `internal/cli/eval.go` to the verdict and the non-zero exit code on fail (depends on T014)
- [x] T016 [US2] Add a `test-eval` target to `Makefile` that runs `./bin/go-rag eval --embedder offline --baseline testdata/golden/baseline.json --tolerance 2.0` (offline, no network) (depends on T009, T015)
- [x] T017 [US2] Add the gate to `.github/workflows/ci.yml`: run `make test-eval`, scoped to PRs touching `internal/chunk`, `internal/index`, `internal/rerank`, or hybrid/RRF-weight files (so unrelated changes don't trip it)
- [x] T018 [US2] US2 tests: gate PASSES on a no-op change, FAILS on an induced regression (e.g. truncated candidate pool lowering recall@10 beyond tolerance), and `--record-baseline` round-trips — in `internal/eval/run_test.go`
- [x] T019 [US2] Record the initial offline baseline to `testdata/golden/baseline.json` via `go-rag eval --record-baseline`; validate via quickstart Scenario B (gate pass/fail) and Scenario C (record/refresh baseline)

**Checkpoint**: User Stories 1 AND 2 both work independently — measurement + a CI regression gate.

---

## Phase 5: User Story 3 — Versioned, Growable Golden Dataset (Priority: P3)

**Goal**: The golden dataset is a committed, versioned, diff-friendly artifact that
maintainers can grow; an optional synthetic query generator has a fixed contract.

**Independent Test**: Add a new labeled query to `testdata/golden/v1.jsonl`, re-run
eval, and confirm it is picked up in the per-query breakdown without code changes
(US3 acceptance #1).

### Implementation for User Story 3

- [x] T020 [P] [US3] Add dataset versioning + contribution guidance: a header/comment block in `testdata/golden/v1.jsonl` (or a `testdata/golden/README.md`) documenting the schema, how to add labeled queries, that `relevant` uses content-addressed chunk_ids, and that labels are human-authored
- [x] T021 [US3] Add the optional `go-rag eval-gen` command in `internal/cli/eval_gen.go`: `--corpus <dir> --n <count>` emits *candidate* `{query, chunk_id}` pairs to stdout for **human triage only** (never auto-committed to `v1.jsonl`) — contract per research.md D8; register on root command (deferrable: the core US3 value — a growable committed dataset — already exists without it)

**Checkpoint**: All three user stories independently functional; the golden set is growable and version-controlled.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-story validation, docs, and closing the loop on the audit.

- [x] T022 [P] Update CLI top-level help / any command overview to list `eval` (and `eval-gen`) — in `internal/cli/` root help text
- [x] T023 Final validation: run `make build && make vet && make test && make lint` green; run the full quickstart (Scenarios A–F); confirm `CGO_ENABLED=0` static build and no new third-party deps (`go mod tidy` clean)
- [x] T024 **Update the audit backlog**: check off item **H02** in `RAG_BOOK_AUDIT_BACKLOG.md` (mark implemented, reference `specs/004-retrieval-eval-harness/`), since this feature delivers it — the user's explicit closing instruction
- [x] T025 [P] Commit history follows Conventional Commits (`feat(eval): …`); ensure `cliff.toml` changelog generation picks up the new `eval` capability

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Setup; **BLOCKS all user stories**. T002→T003 are sequential (same engine area); T004/T005/T006 are parallel after T001; T007 depends on T004/T005/T006.
- **User Stories (Phase 3–5)**: All depend on Foundational completion.
  - **US1** is the MVP and must complete first (US2's gate measures US1's output).
  - **US2** depends on US1 (EvalRunner) — the gate wraps the measurement.
  - **US3** is independent of US2 (dataset growth works on top of US1 alone).
- **Polish (Phase 6)**: Depends on the implemented stories being complete; T024 (backlog update) runs last.

### User Story Dependencies

- **US1 (P1)**: After Foundational. No story dependencies. **MVP.**
- **US2 (P2)**: After Foundational + US1 (uses EvalRunner for the gate).
- **US3 (P3)**: After Foundational + US1 (grows the dataset US1 consumes). Independent of US2.

### Within Each User Story

- Leaf modules / data before the runner (US1: golden data T011 can be done in parallel with runner code).
- Runner (T008) before adapters (CLI T009, MCP T010).
- Tests (T012/T018) after implementation of that story.
- Story complete and independently testable before the next priority.

### Parallel Opportunities

- Foundational: T004 (embedder), T005 (metrics), T006 (dataset) run in parallel after T001.
- US1: T011 (golden data authoring) runs in parallel with T008–T010 (code).
- US3: T020 (docs) runs in parallel with T021 (eval-gen).
- Polish: T022 and T025 run in parallel.

---

## Parallel Example: Foundational + US1

```bash
# After T001, run the three leaf modules together:
Task: "Deterministic embedder in internal/eval/embedder.go"          # T004
Task: "IR metrics in internal/eval/metrics.go"                       # T005
Task: "Golden dataset loader in internal/eval/dataset.go"            # T006

# Within US1, data + code in parallel:
Task: "Author golden dataset testdata/golden/v1.jsonl + corpus/"     # T011 [P]
Task: "EvalRunner in internal/eval/run.go"                           # T008 (then T009, T010)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 (Setup) → Phase 2 (Foundational: engine injection + metrics/dataset/embedder).
2. Phase 3 (US1): runner + CLI + MCP + golden data + tests.
3. **STOP and VALIDATE**: US1 independently — offline eval prints reproducible numbers, read-only, MCP parity. This alone closes the audit's #1 risk.
4. Demo / merge as MVP.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. + US1 → measure retrieval quality (MVP — the blind spot is closed).
3. + US2 → regression gate (measurement becomes structural).
4. + US3 → growable dataset (measurement stays trustworthy over time).
5. Polish → docs, full quickstart, **check off H02 in the backlog** (T024).

### Notes

- Every existing caller of `engine.NewWithDB`/`engine.Query` is unchanged by T002/T003 — the injection is additive with a fallback.
- No new third-party dependencies (Principle III); metrics are hand-rolled.
- Commit after each task or logical group (Conventional Commits).
- Stop at any checkpoint to validate the story independently.
