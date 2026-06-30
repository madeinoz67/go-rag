# go-rag — Bridge Integration Backlog

> **Author**: Stephen Eaton  
> **Date**: 2026-06-25  
> **Repo**: `github.com/madeinoz67/go-rag`  
> **Source**: Derived from `go-rag-bridge-feature-brief.md`  
> **Labels used**: `api` `grpc` `streaming` `metadata` `bridge` `enhancement` `feat`

> **POST-REVIEW UPDATE (2026-06-30):** MuninnDB maintainer approved the bridge. **None of these BL items are blocked by MuninnDB** — Stream A (go-rag side) is fully unblocked and independent of the upstream timeline. Priority call from the maintainer: the **Obsidian wikilink → `Link` pipeline** is "the best idea in the RFC" — that bumps **BL-004** (expose wikilink targets in `Chunk.metadata`) to the headline enabler, with Hebbian edges written at weight **0.6–0.8** (on-query co-retrieval edges: **0.1–0.2**). Other maintainer invariants that bind the future bridge consumer: send `embedding: nil`, set `stability: 30.0` on chunk engrams, use gRPC (not MBP) for v1. Full mapping + the two independent work streams: see [`bridge-map-post-review.md`](./bridge-map-post-review.md). **Upstream:** the [#556](https://github.com/scrypster/muninndb/issues/556) UPSERT proto comment was posted 2026-06-30 — tracked in [`muninndb-bridge-backlog.md`](./muninndb-bridge-backlog.md); no BL item is blocked by it.

Backlog items required for effective integration between go-rag and MuninnDB via the `go-rag-muninn-bridge`. Items are ordered within each phase by implementation dependency — earlier items unblock later ones.

---

## Summary

| ID | Title | Priority | Size | Phase | Status |
|---|---|---|---|---|---|
| [BL-001](#bl-001) | `GetChunk` RPC — fetch single chunk by ID | P1 | S | 1 | open |
| [BL-002](#bl-002) | `GetChunkContext` RPC — chunk with surrounding window | P1 | S | 1 | open |
| [BL-003](#bl-003) | `BatchGetChunks` RPC — fetch up to 100 chunks in one call | P1 | S | 1 | open |
| [BL-004](#bl-004) | Expose wikilink targets in `Chunk` metadata | P1 | S | 1 | open ⭐ maintainer's priority pick — wikilink→`Link` enabler |
| [BL-005](#bl-005) | Expose section heading in `Chunk` metadata | P1 | S | 1 | open |
| [BL-006](#bl-006) | Expose extraction quality score in `Chunk` metadata | P1 | S | 1 | open |
| [BL-007](#bl-007) | `ListDocuments` — reliable `ingested_at` cursor + `status` filter | P1 | S | 1 | open |
| [BL-008](#bl-008) | `WatchDocuments` — gRPC server-streaming document event stream | P2 | M | 2 | open |
| [BL-009](#bl-009) | `EMBEDDED` event type — distinct from `INGESTED` | P2 | S | 2 | open |
| [BL-010](#bl-010) | Chunk delta in `RE_INGESTED` events | P2 | M | 2 | open |
| [BL-011](#bl-011) | REST webhook registration for document lifecycle events | P2 | M | 2 | open |
| [BL-012](#bl-012) | `QueryStream` — gRPC streaming query results | P3 | M | 3 | open |
| [BL-013](#bl-013) | `RecordUsage` RPC — chunk usage feedback | P3 | S | 3 | open |
| [BL-014](#bl-014) | `FederatedQuery` RPC — single-call multi-vault query | P3 | M | 3 | open |
| [BL-015](#bl-015) | Named entity metadata in `Chunk` response | P3 | L | 3 | open |
| [BL-016](#bl-016) | Soft-delete document records on re-ingest | P4 | M | 4 | open |
| [BL-017](#bl-017) | `GetDocumentHistory` RPC — version history by source path | P4 | S | 4 | open |
| [BL-018](#bl-018) | Chunk content diff on re-ingest | P4 | M | 4 | open |

**Size key**: S = hours–1 day · M = 2–5 days · L = 1–2 weeks  
**Priority key**: P1 = blocks bridge core · P2 = replaces polling with push · P3 = query quality & feedback · P4 = full lifecycle

---

## Phase 1 — Unblock the bridge core

Items that must exist before the bridge can function reliably. All are small changes; most expose data go-rag already holds internally.

---

### BL-001

**`GetChunk` RPC — fetch single chunk by its content-addressed ID**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge's `ActivateWithRAG` pattern receives a MuninnDB engram that carries `metadata["chunk_id"]`. It needs to verify the chunk still exists in go-rag and optionally re-promote it. There is currently no way to fetch a chunk by its `chunk_id` — only by listing all chunks on a document. `GetChunk` is the missing primitive that makes `chunk_id` a usable foreign key from MuninnDB back into go-rag.

#### Proto

```protobuf
rpc GetChunk(GetChunkRequest) returns (GetChunkResponse);

message GetChunkRequest {
  string chunk_id = 1;
  string vault    = 2;
}

message GetChunkResponse {
  Chunk        chunk    = 1;
  DocumentMeta document = 2;
}
```

#### Acceptance criteria

- [ ] `GetChunk` returns the full `Chunk` struct and its parent `DocumentMeta` for a valid `chunk_id`
- [ ] Returns `NOT_FOUND` gRPC status if `chunk_id` does not exist in the vault
- [ ] Returns `NOT_FOUND` if `chunk_id` exists but belongs to a different vault
- [ ] Response includes all metadata fields (see BL-004, BL-005, BL-006 for metadata field requirements)
- [ ] REST equivalent: `GET /api/vaults/{vault}/chunks/{chunk_id}`
- [ ] Round-trip test: ingest a document, retrieve a chunk ID from `GetDocument`, fetch it via `GetChunk`, assert content matches

#### Dependencies

None — can be implemented independently.

#### Bridge integration value

Unblocks `ActivateWithRAG` (pattern D1). Enables bridge idempotency recovery when state store is lost.

> **RESOLVED by spec 035** (`/speckit-implement`, 2026-06-30). Two deltas from the
> draft above, grounded in the engine's actual conventions (see
> [`specs/035-get-chunk-rpc/research.md`](../../specs/035-get-chunk-rpc/research.md)):
>
> 1. **No `vault` field** on `GetChunkRequest`. The engine is single-vault-per-
>    process — every chunk-scoped RPC (`ReleaseChunk`/`ResetChunk`) takes only
>    `chunk_id`. The bridge connects to the daemon bound to the target vault
>    (`--vault`/`--db-path`), so `vault` is a connection-time concern, not a
>    per-call field. `GetChunkRequest { string chunk_id = 1; }`.
> 2. **REST path is `GET /v1/chunks/{id}`** — NOT `/api/vaults/{vault}/chunks/{chunk_id}`.
>    The existing REST API uses `/v1/<resource>` with `{id}` path params
>    (`/v1/poison/{id}/release`); there is no `/api/` base and no per-vault URL
>    segment on any route.
>
> The bridge consumer must send `chunk_id`-only requests and use `/v1/chunks/{id}`.
> Not-found surfaces as gRPC `NOT_FOUND` / HTTP 404 / MCP `-32001` / CLI non-zero.

---

### BL-002

**`GetChunkContext` RPC — fetch a chunk with its surrounding neighbours**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

`ActivateWithRAG` needs surrounding document context around a retrieved chunk — not just the chunk itself. The `Chunk` data model already has `PreviousChunkID` and `NextChunkID` fields forming a linked list through the document. `GetChunkContext` traverses that list and returns a window of N chunks on each side in a single call. Without this, the bridge must chain N individual `GetChunk` calls with sequential latency.

#### Proto

```protobuf
rpc GetChunkContext(GetChunkContextRequest) returns (GetChunkContextResponse);

message GetChunkContextRequest {
  string chunk_id = 1;
  string vault    = 2;
  int32  window   = 3; // Chunks before AND after target (default 2, max 10)
}

message GetChunkContextResponse {
  repeated Chunk chunks       = 1; // Ordered: [before…] [target] [after…]
  int32          target_index = 2; // Index of requested chunk_id in chunks[]
  DocumentMeta   document     = 3;
}
```

#### Acceptance criteria

- [ ] Returns `[window]` chunks before and `[window]` chunks after the target in document order
- [ ] `target_index` correctly identifies the requested chunk within the returned slice
- [ ] At document boundaries (first or last chunk), returns as many neighbours as exist — does not error
- [ ] `window=0` is equivalent to `GetChunk` — returns only the target chunk
- [ ] `window` capped at 10; values above 10 return an `INVALID_ARGUMENT` error with message
- [ ] All returned chunks include full metadata fields
- [ ] REST equivalent: `GET /api/vaults/{vault}/chunks/{chunk_id}/context?window=2`
- [ ] Single Pebble read transaction — not N sequential reads

#### Dependencies

BL-001 (`GetChunk` — the single-chunk read is the inner loop of this implementation)

#### Bridge integration value

Directly implements `ActivateWithRAG`. Reduces N sequential chunk fetches to 1 call.

---

### BL-003

**`BatchGetChunks` RPC — fetch up to 100 chunks by ID in one call**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge sync worker calls `GetDocument` to retrieve a document's chunk ID list, then needs the content of all those chunks to build `RememberItem` structs for MuninnDB's `BatchRemember`. With no batch fetch endpoint, the worker serialises one Pebble read per chunk. For a 50-chunk document this is 50 round-trips where one would do. `BatchGetChunks` resolves all chunk IDs in a single Pebble batch read.

#### Proto

```protobuf
rpc BatchGetChunks(BatchGetChunksRequest) returns (BatchGetChunksResponse);

message BatchGetChunksRequest {
  repeated string chunk_ids = 1; // Max 100
  string          vault     = 2;
}

message BatchGetChunksResult {
  string chunk_id = 1;
  Chunk  chunk    = 2; // Zero value if not found
  string error    = 3; // Non-empty if this chunk_id failed
}

message BatchGetChunksResponse {
  repeated BatchGetChunksResult results = 1; // Same order as request chunk_ids
}
```

#### Acceptance criteria

- [ ] Returns results in the same order as the request `chunk_ids` slice
- [ ] Missing chunk IDs return a result with empty `chunk` and `error = "not found"` — the call itself does not error
- [ ] Requests above 100 chunk IDs return `INVALID_ARGUMENT`
- [ ] Implemented as a single Pebble batch read, not N sequential reads
- [ ] REST equivalent: `POST /api/vaults/{vault}/chunks/batch` with JSON body `{"chunk_ids": [...]}`
- [ ] Benchmark: 100 chunks returned in under 20ms on a local Pebble instance

#### Dependencies

BL-001 (shares the single-chunk read logic)

#### Bridge integration value

Reduces sync worker call count by ~50x for average-sized documents. Required for efficient full-vault sync.

---

### BL-004

**Expose wikilink targets in `Chunk` metadata**

**Type**: `enhancement` `metadata` `bridge`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

go-rag's Obsidian-aware markdown reader already identifies `[[wikilink]]` targets during ingestion — they are used internally but not serialised into the chunk response. The bridge needs these to implement pattern E2 (Obsidian backlinks as Hebbian edges in MuninnDB) without re-parsing markdown files itself. Exposing wikilinks is a serialisation change to an existing computed value — no new computation required.

#### Specification

Add to `Chunk.metadata` map:

```
metadata["wikilinks"] = "[\"authentication\",\"JWT tokens\",\"RBAC\"]"
```

Value: JSON array of wikilink target strings (filename without `.md` extension), as encountered in the chunk text. Empty array `"[]"` if none found. Present on all chunks, not just markdown — other formats return `"[]"`.

The wikilink set is chunk-scoped (only links appearing in this chunk's text range), not document-scoped. A document-level union of all chunk wikilinks can be reconstructed by the consumer.

#### Acceptance criteria

- [ ] Markdown chunks containing `[[target]]` syntax include `metadata["wikilinks"]` as a valid JSON array string
- [ ] Links to non-existent files are included verbatim — the reader does not validate targets
- [ ] Aliased links `[[target|display]]` store the target (`target`), not the display text
- [ ] Embedded links `![[image.png]]` are excluded (image embeds are not knowledge graph edges)
- [ ] Non-markdown chunks (PDF, docx, txt) include `metadata["wikilinks"] = "[]"` for consistent consumer code
- [ ] Existing tests for the markdown reader are extended to assert wikilink metadata presence

#### Dependencies

None — the reader already computes this.

#### Bridge integration value

Directly implements E2 (Obsidian backlinks → MuninnDB Hebbian edges) without any bridge-side markdown parsing.

---

### BL-005

**Expose section heading in `Chunk` metadata**

**Type**: `enhancement` `metadata` `bridge`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The chunker knows which document section each chunk falls under — it splits at paragraph boundaries relative to the heading structure. This heading context is used to produce better chunk splits but is not preserved in the output. The bridge's concept extraction cascade relies on section headings as the primary label source (title → **heading** → filename). Without this field, the bridge has to heuristically scan chunk text for lines beginning with `#` — unreliable for docx and PDF.

#### Specification

Add to `Chunk.metadata` map:

```
metadata["section_heading"] = "## Token refresh flow"
metadata["section_depth"]   = "2"
```

`section_heading`: the full heading text including leading `#` characters (markdown) or equivalent for other formats. Empty string if the chunk does not fall under any heading.

`section_depth`: `"1"` for h1, `"2"` for h2, etc. `"0"` if no heading.

For PDF: use the PDF outline/bookmark entry that covers this page range, if present.  
For docx: use the paragraph marked with Heading style that precedes this chunk.

#### Acceptance criteria

- [ ] Markdown chunks carry `section_heading` matching the nearest preceding ATX heading (`# …` / `## …` / etc.)
- [ ] Chunks before any heading in a document carry `section_heading = ""` and `section_depth = "0"`
- [ ] PDF chunks carry the outline entry heading if the PDF has a bookmark structure; otherwise empty
- [ ] docx chunks carry the Heading-style paragraph text that precedes the chunk
- [ ] `section_depth` is always a string-encoded integer in range `"0"`–`"6"`
- [ ] Test: ingest a markdown file with three heading levels; assert each chunk carries the correct heading

#### Dependencies

None.

#### Bridge integration value

Directly improves concept extraction quality for all sync modes. Better engram labels in MuninnDB.

---

### BL-006

**Expose extraction quality score in `Chunk` metadata**

**Type**: `enhancement` `metadata` `bridge`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

pdfcpu distinguishes between native text PDFs and scanned/image PDFs during extraction. OCR-derived text contains transcription errors, broken hyphenation, and noise that makes for low-quality engrams in MuninnDB. The bridge needs this signal to gate promotion: chunks below a quality threshold are tagged `"low-quality"` rather than promoted as authoritative knowledge. go-rag already has this information at extraction time; it is simply not being forwarded.

#### Specification

Add to `Chunk.metadata` map:

```
metadata["extraction_quality"] = "0.97"
metadata["extraction_method"]  = "native"
```

`extraction_quality`: float in range `0.0`–`1.0`.

| Value range | Meaning |
|---|---|
| `0.9`–`1.0` | Native text extraction — high confidence |
| `0.6`–`0.9` | Mixed (some pages native, some OCR) |
| `0.3`–`0.6` | Predominantly OCR |
| `0.0`–`0.3` | Poor OCR or image-only pages |

`extraction_method`: `"native"` · `"ocr"` · `"mixed"` · `"image"` (no text extracted)

For non-PDF formats (markdown, docx, txt): `extraction_quality = "1.0"`, `extraction_method = "native"`.

#### Acceptance criteria

- [ ] PDF chunks from a text-based PDF carry `extraction_quality` ≥ `"0.9"` and `extraction_method = "native"`
- [ ] All non-PDF chunks carry `extraction_quality = "1.0"` and `extraction_method = "native"`
- [ ] Values are stable across repeated ingestion of the same file
- [ ] `extraction_quality` is parseable as `float64` without error
- [ ] Existing PDF reader tests extended to assert metadata presence

#### Dependencies

None.

#### Bridge integration value

Enables B2 (confidence-filtered context assembly). Prevents OCR noise from entering MuninnDB as authoritative knowledge.

---

### BL-007

**`ListDocuments` — reliable `ingested_at` cursor + `status` filter**

**Type**: `enhancement` `api`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge's change-event poller (until BL-008 is implemented) relies on `ListDocuments` with an `after=<cursor>` parameter to fetch only documents ingested since the last poll. Two issues exist with the current implementation: (1) `ingested_at` may not be consistently populated for documents that went through re-ingest, making the cursor unreliable; (2) there is no way to filter by `status=embedded` — the bridge promotes only embedded documents and must filter client-side, wasting bandwidth on pending and error-state documents.

#### Specification

The existing `ListDocumentsRequest` gains two fields:

```protobuf
message ListDocumentsRequest {
  string vault      = 1;
  int32  page_size  = 2;
  string page_token = 3;
  string after      = 4; // RFC3339; return docs with ingested_at > this value
  string status     = 5; // "embedded" | "pending" | "error" | "" (all)
}
```

`ingested_at` must be set on every document record, including re-ingested documents (it should reflect the most recent ingest time, not the original).

#### Acceptance criteria

- [ ] All documents in the database have a non-empty, non-zero `ingested_at` field
- [ ] Re-ingested documents have `ingested_at` updated to the re-ingest timestamp, not the original ingest time
- [ ] `after` parameter correctly excludes documents with `ingested_at` ≤ the given timestamp
- [ ] `status=embedded` returns only documents that have completed async embedding
- [ ] Combining `after` + `status` parameters works correctly (AND semantics)
- [ ] `page_token`-based pagination works correctly when combined with `after` and `status` filters
- [ ] Integration test: ingest 5 documents, advance time, ingest 3 more, assert `after=<midpoint>` returns exactly 3

#### Dependencies

None.

#### Bridge integration value

Makes the change-event poller reliable as a polling fallback even after BL-008 is implemented. Required for crash recovery (bridge re-reads from last known cursor on startup).

---

## Phase 2 — Replace polling with push

Items that eliminate the 60–90 second lag inherent in polling and enable sub-second document promotion.

---

### BL-008

**`WatchDocuments` — gRPC server-streaming document event stream**

**Type**: `feat` `api` `grpc` `streaming`  
**Priority**: P2 · **Size**: M · **Phase**: 2

#### Description

The bridge's primary sync mechanism should be event-driven, not poll-driven. `WatchDocuments` is a long-lived gRPC server-streaming RPC: the bridge connects once and receives document lifecycle events as they happen, rather than polling every 60 seconds. Events carry a resumable cursor so the bridge can reconnect after crashes without missing events or re-receiving prior ones.

#### Proto

```protobuf
rpc WatchDocuments(WatchRequest) returns (stream DocumentEvent);

message WatchRequest {
  string vault  = 1; // Empty = watch all vaults
  string cursor = 2; // Resume token from last received event; empty = start from now
}

enum DocumentEventType {
  INGESTED    = 0; // Metadata committed; embedding may still be pending
  EMBEDDED    = 1; // Async embedding complete — safe to promote to MuninnDB
  RE_INGESTED = 2; // Document modified; see BL-009 and BL-010 for sub-fields
  DELETED     = 3;
}

message DocumentEvent {
  DocumentEventType type        = 1;
  string            document_id = 2;
  string            source_path = 3;
  string            vault       = 4;
  string            cursor      = 5; // Opaque; persist and pass on reconnect
  DocumentMeta      before      = 6; // RE_INGESTED only: previous document version
  DocumentMeta      after       = 7; // INGESTED / RE_INGESTED: new version
  int64             timestamp_ms = 8;
}
```

#### Acceptance criteria

- [ ] Stream delivers `INGESTED` within 500ms of `go-rag add` completing
- [ ] Stream delivers `EMBEDDED` within 500ms of the async embedding worker completing for a document
- [ ] Stream delivers `DELETED` within 500ms of `go-rag scan` detecting a file deletion
- [ ] `cursor` in each event is an opaque string that can be passed back in `WatchRequest.cursor` to resume
- [ ] Reconnecting with a valid cursor delivers all events since that cursor with no duplicates and no gaps
- [ ] Reconnecting with an expired cursor (older than retention window, default 7 days) returns `OUT_OF_RANGE` gRPC status
- [ ] Reconnecting with `cursor=""` starts from the current moment (no historical replay)
- [ ] If go-rag restarts, in-flight watch streams reconnect automatically (client-side reconnect logic in the bridge)
- [ ] Stream survives idle periods (no events) for at least 1 hour without timing out — keepalive pings sent every 30s
- [ ] Multiple bridge instances can hold concurrent streams on the same vault without interfering
- [ ] Load test: 1000 events/minute delivered with < 1s p99 latency

#### Dependencies

BL-009 (`EMBEDDED` event type), BL-007 (cursor semantics informed by the `ingested_at` field design)

#### Bridge integration value

Eliminates 60–90s polling lag. Drops bridge sync lag to <2s end-to-end. Foundation for all Phase 2 bridge patterns.

---

### BL-009

**`EMBEDDED` event type — distinct from `INGESTED`**

**Type**: `feat` `api` `streaming`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

go-rag uses an async-after-ACK ingest model: `INGESTED` fires when the document record is committed to Pebble, but embedding runs asynchronously afterwards. The bridge must not promote a chunk to MuninnDB before its vector embedding exists — doing so means the engram's embedding is missing when MuninnDB tries to run semantic similarity. The current workaround is a 30-second `sync_delay_secs` buffer, which is a guess. The `EMBEDDED` event removes the guesswork: the bridge promotes on receipt of this event, not `INGESTED`.

This is a small addition to the existing `WatchDocuments` event stream (BL-008) — a new enum value and the corresponding emit point in the embedding worker.

#### Specification

The embedding worker emits a `DocumentEvent{type: EMBEDDED}` for each document immediately after:
- All chunks for that document have been embedded by the Ollama worker
- The embedding records are committed to Pebble
- `Document.status` is set to `"embedded"`

The event is emitted per document (not per chunk). It carries the same `document_id`, `source_path`, `vault`, and `cursor` fields as other events; `before` and `after` are not populated (the document metadata itself hasn't changed, only its status).

#### Acceptance criteria

- [ ] `EMBEDDED` event fires after the last chunk of a document is embedded, not before
- [ ] `EMBEDDED` event fires for both new ingests (`INGESTED` followed by `EMBEDDED`) and re-ingests (`RE_INGESTED` followed by `EMBEDDED`)
- [ ] `EMBEDDED` event is not emitted for documents that error during embedding — those emit a future `EMBED_FAILED` event (or are observable via `status` in `ListDocuments`)
- [ ] `Document.status` is `"embedded"` by the time the `EMBEDDED` event is delivered
- [ ] Test: ingest a document, assert stream delivers `INGESTED` then `EMBEDDED` in order, with `EMBEDDED` delivered after `go-rag status` shows the document as embedded

#### Dependencies

BL-008 (`WatchDocuments` stream — the event is delivered on this stream)

#### Bridge integration value

Removes the 30s `sync_delay_secs` buffer from bridge config. Bridge promotes immediately on `EMBEDDED` event receipt. Zero-guesswork promotion timing.

---

### BL-010

**Chunk delta in `RE_INGESTED` events**

**Type**: `feat` `api` `streaming`  
**Priority**: P2 · **Size**: M · **Phase**: 2

#### Description

When a document is re-ingested after modification, the bridge currently re-promotes all of its chunks — even the 90% that didn't change. For a 50-chunk document with one edited paragraph, this means 50 MuninnDB `BatchRemember` calls where 5 would do. Chunk deltas in the `RE_INGESTED` event let the bridge promote only `ADDED` chunks, update `UNCHANGED` chunks' timestamps, and tag `REMOVED` chunks as `"superseded"` in MuninnDB.

#### Proto addition to `DocumentEvent`

```protobuf
message DocumentEvent {
  // ... existing fields ...
  repeated ChunkDelta chunk_deltas = 9; // Populated on RE_INGESTED + EMBEDDED only
}

message ChunkDelta {
  enum ChangeType {
    ADDED     = 0; // New chunk_id not present in previous version
    REMOVED   = 1; // chunk_id present in previous version but not new
    UNCHANGED = 2; // Same chunk_id and content hash in both versions
  }
  ChangeType change_type   = 1;
  string     chunk_id      = 2; // New chunk_id (ADDED / UNCHANGED)
  string     prev_chunk_id = 3; // Previous chunk_id (REMOVED / UNCHANGED)
}
```

`chunk_deltas` is populated on `RE_INGESTED` events after embedding completes (i.e., attached to the corresponding `EMBEDDED` event, not the initial `RE_INGESTED` event). The delta is computed by diffing the chunk ID sets of the previous and new document versions.

#### Acceptance criteria

- [ ] `chunk_deltas` is present and non-empty on `RE_INGESTED` / `EMBEDDED` event pairs where the document content changed
- [ ] `chunk_deltas` is empty for `INGESTED` events (new documents have no previous version to diff against)
- [ ] An `UNCHANGED` chunk has the same content hash as the corresponding chunk in the prior version
- [ ] Sum of `ADDED` + `UNCHANGED` chunk counts equals the new document's total chunk count
- [ ] Sum of `REMOVED` + `UNCHANGED` chunk counts equals the prior document's total chunk count
- [ ] Editing one paragraph in a 20-chunk document produces exactly 1–2 `ADDED`, 1–2 `REMOVED`, and 18+ `UNCHANGED` deltas
- [ ] Test: ingest a markdown file, modify one section, re-ingest, assert delta structure

#### Dependencies

BL-008, BL-009 (delta is delivered on the `EMBEDDED` event after re-ingest)

#### Bridge integration value

Reduces re-ingest promotion work by ~90% for documents with localised changes. Enables surgical MuninnDB updates rather than full re-promotion.

---

### BL-011

**REST webhook registration for document lifecycle events**

**Type**: `feat` `api`  
**Priority**: P2 · **Size**: M · **Phase**: 2

#### Description

Not all bridge deployments can maintain a long-lived gRPC stream (firewalls, serverless environments, or consumers using go-rag's REST API). Webhook support provides the same event delivery as `WatchDocuments` (BL-008) over standard HTTP POST calls, with HMAC-SHA256 request signing for authenticity.

#### Specification

```
POST /api/webhooks
Content-Type: application/json

{
  "url":    "http://127.0.0.1:9090/gorag-events",
  "vault":  "cyber-notes",              // "" = all vaults
  "events": ["embedded", "deleted"],    // Subset of event types; [] = all
  "secret": "hmac-signing-secret"       // Optional; used for X-GoRag-Signature header
}

Response 201 Created:
{
  "webhook_id": "wh_abc123",
  "url": "http://127.0.0.1:9090/gorag-events",
  "vault": "cyber-notes",
  "events": ["embedded", "deleted"],
  "created_at": "2026-06-25T03:00:00Z"
}
```

Delivery: go-rag POSTs a JSON `DocumentEvent` body to the registered URL. Includes `X-GoRag-Signature: sha256=<hmac>` header if a secret was provided. Times out after 5s; retries up to 3 times with exponential backoff on non-2xx responses.

Management endpoints:
- `GET /api/webhooks` — list all registered webhooks
- `DELETE /api/webhooks/{webhook_id}` — remove a webhook
- `GET /api/webhooks/{webhook_id}/deliveries` — last 20 delivery attempts with status codes

Webhooks are stored in a new Pebble prefix (`0x11`) and survive go-rag restarts.

#### Acceptance criteria

- [ ] Webhook URL is called within 2s of an event occurring
- [ ] `X-GoRag-Signature` header is present and verifiable when a secret is configured
- [ ] go-rag retries failed deliveries (non-2xx or timeout) up to 3 times with 1s / 2s / 4s backoff
- [ ] After 3 failed attempts, the delivery is marked failed and logged; the webhook remains registered
- [ ] Webhooks survive go-rag restart — registrations are persisted to Pebble
- [ ] `GET /api/webhooks/{id}/deliveries` shows success/failure for the last 20 attempts
- [ ] Loopback-only restriction applies: `--bind-external` required to register webhooks pointing at non-loopback URLs
- [ ] `DELETE /api/webhooks/{id}` immediately stops future deliveries

#### Dependencies

BL-008 (webhook payload is the same `DocumentEvent` struct)

#### Bridge integration value

Enables bridge deployments that cannot hold gRPC streams. Provides an alternative delivery path for environments with restricted outbound TCP.

---

## Phase 3 — Query quality and feedback

Items that improve retrieval quality for joint go-rag + MuninnDB pipelines and close the usage feedback loop.

---

### BL-012

**`QueryStream` — gRPC streaming query results**

**Type**: `feat` `api` `grpc` `streaming`  
**Priority**: P3 · **Size**: M · **Phase**: 3

#### Description

The bridge's on-query hook fires MuninnDB promotions after `go-rag.Query()` returns its full result set. With cross-encoder reranking enabled, that unary call takes 800–1500ms. `QueryStream` allows the bridge to begin promoting early results while later results are still being scored — the caller receives its results synchronously from the stream client side with no perceptible change in behaviour.

#### Proto

```protobuf
rpc QueryStream(QueryRequest) returns (stream QueryStreamEvent);

enum QueryStreamEventType {
  RESULT   = 0; // A chunk result at its initial RRF score
  RERANKED = 1; // Final reranked scores for all previously sent chunks
  DONE     = 2; // Stream complete; no more events
}

message QueryStreamEvent {
  QueryStreamEventType    type           = 1;
  ChunkResult             result         = 2; // Set for RESULT events
  repeated RerankedScore  reranked_scores = 3; // Set for RERANKED event
}

message RerankedScore {
  string chunk_id    = 1;
  float  final_score = 2;
  int32  final_rank  = 3;
}
```

The stream delivers `RESULT` events as they emerge from RRF fusion (approximately best-first). If reranking is enabled, one `RERANKED` event follows containing updated scores and ranks for all previously sent chunks. `DONE` signals end of stream. If reranking is disabled, `DONE` follows directly after the last `RESULT`.

#### Acceptance criteria

- [ ] First `RESULT` event arrives within 150ms of request for a primed query cache
- [ ] Results arrive in approximate descending score order (best first), not random order
- [ ] `RERANKED` event arrives after all `RESULT` events and before `DONE`
- [ ] `RERANKED` event covers every `chunk_id` previously sent in `RESULT` events
- [ ] Disabling reranking with `--no-rerank` causes `DONE` to follow directly after the last `RESULT`
- [ ] The existing unary `Query` RPC continues to work unchanged (backwards compatible)
- [ ] Stream produces identical results to the unary `Query` for the same request parameters
- [ ] Cancelling the stream mid-delivery does not leak goroutines or Pebble transactions

#### Dependencies

None — can be implemented against the existing RRF + reranking pipeline.

#### Bridge integration value

On-query hook (bridge sync mode B) begins promotions within ~150ms of first result rather than ~1000ms after last result. Particularly valuable for high-`k` queries.

---

### BL-013

**`RecordUsage` RPC — chunk usage feedback**

**Type**: `feat` `api` `grpc`  
**Priority**: P3 · **Size**: S · **Phase**: 3

#### Description

go-rag currently has no visibility into which chunks were actually useful in LLM responses. `RecordUsage` closes this loop: the bridge calls it after the agent framework provides a quality signal. go-rag can use accumulated usage data to weight BM25 term frequencies, surface a `--sort=most-used` mode in `ListDocuments`, and flag persistently low-quality chunks for automatic reprocessing. The data is also valuable for future auto-tuning of BM25 `k1`/`b` parameters per vault.

#### Proto

```protobuf
rpc RecordUsage(RecordUsageRequest) returns (RecordUsageResponse);

message RecordUsageRequest {
  string              vault      = 1;
  string              session_id = 2; // Optional; for analytics grouping
  string              query      = 3; // The query that retrieved these chunks
  repeated ChunkUsage usages     = 4;
}

message ChunkUsage {
  string chunk_id   = 1;
  float  quality    = 2; // 0.0 = not useful · 1.0 = highly useful; -1 = unrated
}

message RecordUsageResponse {
  int32 accepted = 1; // Number of usage records written
}
```

Usage is stored in a new Pebble prefix (`0x12`): `chunk_id → {use_count, quality_sum, quality_count, last_used_at}`. The per-chunk usage summary is surfaced in `GetChunk` and `GetDocument` responses as `metadata["use_count"]` and `metadata["avg_quality"]`.

#### Acceptance criteria

- [ ] `RecordUsage` writes atomically — all usages in a request are accepted or none are
- [ ] Calling `RecordUsage` with the same `chunk_id` multiple times accumulates (does not overwrite)
- [ ] `quality=-1` (unrated) increments `use_count` but does not affect `avg_quality`
- [ ] `metadata["use_count"]` in `GetChunk` response reflects the accumulated count after `RecordUsage`
- [ ] Records survive go-rag restart (written to Pebble, not in-memory)
- [ ] REST equivalent: `POST /api/vaults/{vault}/usage`
- [ ] Removing a document (`go-rag scan` detects deletion) removes its usage records

#### Dependencies

None.

#### Bridge integration value

Closes the agent feedback loop. Feeds provenance tracking (D2). Gives go-rag data for future self-tuning.

---

### BL-014

**`FederatedQuery` RPC — single-call multi-vault query**

**Type**: `feat` `api` `grpc`  
**Priority**: P3 · **Size**: M · **Phase**: 3

#### Description

Bridge pattern B3 (multi-vault routing) fans queries out across multiple go-rag vaults and RRF-merges the results. Currently this requires N separate `Query` calls, N network round-trips, and bridge-side merge logic. `FederatedQuery` moves the fan-out and merge inside go-rag, where direct Pebble access eliminates inter-vault network overhead and the existing RRF implementation can be reused for cross-vault fusion.

#### Proto

```protobuf
rpc FederatedQuery(FederatedQueryRequest) returns (FederatedQueryResponse);

message FederatedQueryRequest {
  string          query       = 1;
  repeated string vaults      = 2; // Empty = all vaults
  int32           k_per_vault = 3; // Candidates per vault before cross-vault merge (default 20)
  int32           k_total     = 4; // Total results after merge (default 10)
  string          mode        = 5; // hybrid | semantic | keyword
  float           rrf_k       = 6;
  bool            no_rerank   = 7;
}

message FederatedQueryResponse {
  repeated ChunkResult       results    = 1;
  map<string, int32>         vault_hits = 2; // vault_name → result count in top-k
  map<string, string>        vault_errors = 3; // vault_name → error message if that vault failed
}
```

Per-vault errors are soft failures: if one vault errors, results from other vaults are still returned and `vault_errors` records the failure. The call returns an overall error only if all vaults fail.

#### Acceptance criteria

- [ ] `FederatedQuery` with `vaults=[]` queries all vaults present in the daemon
- [ ] Results are merged using the same RRF implementation as single-vault `Query`
- [ ] `vault_hits` correctly counts how many top-k results originated from each vault
- [ ] A single vault error does not fail the whole request — partial results with `vault_errors` populated
- [ ] `k_per_vault` × N vaults candidates are gathered before final top-`k_total` selection
- [ ] REST equivalent: `POST /api/query/federated`
- [ ] Benchmark: federated query across 3 vaults faster than 3 sequential single-vault queries (network savings)
- [ ] Streaming variant `FederatedQueryStream` returns results as they score across vaults (future; not in this item)

#### Dependencies

None — can be implemented by composing existing per-vault query logic.

#### Bridge integration value

Directly implements B3 (multi-vault routing). Reduces bridge-side complexity and N network round-trips to 1.

---

### BL-015

**Named entity metadata in `Chunk` response**

**Type**: `feat` `metadata` `bridge`  
**Priority**: P3 · **Size**: L · **Phase**: 3

#### Description

Bridge pattern C2 (entity graph enrichment) requires named entities extracted from chunk text to be written to MuninnDB's entity graph. Without go-rag providing this, the bridge must run its own NER pass — adding an Ollama round-trip per chunk to the sync worker. go-rag's optional Ollama enrichment pipeline (triggered by `MUNINN_ENRICH_URL` equivalent) should write entity extraction results back into chunk metadata so the bridge reads, not re-computes.

This item requires integration between the enrichment pipeline and the chunk metadata store — more invasive than the other metadata items, hence Large.

#### Specification

When enrichment is enabled, the embedding worker's async pass includes an entity extraction step. Results are written to `Chunk.metadata`:

```
metadata["entities"] = "[{\"text\":\"Palo Alto Networks\",\"type\":\"ORG\"},{\"text\":\"CVE-2024-1234\",\"type\":\"ID\"}]"
```

Value: JSON array of `{text, type}` objects.  
Entity types: `ORG` · `PERSON` · `LOC` · `PRODUCT` · `ID` (CVEs, RFCs, version strings) · `TECH` (framework/language names)

Entity extraction uses the configured Ollama model with a structured prompt that returns JSON only. The result is written to Pebble and is part of the chunk's `metadata` map from that point forward.

#### Acceptance criteria

- [ ] `metadata["entities"]` is present on chunks when Ollama enrichment is enabled in config
- [ ] `metadata["entities"]` is `"[]"` (not absent) when enrichment is disabled, for consistent consumer code
- [ ] Entity extraction runs asynchronously after the `EMBEDDED` event fires — it does not block embedding
- [ ] A new `ENRICHED` event type on the `WatchDocuments` stream (BL-008 extension) signals when entity extraction completes for a document
- [ ] Entity extraction failures are logged and surfaced in `go-rag status` but do not put the document in error state
- [ ] `metadata["entities"]` is valid JSON parseable as `[]struct{Text, Type string}` 
- [ ] Test: ingest a document containing known org names and CVE IDs; assert entities appear in chunk metadata

#### Dependencies

BL-008 (the `ENRICHED` event extends the watch stream); go-rag enrichment plugin configuration

#### Bridge integration value

Directly implements C2 (entity graph enrichment) without the bridge running its own NER. Entities flow from go-rag → bridge → MuninnDB entity graph with no additional Ollama calls in the bridge.

---

## Phase 4 — Full document lifecycle

Items that give go-rag and the bridge a complete view of document history and change over time.

---

### BL-016

**Soft-delete document records on re-ingest**

**Type**: `feat` `storage`  
**Priority**: P4 · **Size**: M · **Phase**: 4

#### Description

go-rag currently hard-deletes the previous document record when a file is re-ingested with new content. This destroys the version history needed by BL-017 (`GetDocumentHistory`) and means the bridge cannot track which engrams correspond to which document version. Soft-delete retains prior `document_id` records with a `superseded_at` timestamp in a new Pebble key prefix, without keeping the full chunk content (just the stub with `document_id`, `content_hash`, `ingested_at`, `superseded_at`).

#### Specification

New Pebble key prefix `0x13`: historical document stubs.  
Key format: `0x13 | source_path_hash | ingested_at_unix_ms`  
Value: `DocumentStub{document_id, content_hash, ingested_at, superseded_at, chunk_count}`

On re-ingest:
1. Current document record is copied to `0x13` prefix with `superseded_at = now`
2. New document record replaces the `0x02` prefix entry
3. Historical chunks are NOT retained — only the stub (chunk content is expensive; the bridge can fetch it from MuninnDB if needed)

Retention policy: stubs older than `history_retention_days` (default 90, configurable) are pruned by a background worker.

#### Acceptance criteria

- [ ] Re-ingesting a changed document creates a historical stub in `0x13` prefix before overwriting `0x02`
- [ ] Re-ingesting an unchanged document (same content hash) does NOT create a new historical stub
- [ ] `go-rag status` shows historical stub count per vault
- [ ] `go-rag reprocess` creates a historical stub (treating forced re-process as a new version)
- [ ] Background pruner removes stubs older than `history_retention_days` on a daily schedule
- [ ] Hard delete (`go-rag vault delete`) removes all stubs for that vault

#### Dependencies

None — prerequisite for BL-017 and BL-018.

#### Bridge integration value

Foundational for E3 (temporal knowledge versioning). Enables BL-017 and BL-018.

---

### BL-017

**`GetDocumentHistory` RPC — version history by source path**

**Type**: `feat` `api` `grpc`  
**Priority**: P4 · **Size**: S · **Phase**: 4

#### Description

With soft-delete in place (BL-016), the bridge and other consumers can ask "what versions of this file have been ingested and when?" This is the read API over the historical stubs. The bridge uses it to implement E3 (temporal versioning): when an engram is promoted from a re-ingested document, it carries `metadata["document_version"]` pointing to the specific `document_id` version it was sourced from.

#### Proto

```protobuf
rpc GetDocumentHistory(GetDocumentHistoryRequest) returns (GetDocumentHistoryResponse);

message GetDocumentHistoryRequest {
  string source_path   = 1; // Relative path from vault root
  string vault         = 2;
  int32  max_versions  = 3; // Default 10
}

message DocumentVersion {
  string document_id   = 1;
  string content_hash  = 2;
  string ingested_at   = 3; // RFC3339
  string superseded_at = 4; // RFC3339; empty if this is the current version
  int32  chunk_count   = 5;
  bool   is_current    = 6;
}

message GetDocumentHistoryResponse {
  string                   source_path = 1;
  string                   vault       = 2;
  repeated DocumentVersion versions    = 3; // Most recent first
}
```

#### Acceptance criteria

- [ ] Returns versions most-recent-first
- [ ] The current version has `is_current=true` and empty `superseded_at`
- [ ] All historical versions have `is_current=false` and populated `superseded_at`
- [ ] A document with only one ingestion returns a single version with `is_current=true`
- [ ] `max_versions` limits results; older versions are truncated, not newer
- [ ] REST equivalent: `GET /api/vaults/{vault}/history?path=<source_path>`
- [ ] Returns `NOT_FOUND` if the path has never been ingested into the vault

#### Dependencies

BL-016 (soft-delete must exist for history to accumulate)

#### Bridge integration value

Directly implements E3 (temporal knowledge versioning). Bridge can tag engrams with their specific `document_id` version and record transitions when documents change.

---

### BL-018

**Chunk content diff on re-ingest**

**Type**: `feat` `api`  
**Priority**: P4 · **Size**: M · **Phase**: 4

#### Description

BL-010 adds chunk deltas to `WatchDocuments` events (which chunks were added, removed, or unchanged). This item makes the diff available on-demand via a dedicated RPC, enabling the bridge and other consumers to ask "what changed between version A and version B of this document?" without having to hold the full chunk set themselves. It also populates the `chunk_deltas` field for documents that were re-ingested before BL-010 was deployed.

#### Proto

```protobuf
rpc DiffDocumentVersions(DiffDocumentVersionsRequest) returns (DiffDocumentVersionsResponse);

message DiffDocumentVersionsRequest {
  string vault           = 1;
  string source_path     = 2;
  string from_document_id = 3; // Older version; empty = previous version
  string to_document_id   = 4; // Newer version; empty = current version
}

message DiffDocumentVersionsResponse {
  string             from_document_id = 1;
  string             to_document_id   = 2;
  repeated ChunkDelta deltas          = 3; // Reuses ChunkDelta from BL-010
  int32              added_count      = 4;
  int32              removed_count    = 5;
  int32              unchanged_count  = 6;
}
```

The diff is computed by comparing chunk ID sets between the two versions. Since historical chunk content is not retained (BL-016 keeps only stubs), `UNCHANGED` means same `chunk_id` (i.e., same content hash), and `ADDED`/`REMOVED` are set differences.

#### Acceptance criteria

- [ ] `from_document_id=empty` uses the immediately prior version (second entry in `GetDocumentHistory`)
- [ ] `to_document_id=empty` uses the current version
- [ ] `UNCHANGED` chunks appear in both `from` and `to` chunk ID sets
- [ ] `ADDED` chunks appear only in `to`; `REMOVED` only in `from`
- [ ] `added_count + unchanged_count` equals the total chunk count of `to_document_id`
- [ ] `removed_count + unchanged_count` equals the total chunk count of `from_document_id`
- [ ] Returns `NOT_FOUND` if either `document_id` does not exist in history
- [ ] REST equivalent: `GET /api/vaults/{vault}/diff?from=<doc_id>&to=<doc_id>`

#### Dependencies

BL-016 (soft-delete), BL-017 (version history — needed to resolve `from=empty` to an actual prior document ID)

#### Bridge integration value

Completes E3 (temporal versioning). Enables bridge to compute surgical engram updates when documents are re-ingested, beyond what the event stream delta (BL-010) provides.

---

## Appendix: MuninnDB items

Items required on the MuninnDB side to complete the same bridge patterns. Logged here for cross-reference; the actual backlog entries belong in the MuninnDB repo.

| Item | Description | Bridge pattern |
|---|---|---|
| `GetByMetadata` RPC | Look up engrams by `metadata["chunk_id"]` — eliminates need for bridge-side state store as idempotency registry | All sync modes |
| `StrengthenEdge` RPC | Write explicit Hebbian edge between two engram IDs with a given weight | E2 (Obsidian backlinks) |
| `PatchEngram` RPC | Adjust Bayesian confidence delta and add/remove tags on an existing engram | C1 (contradiction detection) |
| `ENRICHED` event on watch stream | Notify bridge when MuninnDB's own enrichment completes for a promoted engram | C2 (entity graph) |

---

*Backlog authored 2026-06-25. Items are implementation-ready as specified — proto field numbers are illustrative and should be set according to the upstream go-rag proto conventions when implemented.*
