---
description: "Task list for spec 040 — WatchDocuments (BL-008)"
---

# Tasks: WatchDocuments (BL-008)

**Input**: Design documents from `/specs/040-watch-documents-rpc/` — `spec.md` (3 user stories, FR-001..FR-016), `plan.md`, `research.md` (R1–R5), `data-model.md`, `contracts/api.md`, `quickstart.md`.

**Prerequisites**: `plan.md` (required), `spec.md` (required); all Phase 0/1 artifacts present and consistent.

**Tests**: INCLUDED — the go-rag constitution mandates `go build`/`go vet`/`go test` green on every change, AND this is concurrency-heavy code (races/deadlocks/leaks are the risk) — the `-race` streaming test is load-bearing.

**Workflow**: single-author repo, commits to `main`. After each checkpoint: `make build && make vet && make test && make lint`, Conventional Commit, push.

**Organization**: grouped by user story (US1 P1 = MVP — the stream + event bus + 3 events; US2 P2 = cursor resume; US3 P3 = concurrency/slow-consumer robustness). Foundational phase holds the streaming proto contract.

**Load-bearing invariants** (every implementation task must preserve these):
- **Publish is non-blocking** — `select { case ch <- ev: default: drop }`; NEVER blocks the write path (Principle IV's <10ms ACK).
- **Subscriber isolation** — a slow consumer's full buffer drops for THAT subscriber only; publisher + other watchers unaffected.
- **ctx.Done() → unsubscribe** — no goroutine/channel leak on client disconnect.
- **INGESTED after the durable commit; EMBEDDED after async embed** (Principle IV).

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable — different file, no dependency on an incomplete task in the same phase.
- **[USx]**: user-story phase label.
- Every task names an exact file path.

---

## Phase 1: Setup

**Purpose**: confirm a green baseline. No new deps (grpc-go server-streaming is already in the dep tree; the event bus is pure Go stdlib).

- [x] T001 Verify baseline `make build && make vet && make test` is green on `main` before starting.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the streaming wire contract (go-rag's first streaming rpc + first enum). **No story work begins until this phase is green.**

- [x] T002 [P] Add the gRPC streaming contract in `proto/gorag.proto`: `rpc WatchDocuments(WatchRequest) returns (stream DocumentEvent);` to the `Gorag` service (after `ListDocuments`, before the closing `}`), the `enum DocumentEventType { INGESTED = 0; EMBEDDED = 1; RE_INGESTED = 2; DELETED = 3; }` (RE_INGESTED reserved — BL-010, not emitted), `message WatchRequest { string cursor = 1; }` (no vault), and `message DocumentEvent { DocumentEventType type = 1; string document_id = 2; string source_path = 3; string cursor = 4; DocumentMeta after = 5; int64 timestamp_ms = 6; }`. [FR-001; contracts/api.md]
- [x] T003 Regenerate `proto/gen/gorag.pb.go` + `proto/gen/gorag_grpc.pb.go` from the updated `proto/gorag.proto` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). Confirm the generated `Gorag_WatchDocumentsServer` streaming interface exists. (depends T002)

**Checkpoint**: the streaming wire contract exists + compiles. Story work can begin.

---

## Phase 3: User Story 1 — Receive lifecycle events as they happen (Priority: P1) 🎯 MVP

**Goal**: open a `WatchDocuments` stream and receive `INGESTED`/`EMBEDDED`/`DELETED` events within ~500ms of the lifecycle change. Requires the in-memory event bus + the lifecycle emit-hooks + the streaming handler.

**Independent Test**: open a stream over bufconn; `add` → INGESTED within ~500ms; embed completes → EMBEDDED within ~500ms; delete+scan → DELETED within ~500ms. (`quickstart.md` Scenarios 1–3.)

### Implementation for User Story 1

- [x] T004 [US1] Implement the in-memory event bus in a new `internal/events/` package (`bus.go`): `Bus` (sync.RWMutex-guarded `map[subscriberID]*subscriber` + `atomic.Uint64` sequence counter), `DocumentEvent` struct (Type, DocumentID, SourcePath, Seq, After, TimestampMs), `DocumentEventType` constants (INGESTED/EMBEDDED/DELETED). `Subscribe(buf int) (<-chan DocumentEvent, nextSeq uint64, unsub func())` registers a subscriber with a buffered channel (cap = buf, default 64) + returns its channel + the next sequence + an unsubscribe func. `Publish(ev DocumentEvent)` assigns `ev.Seq = next()` + fan-outs a **non-blocking** send (`select { case ch <- ev: default: bump dropped; log }`) to every subscriber. Drop-behind per subscriber; never blocks the caller. [FR-001, FR-010, FR-011, FR-013, FR-016; data-model.md; research.md R1/R2]
- [x] T005 [US1] Wire the engine to own the bus + the three lifecycle emit-hooks: (a) `internal/engine/events.go` — `Engine` holds a `*events.Bus` (created at open, closed on close) + a `PublishEvent` helper; (b) `internal/pipeline/pipeline.go` `processFile` — publish `INGESTED` AFTER the durable Pebble commit (fsync of the document record), before returning; wire `Pipeline.OnNotifyEmbed` to publish `EMBEDDED` on embed completion; (c) `internal/watcher` `ChangeDetector` — publish `DELETED` in the deletion path. The pipeline/watcher get the bus (or a publish callback) via injection. Publish must be non-blocking (T004 guarantees it). [FR-002, FR-003, FR-004, FR-014; research.md R5] (depends T004)
- [x] T006 [US1] Implement the gRPC streaming handler in `internal/grpc/watch_documents.go`: `func (a *Adapter) WatchDocuments(req *goragpb.WatchRequest, stream goragpb.Gorag_WatchDocumentsServer) error` — `ch, nextSeq, unsub := a.eng.Events().Subscribe(64); defer unsub()`; if `req.Cursor` non-empty + decodes → `startSeq = decode+1` else `startSeq = nextSeq` (from-now); loop `select { case ev := <-ch: if ev.Seq < startSeq { continue }; if err := stream.Send(toEventProto(ev)); err != nil { return err } ; case <-stream.Context().Done(): return nil }`. `toEventProto` maps the internal `DocumentEvent` to the proto (cursor = base64-url of Seq; `after` via `toDocumentMetaPB`). [FR-001, FR-005, FR-007, FR-009; research.md R3/R4] (depends T003, T004, T005)

### Tests for User Story 1

- [x] T007 [US1] Streaming test in `internal/grpc/watch_documents_test.go` (package grpc, over bufconn — mirror the parity test's `dialGRPC` helper): open a `WatchDocuments` stream; `add` a doc via the engine; assert an `INGESTED` event arrives within ~1s (relaxed from 500ms for CI) carrying the document_id + a non-empty cursor + the `after` DocumentMeta; wait for embeddings → an `EMBEDDED` event arrives; delete + scan → a `DELETED` event arrives. Assert event ordering (INGESTED before EMBEDDED for the same doc). Run under `-race`. [US1 #1..4; FR-002..004] (depends T006)

**Checkpoint**: US1 MVP delivers — the stream + event bus + 3 lifecycle events, green under `-race`.

---

## Phase 4: User Story 2 — Resume after a disconnect (Priority: P2)

**Goal**: a non-empty cursor resumes strictly after it; empty/unrecognized → from-now. Honest MVP limitation: resume covers only events still in the bus's in-flight window (drop-behind; the ListDocuments poll is the lossless fallback).

**Independent Test**: receive events, note the last cursor, close the stream, add more, reconnect with the cursor → receive the events since (within the buffer); reconnect with `cursor=""` → from-now. (`quickstart.md` Scenario 4.)

### Tests for User Story 2

- [x] T008 [US2] Cursor + resume tests in `internal/grpc/watch_documents_test.go` + `internal/events/bus_test.go`: (a) cursor codec round-trip (`encode/decode` of uint64 seq); (b) reconnect with a valid cursor delivers events with `Seq > cursor` (no duplicate of the event at the cursor); (c) `cursor=""` starts from now (no replay); (d) an unrecognized/garbage cursor is treated as from-now (graceful, no error); (e) DOCUMENT the limitation: a disconnect long enough to overflow the buffer (>64 events) fast-forwards (older events dropped) — assert the reconnect still succeeds + delivers subsequent events. [US2 #1..3; FR-005..008] (depends T006)

**Checkpoint**: cursor resume works within the process-lifetime window; the lossy limitation is tested + documented.

---

## Phase 5: User Story 3 — Concurrency + slow-consumer robustness (Priority: P3)

**Goal**: multiple concurrent watchers each receive events; a slow/stuck consumer doesn't block the publisher or starve other watchers.

**Independent Test**: two concurrent streams both receive the same events; a stuck consumer + rapid adds → ingest keeps completing promptly (publisher not blocked) + the other stream keeps receiving. (`quickstart.md` Scenarios 5–6.)

### Tests for User Story 3

- [x] T009 [US3] Concurrency + slow-consumer tests in `internal/events/bus_test.go` (the bus in isolation) + `internal/grpc/watch_documents_test.go` (end-to-end): (a) two subscribers both receive every published event (fan-out); (b) a subscriber that never drains its channel: the publisher's `Publish` returns promptly (non-blocking — assert publish latency stays bounded under a stuck subscriber), and a second normally-draining subscriber still receives every event; (c) the stuck subscriber's `dropped` counter climbs (events dropped for it only). Run under `-race`. [US3 #1..3; FR-010, FR-011; research.md R1/R2] (depends T004, T006)

**Checkpoint**: the bus is robust under concurrency + a slow consumer (the load-bearing non-blocking + isolation invariants proven under `-race`).

---

## Phase 6: Polish & Cross-Cutting

- [x] T010 [P] Run `make lint` (golangci-lint — the `ci.yml` gate) and resolve every finding; run `quickstart.md` validation end-to-end on an isolated DB (Scenarios 1–6, non-default `--db-path`/ports per project CLAUDE.md); affirm constitution compliance in the commit (in-memory event bus, no on-disk change, no migration, `migrate.ExpectedVersion` unchanged, pure Go, no new deps; Principle V's gRPC-only scope justified). Mark BL-008 resolved in `docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md` (mirror BL-002/003/004/007's resolved note — note BL-009 EMBEDDED absorbed; the in-memory lossy-resume limitation; the Pebble-backed-bus follow-on is migration-gated). Note: the concurrency-heavy implementation MUST get a Forge/Cato adversarial review (races/deadlocks/leaks) before merge — schedule it in `/speckit-implement`.

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (T001)**: none.
- **Foundational (T002–T003)**: T003 depends on T002. **Blocks the gRPC handler (T006).** (The bus T004 + hooks T005 don't need the proto.)
- **US1 (T004–T007)**: T004 (bus) is standalone; T005 (hooks) depends on T004; T006 (streaming handler) depends on T003 + T004 + T005; T007 (test) depends on T006.
- **US2 (T008)**: depends on T006 (the handler does cursor resume).
- **US3 (T009)**: depends on T004 (bus isolation) + T006 (end-to-end).
- **Polish (T010)**: after all stories.

### Story completion order (single-author, sequential)
Foundational → US1 (MVP, stop+validate under -race) → US2 → US3 → Polish.

### Parallel opportunities (within phase, different files)
- Foundational: T002 (`gorag.proto`) is the only file.
- US1: T004 (`internal/events`) is standalone (parallel with T002/T003); T005 (pipeline/watcher) after T004; T006 (grpc) after T003+T004+T005.

---

## Parallel Example: Foundational + US1 bus

```text
# Foundational (proto):
Task: "T002 proto streaming rpc + enum + messages (proto/gorag.proto)"
Task: "T003 protoc regen (proto/gen)"   # after T002

# US1 (the bus is independent of the proto — parallel):
Task: "T004 EventBus pkg (internal/events/bus.go + tests)"
# then T005 (hooks: pipeline + watcher) after T004;
# then T006 (grpc streaming handler) after T003 + T004 + T005;
# then T007 (streaming test) after T006.
```

---

## Implementation Strategy

### MVP First (US1 only)
1. T001 baseline green.
2. Foundational T002–T003 (streaming proto).
3. US1 T004–T007 (event bus + lifecycle hooks + streaming handler + the -race streaming test).
4. **STOP & VALIDATE**: `quickstart.md` Scenarios 1–3 on an isolated DB. Demo-able: a live event stream.

### Incremental delivery
- + US2 (T008): cursor resume (within the in-flight window).
- + US3 (T009): concurrency + slow-consumer robustness (the non-blocking/isolation invariants proven).
- Polish (T010): lint, quickstart, BL-008 resolved, constitution affirmation.

### Commit cadence
Conventional Commits to `main` after each checkpoint (`feat(spec040): ...`); `make build && vet && test && lint` green before every push. **The concurrency-heavy bus + streaming handler MUST get an adversarial Forge/Cato review (races/deadlocks/channel-leaks) at `/speckit-implement` before merge.**

---

## FR / Acceptance coverage

| Requirement | Tasks |
|-------------|-------|
| FR-001 streaming rpc | T002, T006, T007 |
| FR-002/003/004 INGESTED/EMBEDDED/DELETED | T005, T007 |
| FR-005/006/007 cursor + resume + from-now | T006, T008 |
| FR-008 unrecognized cursor → from-now | T008 |
| FR-009 gRPC-only | T002, T006 (contracts/api.md justifies) |
| FR-010 concurrent subscribers | T004, T009 |
| FR-011 slow-consumer isolation (drop-behind) | T004, T009 |
| FR-012 no vault field | T002 |
| FR-013 local bus (no egress) | T004 |
| FR-014 INGESTED after commit / EMBEDDED after async | T005 |
| FR-015 pure Go, no new deps | T010 (affirm) |
| FR-016 in-memory, no migration | T004, T010 (affirm) |

Every spec acceptance scenario (US1 #1–4, US2 #1–3, US3 #1–3) is covered by T007, T008, T009 respectively.

## Notes
- All tasks carry `[ID]`, a file path, and (`[P]`/`[USx]`) markers per the format rules.
- This is the **most complex feature in the backlog** (first streaming RPC + first event bus + concurrency). The `-race` tests (T007/T009) are load-bearing — races/deadlocks/channel-leaks are the primary risk.
- **Forge/Cato adversarial review is MANDATORY at implement** (races, deadlock on publish, channel-leak on disconnect, the non-blocking invariant under a stuck subscriber). Schedule it.
- BL-009 (EMBEDDED event) is absorbed into this spec (T005's OnNotifyEmbed hook).
- The in-memory lossy-resume limitation is documented honestly (T008 tests it; quickstart notes it; the Pebble-bus follow-on is migration-gated).

## Follow-up: DELETED wiring (post-checkpoint correction)

The T005/T007 checkboxes were marked complete at the T005–T010 commit, but `DELETED` was **not** actually wired at that point (the `EventDeleted` constant had zero references; the streaming test covered only `INGESTED`/`EMBEDDED`). The bridge backlog's "DELETED deferred" note reflected that gap. Now closed:

- **DELETED publishes from `Pipeline.DeleteDoc`** (`internal/pipeline/delete.go`) after the durable document-record delete — not from the watcher directly. `DeleteDoc` is the single chokepoint every deletion trigger shares (`ChangeDetector.ScanOnce` at `watcher.go:96,105`; `Reprocess`; `ReprocessAll`), so publishing there is a strict superset of the original "watcher ChangeDetector" prescription and also covers explicit deletes. The publish is gated on a record actually existing (no phantom DELETED on a double-scan) and mirrors `INGESTED`/`EMBEDDED`'s `p.OnEvent(...)` pattern (engine binds `OnEvent = e.bus.Publish` at `engine.go:245`).
- **Tests**: `internal/pipeline/delete_test.go` — `TestDeleteDoc_PublishesDeletedEvent` (DELETED carries document_id + source_path + a Publish-stamped timestamp) and `TestDeleteDoc_MissingDocDoesNotPublish` (the existence gate). The gRPC stream projection of DELETED is the same `stream.Send` path already proven for INGESTED/EMBEDDED by `TestGRPC_WatchDocuments_IngEmbedded`; there is no public `Engine.Delete` trigger, so the wiring is pinned at the `DeleteDoc` unit level.

## Adversarial concurrency audit (pre-merge)

5-lens review (`-race` catches data races; the audit covers what it cannot: leaks, deadlock-under-timing, invariant/logic defects). Result: **send-on-close** + **ordering/cursor** lenses clean; 4 findings.

- ✅ **FIXED — DeleteDoc TOCTOU (medium) + duplicate-DELETED (low).** `DeleteDoc` took no lock while the embedder's `markStatus` (Get→Set→publish-EMBEDDED) holds `p.mu`; a racing `markStatus` could re-create the deleted record (resurrection) and emit `EMBEDDED` strictly after `DELETED`, and two concurrent `DeleteDoc`s could both emit `DELETED`. Fixed by holding `p.mu` across `DeleteDoc`'s document-record section — the codebase's own documented discipline (`workers.go:~325`: "PrefixDocument writers MUST take p.mu"). Lock order `p.mu→bus.mu` is acyclic (`markStatus` already holds it). Full `-race` suite green.

- ✅ **FIXED — `stream.Send` decoupled from `ctx.Done` (high).** The handler's `select { send; ctx.Done }` could not interrupt a `Send` blocked on HTTP/2 flow control (a connected-but-not-reading client), wedging the handler + subscriber until grpc tore the stream down. Fixed: a sender goroutine is now the sole reader of the subscriber channel and owns `stream.Send`; the main handler waits on `{ctx.Done, term}` (`term` buffered-1 for the sender's terminal nil/err). `ctx.Done` is now always selectable, so cancellation always unwinds the handler → `defer unsub()` runs → no leak. The cursor/from-now `Seq < startSeq` filter stays with the send. (The companion *policy* — grpc server keepalive enforcement to bound a genuinely stalled client — remains a smaller follow-up; the handler-structure fix is the part that belonged in the code now.) Covered by the existing streaming tests under `-race`; pre-existing (shipped at `dd9ecf2`).

- ✅ **FIXED — `Bus.Close` + `Engine.Close` wiring (high).** `Engine.Close` never closed subscriber channels, so a live `WatchDocuments` handler blocked on `<-ch` on shutdown UNLESS `grpc.GracefulStop` cancelled the stream ctx first (the daemon does; a direct `Engine.Close` did not). Fixed: `Bus.Close()` — `sync.Once`-gated, takes the write lock, sets a `closed` flag, empties the map, closes every subscriber channel (so late `Subscribe` returns an already-closed channel + a no-op unsub) — is called first in `Engine.Close`, making the handler's `!ok` "engine shutdown" branch real for gRPC + any future in-process subscriber. Race-safe: `Close`'s write lock excludes `Publish`'s `RLock`, so close never overlaps a send. Tests: `internal/events/bus_close_test.go` (closes all subscriber channels; idempotent; late-Subscribe returns a closed channel; Publish-after-Close is a no-op; the load-bearing **blocked-receiver unblocks on Close**; concurrent Publish-vs-Close under `-race`). Pre-existing.
