---
description: "Task list for spec 038 — BatchGetChunks (BL-003)"
---

# Tasks: BatchGetChunks (BL-003)

**Input**: Design documents from `/specs/038-batch-get-chunks-rpc/` — `spec.md` (3 user stories, FR-001..FR-015), `plan.md`, `research.md` (R1–R4), `data-model.md`, `contracts/api.md`, `quickstart.md`.

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present and consistent.

**Tests**: INCLUDED — the go-rag constitution mandates `go build`/`go vet`/`go test` green on every change ("the repository is never left red"), and the design names the test surfaces (`batch_get_chunks_test.go`, `parity_test.go`). Interleaved per story.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = MVP — batch resolve with per-id error; US2 P2 = validation/edges; US3 P3 = four-transport parity). Foundational phase holds the wire contract (proto) every gRPC/CLI/MCP transport depends on.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new project structure or deps (pure-read RPC, Constitution III).

- [ ] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the wire contract (proto) that the gRPC transport (US3) depends on. Defined early so all transports share one surface. **No story work begins until this phase is green.**

- [ ] T002 [P] Add the gRPC contract in `proto/gorag.proto`: `rpc BatchGetChunks(BatchGetChunksRequest) returns (BatchGetChunksResponse);` to the `Gorag` service (after `GetChunkContext`, before the closing `}`), and the three messages — `BatchGetChunksRequest { repeated string chunk_ids = 1; }` (no vault), `BatchGetChunksResult { string chunk_id = 1; Chunk chunk = 2; string error = 3; DocumentMeta document = 4; }` (reuse the spec-035 `Chunk` + `DocumentMeta`; R2 — per-result document), `BatchGetChunksResponse { repeated BatchGetChunksResult results = 1; }`. [FR-001, FR-002, FR-003, FR-008; contracts/api.md]
- [ ] T003 Regenerate `proto/gen/gorag.pb.go` + `proto/gen/gorag_grpc.pb.go` from the updated `proto/gorag.proto` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). (depends T002)

**Checkpoint**: the wire contract exists and compiles. User-story work can begin.

---

## Phase 3: User Story 1 — Resolve up to 100 chunks in one call, with per-id tolerance (Priority: P1) 🎯 MVP

**Goal**: a client calls `BatchGetChunks(chunk_ids)` and receives one result per requested ID, **in request order**, over REST — live IDs carry full chunks, a missing/cross-vault ID carries `error="not found"`, and the call itself succeeds (partial success).

**Independent Test**: ingest a multi-chunk doc, batch-resolve its chunk IDs plus one fabricated ID → one result per requested ID in request order, the live IDs carry full chunks + parent document, the fabricated ID carries an empty chunk + `error="not found"`, exit 0. (`quickstart.md` Scenarios 1–2.)

### Implementation for User Story 1

- [ ] T004 [US1] Implement `Engine.BatchGetChunks(chunkIDs []string) (*BatchResult, error)` + the `BatchResult{ Results []BatchItem }` / `BatchItem{ ChunkID string; Chunk *model.Chunk; Document model.Document; Source model.Source; Err string }` types in `internal/engine/` (new `batch_get_chunks.go`). Reuse `lookupChunk`/`lookupDoc` + `ErrInvalid`. Logic per `data-model.md`: validate `len==0` / `len>100` / any empty-whitespace element → `ErrInvalid` (before any lookup); else loop `lookupChunk` per id — miss → `BatchItem{ChunkID:id, Err:"not found"}`; hit → `BatchItem{ChunkID:id, Chunk:&c}` + tolerant `lookupDoc`/source (zero-valued Document when orphan, mirroring GetChunk). No call-level error for missing ids. [FR-001..FR-003, FR-005..FR-009, FR-012..FR-016]
- [ ] T005 [P] [US1] Add the REST endpoint `POST /v1/chunks/batch` in `internal/rest/` (new `batch_get_chunks.go`; register the route in `server.go` `routes` table + `handlerFor` switch beside the other `/v1/chunks/*` routes; add the path to `openapi.yaml` — the openapi parity test asserts the two match). Parse `{ "chunk_ids": […] }`; validate (empty list / `>100` / empty-whitespace element → 400); return `200` `{ results: [{ chunk_id, chunk, error, document }] }` reusing the GetChunk DTOs (`toChunkDTO`/`toDocumentMetaDTO`). **Never 404** — missing ids are in-band `error` fields on a 200. [FR-010, FR-011] (depends T004)

### Tests for User Story 1

- [ ] T006 [US1] Happy-path test in `internal/engine/batch_get_chunks_test.go`: ingest a multi-chunk doc (mirror the engine helpers `newCacheEngine`/`addDoc`), collect its chunk IDs, call `BatchGetChunks` with that list PLUS one fabricated id; assert (a) `len(results) == len(request)`, (b) results are in request order (`results[i].ChunkID == request[i]`), (c) live IDs carry full chunks (content non-empty) + a resolved `Document`, (d) the fabricated id carries `Chunk==nil` + `Err=="not found"`, (e) the call returned `nil` error. [US1 acceptance #1/#2/#3] (depends T004)

**Checkpoint**: US1 MVP delivers — `BatchGetChunks` resolves a batch with per-id tolerance over REST. `make test` green.

---

## Phase 4: User Story 2 — Correct at every boundary and edge value (Priority: P2)

**Goal**: the batch is bounded and validated — every edge behaves per the contract.

**Independent Test**: `>100` → `ErrInvalid`; empty list → `ErrInvalid`; `["valid","  ","valid"]` → `ErrInvalid`; `["a","a"]` → two identical results (no dedup); all-missing → all `Err="not found"`, call succeeds; single id → one result. (`quickstart.md` Scenarios 3–5.)

### Tests for User Story 2

- [ ] T007 [US2] Validation + edge tests in `internal/engine/batch_get_chunks_test.go`: (a) `len>100` → `ErrInvalid`; (b) empty list → `ErrInvalid`; (c) a list containing an empty/whitespace id → `ErrInvalid` (no lookup); (d) duplicates `["a","a"]` → two identical result entries (no de-dup, positional); (e) all-missing request → every result `Err="not found"`, call returns `nil` error; (f) single-id batch → one result; (g) orphan chunk (delete the doc record via `e.db.DeleteWithPrefix(PrefixDocument, …)`) → chunk resolves, `Document` zero-valued, no error. [FR-003..FR-007, US2 acceptance #1..5] (depends T004)

**Checkpoint**: the batch contract is correct at every boundary and edge value.

---

## Phase 5: User Story 3 — Every transport, identical results (Priority: P3)

**Goal**: `BatchGetChunks` is byte-identical across gRPC, REST, MCP, and CLI.

**Independent Test**: same `chunk_ids` (with one missing id) over all four transports → identical per-position results (chunk-id list + per-position error string). (`quickstart.md` Scenario 6.)

### Implementation + Tests for User Story 3

- [ ] T008 [US3] Add the gRPC handler + response projection in `internal/grpc/batch_get_chunks.go`: implement `BatchGetChunks(ctx, *BatchGetChunksRequest) (*BatchGetChunksResponse, error)` — call-level validation maps to `INVALID_ARGUMENT` via `toStatusErr` (engine `ErrInvalid`); call `engine.BatchGetChunks`; project each `BatchItem` (reuse `toChunkPB` + `toDocumentMetaPB`; `chunk` nil + `error="not found"` preserved per-item; `document` nil when zero-valued). **No top-level NOT_FOUND** — missing ids are in `result.error`. [FR-010] (depends T003, T004)
- [ ] T009 [P] [US3] Add the CLI command `go-rag chunk batch <chunk_id> [<chunk_id>…]` in `internal/cli/chunk.go` (beside `chunk get` / `chunk context`): positional args are the chunk-id list; `--format json|text` (default json); reject `>100` args with a non-zero exit + clear message. JSON envelope matches the proto/REST shape (`{ results: […] }`). [FR-010] (depends T004)
- [ ] T010 [P] [US3] Add the MCP tool `go_rag_batch_get_chunks` in `internal/mcp/server.go` (beside `go_rag_get_chunk` / `go_rag_get_chunk_context`): wire `dispatchDB` case + `toolDefs()` entry (inputSchema `{ chunk_ids: array of string }`); `renderBatchGetChunks` parses the `chunk_ids` array (JSON `[]any` of `string`), validates count/elements, calls `engine.BatchGetChunks`, renders one line per result (chunk_id, `ok`/`not found`, document line). **Bump the tool-count assertions**: `TestMCP_ToolsListCount` + `TestHTTPToolsList` 21 → 22 and add `go_rag_batch_get_chunks` to the want-list. [FR-010] (depends T004)
- [ ] T011 [US3] Extend `internal/engine/parity_test.go` with `TestCrossTransport_BatchGetChunksParity`: assert `BatchGetChunks` returns identical per-position results (chunk-id list + per-position `error` string + live-position document id/file_path) across CLI, REST, gRPC, and MCP for the same `chunk_ids` over a batch mixing live + missing + duplicate ids; cover the call-level invalid-argument mapping (REST 400 / gRPC INVALID_ARGUMENT) for a `>100` request. [FR-010; SC-001] (depends T005, T008, T009, T010)

**Checkpoint**: all four transports return identical batch results.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T012 [P] Run `make lint` (golangci-lint — the `ci.yml` gate) and resolve every finding; run `quickstart.md` validation end-to-end on an isolated DB (Scenarios 1–7, non-default `--db-path`/ports per project CLAUDE.md); affirm constitution compliance in the commit (pure read, no on-disk layout change, no migration, `migrate.ExpectedVersion` unchanged, pure Go). Mark BL-003 resolved in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` (mirror BL-002/BL-004's resolved note — note the vault-field drop per FR-015 + the per-id-error model).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002–T003)**: T003 depends on T002. **Blocks US3's gRPC path.** (US1/US2 use only the engine method + REST, not the proto.)
- **US1 (T004–T006)**: T004 (engine) depends on nothing (reuses `lookupChunk`/`lookupDoc`); T005 (REST) depends on T004; T006 (test) depends on T004.
- **US2 (T007)**: depends on T004.
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
# US1 (once T004 engine lands):
Task: "T005 REST POST /v1/chunks/batch (rest/batch_get_chunks.go + server.go + openapi.yaml)"
Task: "T006 happy-path engine test (batch_get_chunks_test.go)"

# US3 (parallel — different files):
Task: "T008 gRPC handler (grpc/batch_get_chunks.go)"        # after T003 regen
Task: "T009 CLI `chunk batch` (cli/chunk.go)"
Task: "T010 MCP go_rag_batch_get_chunks (mcp/server.go)"
# then T011 parity test once T005+T008+T009+T010 land.
```

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T003 (proto contract).
3. US1 T004–T006 (engine method + REST + happy-path test).
4. **STOP & VALIDATE**: `quickstart.md` Scenarios 1–2 on an isolated DB. Demo-able: a batch resolve with per-id tolerance over REST.

### Incremental delivery
- + US2 (T007): the batch contract proven correct at every boundary/edge.
- + US3 (T008–T011): gRPC + CLI + MCP + cross-transport parity.
- Polish (T012): lint, quickstart, BL-003 resolved note, constitution affirmation.

### Commit cadence
Conventional Commits to `main` after each checkpoint (`feat(spec038): ...`); `make build && vet && test && lint` green before every push.

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001/FR-002 batch + order | T004, T006, T011 |
| FR-003 per-id error (partial success) | T004, T006, T007, T011 |
| FR-004 cap 100 / INVALID_ARGUMENT | T004, T005, T007, T009, T011 |
| FR-005 empty list / FR-006 empty element | T004, T005, T007 |
| FR-007 duplicates positional (no dedup) | T004, T007 |
| FR-008/FR-009 full metadata + single read | T004, T006 |
| FR-010/FR-011 all 4 transports + REST route | T005, T008, T009, T010, T011 |
| FR-012 reuse lookupChunk + Chunk message | T002, T004 |
| FR-013 no migration | T012 (affirm) |
| FR-014 pure Go | T012 (affirm) |
| FR-015 no vault field | T002, T004 |

Every spec acceptance scenario (US1 #1–3, US2 #1–5, US3 #1–2) is covered by T006, T007, T011 respectively.

## Notes
- All tasks carry `[ID]`, a file path, and (`[P]`/`[USx]`) markers per the format rules.
- Pure-read feature — Constitution Principles I–V + storage discipline hold throughout; no migration, no new deps.
- The engine method (T004) is the one substantive logic task; everything else is mechanical projection + tests, cloning the shipped GetChunk (spec 035) / GetChunkContext (spec 037).
- **Key correctness invariant** (the reason this spec exists vs looping GetChunk): missing ids are an in-band per-result `error`, NEVER a call-level failure. Every transport task (T005/T008/T009/T010) must preserve this — REST 200 (not 404), gRPC in-band (not a status), exit 0.
