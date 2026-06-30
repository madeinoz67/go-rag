---

description: "Task list for GetChunk RPC (spec 035)"

---

# Tasks: GetChunk — Fetch a Single Chunk by Content-Addressed ID

**Input**: Design documents from `/specs/035-get-chunk-rpc/` — `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/get-chunk.md`, `quickstart.md`.

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/get-chunk.md.

**Tests**: Included. The spec defines an Independent Test + acceptance scenarios per story, and the constitution's build gate requires `go test ./...` to pass — so each story ships its own tests.

**Organization**: Tasks grouped by user story (US1 P1 / US2 P2 / US3 P3). Each story is an independently testable increment; US1 alone is a viable MVP.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 for story-phase tasks only
- File paths are go-rag project-relative (see `plan.md` Project Structure)

**Scope guard (applies to every task)**: `GetChunk` is storage-read-only. Do **not** edit `internal/model`, `internal/storage`, or `internal/storage/migrate`. No new Pebble prefix, value encoding, or key construction → no migration → `migrate.ExpectedVersion` stays at `1` (research.md R4). Pure Go, `CGO_ENABLED=0`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish a green baseline before any change.

- [x] T001 Verify clean baseline — run `make build && make vet && make test` from repo root; confirm green. Confirm `migrate.ExpectedVersion == 1` (`internal/storage/migrate/migrate.go:22`) and that no `GetChunk` method/RPC exists yet (`tokensave_search GetChunk` returns only `QueryHit` accessors + this spec).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared infrastructure every story depends on — the wire contract + the not-found error type. **No user-story work begins until this phase is complete.**

- [x] T002 [P] Add `engine.ErrNotFound` sentinel to `internal/engine/errors.go` — `var ErrNotFound = errors.New("not found")`, with a doc comment stating transport adapters map it to gRPC `codes.NotFound` / HTTP 404 / MCP `-32001` / CLI non-zero exit (distinct from `ErrInvalid` → 400/InvalidArgument). See research.md R5, contracts/get-chunk.md.
- [x] T003 [P] Add the `GetChunk` wire contract to `proto/gorag.proto`: (a) `rpc GetChunk(GetChunkRequest) returns (GetChunkResponse);` in the `Gorag` service block beside `ReleaseChunk`/`ResetChunk` (no `vault` field — research.md R1); (b) `message GetChunkRequest { string chunk_id = 1; }`; (c) `message GetChunkResponse { Chunk chunk = 1; DocumentMeta document = 2; }`; (d) `message Chunk { … }` and `message DocumentMeta { … }` per data-model.md, **reusing** the existing `Poisoning` / `NearDup` / `PoisoningSignals` messages (do not redefine).
- [x] T004 Regenerate `proto/gen` from the updated `proto/gorag.proto` (buf/protoc — match the repo's existing generation step; generated package `github.com/madeinoz67/go-rag/proto/gen;goragpb`). Depends on T003.

**Checkpoint**: Wire contract + `ErrNotFound` exist. Story implementation can begin.

---

## Phase 3: User Story 1 — Resolve a `chunk_id` to its full chunk (Priority: P1) 🎯 MVP

**Goal** (spec US1, FR-001/002/004/007/008/009): A client holding a `chunk_id` can resolve it to the full stored chunk in one constant-time call. Delivering this story alone is a viable MVP — any client can resolve any `chunk_id` it holds. Delivered over **gRPC** (the bridge's transport) + **CLI** (dev validation).

**Independent Test**: Ingest a document, obtain a chunk_id, fetch it via `GetChunk`; the returned content matches the chunk produced at ingestion. A never-ingested `chunk_id` returns a clear not-found. (quickstart.md Scenarios 1, 2, 4.)

> **Design note:** the engine method (T007) performs the full read (chunk GET#1 + document GET#2 + optional source GET#3) per research.md R3 — that is the decided read path. US1 projects the **chunk**; US2 projects the **document**. Both reads land in US1's engine method because splitting them would add a flag with no benefit.

### Tests for User Story 1

- [x] T005 [P] [US1] Unit test `engine.GetChunk` in `internal/engine/get_chunk_test.go` — cases: (a) found → returns chunk whose content/`chunk_index`/`page_number` match an ingested document; (b) missing/stale `chunk_id` → `errors.Is(err, engine.ErrNotFound)`; (c) empty/whitespace `chunk_id` → `errors.Is(err, engine.ErrInvalid)`, no scan; (d) orphan chunk (chunk present, parent doc absent) → succeeds with empty/zero document (research.md R3 edge). Uses an isolated tmp Pebble store.
- [x] T006 [P] [US1] gRPC adapter test in `internal/grpc/` — `GetChunk` returns the `Chunk` for a valid id; missing id → `codes.NotFound`; empty id → `codes.InvalidArgument`.

### Implementation for User Story 1

- [x] T007 [US1] Implement `func (e *Engine) GetChunk(chunkID string) (*ChunkResult, error)` in `internal/engine/get_chunk.go` — compose the existing `lookupChunk` (prefix `0x03`) + `lookupDoc` (prefix `0x02`, keyed by inline `chunk.DocumentID`) + optional `Source` read (prefix `0x01` for `source_path`, per the recommendation in data-model.md "Open implementer decision"). Validate input (`ErrInvalid` on empty/whitespace). Wrap not-found: `fmt.Errorf("%w: chunk %s", engine.ErrNotFound, chunkID)`. Orphan-chunk tolerant (chunk + empty doc). Two-three Pebble point Gets, no scan. Depends on T002.
- [x] T008 [US1] Implement the gRPC adapter in `internal/grpc/engine_adapter.go`: `func (a *Adapter) GetChunk(ctx, *pb.GetChunkRequest)` calling `a.eng.GetChunk(req.GetChunkId())`; add `toChunkPB(model.Chunk) *pb.Chunk` mapper (style of `toPoisoningPB`); extend `toStatusErr` (`engine_adapter.go:14-19`) with `errors.Is(err, engine.ErrNotFound) → codes.NotFound`. (Project `Chunk` only here; `document` left empty — US2 populates it.) Depends on T004, T007.
- [x] T009 [US1] Implement the CLI in `internal/cli/chunk.go`: `go-rag chunk get <chunk_id>` (`--json` default; `RunE` pattern from `newPoisonReleaseCmd`, `poison.go:88-103`) → `openDB(dbPath)` → `engine.NewWithDB(cfg,db).GetChunk(args[0])` → print chunk. Register `newChunkCmd()` in `internal/cli/root.go` (beside `newVaultCmd`). `ErrNotFound` → `chunk not found: <id>` to stderr + non-zero exit. Depends on T007.

**Checkpoint**: US1 functional — a client can resolve any `chunk_id` to its chunk over gRPC or CLI; not-found/invalid are clean. (Stories US2 and US3 can proceed.)

---

## Phase 4: User Story 2 — Parent document metadata in the same call (Priority: P2)

**Goal** (spec US2, FR-005): The response also carries the parent document's metadata, so resolving a chunk takes exactly one round-trip. The engine already reads the document (T007); this story projects it onto the response.

**Independent Test**: Fetch a chunk by id and assert the response also carries its parent document's metadata (file_path, file_type, status, summary/enrichment_status) matching the ingested document — no second call. After re-ingest/enrichment, the metadata reflects current state. (quickstart.md Scenario 5.)

### Tests for User Story 2

- [x] T010 [P] [US2] Test `DocumentMeta` projection in `internal/grpc/get_chunk_test.go` (extend T006's file) — assert `GetChunkResponse.document` and the CLI `chunk get` output carry correct document metadata: `file_path`/`file_type`/`status` match the ingested doc; `enrichment_status`/`summary` reflect current state (incl. after spec-029 enrichment); both hashes `id` and `content_hash` present and distinct.

### Implementation for User Story 2

- [x] T011 [US2] Project `DocumentMeta` onto the response: add `toDocumentMetaPB(model.Document, model.Source) *pb.DocumentMeta` mapper in `internal/grpc/engine_adapter.go` and populate `GetChunkResponse.document`; extend the CLI `chunk get` output (`internal/cli/chunk.go`) to print the document block. Read enrichment off the document, flattening tags/summary/status/model/generated_at (data-model.md). Depends on T008.

**Checkpoint**: US1 + US2 both work — one call returns the chunk and its parent document metadata.

---

## Phase 5: User Story 3 — The same fetch from any transport (Priority: P3)

**Goal** (spec US3, FR-006, Constitution V): gRPC, REST, MCP, and CLI return byte-identical results for the same `chunk_id`; not-found surfaces equivalently in each transport's native form. gRPC + CLI ship in US1; this story adds **REST** + **MCP** and the cross-transport parity guarantee.

**Independent Test**: Fetch the same `chunk_id` over gRPC, REST, MCP, and CLI → identical chunk + document metadata; a missing id yields an equivalent not-found per transport (REST 404 / gRPC `NOT_FOUND` / MCP `-32001` / CLI non-zero). (quickstart.md Scenario 6.)

### Tests for User Story 3

- [x] T012 [P] [US3] Cross-transport parity test in `internal/rest/get_chunk_parity_test.go` — same `chunk_id` over gRPC / REST / MCP / CLI returns identical chunk + document metadata (normalised JSON diff == 0); not-found surfaces equivalently per transport. Spins an isolated daemon with non-default `--mcp-addr/--rest-addr/--grpc-addr` + `--db-path <tmp>` (project rule).
- [x] T013 [P] [US3] Extend the REST parity invariant `T035` (`internal/rest/server.go:39-42`, `routes == openapi.yaml`) to cover the new `/v1/chunks/{id}` route — fails CI if routes/openapi drift.

### Implementation for User Story 3

- [x] T014 [US3] REST adapter in `internal/rest/`: add `{"GET", "/v1/chunks/{id}", true}` to `routes` + a `handlerFor` `case` + `handleGetChunk` reading `r.PathValue("id")` (pattern: `handlePoisonRelease`) in `server.go`/`engine_adapter.go`; add `chunkDTO`, `documentMetaDTO`, `getChunkResponse` to `types.go` (snake_case, parity with `queryHit`); extend `writeEngineErr` (`server.go:182-187`) for `ErrNotFound → http.StatusNotFound`. Depends on T007.
- [x] T015 [US3] REST OpenAPI — add the `GET /v1/chunks/{id}` path (`{id}` param + `200`/`404` responses, mirror `/v1/poison/{id}/release`) to `internal/rest/openapi.yaml`. **Same commit as T014** or the `T035` parity test fails. Depends on T014.
- [x] T016 [US3] MCP adapter in `internal/mcp/server.go`: register tool `go_rag_get_chunk` (`inputSchema: {chunk_id: string, required:[chunk_id]}`, mirror `go_rag_poison_release`); add `case "go_rag_get_chunk":` dispatch + `renderGetChunk` formatter (mirror `renderQuery`); map `ErrNotFound → JSON-RPC -32001` ("chunk not found") — do **not** collapse into the `-32603` Internal bucket. Depends on T007.

**Checkpoint**: All three stories functional — `GetChunk` is byte-identical across all four transports.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Consistency, compliance, and docs that span stories.

- [x] T017 [P] Back-fill `ErrNotFound` into `ReleaseChunk` / `ResetChunk` (`internal/engine/poison.go:60,79`) — replace the bare `fmt.Errorf("chunk not found: %s", chunkID)` with `fmt.Errorf("%w: chunk %s", engine.ErrNotFound, chunkID)`. Fixes the latent 500-instead-of-404 bug on those RPCs and keeps the chunk-scoped family consistent (research.md R5 — recommended, not strictly required).
- [x] T018 Constitution compliance check — confirm `migrate.ExpectedVersion` is still `1`, no new Pebble prefix was introduced, and `CGO_ENABLED=0 go build ./...` succeeds; include the PR compliance statement from `plan.md` in the commit ("No on-disk layout change…").
- [x] T019 [P] Bridge-backlog sync — record the two deltas in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` BL-001 so the bridge consumer stays correct: (a) `vault` field dropped from `GetChunkRequest`; (b) REST path is `/v1/chunks/{id}`, not `/api/vaults/{vault}/chunks/{chunk_id}` (research.md R1/R1.D).
- [x] T020 Final validation — `make lint && make test` green; run the `quickstart.md` scenarios end-to-end on an isolated vault (Scenarios 1–7), including the cross-transport parity diff (Scenario 6) and the corpus-size-independent latency spot-check (Scenario 7).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)** — no dependencies; start immediately.
- **Foundational (Phase 2)** — depends on Setup; **blocks all stories**. T002 ∥ T003; T004 depends on T003.
- **US1 (Phase 3)** — depends on Phase 2. T007 (engine) depends on T002; T008 (gRPC) depends on T004+T007; T009 (CLI) depends on T007. T008 ∥ T009 after T007. Tests T005 ∥ T006.
- **US2 (Phase 4)** — depends on US1's T008 (gRPC projection exists). T011 depends on T008.
- **US3 (Phase 5)** — depends on US1's T007 (engine method). REST (T014→T015) ∥ MCP (T016) after T007.
- **Polish (Phase 6)** — after the desired stories land. T017/T019 are independent; T020 is last.

### User-story independence
- **US1**: depends only on Foundational. **Independently testable** (resolve a chunk + not-found).
- **US2**: builds on US1's gRPC/CLI projection. **Independently testable** (assert document metadata present).
- **US3**: builds on US1's engine method. **Independently testable** (REST + MCP + parity).

### Parallel opportunities
- Phase 2: T002 ∥ T003.
- US1: T005 ∥ T006 (tests); T008 ∥ T009 (gRPC ∥ CLI) after T007.
- US3: T014 (REST) ∥ T016 (MCP); T012 ∥ T013 (tests).
- Polish: T017 ∥ T019.

### Within each story
Tests before/alongside implementation → engine method → transport projection → integration. Each story is a complete, independently testable increment.

---

## Implementation Strategy

**MVP = User Story 1.** Ship US1 (engine `GetChunk` + gRPC + CLI + `ErrNotFound`) and a client can already resolve any `chunk_id` it holds — the primitive the bridge's `ActivateWithRAG` needs. US2 then removes the follow-up document call; US3 completes four-transport parity. Deliver P1 → P2 → P3; do not start US2/US3 feature work until US1 is green.

**Non-negotiable invariants** (every task): pure Go (`CGO_ENABLED=0`); storage-read-only (no model/storage/migrate edits); no on-disk layout change; four-transport parity; cross-vault isolation via single-vault binding (FR-003).
