---
description: "Task list for spec 037 — GetChunkContext (BL-002)"
---

# Tasks: GetChunkContext (BL-002)

**Input**: Design documents from `/specs/037-get-chunk-context-rpc/` — `spec.md` (3 user stories, FR-001..FR-016), `plan.md`, `research.md` (R1–R4), `data-model.md`, `contracts/api.md`, `quickstart.md`.

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present and consistent.

**Tests**: INCLUDED — the go-rag constitution mandates `go build`/`go vet`/`go test` green on every change ("the repository is never left red"), and the design names the test surfaces (`get_chunk_context_test.go`, `parity_test.go`). Interleaved per story.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = MVP, US2 P2 = windowing correctness, US3 P3 = four-transport parity). Foundational phase holds the wire contract (proto) every gRPC/CLI/MCP transport depends on.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new project structure or deps (pure-read RPC, Constitution III).

- [x] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the wire contract (proto) that the gRPC transport (US3) depends on. Defined early so all transports share one surface. **No story work begins until this phase is green.**

- [x] T002 [P] Add the gRPC contract in `proto/gorag.proto`: `rpc GetChunkContext(GetChunkContextRequest) returns (GetChunkContextResponse);` to the `Gorag` service (after `GetChunk`, before the closing `}`), and the two messages — `GetChunkContextRequest { string chunk_id = 1; int32 window = 2; }` and `GetChunkContextResponse { repeated Chunk chunks = 1; int32 target_index = 2; DocumentMeta document = 3; }` (reuse the spec-035 `Chunk` + `DocumentMeta`). [FR-013; contracts/api.md]
- [x] T003 Regenerate `proto/gen/gorag.pb.go` + `proto/gen/gorag_grpc.pb.go` from the updated `proto/gorag.proto` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). (depends T002)

**Checkpoint**: the wire contract exists and compiles. User-story work can begin.

---

## Phase 3: User Story 1 — Resolve a chunk + its window in one call (Priority: P1) 🎯 MVP

**Goal**: a client calls `GetChunkContext(chunk_id, window)` and receives the ordered chunk window with `target_index` + parent document, over REST.

**Independent Test**: ingest a multi-chunk doc, fetch an interior chunk with `window=2` → 5 chunks in document order, `target_index=2`. (`quickstart.md` Scenario 1.)

### Implementation for User Story 1

- [x] T004 [US1] Implement `Engine.GetChunkContext(chunkID string, window int) (*ContextResult, error)` + the `ContextResult{Chunks []model.Chunk; TargetIndex int; Document model.Document; Source model.Source}` type in `internal/engine/` (new `get_chunk_context.go`). Reuse `lookupChunk`/`lookupDoc` + `ErrInvalid`/`ErrNotFound`. Logic per `data-model.md`: validate id (non-empty) + window (default 2, range 0–10); `lookupChunk(target)` → `ErrNotFound` if missing; walk `PreviousChunkID` (≤window hops, prepend) and `NextChunkID` (≤window hops, append), breaking on empty id or a miss; assemble `chunks` + `target_index=len(predecessors)`; `lookupDoc` (tolerant). [FR-001..FR-009, FR-012..FR-016]
- [x] T005 [P] [US1] Add the REST endpoint `GET /v1/chunks/{id}/context?window=N` in `internal/rest/` (register the route in `server.go` beside `/v1/chunks/{id}`; handler in `get_chunk.go` or a new file). Parse `window` (default 2; `>10`/`<0` → 400), return JSON `{chunks: []chunkDTO, target_index, document: documentMetaDTO}` reusing the GetChunk DTOs. 404 for missing id, 400 for empty/whitespace id. [FR-010, FR-011] (depends T004)

### Tests for User Story 1

- [x] T006 [US1] Happy-path test in `internal/engine/get_chunk_context_test.go`: ingest a multi-chunk doc (mirror the engine test helpers `newCacheEngine`/`addDoc`), fetch an interior chunk with `window=2`, assert 5 chunks in document order with `target_index=2` and the parent document resolved; assert each returned chunk carries `Wikilinks`/`SectionContext` (full metadata, FR-008). [US1 acceptance #1/#2] (depends T004)

**Checkpoint**: US1 MVP delivers — `GetChunkContext` returns a correct context window over REST. `make test` green.

---

## Phase 4: User Story 2 — Correct windowing at boundaries and edge values (Priority: P2)

**Goal**: windowing is correct at every boundary and for every edge value.

**Independent Test**: first chunk → `target_index=0` (successors only); last chunk → predecessors only; `window=0` → one chunk; `window=11` → invalid-argument; single-chunk doc → one chunk. (`quickstart.md` Scenarios 2–5.)

### Tests for User Story 2

- [x] T007 [US2] Windowing tests in `internal/engine/get_chunk_context_test.go`: (a) first chunk `window=5` → `target_index=0`, successors only; (b) last chunk → predecessors only; (c) `window=0` → exactly `[target]`, `target_index=0` (≡ GetChunk); (d) `window=11` → `ErrInvalid`; (e) negative `window` → `ErrInvalid`; (f) single-chunk document → one chunk; (g) `window` larger than the document → whole doc, target at real index; (h) empty/whitespace `chunk_id` → `ErrInvalid`; (i) missing `chunk_id` → `ErrNotFound`; (j) orphan chunk (parent removed) → window returned, document zero-valued (no error). [FR-003..FR-007, US2 acceptance #1..#5] (depends T004)

**Checkpoint**: windowing is correct at every boundary and edge value.

---

## Phase 5: User Story 3 — Every transport, identical results (Priority: P3)

**Goal**: `GetChunkContext` is byte-identical across gRPC, REST, MCP, and CLI.

**Independent Test**: same `(chunk_id, window)` over all four transports → identical `chunks`/`target_index`/`document`. (`quickstart.md` Scenario 6.)

### Implementation + Tests for User Story 3

- [x] T008 [US3] Add the gRPC handler + response projection in `internal/grpc/engine_adapter.go`: implement `GetChunkContext(ctx, *GetChunkContextRequest) (*GetChunkContextResponse, error)` — validate (window default 2, clamp/range → INVALID_ARGUMENT), call `engine.GetChunkContext`, project `[]Chunk` (reuse the existing `Chunk` projection used by GetChunk) + `target_index` + `DocumentMeta`. INVALID_ARGUMENT / NOT_FOUND error mapping. [FR-010] (depends T003, T004)
- [x] T009 [P] [US3] Add the CLI command `go-rag chunk context <id> [--window N]` in `internal/cli/chunk.go` (beside `chunk get`): default `--window 2`; render the ordered chunks with the target marked (`>>>`) and `target_index`; reject `>10` with a non-zero exit + clear message. [FR-010] (depends T004)
- [x] T010 [P] [US3] Add the MCP tool `go_rag_get_chunk_context` in `internal/mcp/server.go` (beside `go_rag_get_chunk`): render the window as a numbered list with the target marked + the document line. [FR-010] (depends T004)
- [x] T011 [US3] Extend `internal/engine/parity_test.go` with `TestCrossTransport_GetChunkContextParity`: assert `GetChunkContext` returns identical `chunks` (chunk-id list) / `target_index` / `document` across CLI, REST, gRPC, and MCP for the same `(chunk_id, window)`; cover an interior chunk and a boundary (first) chunk. [FR-010; SC-001] (depends T005, T008, T009, T010)

**Checkpoint**: all four transports return identical context windows.

---

## Phase 6: Polish & Cross-Cutting

- [x] T012 [P] Run `make lint` (golangci-lint — the `ci.yml` gate) and resolve every finding; run `quickstart.md` validation end-to-end on an isolated DB (Scenarios 1–7, non-default `--db-path`/ports per project CLAUDE.md); affirm constitution compliance in the commit (pure read, no on-disk layout change, no migration, `migrate.ExpectedVersion` unchanged, pure Go). Mark BL-002 resolved in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` (mirror BL-001/BL-004's resolved note).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002–T003)**: T003 depends on T002. **Blocks US3's gRPC path.** (US1/US2 use only the engine method, not the proto.)
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
Task: "T005 REST GET /v1/chunks/{id}/context (rest/get_chunk.go + server.go)"
Task: "T006 happy-path engine test (get_chunk_context_test.go)"

# US3 (parallel — different files):
Task: "T008 gRPC handler (grpc/engine_adapter.go)"        # after T003 regen
Task: "T009 CLI `chunk context` (cli/chunk.go)"
Task: "T010 MCP go_rag_get_chunk_context (mcp/server.go)"
# then T011 parity test once T005+T008+T009+T010 land.
```

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T003 (proto contract).
3. US1 T004–T006 (engine method + REST + happy-path test).
4. **STOP & VALIDATE**: `quickstart.md` Scenario 1 on an isolated DB. Demo-able: a context window over REST.

### Incremental delivery
- + US2 (T007): windowing proven correct at every boundary/edge.
- + US3 (T008–T011): gRPC + CLI + MCP + cross-transport parity.
- Polish (T012): lint, quickstart, BL-002 resolved note, constitution affirmation.

### Commit cadence
Conventional Commits to `main` after each checkpoint (`feat(spec037): ...`); `make build && vet && test && lint` green before every push.

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001/FR-002 window + target_index | T004, T006, T011 |
| FR-003 default 2, window=0 ≡ GetChunk | T004, T007 |
| FR-004 cap 10 / INVALID_ARGUMENT | T004, T005, T007, T009 |
| FR-005 boundary tolerance | T004, T007 |
| FR-006 not-found / cross-vault | T004, T007 |
| FR-007 invalid id | T004, T005, T007 |
| FR-008/FR-009 full metadata + document | T004, T006 |
| FR-010/FR-011 all 4 transports + REST route | T005, T008, T009, T010, T011 |
| FR-012 one logical read | T004 |
| FR-013 reuse linked list + Chunk message | T002, T004 |
| FR-014 no migration | T012 (affirm) |
| FR-015 pure Go | T012 (affirm) |
| FR-016 no vault field | T002, T004 |

Every spec acceptance scenario (US1 #1–4, US2 #1–5, US3 #1–2) is covered by T006, T007, T011 respectively.

## Notes
- All tasks carry `[ID]`, a file path, and (`[P]`/`[USx]`) markers per the format rules.
- Pure-read feature — Constitution Principles I–V + storage discipline hold throughout; no migration, no new deps.
- The engine method (T004) is the one substantive logic task; everything else is mechanical projection + tests, cloning the shipped GetChunk (spec 035).
