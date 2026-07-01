# Implementation Plan: WatchDocuments (BL-008)

**Branch**: `040-watch-documents-rpc` *(single-author repo — work commits to `main`; slug identifies the spec, not a git branch)* | **Date**: 2026-07-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/040-watch-documents-rpc/spec.md` (bridge backlog item BL-008).

## Summary

`WatchDocuments(WatchRequest) returns (stream DocumentEvent)` — a long-lived gRPC server-streaming RPC that delivers document lifecycle events (`INGESTED` / `EMBEDDED` / `DELETED`) as they happen, replacing the bridge's 60s poll (BL-007) with sub-second push. Events carry an opaque cursor for crash-recovery resume. This is go-rag's **first streaming RPC** and **first internal event bus** — architecturally the biggest backlog item.

The MVP delivers the stream + three event types + cursor resume **within a process lifetime**, backed by an **in-memory channel-per-subscriber event bus** (non-blocking publish, drop-behind overflow). No persisted state, no migration. The harder bits — `RE_INGESTED` (BL-010), retention window / `OUT_OF_RANGE`, cross-restart resume (Pebble-backed event log → migration), keepalive tuning, load test — are explicitly deferred (spec Out of Scope). BL-009 (`EMBEDDED`) is absorbed here (its enum value + the `OnNotifyEmbed` emit point).

**Deliberate transport deviation:** `WatchDocuments` is **gRPC-server-streaming only** — the first go-rag operation not on all four transports. Streaming doesn't map to unary REST/MCP/CLI; REST's push equivalent is BL-011 (webhook, separate).

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra, pebble, grpc-go (server-streaming is a grpc-go feature already in the dep tree), protobuf. No new dependencies.

**Storage**: Pebble KV (read-only for this feature — the event bus is in-memory). No new key, no new prefix, no migration.

**Testing**: `go test -race -cover ./...`. New: a streaming-RPC test over bufconn (events in order + latency; reconnect-with-cursor; concurrent streams; slow-consumer isolation).

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine.

**Performance Goals**: event delivery ≤ ~500ms (in-process fan-out, no network on the event path); publish is non-blocking (does not enter the <10ms write-ACK path — Principle IV).

**Constraints**: pure Go; no schema migration (in-memory MVP); gRPC-server-streaming only; cursor resume within process lifetime; single-vault.

**Scale/Scope**: **size M** — one new internal event-bus package + one streaming RPC + three lifecycle emit-hooks + a streaming test. The streaming RPC + the event bus are both new patterns for go-rag (the Phase-1 items were thin projections of existing engine methods; this introduces new infrastructure).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | In-process event bus; no network egress for event delivery. |
| **II. Content-Addressed Identity** | ✅ PASS | Read-side observability of writes — no identity created or changed; events describe existing documents. |
| **III. Pure Go — No CGo** | ✅ PASS | grpc-go server-streaming is already a dep; no new deps, no CGo. |
| **IV. Async-After-ACK Writes** | ✅ PASS | Publish is non-blocking (R1 — never enters the write-ACK path). `INGESTED` fires AFTER the durable commit; `EMBEDDED` after async embed. The <10ms write budget is unaffected. |
| **V. Extension by Interface, MCP-First** | ✅ PASS (justified deviation) | `WatchDocuments` is gRPC-server-streaming ONLY — the first operation not on all four transports. **Justification:** streaming does not map to REST/MCP/CLI's unary request/response model; the bridge (the consumer) uses gRPC. REST's push equivalent is BL-011 (webhook, a separate spec); MCP notifications + CLI streaming are separate concerns. Principle V's *intent* (every operation reachable by agents + humans) is preserved by the existing unary surface; this streaming op serves a gRPC consumer that has no unary equivalent. |
| **Storage discipline / Schema evolution** | ✅ PASS | In-memory event bus → no new/retired prefix, no key-construction change, no migration, `migrate.ExpectedVersion` unchanged. **The Pebble-backed event-log follow-on (cross-restart resume) is explicitly migration-gated** — out of MVP scope (FR-016). |

No violations. No Complexity Tracking entries needed (the in-memory MVP has zero schema impact; the Pebble follow-on would trigger the schema-evolution gate when taken).

## Project Structure

### Documentation (this feature)

```text
specs/040-watch-documents-rpc/
├── plan.md              # this file
├── research.md          # Phase 0 — event-bus/backpressure/cursor/streaming/hooks (R1–R5)
├── data-model.md        # Phase 1 — DocumentEvent, EventBus, cursor, subscriber lifecycle
├── quickstart.md        # Phase 1 — runnable validation (streaming scenarios)
├── contracts/
│   └── api.md           # Phase 1 — wire contract (proto streaming rpc + enum + messages)
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root — files touched)

```text
internal/events/ (new pkg)            # EventBus: Subscribe/Publish, channel-per-subscriber, drop-behind
internal/engine/events.go (new)       # Engine owns the EventBus; lifecycle publish helpers
internal/pipeline/pipeline.go         # processFile: publish INGESTED after durable commit; OnNotifyEmbed → EMBEDDED
internal/watcher/*.go                  # ChangeDetector delete path: publish DELETED
internal/grpc/watch_documents.go (new)# WatchDocuments streaming handler (ServerStream.Send + ctx.Done unsubscribe)
proto/gorag.proto                     # + rpc WatchDocuments(stream); + WatchRequest + DocumentEvent + DocumentEventType enum
proto/gen/                            # regenerated (protoc) — first streaming service method
internal/grpc/server.go (or adapter)  # register the streaming handler (grpc.ServiceDesc already generated)
```

**Structure Decision**: a new `internal/events` package owns the `EventBus` (pure concurrency primitive, testable in isolation); the engine owns an instance; the pipeline + watcher publish through it (via an injected publish callback or a bus reference); the gRPC adapter subscribes. The proto change is additive (one streaming rpc + one enum + two messages). No new Pebble prefix.

## Phase Status

- **Phase 0 (Research)** — ✅ complete → [research.md](./research.md). The five Research-Note forks are resolved: R1 event-bus = channel-per-subscriber; R2 backpressure = drop-behind (with the honest in-memory lossy-resume limitation documented); R3 cursor = base64 monotonic sequence; R4 streaming = grpc ServerStream + ctx.Done; R5 hooks = processFile / OnNotifyEmbed / ChangeDetector. No `NEEDS CLARIFICATION` remains.
- **Phase 1 (Design & Contracts)** — ✅ complete → [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md).
- **Phase 2 (Tasks)** — ⏭ next: `/speckit-tasks`.
