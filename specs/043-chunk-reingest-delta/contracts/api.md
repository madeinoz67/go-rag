# Contract — `RE_INGESTED` + chunk deltas (spec 043, BL-010)

> Phase 1 output for `/speckit-plan`. Extends the spec 040 `WatchDocuments` stream. gRPC-server-streaming **only** (same justified deviation as the rest of `WatchDocuments` — streaming has no unary REST/MCP/CLI equivalent; the bridge, the consumer, uses gRPC). See [data-model.md](../data-model.md) for the entity semantics + [research.md](../research.md) for the decisions.

## Operation contract

| Property | Value |
|----------|-------|
| Name | `RE_INGESTED` (a new `DocumentEventType` value on the existing `WatchDocuments` stream — NOT a new RPC) |
| Trigger | a re-ingest: a source path that already maps to a committed document is re-ingested with changed content |
| Stream payload | one `DocumentEvent` (type=`RE_INGESTED`) carrying the full per-chunk delta + the old→new chunk-ID map |
| Ordering | **replaces** the `INGESTED(new)` + `DELETED(old)` pair a re-ingest surfaces today (spec 040) — exactly one `RE_INGESTED` per re-ingested document, no `INGESTED`/`DELETED` for it |
| Embed-skip | go-rag skips embedding generation for `UNCHANGED` chunks when the embedding baseline is unchanged (an internal optimization — invisible on the wire; the event carries the delta regardless) |
| Transport | gRPC only. No REST/MCP/CLI surface (same as `WatchDocuments`). |

## Wire additions to `proto/gorag.proto`

`DocumentEventType_RE_INGESTED = 2` is **already reserved** (spec 040 emitted only `INGESTED`/`EMBEDDED`/`DELETED`; `RE_INGESTED` was the reserved-for-BL-010 slot). Two **additive** changes:

```proto
// A per-chunk change in a RE_INGESTED event (spec 043 / BL-010).
message ChunkDelta {
  enum ChangeType {
    ADDED     = 0; // new version chunk with no content-match in the old
    REMOVED   = 1; // old version chunk with no content-match in the new
    UNCHANGED = 2; // content-match; prev_chunk_id -> chunk_id is the remap
  }
  ChangeType change_type  = 1;
  string     chunk_id     = 2; // the NEW chunk's id (ADDED + UNCHANGED)
  string     prev_chunk_id = 3; // the OLD chunk's id (UNCHANGED + REMOVED)
}

message DocumentEvent {
  // ...existing fields 1-6 (type, document_id, source_path, cursor, after, timestamp_ms)...
  DocumentEventType type          = 1;
  string            document_id   = 2;
  string            source_path   = 3;
  string            cursor        = 4;
  DocumentMeta      after         = 5;
  int64             timestamp_ms  = 6;
  // spec 043 / BL-010: the chunk delta. Present only on RE_INGESTED events
  // (empty for INGESTED/EMBEDDED/DELETED). The bridge uses prev_chunk_id of
  // UNCHANGED/REMOVED entries to remap its stored chunk_id references.
  repeated ChunkDelta chunk_deltas = 7;
}
```

Regenerate `proto/gen` (`protoc -I proto --go_out=. --go_opt=module=github.com/madeinoz67/go-rag --go-grpc_out=. --go-grpc_opt=module=github.com/madeinoz67/go-rag proto/gorag.proto`).

## Engine (canonical) — the diff

The engine computes the delta (a pure multiset diff over `ContentHash`, [data-model.md](../data-model.md)) during the re-ingest reorder, threads it into the `RE_INGESTED` `DocumentEvent`, and publishes via the existing `events.Bus` (`EventReingested`, already mapped by `toEventTypePB` in `internal/grpc/watch_documents.go`). The gRPC `WatchDocuments` handler (spec 040) projects it unchanged — no handler change beyond the proto field surfacing.

## Errors

- **No call-level error** for a re-ingest that yields zero changes (a metadata-only edit) — a `RE_INGESTED` with all-`UNCHANGED` deltas is emitted; the consumer may no-op.
- A re-ingest where the prior version's chunks were already gone (a race / concurrent re-ingest) yields the **conservative** delta (treat as `ADDED` rather than miss a change) — never an error.

## Backward compatibility

- Pure-additive proto (one reserved enum value goes live + one new `repeated` field + one new message). No existing field's tag/type/value changes.
- The v2 migration backfills `ContentHash` (a value-encoding change to `PrefixChunk` records); `migrate.ExpectedVersion` 1→2; no new on-disk prefix (Constitution: Storage discipline — compliant).
- Existing unary RPCs + the `INGESTED`/`EMBEDDED`/`DELETED` events are unaffected.
