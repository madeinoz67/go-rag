# Tasks: go-rag Management Console — Documents View (Slice 1)

**Input**: Design documents from `/specs/047-ui-documents-view/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/ui-documents.md](./contracts/ui-documents.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED — go-rag is test-gated (`make test -race`, `make lint(0)`) and the constitution enforces "Spec/Test/Evals First". Every story ships a test task.

**Organization**: Tasks grouped by user story. Research decision tags (R1–R8) cross-link to [research.md](./research.md) for grounding.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish tasks carry none)
- Every task names its exact file path + the symbol/seam it touches

## Path conventions

New files: `internal/engine/list_chunks.go`, `internal/rest/list_chunks.go`, `internal/grpc/list_chunks.go`, `internal/ui/documents.go`. Edits: `proto/gorag.proto` (+regen `proto/gen`), `internal/engine/list_documents.go`, `internal/mcp/server.go`, `internal/cli/chunk.go`, `internal/engine/parity_test.go`, `internal/ui/{ui.go,ui_test.go,web/static/js/app.js,web/templates/index.html}`.

---

## Phase 1: Setup (Schema + UI skeleton)

**Purpose**: Land the protobuf contract surface for `ListChunks` + the `tags` filter, and the UI file skeleton, so everything downstream compiles.

- [X] T001 Add the `ListChunks` schema + `tags` filter to `proto/gorag.proto`: define `message ListChunksRequest { string document_id = 1; int32 page_size = 2; string page_token = 3; }`, `message ListChunksResponse { repeated Chunk chunks = 1; string next_page_token = 2; }`, and `rpc ListChunks(ListChunksRequest) returns (ListChunksResponse)` on the `Gorag` service; add `repeated string tags = 5;` to the existing `ListDocumentsRequest`. Regenerate `proto/gen/` via the spec-003 protoc invocation (`protoc --proto_path=proto --go_out=proto/gen --go-grpc_out=proto/gen --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative proto/gorag.proto` — confirm against spec 003). Build clean. (R1, R3)
- [X] T002 [P] Create `internal/ui/documents.go`: package comment + Slice-1 scope note; mirror DTO structs `documentDTO` and `chunkDTO` (field-for-field copies of `rest.documentMetaDTO` / `rest.chunkDTO` — see `internal/rest/get_chunk.go`) + projection helpers `toDocumentDTO(model.Document, model.Source)` and `toChunkDTO(model.Chunk)`; empty handler stubs so the package compiles before logic lands. (R8)

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; proto bindings carry `ListChunks`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the enabling accessor (`Engine.ListChunks`) + the `Tags` filter + the parity test.

**Status (US1 checkpoint):** T003–T005 (engine accessor + Tags filter + tests) ✅ done. T006–T010 (REST/gRPC/MCP/CLI `ListChunks` projections + cross-transport parity test) are **deferred to the US2 phase** — they consume `Engine.ListChunks`, which only the detail view (US2) needs; US1 reads `ListDocuments` only. Lint-clean at the checkpoint (no unused symbols).

- [X] T003 Implement `Engine.ListChunks` — `internal/engine/list_chunks.go`: `func (e *Engine) ListChunks(documentID string, req ListChunksRequest) (*ListChunksResult, error)` with `ListChunksRequest{PageSize, PageToken}` / `ListChunksResult{Chunks []model.Chunk, NextPageToken}`. One `PrefixScan` over chunk prefix (0x03) filtered by `document_id`, ordered `(chunk_index ASC, chunk_id ASC)`; opaque cursor pagination reusing the `list_documents.go` encode/decode-page-token pattern (token encodes the resume point; client re-sends page_size/page_token each page). page_size default 50 / max 200 (`defaultListPageSize`/`maxListPageSize`). Pure read, no new key. (R1, R7)
- [X] T004 `Engine.ListChunks` tests — `internal/engine/list_chunks_test.go`: pagination + cursor resume; ordering; empty document (empty `Chunks`, empty token — not an error); unknown document (empty result, not an error); invalid page_token / out-of-range page_size → `ErrInvalid`. Mirror `list_documents_test.go::TestListDocuments_{Pagination,CursorAndFilter,InvalidInput}`.
- [X] T005 Add `Tags []string` (match-any) filter to `ListDocumentsRequest` + apply it in the existing in-memory filter pass in `internal/engine/list_documents.go`; empty/nil = all documents (backward-compatible). Add a tag-filter case to `internal/engine/list_documents_test.go`. (R3)
- [ ] T006 [P] REST projection — `internal/rest/list_chunks.go`: `GET /v1/documents/{document_id}/chunks?page_size=&page_token=` → `{chunks: [documentMetaDTO-shape], next_page_token}` (reuse the `chunkDTO` projection); 200 always (empty = empty array), 400 invalid-arg, 404 unknown doc. Also bind repeatable `?tag=` in `internal/rest/list_documents.go::handleListDocuments` → `req.Tags`. (R1, R3)
- [ ] T007 [P] gRPC projection — `internal/grpc/list_chunks.go`: `func (a *Adapter) ListDocuments`-style `Adapter.ListChunks(ctx, *goragpb.ListChunksRequest) (*goragpb.ListChunksResponse, error)` delegating to `eng.ListChunks` (depends on T001 regen). (R1)
- [ ] T008 [P] MCP projection — `internal/mcp/server.go`: add `renderListChunks(eng, args)` + a `go_rag_list_chunks` tool registration; thread a `tags` arg through `renderListDocuments`. (R1, R3)
- [ ] T009 [P] CLI projection — `internal/cli/chunk.go`: add a `go-rag chunk list <document_id> [--page-size N] [--page-token T]` subcommand (JSON default + `--format text`), mirroring `newDocumentsListCmd`. (R1; constitution V: CLI op → also MCP, satisfied by T008)
- [ ] T010 Cross-transport parity test — `internal/engine/parity_test.go::TestCrossTransport_ListChunksParity` (pattern of `TestCrossTransport_ListDocumentsParity`): against one engine assert engine / REST / gRPC / MCP / CLI `ListChunks` return byte-identical chunks for a document; add a `Tags`-filter parity case to the ListDocuments parity test. (R1, R8, FR-013)

**Checkpoint**: `Engine.ListChunks` live engine + REST + gRPC + MCP + CLI; parity pinned; `make build && make vet` clean. UI may now call the engine in-process.

---

## Phase 3: User Story 1 — Browse the document corpus (Priority: P1) 🎯 MVP gate

**Goal**: An authenticated operator opens Documents and sees a paginated, sortable list of every document with status/enrichment/tags — counts matching `go-rag status`.

**Independent Test**: [quickstart.md](./quickstart.md) §3 — list row total (across pages) == `go-rag status` document count; each row's `chunk_count` matches; `status`/`tag` filters narrow; no Bearer → 401; empty corpus → empty array.

### Implementation

- [X] T011 [US1] Implement `handleDocumentsList` — `internal/ui/documents.go`: parse `page_size`/`page_token`/`after`/`status`/`tag` (repeatable) query params → `s.eng.ListDocuments(...)` → JSON `{documents: [documentDTO], next_page_token}`; register `GET /api/documents` (guarded, `s.guard`) in `internal/ui/ui.go::Handler`. 200 always (empty = `[]`), 400 invalid-arg. List rows omit `source_path` (listing skips source resolution). (R4, R3, contracts/ui-transport.md → ui-documents.md)
- [X] T012 [US1] Alpine Documents list view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: on Documents view-entry fetch `/api/documents`; render rows (file_name/file_path, file_size, chunk_count, `status` badge: embedded/pending/error, tags, summary snippet, ingested_at); pagination via `next_page_token` (Next/Prev); page-local sort by name/size/chunks/date (R7 — corpus-wide non-date sort deferred); sidebar "Documents" active state (replaces the placeholder). No full-page reload on nav.
- [X] T013 [US1] US1 tests — `internal/ui/ui_test.go`: (a) `GET /api/documents` 200 + total matches engine document count; (b) pagination cursor returns all docs once; (c) `?status=pending` and `?tag=X` filter; (d) 401 without Bearer (initialized vault); (e) 400 on bad `page_size`/`after`/`status`/`page_token`; (f) empty corpus → `{"documents":[]}`. (FR-013)

**Checkpoint**: US1 independently testable — the browsable corpus (MVP).

---

## Phase 4: User Story 2 — Inspect a document's contents and state (Priority: P1)

**Goal**: Click a document → detail header (metadata + summary/tags) + paginated chunks (text + section context) + chunk neighbour window.

**Independent Test**: [quickstart.md](./quickstart.md) §4 — chunk total == document `chunk_count` == `go-rag status`; section context renders; un-enriched doc shows empty state.

### Implementation

- [ ] T014 [US2] Implement `handleDocumentDetail` — `internal/ui/documents.go`: resolve the document by id and its `Source` (so `source_path` is populated — unlike the list row); return `documentDTO`; register `GET /api/documents/{id}` (guarded). 404 unknown id, 400 empty id. (R8, contracts)
- [ ] T015 [US2] Implement `handleDocumentChunks` — `internal/ui/documents.go`: `s.eng.ListChunks(id, ListChunksRequest{PageSize, PageToken})` → `{chunks: [chunkDTO], next_page_token}`; register `GET /api/documents/{id}/chunks` (guarded). 404 unknown doc, 400 invalid paging. (R1, contracts)
- [ ] T016 [US2] Implement `handleChunkContext` — `internal/ui/documents.go`: `s.eng.GetChunkContext(chunkID, window)` (window 0–10, default 2) → `{chunks: [chunkDTO], target_index, document: documentDTO}`; register `GET /api/documents/{id}/chunks/{chunkID}/context` (guarded). 404 unknown chunk, 400 window out of range. (contracts; mirrors REST `GET /v1/chunks/{id}/context`)
- [ ] T017 [US2] Alpine detail view — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: click a list row → detail pane (full metadata, `source_path`, summary + tags or a clear empty state when un-enriched); chunk list paginated with `section_context`/`section_depth` breadcrumbs; select a chunk → fetch its context window. No page reload.
- [ ] T018 [US2] US2 tests — `internal/ui/ui_test.go`: (a) detail 200 + `source_path` resolved + 404 unknown + 400 empty; (b) chunks total (across pages) == `Document.ChunkCount`; (c) context window shape + `target_index`; (d) un-enriched doc → empty summary state, not error; (e) 401/400/404 matrix. (FR-013, SC-004)

**Checkpoint**: US2 independently testable — inspectable documents.

---

## Phase 5: User Story 3 — Find documents by name, tag, or status (Priority: P2)

**Goal**: Content search (name/path + chunk content) and tag/status filters narrow the list; clear restores it.

**Independent Test**: [quickstart.md](./quickstart.md) §5 — a content term returns only matching documents; absent term → empty array; combined filters intersect; clear → full list.

### Implementation

- [ ] T019 [US3] Implement `handleDocumentsSearch` — `internal/ui/documents.go`: `s.eng.Query(ctx, QueryRequest{Query: q, ...})` → project `QueryHit[]` to **distinct parent documents** (dedup on document_id, preserve retrieval rank) → `{query, documents: [documentDTO]}`; `limit` 1–100 (default 20); register `GET /api/documents/search?q=&limit=` (guarded). 400 on empty/missing `q`. (R2, contracts)
- [ ] T020 [US3] Alpine search + filter UI — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: a search box (submits to `/api/documents/search`; folds name/path matching client-side over the ranked result); tag + status filter controls applied to `/api/documents`; combined filters intersect; "clear" restores the full list. Reuses the list-row rendering from US1.
- [ ] T021 [US3] US3 tests — `internal/ui/ui_test.go`: (a) content search returns only documents whose chunks match; (b) absent term → `{"documents":[]}`; (c) `?tag=`/`?status=` filters narrow; (d) search + filter combine by intersection; (e) 400 empty `q`; (f) 401 without Bearer. (R2, R3)

**Checkpoint**: US3 independently testable — a findable corpus.

---

## Phase 6: User Story 4 — Read-only and shell-consistent (Priority: P2)

**Goal**: Prove the slice introduces no writes, no Node chain, degrades gracefully on edge corpora, and holds cross-transport parity.

**Independent Test**: [quickstart.md](./quickstart.md) §7 — no Node artifacts; every `/api/documents*` route is `GET`; UI `documentDTO` byte-identical to REST/MCP.

### Implementation / Verification

- [ ] T022 [US4] No-Node + read-only assertion tests — `internal/ui/ui_test.go`: repo-root scan finds no `package.json`/`node_modules`/`vite.config.*`/`tailwind.config.*`; enumerate the UI mux and assert every `/api/documents*` route is `GET` (no write verb registered); `/static/*` assets serve 200 from the embed FS. (FR-009, FR-011, SC-005, SC-006)
- [ ] T023 [US4] Cross-transport parity test — `internal/ui/ui_test.go`: against one engine, assert the UI `documentDTO` from `GET /api/documents` is byte-identical to REST `GET /v1/documents` and MCP `go_rag_list_documents`; assert the UI `chunkDTO` from `/api/documents/{id}/chunks` matches REST. (R8, FR-013, SC-004)
- [ ] T024 [US4] Empty/edge-state rendering — `internal/ui/web/static/js/app.js` + `internal/ui/web/templates/index.html`: empty corpus (healthy empty state, not error), zero-chunk document, `status==error` (failed-embed badge), drifted embedding, un-enriched summary — each renders a deliberate state, never a crash. (FR-012)

**Checkpoint**: US4 independently testable — the invariants are pinned.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T025 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` clean across `internal/engine`, `internal/rest`, `internal/grpc`, `internal/mcp`, `internal/cli`, `internal/ui`.
- [ ] T026 [P] quickstart curl smoke DONE on an isolated DB (list/detail/chunks/context/search + 401 regimes + no `Set-Cookie` + no-Node); Interceptor browser verify of browse/inspect/search render = remaining manual step.
- [ ] T027 [P] Doc sync — update spec 046's Slice Decomposition row (047 status); update `PROJECTS.md` go-rag entry + MuninnDB memory to reflect Slice 1 tasked.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps — start immediately. T001 (proto) blocks T007 (gRPC); T002 (UI skeleton) blocks US handlers.
- **Foundational (Phase 2)**: depends on Setup; **blocks US1 + US2** (they need `Engine.ListChunks` + the `Tags` filter + the mirror DTOs). T003/T004/T005 are the blocking core; T006–T010 (transports + parity) are [P] and may finish alongside US work but must precede Polish.
- **US1 (Phase 3)**: depends on Foundational.
- **US2 (Phase 4)**: depends on Foundational + US1 (detail opens from a list row; reuses list-row rendering).
- **US3 (Phase 5)**: depends on Foundational + US1 (search/filter reuse the list view).
- **US4 (Phase 6)**: depends on US1–US3 (verifies them).
- **Polish (Phase 7)**: depends on all stories complete.

### User-story independence
- US1 is the MVP gate — testable alone once Foundational lands.
- US2/US3 bind to distinct handlers/files but reuse US1's list-row rendering.
- US4 is cross-cutting verification of US1–US3.

### Parallel opportunities
- Phase 1: T002 parallel with T001.
- Phase 2: T006–T009 (the four transport projections) are all `[P]` — fully parallel once T003 lands; T010 (parity) runs after them.
- Story test tasks can run alongside their implementation tasks (different files).

---

## Parallel Example: Phase 2 (transport projections)

```bash
Task: "REST projection  in internal/rest/list_chunks.go (+ ?tag bind)"   # T006
Task: "gRPC projection  in internal/grpc/list_chunks.go"                 # T007
Task: "MCP projection   in internal/mcp/server.go"                        # T008
Task: "CLI projection   in internal/cli/chunk.go (chunk list)"           # T009
```

---

## Implementation Strategy

### MVP First
1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — `Engine.ListChunks` live across all transports.
2. Complete Phase 3 (US1 — browse). **STOP and VALIDATE**: list counts match `go-rag status`, filters work (quickstart §3). This is the **MVP gate** — a browsable corpus.
3. Complete Phase 4 (US2) — the **demo-complete** point: browse → inspect a document's chunks. The euphoric-surprise moment.
4. Phase 5 (US3) + Phase 6 (US4) + Phase 7 (Polish) add find + harden + verify.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main` (CLAUDE.md). Commit after each task or logical group; run `make lint && make test -race` before push.

---

## Notes

- All document/chunk data funnels through `internal/engine` — the UI makes no independent data decision (R4). Cross-transport parity is the proof (FR-013).
- `Engine.ListChunks` is the **one** new engine capability; it ships across all transports (constitution V; spec 035/037/038/039 precedent). No new storage, no migration.
- Vendoring (not building) remains the constraint: no Node/Vite/Tailwind, single binary (R8, FR-011).
- Sort is page-local for Slice 1; corpus-wide non-date sort + the "source changed" staleness indicator are deferred (R6, R7) — tracked in research.md, not blockers.
