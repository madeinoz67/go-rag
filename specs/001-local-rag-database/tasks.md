# Tasks: Local RAG Database (go-rag v1)

**Input**: Design documents from `specs/001-local-rag-database/` (plan.md, spec.md,
research.md, data-model.md, contracts/, quickstart.md)

**Prerequisites**: plan.md (required), spec.md (required)

**Tests**: **TDD requested** (`add tdd`). Within every phase, test tasks come FIRST
(Red — write the test, watch it fail) and implementation tasks SECOND (Green — make
it pass). Test files are Go `*_test.go` alongside the code they cover. Run `go test
./...` after each pair.

**Organization**: Tasks grouped by user story (US1 = MVP, US2 status, US3 config,
US4 watch) so each story is independently implementable and testable.

> The go-rag scaffold already exists (cmd/internal tree + interface stubs, from
> project setup). These tasks implement the bodies behind the stubs, test-first.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: which user story (US1–US4); setup/foundational/polish have no label
- Exact Go file path on every task

## Path Conventions

- Go module `github.com/madeinoz67/go-rag`, layout `cmd/` + `internal/`
- Single entrypoint: `cmd/go-rag/main.go`; private code under `internal/`; tests as
  `internal/<pkg>/<pkg>_test.go` (or `<topic>_test.go`)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Add the runtime dependencies and fixtures the scaffold is missing.

- [x] T001 [P] Add runtime deps: run `go get github.com/cockroachdb/pebble github.com/philippgille/chromem-go github.com/pdfcpu/pdfcpu github.com/fsnotify/fsnotify` then `go mod tidy` (updates `go.mod`/`go.sum`)
- [x] T002 [P] Create test fixtures in `testdata/`: `sample.txt`, `sample.md` (with frontmatter), `sample.docx`, and a small `sample.pdf` (consumed by US1 tests and `quickstart.md`)
- [x] T003 [P] Add a "Watching large trees" note to `README.md` documenting the Linux `fs.inotify.max_user_watches` tuning (research Q11)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure every user story depends on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

### Tests for Foundational (write FIRST, watch fail)

- [x] T004 [P] Write storage tests in `internal/storage/db_test.go`: Set/Get/Delete round-trip; `PrefixScan` returns only the requested prefix's keys; a second `Open` on the same path is rejected (single-writer lock); a `Sync` write is readable after reopen
- [x] T005 [P] Write model tests in `internal/model/model_test.go`: `GenerateID` is deterministic and order-independent across the metadata map; `GenerateID` ≠ `ContentHash` for the same content (identity vs change-detection)
- [x] T006 [P] Write config tests in `internal/config/config_test.go`: `Default()` returns documented values; `Validate` rejects malformed URLs and non-positive ints; `Load`/`Save` round-trip preserves all fields

### Implementation (make the tests pass)

- [x] T007 Implement the Pebble store in `internal/storage/db.go`: `Open(path)` with single-writer file lock, `Close`, prefix-keyed `Get/Set/Delete/PrefixScan` using the `0x01`–`0x0F` constants in `internal/storage/storage.go`; every write uses `Sync` for durable <10ms ACK
- [x] T008 [P] Flesh out `internal/model/model.go`: JSON tags + `Document.GenerateID()` (SHA-256 over content + canonicalized metadata) and `ContentHash(raw []byte)` helper
- [x] T009 [P] Implement `internal/config/config.go`: `Default()`, `Load(path)`, `Save(path)`, `Validate()`, `Get(key)`/`Set(key, val)`

**Checkpoint**: storage + model + config pass their tests — user-story work can begin.

---

## Phase 3: User Story 1 - Index & Query (Priority: P1) 🎯 MVP

**Goal**: Set up, ingest a folder, and query it with grounded, source-cited answers.
**Independent Test**: Add one PDF, ask a question whose answer is in it, get a cited answer.

### Tests for User Story 1 (write FIRST, watch fail)

- [x] T010 [P] [US1] Write reader tests in `internal/reader/readers_test.go`: each reader extracts expected text from its `testdata/` fixture; unsupported extensions return "no reader"; Markdown frontmatter lands in metadata; PDF test asserts page markers + page_count metadata
- [x] T011 [P] [US1] Write chunk tests in `internal/chunk/chunk_test.go`: chunk count scales with input length; adjacent chunks overlap by the configured tokens; no chunk below the minimum; page numbers pass through for PDFs
- [x] T012 [P] [US1] Write embed tests in `internal/embed/ollama_test.go`: against a fake HTTP server returning a fixed vector, `Embed` returns one vector per input of the right dimension; retries on transient 5xx then succeeds; `Dimensions`/`Model` report correctly
- [x] T013 [P] [US1] Write FTS tests in `internal/index/fts_test.go`: indexing then searching a known term returns the right chunk ranked first; field weighting boosts title matches above body; case-folding and stopword removal behave; trigram fallback matches short terms
- [x] T014 [P] [US1] Write vector tests in `internal/index/vector_test.go`: `Add` then `Query` for a near-identical vector returns the added chunk first; persistence survives a close/reopen
- [x] T015 [P] [US1] Write pipeline tests in `internal/pipeline/pipeline_test.go`: ingesting the same file twice yields exactly one Document (idempotent via `0x0D`); a content change produces a new `ContentHash` and re-ingest; write ACK returns before embedding completes
- [x] T016 [P] [US1] Write retrieval tests in `internal/index/retrieval_test.go`: hybrid mode fuses vector + FTS via RRF and ranks a result present in both lists above one in only one; same-document hits collapse to top-1; `--mode` selects the right backend
- [x] T017 [P] [US1] Write CLI contract tests in `internal/cli/commands_test.go`: `init` creates `.go-rag/` + config; `add` reports NEW/SKIPPED/ERROR counts; `query` exits 0 and emits ranked results with source + page + score; `--format json` parses to the documented shape

### Implementation for User Story 1 (make the tests pass)

- [x] T018 [US1] Add `DefaultReaders()` to `internal/reader/reader.go` that registers the built-in readers (Register/Get already present)
- [x] T019 [P] [US1] Implement `TextReader` in `internal/reader/text.go` (`.txt`/`.log`/`.csv`)
- [x] T020 [P] [US1] Implement `MarkdownReader` in `internal/reader/markdown.go` (frontmatter + headings → metadata)
- [ ] T021 [P] [US1] Implement `PDFReader` in `internal/reader/pdf.go` using pdfcpu (per-page text, `--- PAGE N ---` markers, title/author/page_count metadata)
- [x] T022 [P] [US1] Implement `DocxReader` in `internal/reader/docx.go` (ZIP + XML body extract, doc-props metadata)
- [x] T023 [P] [US1] Implement image metadata readers in `internal/reader/image.go` (`.jpg`/`.png` — dimensions/EXIF only, no OCR; research Q1)
- [x] T024 [US1] Implement the chunk splitter in `internal/chunk/chunk.go`: paragraph→sentence→word cascade, ~512 tokens (≈1.3 tokens/word heuristic, research Q2), 50-token overlap, 50-token minimum, page-number passthrough
- [x] T025 [P] [US1] Implement the Ollama embedder in `internal/embed/ollama.go`: `Embed(ctx, texts)` via `POST /api/embed`, `Dimensions()`, `Model()`, retry/backoff
- [x] T026 [US1] Implement BM25 FTS in `internal/index/fts.go`: tokenize (case-fold, stopword removal, trigram fallback), field-weighted inverted index under prefixes `0x05`–`0x08` (title 3×, heading 2×, body 1×), `Index(chunk)` + `Search(query, k)`
- [x] T027 [P] [US1] Implement the vector store in `internal/index/vector.go`: chromem-go `Add(chunks, embeddings)` + `Query(vec, k)` with disk persistence (research Q4)
- [x] T028 [US1] Implement the ingest pipeline in `internal/pipeline/pipeline.go`: walk path → read → hash → dedup via `0x0D` → chunk → store Source/Document/Chunk (`Sync`, <10ms) → ACK → enqueue chunks for async workers
- [x] T029 [US1] Implement async indexing workers in `internal/pipeline/workers.go`: background embed (Ollama) + FTS index + vector index from the queue; update `Document.Status` `pending → embedded | error`
- [x] T030 [US1] Implement hybrid retrieval + RRF in `internal/index/retrieval.go`: embed query → parallel vector top-60 + FTS top-60 → Reciprocal Rank Fusion (`K_vec=40`, `K_fts=60`) → top-K, collapse same-document hits to top-1 by default (research Q8)
- [x] T031 [US1] Implement the `init` command in `internal/cli/init.go`: create `.go-rag/` (+ `config.json`, `data/`), probe Ollama for embedding-capable models, pick/prompt a model, open Pebble, persist config
- [x] T032 [US1] Implement the `add` command in `internal/cli/add.go`: walk path (respect `--recursive`/`--glob`/`--dry-run`), run the pipeline, print per-file `NEW`/`SKIPPED`/`ERROR` + summary + async-embedding notice
- [x] T033 [US1] Implement the `query` command in `internal/cli/query.go`: `--k`/`--mode`/`--format`/`--source`/`--threshold`, call retrieval, render text/JSON with chunk text + source path + page + score

**Checkpoint**: US1 complete and green — `quickstart.md` runs end-to-end. MVP shippable.

---

## Phase 4: User Story 2 - Status & Health (Priority: P2)

**Goal**: Inspect what's in the database and whether the embedding service is healthy.
**Independent Test**: After ingesting, run `status` and see correct counts + `OK`.

### Tests for User Story 2 (write FIRST)

- [ ] T034 [P] [US2] Write status tests in `internal/cli/status_test.go`: after a known ingest, counts (sources/docs/chunks/embedded %), model, and storage size are correct; with the embedding service down, health reports `degraded` (not a crash); `--json` parses to the documented shape

### Implementation for User Story 2

- [ ] T035 [US2] Implement the `status` command in `internal/cli/status.go`: counts, storage size, Pebble health, embedding model + dimensions + provider, last ingested/queried timestamps, health indicator (`OK`/`degraded` via Ollama ping); `--json`

---

## Phase 5: User Story 3 - Configuration (Priority: P2)

**Goal**: View and change settings with validation.
**Independent Test**: Change the embedding URL, restart, confirm it persists.

### Tests for User Story 3 (write FIRST)

- [ ] T036 [P] [US3] Write config-set tests in `internal/cli/config_test.go`: `config set` with a valid URL persists across reload; an invalid value (malformed URL, non-positive int) is rejected with a clear error and the previous value is retained; `config get` returns the current value

### Implementation for User Story 3

- [ ] T037 [US3] Implement `config` (view) in `internal/cli/config.go`: print all current values from `.go-rag/config.json`
- [ ] T038 [US3] Implement `config get [key]` and `config set [key] [value]` in `internal/cli/config.go`: validate via `config.Validate` — reject malformed URLs / non-positive ints, retain previous value on rejection

---

## Phase 6: User Story 4 - Auto-Watch (Priority: P3)

**Goal**: Keep the database in sync as files are added, changed, and deleted.
**Independent Test**: Start the watcher, add a file (indexed), delete a file (removed).

### Tests for User Story 4 (write FIRST)

- [ ] T039 [P] [US4] Write watcher tests in `internal/watcher/watcher_test.go`: a new file → `NEW` → indexed; an unchanged file → `SKIP`; a content change → `MODIFIED` with old chunks replaced; a deletion → `DELETED` with chunks/embeddings/index removed; rapid save bursts coalesce within the debounce window

### Implementation for User Story 4

- [ ] T040 [US4] Implement `ChangeDetector` in `internal/watcher/watcher.go`: fsnotify Layer 1 (500ms debounce/coalesce) + polling Layer 2 (`--poll-interval`), SHA-256 ground-truth comparison, state machine `UNKNOWN → NEW → TRACKED → MODIFIED | SKIP | DELETED` (data-model.md)
- [ ] T041 [US4] Wire the modify/delete cascade in `internal/watcher/watcher.go`: on `MODIFIED` delete old chunks/embeddings/index entries then re-ingest; on `DELETED` hard-delete all entries (research Q10)
- [ ] T042 [US4] Implement the `scan` command in `internal/cli/scan.go`: `--once` (scan + print `[ADDED]`/`[MODIFIED]`/`[DELETED]`) and `--watch` (long-lived, graceful drain on SIGINT/SIGTERM)

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T043 [P] Implement the MCP server in `internal/mcp/server.go`: stdio JSON-RPC, six tools (`go_rag_query/add/status/init/scan/config`) per `contracts/mcp-tools.md`; wire a `mcp` subcommand in `internal/cli/mcp.go` (Principle V / PRD G7)
- [ ] T044 [P] Add benchmark guards in `internal/pipeline/pipeline_test.go` (<10ms write ACK) and `internal/index/retrieval_test.go` (<500ms hybrid top-5 / <50ms keyword top-5)
- [ ] T045 [P] Update `README.md` and `docs/` with real commands, the quickstart flow, and the architecture summary
- [ ] T046 Final green build: `make build && make vet && make test && make lint && make vuln`; then validate `quickstart.md` end-to-end against `testdata/`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately. T001 (deps) first so later code compiles.
- **Foundational (Phase 2)**: depends on T001. Tests (T004–T006) before impl (T007–T009). **BLOCKS all stories.**
- **US1 (Phase 3)**: depends on Phase 2. Tests (T010–T017) precede impl (T018–T033). The MVP.
- **US2 / US3 (Phases 4–5)**: depend on Phase 2; can run in parallel with each other and overlap US1.
- **US4 (Phase 6)**: depends on Phase 2 **and US1's pipeline** (T028/T029) — re-ingest on modify reuses it.
- **Polish (Phase 7)**: depends on US1; MCP (T043) wraps the implemented operations.

### TDD Ordering Within a Phase

For each unit: write its test (RED) → run `go test ./...` and watch it fail → implement (GREEN) → run again and watch it pass. A test task and its implementation task are a pair; do not start the next pair until the current one is green.

### Parallel Opportunities

```bash
# US1 reader tests + impl (different files, no shared state) — all [P]:
go-rag: internal/reader/readers_test.go
go-rag: internal/reader/text.go
go-rag: internal/reader/markdown.go
go-rag: internal/reader/pdf.go
go-rag: internal/reader/docx.go

# US1 indexes (tests + impl, different files):
go-rag: internal/index/fts_test.go  + fts.go
go-rag: internal/index/vector_test.go + vector.go
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (deps + fixtures) → Phase 2 (storage, model, config — test-first)
2. Phase 3 US1 test-first: reader/chunk/embed/index tests, then implementations, then pipeline + workers + retrieval, then init/add/query
3. **STOP & VALIDATE**: `go test ./...` green AND `quickstart.md` runs end-to-end. If US1 passes, the product is shippable.

### Incremental Delivery

- After US1: ship. Then US2 (status) and US3 (config) — quick, independent.
- US4 (watch) last among stories (reuses US1's pipeline).
- Polish (MCP, benchmarks, docs) final.

---

## Notes

- Every task names a concrete Go file under the existing scaffold; the only new package is `internal/mcp` (T043, Principle V).
- TDD is enforced structurally: test task IDs always immediately precede their implementation task IDs within a phase.
- Constitution compliance is structural: pure-Go deps (T001), SHA-256 identity (T005/T008/T028), <10ms Sync ACK (T004/T007/T028), FileReader/Embedder interfaces (T018/T025), MCP exposure (T043).
