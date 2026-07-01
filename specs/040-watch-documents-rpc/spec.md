# Feature Specification: WatchDocuments — Streaming Document Lifecycle Events

**Feature Branch**: `040-watch-documents-rpc` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-07-01 · **Status**: Draft

**Input**: Eighth item (BL-008) of the go-rag ↔ MuninnDB bridge integration backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`), Phase 2 — the Phase-2 headline. *The bridge currently polls `ListDocuments` every ~60s (BL-007, shipped v0.2.1). `WatchDocuments` is a long-lived gRPC server-streaming RPC: the bridge connects once and receives document lifecycle events as they happen — sub-second promotion instead of 60–90s polling lag. Events carry a resumable cursor so the bridge can reconnect after a crash with no duplicates and no gaps.*

> **This is go-rag's first streaming RPC and its first internal event bus** — architecturally bigger than the Phase-1 items (which were thin projections of an existing engine method). It is scoped as an **MVP** here; the harder bits are deferred (see Out of Scope). BL-009 (the `EMBEDDED` event type) is **absorbed into this spec** (its enum value + the embed-worker emit point) rather than tracked separately.
>
> **Transport scope — deliberate deviation from Phase 1:** `WatchDocuments` is **gRPC-server-streaming only**. Streaming does not map to REST/MCP/CLI's unary request/response model; the bridge (the consumer) uses gRPC. REST's push equivalent is BL-011 (webhook, a separate spec); MCP notifications + CLI streaming are separate concerns. This is the **first go-rag operation that is not on all four transports, by design.**

## Out of Scope (MVP deferrals — follow-on specs)

- **`RE_INGESTED` event + before/after delta** — BL-010 (needs the previous-version snapshot). The MVP enum reserves its tag (`RE_INGESTED = 2`) but does not emit it.
- **Retention window + `OUT_OF_RANGE`** — reconnecting with a cursor older than the retention window. The MVP accepts an unrecognized/expired cursor as "start from now" (graceful, not an error); a bounded retention + `OUT_OF_RANGE` is a follow-on.
- **Cross-restart resume** — a cursor that survives a go-rag restart (needs a persisted event log). The MVP's cursor resumes within a single process lifetime; cross-restart resume is a follow-on (it implies a Pebble-backed event log → migration).
- **Keepalive tuning** — the 30s-ping / 1h-idle-survival targets. The MVP uses grpc's default keepalive; tuned keepalive is follow-on hardening.
- **Load test** — 1000 events/min < 1s p99. Follow-on.
- **Multi-vault filtering** — `WatchRequest` watches this single vault (single-vault-per-process, mirrors spec 035/037/038/039). No `vault` field.
- **REST / MCP / CLI streaming surfaces** — separate specs (BL-011 webhook for REST).

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Receive document lifecycle events as they happen (Priority: P1)

A client — the go-rag↔MuninnDB bridge sync worker — opens a `WatchDocuments` stream and receives document lifecycle events the moment they occur: `INGESTED` when a document is durably committed (metadata written, embedding may still be pending), `EMBEDDED` when its async embedding completes (safe to promote to MuninnDB), and `DELETED` when a scan detects the file is gone. This replaces the 60s poll with sub-second push — the bridge promotes a document within ~500ms of it being ready, not up to a minute later.

**Why this priority**: This is the entire reason Phase 2 exists — eliminating the polling lag. Delivering this story alone is a viable MVP: the bridge connects once and reacts to lifecycle events in real time. (`EMBEDDED` is the key signal — the bridge must not promote before the vector exists; today it guesses with a 30s delay.)

**Independent Test**: Open a stream; `go-rag add` a document → an `INGESTED` event arrives within ~500ms; when the async embedding finishes, an `EMBEDDED` event arrives within ~500ms. Delete the file + scan → a `DELETED` event within ~500ms.

**Acceptance Scenarios**:

1. **Given** an open `WatchDocuments` stream, **When** a document is added (metadata durably committed), **Then** an `INGESTED` event for that document arrives within ~500ms, carrying the document id, source path, an opaque cursor, the new `DocumentMeta`, and a timestamp.
2. **Given** an open stream + an `INGESTED` (but not yet embedded) document, **When** the async embedding worker completes for it, **Then** an `EMBEDDED` event arrives within ~500ms.
3. **Given** an open stream + a tracked file that has been deleted, **When** a scan runs and detects the deletion, **Then** a `DELETED` event arrives within ~500ms.
4. **Given** any event, **Then** it carries its `type`, `document_id`, `source_path`, an opaque `cursor`, `after` (the `DocumentMeta`, for INGESTED/EMBEDDED), and `timestamp_ms`.

---

### User Story 2 - Resume after a disconnect with no duplicates or gaps (Priority: P2)

A stream is long-lived but connections drop (network blip, bridge restart). The bridge persists the last received `cursor` and, on reconnect, passes it in `WatchRequest.cursor`; the stream resumes delivering every event **after** that cursor — no duplicates, no gaps (within the process lifetime / retention). An empty cursor starts from the current moment (no historical replay).

**Why this priority**: Crash-recovery correctness — the bridge must not miss events (gap → a document never promoted) nor re-process them (duplicate → wasted work / re-indexing). The MVP delivers resume within a process lifetime; cross-restart resume is deferred (Out of Scope).

**Independent Test**: Open a stream, receive a few events, note the last cursor; close the stream; add more documents; reconnect with the noted cursor → receive exactly the events that occurred since (no dupes, no gaps); reconnect with `cursor=""` → receive only events from this moment forward.

**Acceptance Scenarios**:

1. **Given** a closed stream whose last received cursor was `C`, **When** the client reconnects with `WatchRequest{cursor: C}`, **Then** it receives every event that occurred strictly after `C` (no duplicate of the event at `C`, no gap).
2. **Given** a reconnect with `cursor=""` (empty), **Then** the stream starts from the current moment — no replay of prior events.
3. **Given** a reconnect with an unrecognized/expired cursor (MVP: no retention enforcement), **Then** the stream starts from the current moment (graceful — never an error in the MVP).

---

### User Story 3 - Concurrent watchers and slow consumers don't break the stream (Priority: P3)

Multiple bridge instances (or other consumers) can hold concurrent `WatchDocuments` streams on the same vault without interfering. A slow consumer (one that reads slower than events arrive) must not block the publisher (ingest/embed/scan must keep going) nor starve other watchers — each subscriber is isolated.

**Why this priority**: Robustness of the stream under real conditions. The MVP must not have a global lock or a shared unbounded buffer that one slow reader can blow up. (Tuned keepalive + load-test targets are deferred.)

**Independent Test**: Open two concurrent streams; add documents; assert both receive the same events. Open a stream and deliberately read slowly (or stop reading) while documents keep being added; assert ingest/embed still complete promptly (publisher not blocked) and other watchers still receive events.

**Acceptance Scenarios**:

1. **Given** two open streams on the same vault, **When** a document is added, **Then** both streams receive the `INGESTED` event.
2. **Given** a stream whose client is not reading (slow/stuck consumer), **When** documents continue to be added, **Then** the publisher (ingest/embed/scan) is not blocked, and any other open stream still receives its events.
3. **Given** a slow consumer, **Then** its stream degrades gracefully (e.g. drops behind / is eventually disconnected on overflow per a documented policy) rather than stalling the whole system.

---

### Edge Cases

- **Client disconnect mid-stream:** the server tears down the subscriber; the client reconnects with the last cursor (US2).
- **Reconnect during active writes:** events generated between disconnect and reconnect are delivered on resume (within the process-lifetime retention); no loss.
- **Empty vault / idle stream:** no events arrive; the stream stays open (grpc keepalive; MVP uses defaults — the 1h-idle-survival target is deferred).
- **Event ordering:** per-document events are delivered in lifecycle order (INGESTED before EMBEDDED for the same document); across documents, order is publish order.
- **Publisher fan-out:** one publish reaches all live subscribers.
- **RE_INGESTED (modified document):** out of MVP scope — a re-ingested (changed-content) document is a NEW document id (content-addressed), so it surfaces as INGESTED of the new id + DELETED of the old (if the path remaps); the dedicated RE_INGESTED event with before/after is BL-010.
- **Embedding never completes (error):** EMBEDDED does not fire for a document whose embedding failed; the document stays in the non-embedded state (the bridge's status filter / poll is the fallback). The MVP does not emit an ERROR event.
- **Concurrent writes during a paged read (BL-007/listing):** orthogonal — this spec is the push path; the listing (BL-007) remains the polling fallback.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `WatchDocuments(WatchRequest) returns (stream DocumentEvent)` operation — a long-lived gRPC server-streaming RPC that delivers `DocumentEvent`s as they occur.
- **FR-002**: The system MUST emit an `INGESTED` event when a document's metadata is durably committed (on add/reprocess), within ~500ms.
- **FR-003**: The system MUST emit an `EMBEDDED` event when the async embedding worker completes for a document (the existing `Pipeline.OnNotifyEmbed` hook), within ~500ms.
- **FR-004**: The system MUST emit a `DELETED` event when a scan detects a tracked file's deletion, within ~500ms.
- **FR-005**: Every event MUST carry an opaque `cursor` that identifies its position in the stream.
- **FR-006**: `WatchRequest.cursor` (non-empty) MUST resume the stream strictly after that cursor — every event after it is delivered, with no duplicates and no gaps (within the process-lifetime retention).
- **FR-007**: `WatchRequest.cursor=""` (empty) MUST start the stream from the current moment — no replay of prior events.
- **FR-008**: An unrecognized/expired cursor MUST be treated as "start from now" in the MVP (graceful, never an error). *(Retention + `OUT_OF_RANGE` is out of scope.)*
- **FR-009**: `WatchDocuments` MUST be gRPC-server-streaming only. REST/MCP/CLI push equivalents are separate specs (BL-011 webhook). *(Deliberate deviation from the Phase-1 all-four-transports pattern — streaming doesn't map to unary transports.)*
- **FR-010**: Multiple concurrent `WatchDocuments` streams on the same vault MUST each receive the same events without interfering.
- **FR-011**: A slow/stuck consumer MUST NOT block the publisher (ingest/embed/scan) nor starve other subscribers; each subscriber is isolated, with a documented overflow policy (e.g. drop-behind or disconnect on overflow).
- **FR-012**: The request MUST NOT carry a `vault` field — the engine is single-vault-per-process (spec 035 convention).
- **FR-013**: The event bus MUST be local (in-process); no network egress for event delivery. *(Constitution Principle I.)*
- **FR-014**: `INGESTED` MUST fire after the durable commit (write-ACK); `EMBEDDED` after async embedding. The async-after-ACK write contract is unchanged. *(Constitution Principle IV.)*
- **FR-015**: The feature MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies. *(Constitution Principle III.)*
- **FR-016**: The MVP event bus MUST be in-memory — no on-disk key-space change, no schema migration, `migrate.ExpectedVersion` unchanged. *(A Pebble-backed event log for cross-restart resume is out of scope; the plan confirms the in-memory default.)*

### Key Entities *(include if feature involves data)*

- **DocumentEvent**: one lifecycle event — `type` (INGESTED/EMBEDDED/DELETED), `document_id`, `source_path`, opaque `cursor`, `after` (the `DocumentMeta`, for INGESTED/EMBEDDED), `timestamp_ms`. A transient, in-flight payload — not persisted in the MVP.
- **DocumentEventType**: the enum `{INGESTED, EMBEDDED, DELETED}` (+ reserved `RE_INGESTED` tag, unused in the MVP).
- **Cursor**: an opaque string identifying an event's position in the stream; round-tripped by the client for resume. In-memory monotonic sequence in the MVP (resume within a process lifetime).
- **Event bus** *(internal)*: the in-process pub-sub that lifecycle emit points publish to and `WatchDocuments` subscribes to. New internal infrastructure (go-rag's first); not persisted in the MVP.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A bridge connected via `WatchDocuments` receives `INGESTED`, `EMBEDDED`, and `DELETED` events within ~500ms of the corresponding lifecycle change (durable commit / embed completion / scan-detected deletion) — replacing the 60–90s poll lag with sub-second push.
- **SC-002**: A bridge that disconnects and reconnects with its last cursor receives exactly the events that occurred since — no duplicates, no gaps (within the process lifetime).
- **SC-003**: Multiple concurrent watchers each receive the same events; a stuck consumer does not block ingest/embed/scan or starve other watchers.
- **SC-004**: The MVP lands with no on-disk schema change (in-memory event bus), no new runtime dependency, pure Go — `migrate.ExpectedVersion` unchanged.

---

## Assumptions

- **In-memory event bus (MVP default).** The event bus + cursor live in process memory — resume works within a single go-rag process lifetime. Cross-restart resume (persisted event log) is explicitly out of scope; the plan confirms the in-memory default and scopes what a Pebble-backed follow-on would entail (it would add a new prefix → migration).
- **Lifecycle emit points exist.** `processFile` commits the document (INGESTED), the embed worker signals completion via the existing `Pipeline.OnNotifyEmbed` hook (EMBEDDED), and the watcher's `ChangeDetector` detects deletions (DELETED). The plan wires the bus into these points.
- **gRPC-server-streaming only.** No REST/MCP/CLI streaming surface in this spec (BL-011 webhook is the REST analog). The bridge (the consumer) uses gRPC.
- **Single-vault-per-process.** No `vault` field; the stream is this vault's events.
- **Backpressure isolation.** Each subscriber has its own buffered channel; a slow consumer overflows its own buffer (documented drop/disconnect policy) without affecting others or the publisher. The plan pins the buffer size + overflow policy.
- **Latency target ~500ms.** A budget, not a hard SLA — bounded by the in-process fan-out (no network on the event path). The 1000-events/min < 1s p99 load test is deferred.
- **MVP cursor = monotonic sequence.** Resume within the process lifetime; the cursor does not survive a restart (deferred).

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

- **Event-bus mechanism (the core design fork).** Decide the in-process pub-sub: a buffered channel per subscriber with a central registry (publish fan-outs to all subscriber channels) is the likely MVP. Alternatives: a single ring buffer all subscribers read at their own pace; a Pebble-backed append-only event log (enables cross-restart resume + retention but adds a new prefix → migration, schema-evolution gate). **Recommend the in-memory channel-per-subscriber for the MVP** (no migration); the plan scopes the Pebble-log follow-on. Confirm the registry is concurrency-safe (single-writer fan-out, or per-subscriber non-blocking send).
- **Cursor encoding.** An opaque token encoding the event's monotonic sequence number (the MVP's "position"). Resume = deliver events with sequence > cursor's. The plan pins the encoding (recommend base64 of the uint64 sequence).
- **Backpressure / overflow policy.** Pin the per-subscriber buffer size + what happens on overflow: drop-behind (lose oldest un-read for that subscriber) vs disconnect-the-slow-subscriber. Recommend **drop-behind with a logged warning** for the MVP (a slow consumer misses events but the publisher + others are unaffected; the bridge's cursor-resume + ListDocuments-poll fallback covers the gap). The plan documents this.
- **Lifecycle emit hooks.** Confirm the exact insert points: INGESTED in `processFile` after the durable commit (after the fsync, before/after async-embed spawn); EMBEDDED via `Pipeline.OnNotifyEmbed` (already a callback — wire it to publish); DELETED in the watcher's `ChangeDetector` deletion path. Verify publishing is non-blocking (the publish must not stall the write path — Principle IV's <10ms ACK).
- **Streaming RPC mechanics (new pattern).** grpc-go server-streaming: the handler returns `<-chan DocumentEvent` / sends via `grpc.ServerStream.Send`; handle client disconnect (ctx cancellation) → unsubscribe; handle idle (grpc keepalive defaults for the MVP). The plan confirms the grpc-go server-streaming shape + the subscriber lifecycle (subscribe on stream open, unsubscribe on ctx-done).
- **Proto (additive, the first streaming rpc + enum).** `rpc WatchDocuments(WatchRequest) returns (stream DocumentEvent);` + `enum DocumentEventType { INGESTED = 0; EMBEDDED = 1; RE_INGESTED = 2; DELETED = 3; }` (RE_INGESTED reserved, unused in MVP) + `message WatchRequest { string cursor = 1; }` + `message DocumentEvent { DocumentEventType type = 1; string document_id = 2; string source_path = 3; string cursor = 4; DocumentMeta after = 5; int64 timestamp_ms = 6; }`. (No `vault` field; `before` omitted — RE_INGESTED is deferred.) Add after `ListDocuments` in the `Gorag` service.
- **Tests.** A streaming RPC test: open a stream, add/embed/delete, assert events arrive in order within latency; reconnect-with-cursor (no dupes/gaps); two concurrent streams (both receive); slow-consumer isolation (publisher not blocked). The plan pins the test surface (grpc bufconn + a test subscriber).
- **Constitution compliance to assert in the plan:** Principles I (local event bus — no egress), II (no identity change — read-side observability), III (pure Go), IV (publish non-blocking; INGESTED after durable ACK, EMBEDDED after async embed), V (gRPC — the ONLY transport, by design); Storage discipline (in-memory MVP → no new prefix, no migration, `ExpectedVersion` unchanged; the plan explicitly scopes the Pebble-log follow-on as migration-gated). **Note this is the first feature that does NOT project to all four transports** — justify the gRPC-only scope (streaming doesn't map to unary transports; REST push = BL-011, separate).
