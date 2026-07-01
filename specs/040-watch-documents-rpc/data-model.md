# Data Model — WatchDocuments (BL-008)

> Phase 1 output for `/speckit-plan`. Introduces one **in-memory** internal entity (the `EventBus`) + the transient `DocumentEvent` payload. No persisted state. See [research.md](./research.md) (R1–R5) for the design decisions and [contracts/api.md](./contracts/api.md) for the wire shape.

## Entity: `EventBus` (internal, new — in-memory, NOT persisted)

`internal/events/` — go-rag's first in-process pub-sub. Owned by the `Engine` (created at `engine.Open`, closed on `engine.Close`); the pipeline + watcher publish; the gRPC adapter subscribes.

```go
// DocumentEventType is the lifecycle event kind (spec 040). RE_INGESTED is reserved
// for BL-010 (before/after delta) and is NOT emitted by the MVP.
type DocumentEventType int // { INGESTED, EMBEDDED, DELETED } (RE_INGESTED reserved)

// DocumentEvent is one lifecycle event — the in-memory payload + its sequence.
// The wire projection (proto DocumentEvent) drops the internal Seq (it becomes the
// encoded cursor).
type DocumentEvent struct {
	Type       DocumentEventType
	DocumentID string
	SourcePath string
	Seq        uint64 // monotonic per-process; encoded into the wire `cursor`
	After      model.Document
	TimestampMs int64
}

// Bus is the in-process event bus (channel-per-subscriber, drop-behind).
type Bus struct {
	mu          sync.RWMutex
	next        atomic.Uint64
	subs        map[uint64]*subscriber // subscriberID → subscriber
	nextSubID   uint64
}
type subscriber struct {
	ch      chan DocumentEvent
	dropped atomic.Uint64  // events dropped for this subscriber (buffer full)
}
```

**Bus invariants**

| Property | Rule |
|----------|------|
| Ordering | events are published with a strictly increasing `Seq` (atomic counter) |
| Publish  | non-blocking — `select { case ch <- ev: default: drop }`; a full buffer drops for THAT subscriber only, bumps `dropped`, logs (rate-limited); never blocks the caller |
| Subscribe | returns `(ch <-chan DocumentEvent, nextSeq uint64, unsub func())`; `nextSeq` = the next Seq the bus will publish (for from-now); the channel is buffered (cap = 64) |
| Unsubscribe | removes the subscriber; safe to call multiple times; the caller drains or abandons `ch` |
| Fan-out  | one Publish reaches every live subscriber |
| Persistence | NONE — the bus + its in-flight events live only in memory |

## Entity: `DocumentEvent` / wire projection

`DocumentEvent` is the proto wire shape (contracts/api.md). The internal `Seq` is encoded into the wire `cursor` (base64-url of the uint64); the wire projection reuses `toDocumentMetaPB` for the `after` field.

**Cursor semantics**
- `cursor = base64-url-no-pad(seq)`.
- `WatchRequest.cursor` non-empty + decodes to `S` → resume: deliver events with `Seq > S` still in the bus's in-flight window (within buffer; older dropped events are silently fast-forwarded — R2 honest limitation).
- `cursor = ""` / unrecognized → **from-now** (subscribe to future events only).

## Resolution flow (the streaming handler)

```
WatchDocuments(req, stream):
    ch, nextSeq, unsub := bus.Subscribe(buf=64)
    defer unsub()
    startSeq := nextSeq                                       // from-now default
    if req.Cursor != "" {
        if s, ok := decode(req.Cursor); ok { startSeq = s+1 } // resume after cursor
        // else: unrecognized → from-now (graceful, no error)
    }
    for {
        select {
        case ev := <-ch:
            if ev.Seq < startSeq { continue }                 // skip already-passed
            if err := stream.Send(toEventProto(ev)); err != nil { return err } // client gone
        case <-stream.Context().Done(): return nil             // client disconnect/cancel
        }
    }
```

**Cost**: publish = O(subscribers) non-blocking sends (each a `select/default`); subscribe/unsubscribe = O(1) under the mutex. The Send blocks only that subscriber's handler goroutine on a slow network client — never the publisher or other subscribers.

**Concurrency model**: the bus is safe for concurrent publishers (pipeline + watcher) + concurrent subscribers (many watch streams). Single-engine, single-vault — one bus per engine. The Pebble writer (single-writer constitution) is unaffected — publishing does not touch Pebble.

## Reused entities (unchanged)

- **Document / DocumentMeta** — the `after` field of INGESTED/EMBEDDED events is the same `DocumentMeta` projection GetChunk/ListDocuments return (spec 035/039). Reused verbatim.
- **Pipeline / Watcher lifecycle hooks** — `processFile` (INGESTED), `Pipeline.OnNotifyEmbed` (EMBEDDED), `ChangeDetector` delete (DELETED) are existing emit points; this spec wires them to `bus.Publish`. No new detection logic.

## Identity & storage invariants (constitution Principle II)

- **No new persisted state.** The bus + DocumentEvent payloads are in-memory only. No new key, no new prefix, no persisted struct.
- **On-disk layout**: unchanged. No migration; `migrate.ExpectedVersion` unchanged (FR-016).
- **Identity**: read-side observability — events describe existing documents; no `document_id` is created or changed.

## Validation rules (map to FRs)

- **FR-001**: `WatchDocuments` returns `stream DocumentEvent`.
- **FR-002..004**: INGESTED/EMBEDDED/DELETED emitted from the three lifecycle hooks (R5).
- **FR-005**: every event carries an opaque `cursor` (R3).
- **FR-006**: non-empty cursor resumes strictly after it (within the in-flight window).
- **FR-007**: empty cursor = from-now.
- **FR-008**: unrecognized/expired cursor = from-now (graceful).
- **FR-009**: gRPC-server-streaming only.
- **FR-010**: concurrent subscribers each receive events (R1 isolation).
- **FR-011**: slow consumer → drop-behind (R2); publisher + others unaffected.
- **FR-012**: no `vault` field.
- **FR-013**: local in-process bus (Principle I).
- **FR-014**: INGESTED after durable commit; EMBEDDED after async embed (Principle IV).
- **FR-015**: pure Go, no new deps.
- **FR-016**: in-memory → no migration.
