# Tasks: Structured Audit Log

**Input**: Design documents from `/specs/021-audit-log/` (plan.md, spec.md, research.md, data-model.md, contracts/events.md, quickstart.md)

**Prerequisites**: plan.md ✅, spec.md ✅ (US1–US2), research.md ✅ (D1–D7), data-model.md ✅, contracts/ ✅

**Tests**: Included — quickstart.md requires an append/read/rotate test, a privacy test (no query plaintext), and a typed-events integration test.

**Organization**: Tasks grouped by user story (US1 P1 = the audit trail MVP; US2 P2 = the reader). Go project — `internal/<pkg>/` paths per plan.md.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[Story]**: US1/US2 — maps to spec.md user stories
- All paths project-relative; **stdlib only, no new dependency** (Constitution III)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: the config keys every story uses.

- [X] T001 [P] Add audit config keys to `internal/config` — `audit_log_enabled` (default `true`), `audit_log_max_bytes` (default `~16 MiB`), `audit_path` (optional override of the vault-relative log path); Get/Set/Validate + Load backward-compat (absent ⇒ defaults)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the `internal/audit` package — the append-only JSONL logger (the only audit-importing package). ⚠️ No story work until this lands.

- [X] T002 `internal/audit/event.go` — `AuditEvent` types (query/ingest/auth-fail) + JSON marshaling + `QueryHash(text) string` (SHA-256 hex; **never** plaintext)
- [X] T003 [P] `internal/audit/audit.go` — the `Appender`: callers do a non-blocking channel send of an `AuditEvent`; a single writer goroutine drains, serializes appends, flushes periodically (no per-event fsync) + `Init(path, maxBytes)`/`Close()`. Off the caller's path (Constitution IV)
- [X] T004 [P] `internal/audit/rotate.go` — size-capped rotation: when `audit.log` exceeds the cap, rename → `audit-1.log` (shift older: `audit-2.log`, `audit-3.log`), drop beyond N=3, start fresh. Hand-rolled (stdlib; append-only preserved)
- [X] T005 audit package test — append → read the file → rotate at a tiny cap (archive appears, no file exceeds cap) → **privacy** (a logged query's plaintext is absent, only its hash); `-race` clean

**Checkpoint**: `internal/audit` ready (event types + async appender + rotation). Wiring can begin.

---

## Phase 3: User Story 1 — The audit trail (wiring) (Priority: P1) 🎯 MVP

**Goal**: every query (hashed), ingest, and auth-fail appended to the local JSONL log; privacy-preserving; off the ACK path.

**Independent Test**: run a query + an ingest + trigger an auth failure against a daemon, then read `<dbpath>/audit/audit.log` → three correctly-typed records; no query plaintext.

### Implementation for User Story 1

- [X] T006 [P] [US1] Emit a `query` event in `Engine.Query` (`internal/engine`) — `query_hash` (SHA-256 of the query), `mode`, `k`, `hits`, `status` (alongside the existing observe span/metric record; single instrumentation point)
- [X] T007 [P] [US1] Emit `ingest` events in `Engine.Add`/`Scan`/`Reprocess`/`Migrate` (`internal/engine`) — `op`, `path`, counts (`new`/`skipped`/`errors`), `status`; no content
- [X] T008 [P] [US1] Emit `auth-fail` events at each transport's bearer check — REST `guard` (`internal/rest/server.go`), gRPC `bearerInterceptor` (`internal/grpc/server.go`), MCP auth path (`internal/mcp`) — carrying `transport` + short `detail`, **never** the rejected token
- [X] T009 [US1] Daemon wiring — `audit.Init` at boot + `audit.Close` on stop (`internal/cli/serve.go`), so the appender's writer goroutine runs for the daemon's life and drains on shutdown
- [X] T010 [US1] Integration test — against an isolated daemon/DB: a query + an ingest + an auth-fail each produce a correctly-typed JSONL record; **no query plaintext** appears (privacy); `-race` clean

**Checkpoint**: US1 — the audit trail records every query/ingest/auth-fail, privacy-preserving, off-path.

---

## Phase 4: User Story 2 — Reading + filtering the log (Priority: P2)

**Goal**: a `go-rag audit` reader that tails + filters by type/time.

**Independent Test**: append mixed events, then `--tail N` / `--type query` / `--since 1h` return only matches.

### Implementation for User Story 2

- [X] T011 [P] [US2] `internal/audit/reader.go` — scan the active log (+ `--all` archives), parse JSONL, filter by `type` + `--since` (duration), tail the last N; a `-f json` raw mode for `jq`
- [X] T012 [US2] CLI `go-rag audit` command (`--tail`/`--type`/`--since`/`--all`/`-f`) in `internal/cli` + register in `root.go`
- [X] T013 [US2] Reader test — filter by type + tail N + `--since`; archives included with `--all`

**Checkpoint**: US2 — the log is readable/filterable from the CLI.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: docs + final gates.

- [X] T014 [P] Docs — event schema + privacy posture (query hashed, no content/credentials) + air-gap boundary in `docs/audit.md` (FR-009); reference `contracts/events.md`
- [X] T015 Final gates — `go build ./...`, `go vet ./...`, `go test -race -cover ./...` green; `make test-eval` recall@10 unchanged; `go.mod` unchanged (no new dep — Constitution III); run `quickstart.md` scenarios 1–5

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately
- **Foundational (Phase 2)**: depends on Phase 1 — `internal/audit` BLOCKS both stories
- **US1 (Phase 3)**: depends on Phase 2 — the MVP
- **US2 (Phase 4)**: depends on Phase 2 (the reader reads the file the appender writes); independent of US1
- **Polish (Phase 5)**: depends on US1+US2

### User Story Dependencies

- **US1 (P1)**: starts after Foundational — no story deps. **MVP.**
- **US2 (P2)**: starts after Foundational — independently testable (reader); does not require US1's wiring to be tested (the reader parses any well-formed log)

### Within Each User Story

- Event types / appender before the wiring that emits them
- Engine emission before transport auth-fail wiring (parallel — different files)
- Test last in each story

### Parallel Opportunities

- Phase 1: T001 (config)
- Phase 2: T003 ∥ T004 (appender vs rotation files) once T002 lands
- US1: T006 (engine query) ∥ T007 (engine ingest) ∥ T008 (transport auth-fail) — different files/packages
- US2: T011 (reader) ∥ T014 (docs)

---

## Parallel Example: User Story 1

```bash
# After Foundational (T002–T005), fan out the emission wiring:
Task: "Emit query event in Engine.Query (internal/engine)"
Task: "Emit ingest events in Add/Scan/Reprocess/Migrate (internal/engine)"
Task: "Emit auth-fail at REST guard / gRPC interceptor / MCP (internal/rest, internal/grpc, internal/mcp)"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (config) → Phase 2 (`internal/audit` package)
2. Phase 3 (US1): emit query/ingest/auth-fail events → wire daemon lifecycle → integration test
3. **STOP and VALIDATE**: a query + ingest + auth-fail → three typed JSONL records; no plaintext
4. This alone closes the audit gap (durable, privacy-preserving trail of operations + auth)

### Incremental Delivery

1. Setup + Foundational → `internal/audit` ready
2. + US1 → the audit trail (**MVP** — accountability)
3. + US2 → readable/filterable (`go-rag audit`)
4. Polish → docs + final gates

---

## Notes

- `[P]` = different files, no deps on incomplete tasks
- `[Story]` maps the task to its user story for traceability
- Every story is independently completable and testable; stop at any checkpoint to validate
- Commit (Conventional Commits, straight to `main`) after each task or logical group
- Constitution gates (plan.md): **I** local file + no egress (no SIEM/syslog/cloud forwarding), **III** stdlib-only no dep, **IV** async appender off the ACK path (no per-event fsync), **V** auth-fail recorded consistently across transports
- **Privacy (book §11.4)**: query text SHA-256 hashed — never plaintext; no chunk/document content; no rejected credentials on any record
