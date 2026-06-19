# Tasks: Local RAG Database (go-rag v1)

**Input**: Design documents from `specs/001-local-rag-database/` (plan.md, spec.md,
research.md, data-model.md, contracts/, quickstart.md)

**Prerequisites**: plan.md (required), spec.md (required)

**Tests**: Not requested in spec.md — test tasks are omitted. Add benchmarks/guards
in the Polish phase only.

**Organization**: Tasks grouped by user story (US1 = MVP, US2 status, US3 config,
US4 watch) so each story is independently implementable and testable.

> The go-rag scaffold already exists (cmd/internal tree + interface stubs, from
> project setup). These tasks implement the bodies behind the stubs. All paths are
> project-relative Go paths under `cmd/` and `internal/`.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: which user story (US1–US4); setup/foundational/polish have no label
- Exact Go file path on every task

## Path Conventions

- Go module `github.com/madeinoz67/go-rag`, layout `cmd/` + `internal/`
- Single entrypoint: `cmd/go-rag/main.go`; private code under `internal/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Add the runtime dependencies and fixtures the scaffold is missing.

- [ ] T001 [P] Add runtime deps: run `go get github.com/cockroachdb/pebble github.com/philippgille/chromem-go github.com/pdfcpu/pdfcpu github.com/fsnotify/fsnotify` then `go mod tidy` (updates `go.mod`/`go.sum`)
- [ ] T002 [P] Create test fixtures in `testdata/`: `sample.txt`, `sample.md` (with frontmatter), `sample.docx`, and a small `sample.pdf` (used by US1 and `quickstart.md`)
- [ ] T003 [P] Add a "Watching large trees" note to `README.md` documenting the Linux `fs.inotify.max_user_watches` tuning (research Q11)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure every user story depends on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [ ] T004 Implement the Pebble store in `internal/storage/db.go`: `Open(path)` with single-writer file lock, `Close`, and prefix-keyed `Get/Set/Delete/PrefixScan` using the `0x01`–`0x0F` constants in `internal/storage/storage.go`; every write uses `Sync` for durable <10ms ACK
- [ ] T005 [P] Flesh out `internal/model/model.go`: add JSON tags to the four structs, add `Document.GenerateID()` (SHA-256 over content + canonicalized metadata) and `ContentHash(raw []byte)` helper (distinct from ID — Principle II)
- [ ] T006 [P] Implement `internal/config/config.go`: `Default()`, `Load(path)`, `Save(path)`, `Validate()` (URL format, positive integers), `Get(key)`/`Set(key, val)`

**Checkpoint**: storage + model + config ready — user-story work can begin.

---

## Phase 3: User Story 1 - Index & Query (Priority: P1) 🎯 MVP

**Goal**: Set up, ingest a folder, and query it with grounded, source-cited answers.
**Independent Test**: Add one PDF, ask a question whose answer is in it, get a cited answer.

### Implementation for User Story 1

- [ ] T007 [US1] Confirm `internal/reader/reader.go` registry wiring (Register/Get already present); add a `DefaultReaders()` constructor that registers the built-in readers
- [ ] T008 [P] [US1] Implement `TextReader` in `internal/reader/text.go` (`.txt`/`.log`/`.csv`)
- [ ] T009 [P] [US1] Implement `MarkdownReader` in `internal/reader/markdown.go` (frontmatter + headings → metadata)
- [ ] T010 [P] [US1] Implement `PDFReader` in `internal/reader/pdf.go` using pdfcpu (per-page text, `--- PAGE N ---` markers, title/author/page_count metadata)
- [ ] T011 [P] [US1] Implement `DocxReader` in `internal/reader/docx.go` (ZIP + XML body extract, doc-props metadata)
- [ ] T012 [P] [US1] Implement image metadata readers in `internal/reader/image.go` (`.jpg`/`.png` — dimensions/EXIF only, no OCR; research Q1)
- [ ] T013 [US1] Implement the chunk splitter in `internal/chunk/chunk.go`: paragraph→sentence→word cascade, ~512 tokens (≈1.3 tokens/word heuristic, research Q2), 50-token overlap, 50-token minimum, page-number passthrough
- [ ] T014 [P] [US1] Implement the Ollama embedder in `internal/embed/ollama.go`: `Embed(ctx, texts)` via `POST /api/embed`, `Dimensions()`, `Model()`, with retry/backoff
- [ ] T015 [US1] Implement BM25 FTS in `internal/index/fts.go`: tokenize (case-fold, stopword removal, trigram fallback for short terms), field-weighted inverted index under prefixes `0x05`–`0x08` (title 3×, heading 2×, body 1×), `Index(chunk)` + `Search(query, k)`
- [ ] T016 [P] [US1] Implement the vector store in `internal/index/vector.go`: chromem-go `Add(chunks, embeddings)` + `Query(vec, k)` with disk persistence (research Q4)
- [ ] T017 [US1] Implement the ingest pipeline in `internal/pipeline/pipeline.go`: walk path → read → hash → dedup via `0x0D` → chunk → store Source/Document/Chunk (`Sync`, <10ms) → ACK → enqueue chunks for async workers
- [ ] T018 [US1] Implement async indexing workers in `internal/pipeline/workers.go`: background embed (Ollama) + FTS index + vector index from the queue; update `Document.Status` `pending → embedded | error`
- [ ] T019 [US1] Implement hybrid retrieval + RRF in `internal/index/retrieval.go`: embed query → parallel vector top-60 + FTS top-60 → Reciprocal Rank Fusion (`K_vec=40`, `K_fts=60`) → top-K, collapse same-document hits to top-1 by default (research Q8)
- [ ] T020 [US1] Implement the `init` command in `internal/cli/init.go`: create `.go-rag/` (+ `config.json`, `data/`), probe Ollama for embedding-capable models, pick/prompt a model, open Pebble, persist config
- [ ] T021 [US1] Implement the `add` command in `internal/cli/add.go`: walk path (respect `--recursive`/`--glob`/`--dry-run`), run the pipeline, print per-file `NEW`/`SKIPPED`/`ERROR` + summary + async-embedding notice
- [ ] T022 [US1] Implement the `query` command in `internal/cli/query.go`: `--k`/`--mode`/`--format`/`--source`/`--threshold`, call retrieval, render text/JSON with chunk text + source path + page + score

**Checkpoint**: US1 complete — `quickstart.md` runs end-to-end (build → init → add → query → status-lite). MVP shippable.

---

## Phase 4: User Story 2 - Status & Health (Priority: P2)

**Goal**: Inspect what's in the database and whether the embedding service is healthy.
**Independent Test**: After ingesting, run `status` and see correct counts + `OK`.

- [ ] T023 [US2] Implement the `status` command in `internal/cli/status.go`: counts (sources/files/chunks/embedded %), storage size, Pebble health, embedding model + dimensions + provider, last ingested/queried timestamps, health indicator (`OK`/`degraded` via Ollama ping); support `--json`

---

## Phase 5: User Story 3 - Configuration (Priority: P2)

**Goal**: View and change settings with validation.
**Independent Test**: Change the embedding URL, restart, confirm it persists.

- [ ] T024 [US3] Implement `config` (view) in `internal/cli/config.go`: print all current values from `.go-rag/config.json`
- [ ] T025 [US3] Implement `config get [key]` and `config set [key] [value]` in `internal/cli/config.go`: validate via `config.Validate` — reject malformed URLs / non-positive ints with a clear error and retain the previous value

---

## Phase 6: User Story 4 - Auto-Watch (Priority: P3)

**Goal**: Keep the database in sync as files are added, changed, and deleted.
**Independent Test**: Start the watcher, add a file (indexed), delete a file (removed).

- [ ] T026 [US4] Implement `ChangeDetector` in `internal/watcher/watcher.go`: fsnotify Layer 1 (500ms debounce/coalesce) + polling Layer 2 (`--poll-interval`), SHA-256 ground-truth comparison, state machine `UNKNOWN → NEW → TRACKED → MODIFIED | SKIP | DELETED` (data-model.md)
- [ ] T027 [US4] Wire the modify/delete cascade in `internal/watcher/watcher.go`: on `MODIFIED` delete old chunks/embeddings/index entries then re-ingest; on `DELETED` hard-delete all entries (research Q10)
- [ ] T028 [US4] Implement the `scan` command in `internal/cli/scan.go`: `--once` (scan + print `[ADDED]`/`[MODIFIED]`/`[DELETED]`) and `--watch` (long-lived, graceful drain on SIGINT/SIGTERM)

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T029 [P] Implement the MCP server in `internal/mcp/server.go`: stdio JSON-RPC, six tools (`go_rag_query/add/status/init/scan/config`) per `contracts/mcp-tools.md`; wire a `mcp` subcommand in `internal/cli/mcp.go` (Principle V / PRD G7)
- [ ] T030 [P] Add benchmark guards in `internal/pipeline/pipeline_test.go` (<10ms write ACK) and `internal/index/retrieval_test.go` (<500ms hybrid top-5 / <50ms keyword top-5)
- [ ] T031 [P] Update `README.md` and `docs/` with real commands, the quickstart flow, and the architecture summary
- [ ] T032 Final green build: `make build && make vet && make test && make lint && make vuln`; then validate `quickstart.md` end-to-end against `testdata/`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately. T001 (deps) should land first so later Go files compile.
- **Foundational (Phase 2)**: depends on T001. **BLOCKS all user stories.**
- **US1 (Phase 3)**: depends on Phase 2. The MVP — everything else is incremental.
- **US2 / US3 (Phases 4–5)**: depend on Phase 2; can run in parallel with each other and overlap US1.
- **US4 (Phase 6)**: depends on Phase 2 **and US1's pipeline** (T017/T018) — re-ingest on modify reuses the pipeline.
- **Polish (Phase 7)**: depends on US1; MCP (T029) wraps the implemented operations.

### Within US1

- Readers (T008–T012) and embedder (T014) and indexes (T015–T016) are independent of each other → parallel.
- Pipeline (T017) depends on readers + chunk + storage; workers (T018) depend on embed + indexes.
- Retrieval (T019) depends on both indexes.
- CLI commands (T020–T022) depend on pipeline + retrieval.

### Parallel Opportunities

```bash
# US1 readers (different files, no shared state):
go-rag: internal/reader/text.go
go-rag: internal/reader/markdown.go
go-rag: internal/reader/pdf.go
go-rag: internal/reader/docx.go
go-rag: internal/reader/image.go

# US1 indexes (different files):
go-rag: internal/index/fts.go
go-rag: internal/index/vector.go
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (deps + fixtures) → Phase 2 (storage, model, config)
2. Phase 3 US1 in order: readers → chunk → embed → indexes → pipeline + workers → retrieval → init/add/query
3. **STOP & VALIDATE**: run `quickstart.md` end-to-end. If US1 passes, the product is shippable.

### Incremental Delivery

- After US1: ship. Then add US2 (status) and US3 (config) — both quick, independent.
- Add US4 (watch) last among stories (reuses US1's pipeline).
- Polish (MCP, benchmarks, docs) final.

---

## Notes

- Every task names a concrete Go file under the existing scaffold — no new top-level packages except `internal/mcp` (T029, required by Principle V).
- Constitution compliance is structural: pure-Go deps (T001), SHA-256 identity (T005/T017), <10ms Sync ACK (T004/T017), FileReader/Embedder interfaces (T007/T014), MCP exposure (T029).
- Tests are optional and omitted; T030 adds only performance-budget benchmarks. Full unit/integration tests can be generated separately if TDD is requested.
