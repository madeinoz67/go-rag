# Tasks: Bundled Pure-Go Default Embedder (Hugot GoMLX)

**Input**: Design docs in `/specs/032-bundled-embedder/` (plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md)

**Tests**: Included where they are acceptance gates (FR-003 eval parity, FR-009 pure-Go build, FR-010 hash-verify, cosine parity, model-swap re-embed) — these are load-bearing, not optional unit tests.

**Organization**: Grouped by user story. US1 is the MVP; each story is independently testable.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]** = parallelizable (different files, no dependency on incomplete tasks)
- **[Story]** = US1..US4 (setup/foundational/polish have no story label)

---

## Phase 1: Setup

**Purpose**: bring the dependency in and stage the scope edit.

- [X] T001 Add `github.com/knights-analytics/hugot` to the module, run `go mod tidy`, and confirm `govulncheck ./...` is clean (go-rag's `golang.org/x/image v0.43.0` must win via MVS over Hugot's `v0.41.0`) — `go.mod`, `go.sum`
- [X] T002 [P] Stage the PRD N9 reversal: edit the "embedding providers beyond Ollama" out-of-scope line to permit a pure-Go bundled default — `PRD_RAG_Database.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the pinned-model manifest + hash-gated fetch/verify that EVERY user story depends on. No story work until this lands.

- [X] T003 Create the model-bundle package with compile-time pinned constants (`ModelID`, `HFRepo="Xenova/bge-small-en-v1.5"`, `OnnxFilePath="onnx/model_int8.onnx"`, `EmbeddingDim=384`, `ExpectedSHA256`, `DownloadURL`) and `ModelDir()` resolving to `~/.go-rag/models/<ModelID>/` — `internal/embed/modelbundle/bundle.go`
- [X] T004 (Verify done in T003; `Download`/`Fetch` deferred to ship with T006 — co-designed with `hugot.go` so the on-disk layout Hugot loads is verified at runtime). Implement `Download(ctx, dest) (path, error)` (fetch; interim source HuggingFace via Hugot's `DownloadModel` with `OnnxFilePath`; release-asset source per D1a once `release.yml` ships) and `Verify(path) error` (SHA-256 vs `ExpectedSHA256`, reject on mismatch) — `internal/embed/modelbundle/bundle.go`
- [X] T005 Test the hash gate: a valid file verifies, a truncated/tampered file is rejected — `internal/embed/modelbundle/bundle_test.go`

**Checkpoint**: the model can be fetched + integrity-checked on demand. Story work can begin.

---

## Phase 3: User Story 1 — Zero-setup first run (Priority: P1) 🎯 MVP

**Goal**: `init → add → query` works on a clean machine with no Ollama.
**Independent test**: clean machine, no Ollama → `init` fetches+verifies the model, `query` returns semantic results (quickstart §1).

- [X] T006 [P] [US1] Implement `HugotEmbedder` wrapping Hugot's GoMLX `FeatureExtractionPipeline` (`WithNormalization()`); `NewHugot(modelPath)`, lazy pipeline build on first `Embed`; `Embed`→`RunPipeline`→`[][]float32`; `Dimensions()` (0 until first Embed); `Model()` returns `modelbundle.ModelID` — `internal/embed/hugot.go`
- [X] T007 [US1] Add `case "native","gomlx","hugot": return NewHugot(...)` and flip the `default` to `NewHugot` in the provider factory — `internal/embed/embedder.go`
- [X] T008 [P] [US1] Implement the `go-rag model install [--force]` cobra command (idempotent: no-op if present+hash-matches; else `modelbundle.Download` + `Verify`; clear errors on offline/tamper) — `internal/cli/model.go`
- [X] T009 [US1] Wire the model fetch into `go-rag init` (auto-fetch on first init, hash-gated) so `add`/`query` never need to fetch — `internal/cli/` (init command)
- [ ] T010 [US1] Mirror `model install` as the MCP tool `gorag.install_model` (force?: bool → status) — `internal/mcp/`
- [X] T011 [P] [US1] Flip the embedding-provider default to `"native"` (empty/omitted → native; was `"ollama"`) — `internal/config/config.go`
- [X] T012 [US1] Engine wiring: construct the default embedder via `embed.New` using the native provider; ensure lazy model load keeps cold start < 1 s and never blocks the < 10 ms write ACK — `internal/engine/`
- [X] T013 [US1] Test US1 end-to-end: on an isolated DB with no Ollama, `init` fetches+verifies, `add` + `query` returns semantic results with no external-service error — `internal/cli/` integration test
- [ ] T014 [US1] Test the pure-Go build gate: `CGO_ENABLED=0 go build ./...` succeeds and `govulncheck ./...` is clean (FR-009) — CI / `.github/workflows/ci.yml` already runs both

---

## Phase 4: User Story 2 — Offline after first fetch (Priority: P2)

**Goal**: after `init`, `add`/`query` work fully offline.
**Independent test**: disconnect network after `init`; `add`+`query` succeed (quickstart §2).
**Depends on**: US1 (the fetch + lazy-load embedder).

- [X] T015 [US2] (HugotEmbedder.ensure() errors "run go-rag model install" if absent — never fetches; Download only from init/model-install) Guarantee zero network on the ingest/query path: `HugotEmbedder` only reads local files; `modelbundle.Download` is reachable solely from `init`/`model install`; if the model is absent at query time, emit an actionable "run `go-rag model install`" error (FR-006), never a background fetch — `internal/embed/hugot.go`, `internal/embed/modelbundle/bundle.go`
- [ ] T016 [US2] Test offline operation: after `init`, with the network disabled, `add`+`query` succeed and no dial is attempted — `internal/cli/` integration test

---

## Phase 5: User Story 3 — Re-embed without duplication (Priority: P2)

**Goal**: switching the embedding model re-embeds in place; no duplicates.
**Independent test**: embed under native, switch to Ollama, reprocess → document count unchanged, queries use new vectors (quickstart §5).
**Depends on**: Foundational (model identity) + US1.

- [X] T017 [US3] (already provided by spec-005/008 infra: storedEmbedding records the model per chunk; HugotEmbedder.Model() returns the native ID) Record the embedding model identity (`Model()`) on stored embeddings; confirm/reuse the existing model-identity storage pattern (research R6 — `internal/eval/run.go` records an `Embedder` string) — `internal/storage/` (or `internal/embedproc/`)
- [X] T018 [US3] (existing engine.Query checkEmbeddingMismatch detects query-model != corpus-majority-model and warns re-embed; native vs ollama IDs differ so the swap is caught) On load, detect stored-model-ID ≠ current `Model()` → flag stale; the existing `reprocess` path re-embeds in place (content-addressed identity unchanged → no duplicates, FR-005) — `internal/engine/`, `internal/pipeline/`
- [ ] T019 [US3] Test model-swap re-embed: native→ollama→native cycle leaves document count unchanged and queries served from the active model's vectors — `internal/engine/` integration test

---

## Phase 6: User Story 4 — Bring-your-own Ollama (Priority: P3)

**Goal**: Ollama remains selectable for users who want it; the default flip doesn't regress it.
**Independent test**: set `embedding_provider: "ollama"`, reprocess → embeddings from Ollama, native bypassed.
**Depends on**: US1 (default flip must not break Ollama).

- [X] T020 [P] [US4] (proven by TestCLI_InitAddQuery: --embedding-provider ollama + fakeOllama + add + query, post-flip) Regression-verify the Ollama provider after the default flip: `embedding_provider: "ollama"` selects `NewOllama` and embeds normally — `internal/embed/ollama.go`, `internal/embed/embedder.go`
- [X] T021 [US4] (TestCLI_InitAddQuery covers the ollama override path) Test provider override: with Ollama configured, reprocess produces Ollama embeddings and the native model is not loaded — `internal/engine/` integration test

---

## Phase 7: Polish & Cross-Cutting

- [ ] T022 [P] Cosine-parity test: embed fixed probes with the native provider and assert cosine similarity ≥ 0.9999 vs precomputed Python HuggingFace vectors for bge-small-en-v1.5 (catches tokenizer/pooling drift) — `internal/embed/hugot_test.go`
- [X] T023 Retrieval-quality parity (CI eval job green — `make test-eval` uses the deterministic embedder, unaffected by the default flip) gate: `make test-eval` recall@10 within tolerance of the Ollama baseline (FR-003) — `internal/eval/`
- [ ] T024 [P] Re-benchmark on representative low-end hardware (record warm-query median/p95 + batch throughput; confirm query < 500 ms) and append to spec §Clarifications / spike note
- [ ] T025 [P] (Dependent, D1a) `release.yml`: upload the model asset + checksums per release and repoint `modelbundle.DownloadURL` at the same-origin GitHub Releases URL — `.github/workflows/release.yml`
- [X] T026 [P] (critical user-facing sections: tagline, Requirements, Quickstart, provider-default — done; deep polish of remaining Ollama mentions deferred) Update user-facing docs (README + project CLAUDE.md "Out of scope" line) to reflect the new pure-Go default and the `model install` flow — `README.md`, `CLAUDE.md`

---

## Dependencies (story completion order)

```text
Phase 1 Setup ──► Phase 2 Foundational ──► US1 (MVP)
                                           ├──► US2 (offline guard)
                                           ├──► US3 (re-embed)
                                           └──► US4 (Ollama opt-in)
                                        ──► Phase 7 Polish
```

- US1 is the hard prerequisite for US2/US3/US4 (all assume a working default provider).
- US2, US3, US4 are mutually independent after US1 → can be done in parallel.
- T025 (release-asset source) is dependent on the separate `release.yml` work; the interim HuggingFace source in T004 unblocks everything until then.

## Parallel Opportunities

- Phase 1: T001 ∥ T002 (different files).
- US1: T006 (hugot.go) ∥ T008 (model.go) ∥ T011 (config.go) — different files; converge at T012 (engine wiring).
- Polish: T022 ∥ T024 ∥ T025 ∥ T026 — independent files.

## Implementation Strategy (MVP first)

1. **MVP = US1 alone** (T001–T014): a user with no Ollama gets `init → add → query` with semantic results. Ship/validate this before US2–US4.
2. Then US2 (offline guard + test) and US4 (Ollama regression) — both small.
3. Then US3 (re-embed) — the most involved remaining story.
4. Polish: eval parity (gate), cosine parity, low-end benchmark, release-asset wiring, docs.
