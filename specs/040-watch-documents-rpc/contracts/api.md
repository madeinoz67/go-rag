# Contract — `WatchDocuments` (spec 040, BL-008)

> Phase 1 output for `/speckit-plan`. go-rag's **first streaming RPC** + **first enum**. gRPC-server-streaming **only** (deliberate — see Transport Scope). See [data-model.md](../data-model.md) for the event-bus + cursor rules and [research.md](../research.md) (R1–R5) for the decisions.

## Operation contract

| Property | Value |
|----------|-------|
| Name | `WatchDocuments` |
| Direction | **server-streaming** (client → 1 request; server → N `DocumentEvent`s over time) |
| Request | `WatchRequest { cursor }` (cursor opaque; empty = from-now) |
| Stream | `DocumentEvent`s delivered as lifecycle changes occur, each carrying an opaque `cursor` |
| Events (MVP) | `INGESTED` (durable commit), `EMBEDDED` (async embed done), `DELETED` (scan-detected deletion) |
| Latency | each event delivered within ~500ms of its lifecycle change (in-process fan-out) |
| Cursor resume | non-empty recognized cursor → events after it (within the in-flight window); empty / unrecognized → from-now (graceful, never an error) |
| Concurrency | multiple concurrent streams each receive the same events; a slow consumer is isolated (drop-behind) |
| Transport | **gRPC only** (server-streaming). No REST/MCP/CLI surface in this spec. |
| Persistence | none — in-memory event bus; resume works within a process lifetime |

**MVP limitations (documented honestly):** (1) a slow consumer can lose events irrecoverably from the stream (drop-behind; the `ListDocuments` BL-007 poll is the lossless fallback); (2) cursor resume only covers events still in the bus's in-flight window — a long disconnect fast-forwards; (3) cross-restart resume is NOT supported (the bus + counter reset on restart → a stale cursor is treated as from-now). A Pebble-backed event log (follow-on, migration-gated) removes all three.

---

## Transport scope — gRPC-server-streaming ONLY

`WatchDocuments` is the **first go-rag operation that is not on all four transports.** Streaming does not map to the unary request/response model of REST/MCP/CLI:

- **gRPC**: native server-streaming (this spec). The bridge (the consumer) uses gRPC.
- **REST**: push equivalent is **BL-011 (webhook)** — a separate spec (server POSTs to a registered URL). SSE is another option; both are out of scope here.
- **MCP**: MCP has a notifications mechanism (server-initiated), but it's a separate concern; out of scope.
- **CLI**: no meaningful streaming surface; out of scope.

Principle V's *intent* (every operation reachable by humans + agents) is preserved by the existing unary surface (Query, GetChunk, ListDocuments, …); this streaming op serves a long-lived gRPC consumer that has no unary equivalent.

---

## Protobuf — `proto/gorag.proto`

Add the streaming RPC (after `ListDocuments`, before the closing `}`) + the enum + the messages:

```proto
service Gorag {
  // ...existing RPCs...
  rpc ListDocuments(ListDocumentsRequest) returns (ListDocumentsResponse);
  // spec 040 (BL-008): long-lived server-stream of document lifecycle events
  // (INGESTED/EMBEDDED/DELETED). → engine's event bus (also the foundation for a
  // future REST webhook BL-011). gRPC-only — streaming has no unary equivalent.
  rpc WatchDocuments(WatchRequest) returns (stream DocumentEvent);
}

// spec 040 (BL-008): lifecycle event kinds. RE_INGESTED is reserved for BL-010
// (before/after delta) and is NOT emitted by the MVP.
enum DocumentEventType {
  INGESTED    = 0; // metadata durably committed; embedding may still be pending
  EMBEDDED    = 1; // async embedding complete — safe to promote
  RE_INGESTED = 2; // reserved (BL-010 — not emitted in the MVP)
  DELETED     = 3;
}

message WatchRequest {
  string cursor = 1; // opaque resume token; empty = start from now (no replay)
}

message DocumentEvent {
  DocumentEventType type      = 1;
  string            document_id = 2;
  string            source_path = 3;
  string            cursor      = 4; // opaque; persist + pass on reconnect
  DocumentMeta      after       = 5; // INGESTED/EMBEDDED: the document version
  int64             timestamp_ms = 6;
}
```

Regenerate `proto/gen` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`). grpc-go generates a `Gorag_WatchDocumentsServer` streaming interface (the handler signature takes the stream, not a response). Wire the handler in `internal/grpc/watch_documents.go`.

## Engine (canonical) — the event bus

The engine owns an `internal/events.Bus`. `Engine.PublishEvent(DocumentEvent)` (or the bus reference) is wired into the lifecycle hooks (research.md R5). The gRPC adapter's `WatchDocuments` handler `Subscribe`s + drains to the stream. There is no unary engine method to "call" — the streaming handler subscribes to the bus directly.

## Errors

- **No call-level error** for missing/expired cursors — they're treated as from-now (graceful).
- **Stream errors**: `Send` returns an error if the client disconnects → the handler returns (unsubscribes). The client reconnects with its last cursor.
- The stream stays open until the client cancels/disconnects or the server shuts down.

---

## Parity & determinism

- **Parity**: there is only ONE transport (gRPC), so cross-transport parity does not apply to `WatchDocuments` itself. (The event payloads reuse the spec-035 `DocumentMeta` projection, consistent with the rest of the surface.)
- **Determinism**: per-document events are delivered in lifecycle order (INGESTED before EMBEDDED for the same document); across documents, in publish (Seq) order. Subscribers at different paces see the same order up to their own drop point.

## Backward compatibility

- Pure-additive streaming RPC + enum + messages. No existing field's tag, type, or value changes. (`RE_INGESTED = 2` is reserved but unused.)
- No on-disk layout change; no migration; `migrate.ExpectedVersion` unchanged.
- Existing unary RPCs are unaffected — they simply don't stream.
