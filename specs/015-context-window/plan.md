# Implementation Plan: Context Window — Sibling-Chunk Expansion (H15)

**Branch**: `main` | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/015-context-window/spec.md` (audit backlog item **H15**, P1 — last Phase 3 item).

## Summary

Wire the existing-but-unpopulated `Chunk.PreviousChunkID`/`NextChunkID` linked list (the pipeline never sets them — verified), then add a per-query `ContextWindow` option that fetches up to N sibling chunks around each hit, augmenting the result with their text as context. Context is expanded AFTER ranking/rerank (doesn't affect top-k or ranking). Opt-in (default 0 = byte-identical to today). Exposed on all four transports.

## Technical Context

**Language/Version**: Go 1.22+. Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib; existing `internal/model`, `internal/engine`, `internal/pipeline`. Proto regen for gRPC (protoc command known from spec 009). **No new dependencies.**

**Storage**: Pebble KV — **N/A**. The linked-list fields already exist on the persisted `Chunk` record (model.go:80-81). Populating them adds data to existing fields (no schema change — the JSON record gains `previous_chunk_id`/`next_chunk_id` values, which were already in the struct). Principle II intact (chunk identity unchanged; the linked-list values are derived from chunk ordering, not content).

**Testing**: `go test -race -cover`; H02 eval gate (SC-003 — default ContextWindow=0, no regression).

**Project Type**: CLI + multi-transport server. This feature touches the pipeline (populate linked list), the engine (context expansion in Query), the model (result shape), and all four transport adapters + proto.

**Constraints**: Pure Go; ContextWindow is opt-in (0 = today); context after ranking; no ranking/top-k change; cross-transport parity.

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First | ✅ Pass | In-process sibling lookup; no network. |
| II | Content-Addressed Identity | ✅ Pass | Chunk identity unchanged; linked-list values are ordering metadata on existing fields (no new keys, no identity hash change). |
| III | Pure Go | ✅ Pass | stdlib only. Proto regen is build-time. |
| IV | Async-After-ACK | ✅ Pass | Linked-list population is in `processFile` (sync chunk-store, pre-ACK). Context expansion is query-path (post-ACK). |
| V | Extension by Interface | ✅ Pass | Exposed on all four transports (parity); `ContextWindow` is an optional request field; context is an optional response field. |

**No violations.**

## Project Structure

```text
internal/pipeline/pipeline.go   # populate PreviousChunkID/NextChunkID after chunk construction
internal/engine/types.go        # QueryRequest.ContextWindow + QueryHit.Context ([]ContextChunk)
internal/engine/query.go        # after building hits, expand context if ContextWindow > 0
internal/cli/query.go           # --context-window flag
internal/rest/{types,engine_adapter}.go  # context_window request + context response fields
internal/mcp/server.go          # schema + renderQuery + result display
proto/gorag.proto + proto/gen/  # context_window on QueryRequest + context on QueryHit; regen
internal/grpc/engine_adapter.go # map context_window + serialize context
```

**Structure Decision**: Two implementation halves — (1) **populate** the linked list in `pipeline.processFile` (one loop after chunk construction: `chunks[i].PreviousChunkID = chunks[i-1].ID`), (2) **expand** context in `engine.Query` (after the result-building loop, for each hit, follow Previous/Next chains up to N steps, fetch sibling text via `lookupChunk`, attach to a new `QueryHit.Context` field). Transport exposure mirrors H14 (filter): request field on all transports, response field for context.

## Complexity Tracking

*(Empty — Constitution Check passes on all five principles.)*
