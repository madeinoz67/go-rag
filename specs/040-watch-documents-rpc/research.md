# Research — WatchDocuments (BL-008)

> Phase 0 output for `/speckit-plan`. Resolves the spec's Research-Note forks (event-bus mechanism, backpressure, cursor) plus the streaming RPC mechanics + the lifecycle emit hooks. Grounded in the spec's constraints (in-memory MVP, no migration, gRPC-only) and a direct read of `internal/pipeline/pipeline.go` (`processFile` commit + `OnNotifyEmbed`), `internal/watcher`, and grpc-go's server-streaming API.

## R1 — Event-bus mechanism: in-memory channel-per-subscriber registry

**Decision**: a central `EventBus` (new `internal/events` package) holds a `sync.RWMutex`-guarded `map[subscriberID]*subscriber`, where each `subscriber` owns a buffered `chan DocumentEvent`. `Publish(ev)` fans out a **non-blocking** send to every subscriber's channel (`select { case ch <- ev: default: drop }`). `Subscribe(buf int)` returns `(ch, seq, unsubscribe)`. The bus is owned by the `Engine` (created at engine open, closed on engine close); the pipeline + watcher publish through it.

**Rationale**:
- **Idiomatic Go in-process pub-sub** — a channel-per-subscriber with a registry is the standard, minimal pattern; no third-party dep.
- **Subscriber isolation** — each subscriber drains at its own pace from its own channel; one slow reader cannot stall another.
- **Non-blocking publish** — the `select/default` send means a full subscriber buffer never blocks the publisher (the ingest/embed/scan path). This is load-bearing for Constitution Principle IV (the publish must NOT enter the <10ms write-ACK path).
- **No persisted state** — the bus lives in memory; no new Pebble prefix, no migration (the MVP default, FR-016).

**Alternatives considered**:
- *Single shared ring-buffer read by all subscribers at their own pace* — needs a head/tail per subscriber ≈ the same bookkeeping as channel-per-subscriber, but with manual synchronization. Rejected — channels already give that, cleaner.
- *Pebble-backed append-only event log* — enables cross-restart cursor resume + a retention window, but adds a new key prefix (schema-evolution gate → migration) + write-amplification on the write path. Explicitly a **follow-on** (the plan scopes it as migration-gated); out of MVP scope.

## R2 — Backpressure / overflow: drop-behind per subscriber

**Decision**: each subscriber channel has a fixed capacity (recommended **64** events). When `Publish` finds a subscriber's channel full, it **drops the new event for that subscriber only** (the `select/default` no-op), bumps that subscriber's `dropped` counter, and logs a warning (rate-limited). The publisher and every other subscriber are unaffected.

**Rationale**: a slow consumer misses events but never stalls the system; the bounded buffer caps memory; the drop is per-subscriber (no cross-effect). Drop-behind is gentler than disconnecting the slow subscriber (which forces reconnect churn).

**⚠ Honest limitation (documented in the spec + quickstart):** with an in-memory bus + drop-behind, **dropped events are gone** — a slow consumer cannot recover them via cursor-resume (the cursor only encodes the sequence of events the bus DID publish/deliver-to-buffer; once dropped from a buffer, the event exists nowhere). The bridge's **safety net is the `ListDocuments` (BL-007) poll** — eventual consistency covers the gap (a missed `INGESTED` is caught by the next poll's `after`-cursor). A lossless cursor-resume requires the **Pebble-backed event log** (the migration-gated follow-on). The MVP trades lossless resume for no-migration simplicity; the bridge already polls as a fallback, so this is acceptable for the MVP.

**Alternatives considered**:
- *Disconnect the slow subscriber* (close its channel, force a reconnect) — forces churn + a reconnect storm under sustained slowness; drop-behind degrades more gracefully. Rejected for the MVP.
- *Unbounded buffer* — memory blowup under a stuck consumer. Forbidden.

## R3 — Cursor: opaque base64 of a monotonic sequence

**Decision**: the bus holds an `atomic.Uint64` sequence counter (starts at 1 per process). Each published event gets `seq = next()`, and its `cursor = base64-url-no-pad(fmt(seq))`. `WatchRequest.cursor` non-empty + recognized → resume delivers events with `seq > decode(cursor)` (delivered from the bus's in-flight set; see R4 for the within-process-lifetime caveat). `cursor=""` or unrecognized → **from-now** (subscribe to future events only; no replay) — graceful, never an error (FR-008).

**Rationale**: the simplest total order; the cursor is opaque + URL-safe + trivially decodable for the resume check. Resume works **within a process lifetime** (the counter + the in-flight events live in memory). Cross-restart is out of scope: on restart the counter resets to 1, so a cursor from a prior process is "unrecognized" → from-now (the MVP's graceful behavior).

**Alternatives considered**:
- *`<timestamp>,<document_id>` cursor* (a la spec 039's page_token) — works but ties the cursor to wall-clock; a monotonic sequence is simpler + strictly ordered. Rejected.
- *A Pebble-stored sequence + event log* — cross-restart resume; migration-gated follow-on. Rejected for the MVP.

## R4 — Streaming RPC mechanics (new pattern for go-rag)

**Decision**: the gRPC handler is `func (a *Adapter) WatchDocuments(req *goragpb.WatchRequest, stream goragpb.Gorag_WatchDocumentsServer) error` (grpc-go generates the server-streamer interface). Flow:
1. `ch, startSeq, unsub := bus.Subscribe(buf)`; `defer unsub()`.
2. If `req.Cursor` non-empty + decodes to seq `S` → set `startSeq = S+1` (resume: deliver events with seq ≥ startSeq that are still in the bus's in-flight window). If `""` / unrecognized → `startSeq = bus.NextSeq()` (from-now).
3. **Send loop**: `for { select { case ev := <-ch: if ev.Seq < startSeq { continue }; if err := stream.Send(evProto); err != nil { return err }; case <-stream.Context().Done(): return nil } }`.
4. On return (Send error or ctx-done), `defer unsub()` removes the subscriber (no leak).

grpc's **default keepalive** for the MVP (the 30s-ping / 1h-idle-survival targets are deferred hardening — Out of Scope).

**Rationale**: standard grpc-go server-streaming; `stream.Context().Done()` drives unsubscribe on client disconnect/cancel; no goroutine leak. The `Send` is the only blocking point (it blocks on a slow network client) — but it blocks ONLY that subscriber's handler goroutine, not the publisher or other subscribers (R1 isolation).

**Caveat (the resume window):** because the bus is in-memory + drop-behind (R2), resume can only redeliver events still buffered in the bus since the cursor. If the subscriber was disconnected long enough for >buffer events to publish, the older ones are dropped — resume silently fast-forwards (the `ListDocuments` poll is the lossless fallback). This is the honest MVP behavior; documented in quickstart.

**Alternatives considered**:
- *A separate replay goroutine per subscriber reading a persisted log* — requires the Pebble log (migration). Rejected for the MVP.
- *Long-polling over a unary RPC* — not a true stream; the bridge wants a persistent connection. Rejected.

## R5 — Lifecycle emit hooks (the publish points)

**Decision**: wire `bus.Publish` into the three existing lifecycle points. The publish is **non-blocking** (R1), so it never stalls the write path.

- **`INGESTED`** — in `internal/pipeline/pipeline.go` `processFile`, **after** the durable Pebble commit (the fsync of the document record), before returning. Publish `{type: INGESTED, document_id, source_path, cursor: bus.NextSeq(), after: docMeta, timestamp_ms: now}`. (After the ACK → Principle IV.)
- **`EMBEDDED`** — via the existing **`Pipeline.OnNotifyEmbed`** callback hook (`pipeline.go:89`), which already fires on embed completion. Wire the engine to set this callback to `Publish({EMBEDDED, document_id, ...})` when it owns the bus. (After async embed → Principle IV.)
- **`DELETED`** — in the watcher's `ChangeDetector` (`internal/watcher`) deletion path. Publish `{DELETED, document_id, source_path, ...}`.

The engine owns the `EventBus` (created at `engine.Open`, closed on `engine.Close`); the pipeline + watcher receive a publish callback (or a `*events.Bus` reference) to emit. This keeps the bus lifecycle tied to the engine (one bus per engine/vault).

**Rationale**: reuses the lifecycle signals that already exist (`processFile` commit, `OnNotifyEmbed`, `ChangeDetector` delete) — no new detection logic. The non-blocking publish means zero impact on the write path's latency.

**Alternatives considered**:
- *Polling Pebble for changes inside the bus* — adds latency + complexity vs hooking the existing signals. Rejected.
- *A separate audit-log→stream tailer* — the audit log (`internal/audit`) is append-only but not structured for streaming + lacks EMBEDDED. Rejected (the hooks are cleaner).
