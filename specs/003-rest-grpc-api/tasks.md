# Tasks: Multi-Transport Server APIs (REST + gRPC + MCP)

**Input**: Design documents from `/specs/003-rest-grpc-api/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included where the spec's acceptance scenarios and the cross-transport parity guarantee (FR-002/003) require verification — parity, read-after-write, idempotency, concurrency. Standard per-op unit tests are co-located `*_test.go`.

**Organization**: Tasks grouped by user story. US1 (P1) is the MVP. Go project paths (`internal/`, `cmd/`, `proto/`); tests are co-located `*_test.go` per Go convention.

**Constitution invariants (every task)**: `go build ./...`, `go vet ./...`, `go test ./...` stay green; `CGO_ENABLED=0` static build preserved (Principle III); Conventional Commits (`feat:`, `refactor:`, `test:`, `chore:`); loopback-only bind (Principle I).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[Story]**: US1 / US2 / US3 (setup & foundational phases have no story label)
- Exact file paths in every description

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: New dependencies, protobuf schema + codegen, daemon address-book extension.

- [X] T001 Add `google.golang.org/grpc` to go.mod and promote `google.golang.org/protobuf` to a direct dependency; run `CGO_ENABLED=0 go build ./...` to confirm the binary still builds pure-Go (go.mod, go.sum)
- [X] T002 [P] Author `proto/gorag.proto` from [contracts/gorag.proto](contracts/gorag.proto) (service Gorag + messages); add `buf.yaml` + `buf.gen.yaml` configured for `protoc-gen-go` + `protoc-gen-go-grpc` (proto/gorag.proto, buf.yaml, buf.gen.yaml)
- [X] T003 Generate and commit `proto/gen/gorag.pb.go` + `proto/gen/gorag_grpc.pb.go`; verify the package imports under `github.com/madeinoz67/go-rag/proto/gen` (proto/gen/)
- [X] T004 [P] Extend `daemon.Addrs` with `RESTAddr` + `GRPCAddr` fields and update `WriteAddrs`/`ReadAddrs`/`Status` to carry all three addresses (internal/daemon/pid.go, internal/daemon/lifecycle.go)

**Checkpoint**: grpc-go wired, protobuf types generated, address book ready.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The `internal/engine` facade — the single "unified engine" surface every adapter (REST/gRPC/MCP) calls. This is what makes cross-transport parity structural. **No user-story work can begin until this phase is complete.**

- [X] T005 [P] Create structured result types in `internal/engine/types.go`: `QueryRequest`, `QueryHit`, `QueryResult`, `StatusInfo`, `IngestSummary`, `FileEntry`, `DirEntry`, `VaultEntry` per [data-model.md](data-model.md) (internal/engine/types.go)
- [X] T006 Create `internal/engine/engine.go` with the `Engine` struct (holds `cfg config.Config`, `db *storage.DB`) and `NewWithDB(cfg, db)` constructor (internal/engine/engine.go)
- [X] T007 Implement `Engine.Query` in `internal/engine/query.go` — wraps `pipeline.LoadIndex`, `embed.NewOllama`, `index.NewRetrieval`, optional `rerank`, returns `[]QueryHit`; extract this from `mcp/server.go:query` (:152) (internal/engine/query.go)
- [X] T008 [P] Implement `Engine.Status`, `.Files`, `.Dirs` in `internal/engine/status.go` — extract from `mcp/server.go` (:204, :437, :454); replace duplicated `countPrefix` scans (internal/engine/status.go)
- [X] T009 [P] Implement `Engine.Add`, `.Scan`, `.Reprocess`, `.Migrate` in `internal/engine/ingest.go` — wrap `pipeline.New`/`Ingest`/`Reprocess`/`ReprocessAll` and `watcher.ScanOnce`; extract from `mcp/server.go` (:329, :374, :487, :501) (internal/engine/ingest.go)
- [X] T010 [P] Implement `Engine.GetConfig`, `.SetConfig`, `.ListVaults` in `internal/engine/config.go` — extract from `mcp/server.go` (:401) and `vaultList` (:227) (internal/engine/config.go)
- [X] T011 Add shared lookup helpers (`docOf`, `lookupChunk`, `lookupDoc`, `countPrefix`, `openDB`) to `internal/engine` and remove their duplication from `internal/mcp/server.go` (:538–563) and `internal/cli/wire.go` (internal/engine/helpers.go)
- [X] T012 Refactor `internal/mcp/server.go` so every `dispatchDB` operation calls `internal/engine` instead of inline wiring; keep the 11 tool names, input schemas, and text renderings unchanged (internal/mcp/server.go)
- [X] T013 Refactor `internal/cli/wire.go`, `query.go`, `add.go`, `status.go` to call `internal/engine` for shared operations (dedup); CLI behavior unchanged (internal/cli/wire.go, internal/cli/query.go, internal/cli/add.go, internal/cli/status.go)
- [X] T014 [P] Add `internal/engine/engine_test.go`: table-driven tests asserting each facade method returns the structured types and matches the prior CLI/MCP output on a fixture corpus (internal/engine/engine_test.go)

**Checkpoint**: unified engine live; MCP and CLI now call it; existing behavior unchanged and green. Adapter work can begin.

---

## Phase 3: User Story 1 — One Engine, Many Transports: Query (Priority: P1) 🎯 MVP

**Goal**: Start the server once; the same query over REST, gRPC, and MCP returns identical ranked, cited results — because all three are adapters over `internal/engine`.

**Independent Test**: `go-rag start`; issue one query over `POST /v1/query`, `gorag.Gorag/Query`, and the MCP `go_rag_query` tool; assert identical `hits` (chunk_ids, scores, file_paths). See [quickstart.md](quickstart.md) Scenario 2.

### Implementation for User Story 1

- [X] T015 [P] [US1] Create `internal/grpc/server.go`: construct `*grpc.Server`, register `goragpb.GoragServer`, add a unary bearer-token interceptor reading `authorization` metadata (reuse `daemon.ReadToken`) (internal/grpc/server.go)
- [X] T016 [P] [US1] Implement the gRPC `Query` RPC in `internal/grpc/engine_adapter.go`: map `goragpb.QueryRequest` → `engine.QueryRequest`, call `Engine.Query`, map `[]QueryHit` → `goragpb.QueryResponse` (internal/grpc/engine_adapter.go)
- [X] T017 [P] [US1] Create `internal/rest/server.go`: stdlib `net/http` ServeMux (Go 1.22 patterns), a bearer-guard wrapper (reuse token logic from `mcp.checkBearer`), and a shared `/health` handler (internal/rest/server.go)
- [X] T018 [P] [US1] Implement `POST /v1/query` in `internal/rest/engine_adapter.go` + DTOs in `internal/rest/types.go`: decode JSON → `engine.QueryRequest`, call `Engine.Query`, encode `QueryResponse` JSON; map errors to HTTP status codes (internal/rest/engine_adapter.go, internal/rest/types.go)
- [X] T019 [US1] Extend `internal/cli/serve.go` to start REST (`:7879`) and gRPC (`:7880`) listeners alongside the MCP listener in one process; coordinate graceful shutdown of all three on SIGTERM/SIGINT (internal/cli/serve.go)
- [X] T020 [US1] Add `--rest-addr` (`127.0.0.1:7879`) and `--grpc-addr` (`127.0.0.1:7880`) flags to `serve` and `start`; pass through `daemon.Start` and persist via extended `daemon.Addrs`; harden MCP default to `127.0.0.1:7878` (internal/cli/start.go, internal/daemon/lifecycle.go)
- [X] T021 [P] [US1] Add `internal/grpc/server_test.go`: in-process grpc-go client asserts `Query` returns expected hits on a fixture corpus and rejects missing/invalid bearer tokens (internal/grpc/server_test.go)
- [X] T022 [P] [US1] Add `internal/rest/server_test.go`: `httptest` client asserts `POST /v1/query` returns expected hits and correct HTTP status codes incl. 401 (internal/rest/server_test.go)
- [X] T023 [US1] Add the cross-transport parity test: run the same query through REST, gRPC, and MCP (all backed by one `Engine`) and assert byte-identical structured `hits` (FR-002) (internal/engine/parity_test.go)

**Checkpoint**: server runs three listeners; query is transport-equivalent across REST/gRPC/MCP. **MVP delivered.**

---

## Phase 4: User Story 2 — Write Once, Read Anywhere (Priority: P2)

**Goal**: Ingest + status over REST and gRPC with the async-after-ACK write contract preserved at the API boundary; a document added over one transport is immediately queryable over the others; re-add is idempotent.

**Independent Test**: `POST /v1/add` a path over REST, then immediately query it over gRPC — it is retrievable; re-`POST /v1/add` the same path → `new:0, skipped:N`. See [quickstart.md](quickstart.md) Scenario 3. (Depends on US1's adapter + serve foundation.)

### Implementation for User Story 2

- [X] T024 [P] [US2] Implement gRPC `Add` and `Status` RPCs in `internal/grpc/engine_adapter.go` → `Engine.Add` / `Engine.Status` (internal/grpc/engine_adapter.go)
- [X] T025 [P] [US2] Implement REST `POST /v1/add` and `GET /v1/status` in `internal/rest/engine_adapter.go` → `Engine.Add` / `Engine.Status` (internal/rest/engine_adapter.go)
- [X] T026 [US2] Verify async-after-ACK at the API boundary: an `Add` request returns promptly and never blocks the client on embedding latency (facade already reuses `pipeline.Ingest`; add a regression guard) (internal/engine/ingest.go, internal/rest/server_test.go)
- [X] T027 [P] [US2] Add cross-transport read-after-write + idempotency test: add over REST → query over gRPC/MCP immediately (FR-003); re-add same path over each transport → `new:0` (FR-007) (internal/engine/parity_test.go)

**Checkpoint**: full read+write parity across transports; async-ACK and idempotency hold at the boundary.

---

## Phase 5: User Story 3 — MuninnDB-Style Production Server (Priority: P3)

**Goal**: Remaining operations (scan/files/dirs/reprocess/migrate/config/vaults) across REST+gRPC, plus the production lifecycle: per-port loopback, single-instance, health, concurrency, graceful shutdown.

**Independent Test**: fire concurrent query+add from several clients → all correct, no corruption/double-writes (FR-009); `GET /health` reports status; `go-rag stop` drains and exits cleanly. See [quickstart.md](quickstart.md) Scenarios 1 & 5.

### Implementation for User Story 3

- [X] T028 [P] [US3] Implement remaining gRPC RPCs in `internal/grpc/engine_adapter.go`: `Scan`, `Reprocess`, `Migrate`, `Files`, `Dirs`, `GetConfig`, `SetConfig`, `ListVaults`, `Health` (internal/grpc/engine_adapter.go)
- [X] T029 [P] [US3] Implement remaining REST endpoints in `internal/rest/engine_adapter.go`: `POST /v1/scan`, `/v1/reprocess`, `/v1/migrate`, `GET /v1/files`, `/v1/dirs`, `GET|PUT /v1/config`, `GET /v1/vaults` per [contracts/rest-openapi.yaml](contracts/rest-openapi.yaml) (internal/rest/engine_adapter.go)
- [X] T030 [US3] Unify health: serve a single `/health` (REST) + gRPC `Health` RPC reporting `ok`, `storage_open`, `embedder_reachable`; keep existing `/mcp/health` working (internal/rest/server.go, internal/grpc/engine_adapter.go, internal/mcp/http.go)
- [X] T031 [US3] Extend parity test to cover the full shared operation surface (status, files, dirs, vault_list, config) across all three transports (internal/engine/parity_test.go)
- [X] T032 [US3] Add concurrency test: many simultaneous clients issuing overlapping query+add through the running server; assert single-writer invariant holds — no corruption, no double-writes (FR-008/009) (internal/engine/concurrency_test.go)
- [X] T033 [US3] Verify single-instance enforcement end-to-end: a second `go-rag start` against the same DB fails clearly via PID + Pebble LOCK (FR-011); extend `daemon.Status` to report all three addresses (internal/daemon/lifecycle.go, internal/cli/start.go)

**Checkpoint**: full operation surface across REST/gRPC/MCP; server is correct under concurrency and cleanly lifecycle-managed.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Discoverability, docs, and the constitution gate.

- [X] T034 [P] Serve the REST contract: `GET /openapi.yaml` returns [contracts/rest-openapi.yaml] from the running server (internal/rest/openapi.go)
- [X] T035 [P] Validate OpenAPI parity: a test asserts every REST endpoint in `internal/rest` is present in `rest-openapi.yaml` and vice-versa (internal/rest/openapi_test.go)
- [X] T036 [P] Update root `CLAUDE.md` architecture map: document `internal/engine`, `internal/rest`, `internal/grpc`, `proto/` and the multi-transport server (CLAUDE.md)
- [X] T037 Run the [quickstart.md](quickstart.md) validation scenarios end-to-end (Scenarios 1–5) against the built binary; record results
- [X] T038 Add/confirm the CI gate: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test -race -cover ./...`, and `golangci-lint run` all green (Principle III + constitution) (.github/workflows/ci.yml or Makefile)
- [X] T039 Final cleanup: remove any dead helpers left by the MCP/CLI → engine refactor; run `go mod tidy` (internal/mcp/, internal/cli/, go.mod)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately.
- **Foundational (Phase 2)**: depends on Phase 1 (proto/grpc types from T002/T003). **BLOCKS all user stories.**
- **US1 (Phase 3, MVP)**: depends on Phase 2. Delivers the adapter skeletons + multi-listener `serve` that US2/US3 build on.
- **US2 (Phase 4)**: depends on US1's adapter + serve foundation (T015–T020).
- **US3 (Phase 5)**: depends on US1 (adapter/serve) and US2 (write-path parity).
- **Polish (Phase 6)**: depends on all desired stories complete.

### User Story Dependencies

- **US1 (P1)**: starts after Foundational — no story deps. Stands up the multi-transport server + Query.
- **US2 (P2)**: builds on US1's REST/gRPC adapters and the multi-listener `serve`; independently testable (ingest works).
- **US3 (P3)**: builds on US1+US2; adds remaining ops + production lifecycle; independently testable (concurrency + lifecycle).

### Within Each User Story

- Shared helpers / types before RPC/endpoint wiring.
- gRPC and REST adapters for the same op are `[P]` (different files).
- Parity/acceptance tests last in the story.

### Parallel Opportunities

- Phase 1: T002/T004 `[P]`; Phase 2: T005/T008/T009/T010/T011/T014 `[P]`.
- Within US1: the gRPC adapter (T015/T016/T021) and REST adapter (T017/T018/T022) are fully parallel — different packages.
- Within US2/US3: gRPC and REST op implementations are `[P]` per op.
- Different user stories can be worked in parallel by different developers **only after US1** (which delivers the shared adapter/serve foundation).

---

## Parallel Example: User Story 1

```bash
# gRPC adapter track (one developer):
Task: "T015 [P] [US1] internal/grpc/server.go — grpc.NewServer + bearer interceptor"
Task: "T016 [P] [US1] internal/grpc/engine_adapter.go — Query RPC"
Task: "T021 [P] [US1] internal/grpc/server_test.go — in-process grpc client"

# REST adapter track (another developer, fully parallel):
Task: "T017 [P] [US1] internal/rest/server.go — net/http mux + bearer guard"
Task: "T018 [P] [US1] internal/rest/engine_adapter.go + types.go — POST /v1/query"
Task: "T022 [P] [US1] internal/rest/server_test.go — httptest"

# Then serialize the integration:
Task: "T019 [US1] serve.go multi-listener"
Task: "T020 [US1] --rest-addr/--grpc-addr flags"
Task: "T023 [US1] cross-transport parity test"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (Setup) + Phase 2 (Foundational engine).
2. Complete Phase 3 (US1) — multi-listener server + Query across REST/gRPC/MCP.
3. **STOP and VALIDATE**: run [quickstart.md](quickstart.md) Scenario 2 (transport equivalence).
4. Demo-able: any client, any language, can query go-rag over REST or gRPC.

### Incremental Delivery

1. Setup + Foundational → unified engine, MCP refactored, codegen ready.
2. + US1 → multi-transport server with Query (MVP).
3. + US2 → full read+write parity, async-ACK, idempotency.
4. + US3 → full operation surface + production lifecycle.
5. + Polish → OpenAPI discoverability, docs, constitution CI gate.

---

## Notes

- `[P]` = different files, no deps on incomplete tasks.
- `[US*]` maps a task to its user story for traceability.
- Every story is independently testable at its checkpoint.
- Keep `go build/vet/test` green after each task; commit with Conventional Commits.
- The engine facade (Phase 2) is the linchpin — it removes today's CLI↔MCP duplication and makes parity free.
