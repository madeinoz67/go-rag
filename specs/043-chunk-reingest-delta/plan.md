# Implementation Plan: Chunk Change Deltas on Re-Ingest (RE_INGESTED)

**Branch**: `043-chunk-reingest-delta` *(single-author repo — work commits to `main`; slug identifies the spec)* | **Date**: 2026-07-02 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/043-chunk-reingest-delta/spec.md`. Technical design (the plan-phase HOW): [`docs/design/bl010-chunk-identity.md`](../../docs/design/bl010-chunk-identity.md) (B-simple, validated by a 3-agent design analysis + a 4-facet red team + a 32-agent RedTeam ParallelAnalysis).

## Summary

Detect chunk-level changes on document re-ingest and deliver them as a `RE_INGESTED` event on the existing `WatchDocuments` stream: a per-chunk delta (`ADDED` / `REMOVED` / `UNCHANGED`) computed by content identity (a new non-identity `ContentHash` sidecar = SHA-256 of the chunk's redacted text), plus an old→new chunk-ID map, so consumers (the MuninnDB bridge) update incrementally instead of re-processing the whole document. For `UNCHANGED` chunks whose embedding config hasn't drifted, skip the expensive embedding call (preserve the vector via a direct-key `PrefixEmbedding` copy); always recompute the FTS index + near-dup clusters normally — **no inverted-index rewiring** (the flawed mechanism the red teams rejected). `RE_INGESTED` replaces the `INGESTED(new)`+`DELETED(old)` pair a re-ingest surfaces today.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`), pure Go.

**Primary Dependencies**: existing only — cobra, pebble, chromem-go, grpc-go (server-streaming already shipped, spec 040), protobuf. No new dependencies.

**Storage**: Pebble KV. `ContentHash` rides in the existing `PrefixChunk` (0x03) JSON value (a non-identity sidecar — no new prefix). A numbered migration (v2) backfills it for existing chunks. No new key-space prefix; no key-construction change.

**Testing**: `go test -race -cover ./...`. New: the multiset diff (pure function), the embed-skip gate (config-drift), the re-ingest reorder (capture-before-delete), a streaming test that a re-ingest emits exactly one `RE_INGESTED` with the correct delta + cid map (mirror spec 040's `TestGRPC_WatchDocuments_*`), a migration idempotency test.

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine. `RE_INGESTED` is **gRPC-server-streaming only** — same justified deviation as the rest of `WatchDocuments` (Principle V; streaming has no unary CLI/MCP equivalent).

**Performance Goals**: the diff (SHA-256 + multiset map ops) + the `PrefixEmbedding` copy are bounded (microseconds-per-chunk); the write-ACK stays <10ms (Principle IV — verify in implementation). The embed-skip is the saving (avoids an Ollama round-trip per `UNCHANGED` chunk when the baseline is unchanged).

**Constraints**: pure Go; no schema-prefix change (v2 value-encoding migration only); `RE_INGESTED` gRPC-only; the delta computation must not breach the <10ms ACK budget; chunk identity (`cid`) stays globally unique + unchanged.

**Scale/Scope**: **size L** — a new sidecar field + a migration + a multiset-diff + the re-ingest reorder + the embed-skip gate + the `PrefixEmbedding` preservation + the `RE_INGESTED` event/proto extension. The hardest piece is the embed-skip preservation (gated, synchronous, race-free) + the re-ingest path reorder across `Reprocess`/`ReprocessAll`/the watcher.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | In-process diff + event bus; no network egress. `ContentHash` computed locally. |
| **II. Content-Addressed Identity** | ✅ PASS | `ContentHash` is a NON-identity sidecar — the `cid` formula (`GenerateID(text, mime, {doc, idx})`) is **unchanged**; identity stability for stored data is preserved; no stored ID changes as a side-effect (FR-010). The identity hash and the diff hash are distinct (mirrors the existing `Document.ContentHash` precedent at model.go:32). |
| **III. Pure Go — No CGo** | ✅ PASS | No new deps; the diff is stdlib (`crypto/sha256` + maps); the event extends the existing grpc-go stream. |
| **IV. Async-After-ACK Writes** | ✅ PASS *(verify)* | The diff + `PrefixEmbedding` copy are bounded (µs/chunk); they run on the re-ingest path which must stay <10ms. **Implementation MUST verify the ACK budget holds** with the diff + copy in-place (or move them post-ACK if profiling shows pressure). The embed-skip removes work (the Ollama call), not adds it. |
| **V. Extension by Interface, MCP-First** | ✅ PASS *(justified deviation)* | `RE_INGESTED` is gRPC-server-streaming only — the same justified deviation as `WatchDocuments` (spec 040): streaming has no unary CLI/MCP equivalent; the bridge (the consumer) uses gRPC. The existing unary surface is unchanged. |
| **Storage discipline / Schema evolution** | ✅ PASS | `ContentHash` rides in the existing `PrefixChunk` (0x03) JSON value — **no new prefix, no key-construction change**. The value-encoding change (a new JSON field) gets a numbered idempotent migration: **v2** in `internal/storage/migrate`, `ExpectedVersion` 1→2. No second database; the single Pebble instance + prefix-partitioned key-space is respected. |

**Compliance statement**: Principles I–V pass (V = the documented streaming deviation). **Schema-version impact: migration v2 added (`backfill per-chunk ContentHash`), `migrate.ExpectedVersion` 1→2, no new on-disk prefix.** No violations → no Complexity Tracking entries.

## Project Structure

### Documentation (this feature)

```text
specs/043-chunk-reingest-delta/
├── plan.md              # this file
├── research.md          # Phase 0 — the fork resolution (red-team verdict) + the measurement plan
├── data-model.md        # Phase 1 — ChunkDelta, RE_INGESTED event, ContentHash sidecar
├── quickstart.md        # Phase 1 — runnable validation (re-ingest → RE_INGESTED delta)
├── contracts/
│   └── api.md           # Phase 1 — proto: DocumentEvent.ChunkDeltas + the RE_INGESTED semantics
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root — files touched)

```text
internal/model/model.go            # Chunk.ContentHash sidecar (non-identity)
internal/storage/migrate/          # v2_content_hash.go (backfill) + ExpectedVersion 1→2
internal/pipeline/pipeline.go      # processFile: compute ContentHash; embed-skip gate + PrefixEmbedding copy for UNCHANGED
internal/pipeline/reprocess.go     # Reprocess/ReprocessAll: capture old chunks+embeddings BEFORE DeleteDoc
internal/pipeline/delete.go        # factor chunksOfDoc(docID) (+ embedsOfDoc) read-only helpers
internal/pipeline/watcher.go       # watcher MODIFIED path: route through the oldChunks-threaded re-ingest
internal/pipeline/delta.go (new)   # the multiset diff (ADDED/REMOVED/UNCHANGED + old→new cid map)
internal/engine/baseline.go        # CorpusBaseline drift verdict (the embed-skip gate) — reuse existing
internal/events/bus.go             # EventReingested emission (enum value 2 — already reserved)
internal/grpc/watch_documents.go   # RE_INGESTED projection + ChunkDeltas
proto/gorag.proto                  # DocumentEvent.ChunkDeltas (repeated ChunkDelta) + DocumentEventType_RE_INGESTED (already reserved)
proto/gen/                         # regenerated
```

**Structure Decision**: a new `internal/pipeline/delta.go` owns the pure multiset-diff function (testable in isolation); the re-ingest reorder threads `oldChunks` through the existing `processFile` (one pipeline, one delta hook — avoids forking redaction/poisoning/section-context/wikilink resolution across two paths). The migration follows the spec-034 runner's `v1_bootstrap` precedent. `chunksOfDoc`/`embedsOfDoc` are read-only helpers factored from `DeleteDoc`'s existing scan.

## Complexity Tracking

> None — the Constitution Check passes with no violations. (The embed-skip preservation + the re-ingest reorder are implementation complexity, not constitutional violations; they're tracked in `tasks.md`.)
