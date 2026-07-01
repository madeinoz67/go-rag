---
description: "Task list for spec 039 — ListDocuments (BL-007)"
---

# Tasks: ListDocuments (BL-007)

**Input**: Design documents from `/specs/039-list-documents-rpc/` — `spec.md` (3 user stories, FR-001..FR-016), `plan.md`, `research.md` (R1–R4), `data-model.md`, `contracts/api.md`, `quickstart.md`.

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present and consistent.

**Tests**: INCLUDED — the go-rag constitution mandates `go build`/`go vet`/`go test` green on every change ("the repository is never left red"), and the design names the test surfaces (`list_documents_test.go`, `parity_test.go`). Interleaved per story.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = MVP — after-cursor + status filter; US2 P2 = pagination composing with the cursor+filter; US3 P3 = four-transport parity). Foundational phase holds the wire contract (proto) every gRPC/CLI/MCP transport depends on.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new project structure or deps (pure-read listing, Constitution III).

- [ ] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the wire contract (proto) that the gRPC transport (US3) depends on. Defined early so all transports share one surface. **No story work begins until this phase is green.**

- [ ] T002 [P] Add the gRPC contract in `proto/gorag.proto`: `rpc ListDocuments(ListDocumentsRequest) returns (ListDocumentsResponse);` to the `Gorag` service (after `BatchGetChunks`, before the closing `}`), and the two messages — `ListDocumentsRequest { int32 page_size = 1; string page_token = 2; string after = 3; string status = 4; }` (no vault) and `ListDocumentsResponse { repeated DocumentMeta documents = 1; string next_page_token = 2; }` (reuse the spec-035 `DocumentMeta`). [FR-001, FR-008, FR-016; contracts/api.md]
- [ ] T003 Regenerate `proto/gen/gorag.pb.go` + `proto/gen/gorag_grpc.pb.go` from the updated `proto/gorag.proto` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). (depends T002)

**Checkpoint**: the wire contract exists and compiles. User-story work can begin.

---

## Phase 3: User Story 1 — List documents ingested since a cursor, filtered by status (Priority: P1) 🎯 MVP

**Goal**: a client calls `ListDocuments(after, status)` and receives only the matching documents in ascending `ingested_at` order, over REST — the bridge's incremental-listing primitive.

**Independent Test**: ingest 5 docs, advance time, ingest 3 more; `ListDocuments(after=<midpoint>)` → exactly the 3 later docs in ascending `ingested_at` order; `status=embedded` → only the embedded subset. (`quickstart.md` Scenarios 1–3.)

### Implementation for User Story 1

- [ ] T004 [US1] Implement `Engine.ListDocuments(req ListDocumentsRequest) (*ListDocumentsResult, error)` + the `ListDocumentsRequest{PageSize, PageToken, After, Status}` / `ListDocumentsResult{Documents []model.Document; NextPageToken string}` types + the page-token codec (`encodePageToken(t, id) string` / `decodePageToken(s) (time.Time, string, error)`) in `internal/engine/` (new `list_documents.go`). Reuse `ErrInvalid`. Logic per `data-model.md`: validate `page_size` (∈[1,200], default 50), `after` (RFC3339), `status` (∈{embedded,pending,error,""}), `page_token` (decode; malformed → ErrInvalid); `storage.PrefixScan(PrefixDocument)` → decode each `model.Document`; filter (`after`: `IngestedAt > T`, AND `status` exact-match); sort `(ingested_at ASC, id ASC)`; skip-to-resume (from `page_token`), take `page_size`, emit `next_page_token` iff more remain. Page-token = base64-url-no-pad of `<RFC3339Nano> \x1f <id>` (research.md R1). [FR-001..FR-004, FR-008, FR-013, FR-014..FR-016]
- [ ] T005 [P] [US1] Add the REST endpoint `GET /v1/documents?page_size=&page_token=&after=&status=` in `internal/rest/` (new `list_documents.go`; register the route in `server.go` `routes` table + `handlerFor` switch beside `/v1/files` + `/v1/dirs`; add the path to `openapi.yaml` — the openapi parity test asserts the two match). Parse query params; validate (default page_size 50; `>200`/`<1`/non-RFC3339 `after`/unknown `status`/malformed `page_token` → 400); return `200` `{ documents: [documentMetaDTO…], next_page_token }` reusing `toDocumentMetaDTO`. Empty result → `200` with empty array (never 404, never an error). [FR-011, FR-012] (depends T004)

### Tests for User Story 1

- [ ] T006 [US1] Happy-path + filter tests in `internal/engine/list_documents_test.go`: ingest several docs at distinct times (mirror `newCacheEngine`/`addDoc`), then assert (a) `after=<T>` returns exactly the docs with `ingested_at > T` in ascending order; (b) `status="embedded"` returns only embedded docs (wait for embeddings via `waitEmbedded`); (c) `after`+`status` AND; (d) every returned doc carries full metadata + a non-empty `ingested_at`; (e) empty result (after far future) → empty slice, no error. [US1 acceptance #1..4; FR-003, FR-004, FR-009] (depends T004)

**Checkpoint**: US1 MVP delivers — `ListDocuments` filters by cursor + status over REST. `make test` green.

---

## Phase 4: User Story 2 — Pagination composing with the cursor and filter (Priority: P2)

**Goal**: a listing over a large vault is bounded + resumable, and pagination composes with `after` + `status`.

**Independent Test**: ingest > page_size matching docs; page through with the same `after`+`status`; the concatenation of pages = the full filtered set, in order, no dupes/gaps, empty `next_page_token` on the last page. (`quickstart.md` Scenario 4.)

### Tests for User Story 2

- [ ] T007 [US2] Pagination tests in `internal/engine/list_documents_test.go`: (a) page through a corpus > `page_size` by echoing `next_page_token` → every matching doc exactly once, in order, final `next_page_token` empty; (b) pagination composes with `after` + `status` (every page honours the filter); (c) `page_size` boundary (1, 200, 201→ErrInvalid, 0→default); (d) tie-break: two docs with identical `ingested_at` are ordered by id (deterministic across pages); (e) a malformed `page_token` → ErrInvalid; (f) the codec round-trips (`encodePageToken`/`decodePageToken`). [FR-005, FR-006, FR-007; US2 acceptance #1..4] (depends T004)

**Checkpoint**: pagination is correct + composes with the filters.

---

## Phase 5: User Story 3 — Every transport, identical results (Priority: P3)

**Goal**: `ListDocuments` is byte-identical across gRPC, REST, MCP, and CLI.

**Independent Test**: same `(page_size, page_token, after, status)` over all four transports → identical document-id lists (ordered), same `next_page_token`, same per-document metadata. (`quickstart.md` Scenario 6.)

### Implementation + Tests for User Story 3

- [ ] T008 [US3] Add the gRPC handler + response projection in `internal/grpc/list_documents.go`: implement `ListDocuments(ctx, *ListDocumentsRequest) (*ListDocumentsResponse, error)` — validate (map to `ErrInvalid` → `INVALID_ARGUMENT` via `toStatusErr`), call `engine.ListDocuments`, project `[]DocumentMeta` (reuse `toDocumentMetaPB`) + `next_page_token`. INVALID_ARGUMENT for bad input; empty result is NOT an error. [FR-011] (depends T003, T004)
- [ ] T009 [P] [US3] Add the CLI command `go-rag documents list` in `internal/cli/` (new `documents.go`: a `documents` parent command + a `list` subcommand, registered beside `files`/`dirs`). Flags: `--page-size` (default 50), `--page-token`, `--after`, `--status`, `--format json|text`; reject `--page-size > 200` with non-zero exit. JSON envelope `{ documents: […], next_page_token }` matches the proto/REST shape. [FR-011] (depends T004)
- [ ] T010 [P] [US3] Add the MCP tool `go_rag_list_documents` in `internal/mcp/server.go` (beside the file/dir tools): wire `dispatchDB` case + `toolDefs()` entry (inputSchema `{ page_size?, page_token?, after?, status? }`); `renderListDocuments` renders one line per document (file path, status, ingested_at) + the next_page_token line. **Bump the tool-count assertions**: `TestMCP_ToolsListCount` + `TestHTTPToolsList` 22 → 23 and add `go_rag_list_documents` to the want-list. [FR-011] (depends T004)
- [ ] T011 [US3] Extend `internal/engine/parity_test.go` with `TestCrossTransport_ListDocumentsParity`: assert `ListDocuments` returns identical ordered document-id lists + the same `next_page_token` + the same per-document metadata across CLI, REST, gRPC, and MCP for the same `(page_size, after, status)`; cover a multi-page corpus (page through all four in lockstep). [FR-011; SC-001] (depends T005, T008, T009, T010)

**Checkpoint**: all four transports return identical listings.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T012 [P] Run `make lint` (golangci-lint — the `ci.yml` gate) and resolve every finding; run `quickstart.md` validation end-to-end on an isolated DB (Scenarios 1–7, non-default `--db-path`/ports per project CLAUDE.md); affirm constitution compliance in the commit (pure read, no on-disk layout change, no migration, `migrate.ExpectedVersion` unchanged, pure Go). Mark BL-007 resolved in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` (mirror BL-002/003/004's resolved note — note the new-operation-vs-Files delta + the no-vault-field + no-migration deltas).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002–T003)**: T003 depends on T002. **Blocks US3's gRPC path.** (US1/US2 use only the engine method + REST, not the proto.)
- **US1 (T004–T006)**: T004 (engine + codec) depends on nothing (reuses `PrefixScan`/`PrefixDocument`); T005 (REST) depends on T004; T006 (test) depends on T004.
- **US2 (T007)**: depends on T004 (the codec + pagination logic).
- **US3 (T008–T011)**: T008 (gRPC) depends on T003 + T004; T009 (CLI) + T010 (MCP) depend on T004 (parallel with T008); T011 (parity) depends on T005 + T008 + T009 + T010.
- **Polish (T012)**: after all stories.

### Story completion order (single-author, sequential)
Foundational → US1 (MVP, stop+validate) → US2 → US3 → Polish.

### Parallel opportunities (within phase, different files)
- Foundational: T002 (`gorag.proto`) is the only file.
- US1: T005 (`rest`) parallel with T006 (test) once T004 lands.
- US3: T009 (`cli`), T010 (`mcp`) parallel with T008 (`grpc`); T011 (parity test) after all three.

---

## Parallel Example: US1 + US3

```text
# US1 (once T004 engine + codec lands):
Task: "T005 REST GET /v1/documents (rest/list_documents.go + server.go + openapi.yaml)"
Task: "T006 engine filter/order tests (list_documents_test.go)"

# US3 (parallel — different files):
Task: "T008 gRPC handler (grpc/list_documents.go)"        # after T003 regen
Task: "T009 CLI `documents list` (cli/documents.go)"
Task: "T010 MCP go_rag_list_documents (mcp/server.go)"
# then T011 parity test once T005+T008+T009+T010 land.
```

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T003 (proto contract).
3. US1 T004–T006 (engine method + page-token codec + REST + filter tests).
4. **STOP & VALIDATE**: `quickstart.md` Scenarios 1–3 on an isolated DB. Demo-able: an incremental listing over REST.

### Incremental delivery
- + US2 (T007): pagination proven correct + composes with cursor/filter.
- + US3 (T008–T011): gRPC + CLI + MCP + cross-transport parity.
- Polish (T012): lint, quickstart, BL-007 resolved note, constitution affirmation.

### Commit cadence
Conventional Commits to `main` after each checkpoint (`feat(spec039): ...`); `make build && vet && test && lint` green before every push.

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001/FR-002 list + (ingested_at,id) order | T004, T006, T011 |
| FR-003 after cursor / FR-004 status filter (AND) | T004, T005, T006 |
| FR-005 page_size default/cap / FR-006 page_token | T004, T005, T007 |
| FR-007 pagination composes with after+status | T004, T007, T011 |
| FR-008 full DocumentMeta per doc | T004, T006, T011 |
| FR-009/FR-010 ingested_at reliability (affirm) | T006 (affirm in test) + T012 (constitution affirmation) |
| FR-011/FR-012 all 4 transports + REST route | T005, T008, T009, T010, T011 |
| FR-013 single logical read (PrefixScan) | T004 |
| FR-014 no migration / FR-015 pure Go | T012 (affirm) |
| FR-016 no vault field | T002, T004 |

Every spec acceptance scenario (US1 #1–4, US2 #1–4, US3 #1–2) is covered by T006, T007, T011 respectively.

## Notes
- All tasks carry `[ID]`, a file path, and (`[P]`/`[USx]`) markers per the format rules.
- Pure-read feature — Constitution Principles I–V + storage discipline hold throughout; no migration (R2 verified `ingested_at` reliable), no new deps.
- The engine method + page-token codec (T004) is the one substantive logic task; everything else is mechanical projection + tests, cloning the shipped GetChunk family (spec 035/037/038). Pagination (T007) is the one new-behaviour test surface.
- `Document.Status` values are exactly `pending|embedded|error` (verified in research.md R2) — the `status` filter maps 1:1; use the `StatusPending`/`StatusEmbedded` constants where natural.
