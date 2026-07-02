# Spec 041 — Chunk `section_depth` (BL-005 remainder)

**Spec Kit**: specify · **Backlog**: [BL-005](../../../docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md#bl-005) · **Date**: 2026-07-02

## Summary

Expose the **governing heading's level** (1-6) on every chunk response as
`section_depth`, so a consumer can reconstruct markdown markers (`## ` for level
2) and rank by heading depth without re-scanning chunk text. This closes the one
literal gap BL-005 had vs the already-shipped `section_context` breadcrumb.

## Scope finding (why this is small)

BL-005 asked for `section_heading` + `section_depth` in `Chunk.metadata`.
Audit of the current code:

- **`section_heading` (the text) is already exposed.** `Chunk.section_context`
  (field 13, spec 025) is the ordered heading breadcrumb `[top-level → leaf]`
  projected on `Chunk` + `QueryHit` across all four transports. The leaf
  (`section_context[len-1]`) IS the nearest heading text. BL-005's "open" status
  predates spec 025/035.
- **`section_depth` (the leaf level) was the real gap.** `HeadingSpan.Level`
  (1-6) is captured at read time but `resolveBreadcrumb` returned `[]string`
  (text only) — the level was dropped. `len(section_context)` only approximates
  depth and **breaks when headings skip levels** (h1 → h3 gives a breadcrumb of
  length 2 but the governing heading is level 3).

So the work is: thread `HeadingSpan.Level` through `resolveBreadcrumb` onto the
chunk, and surface it as `section_depth`. No new capture, no reader change.

## Contract

- `model.Chunk.SectionLevel int` (1-6; 0 = no governing heading). Non-identity
  sidecar (like `SectionContext`): the chunk ID folds text+mime+{doc,idx} only.
- `resolveBreadcrumb(spans, startIdx, edits) ([]string, int)` — returns the
  breadcrumb + the **leaf** heading's level (0 when none).
- Proto (additive): `int32 section_depth = 18` on `Chunk`; `int32
  section_depth = 14` on `QueryHit`. 0 = none/pre-feature.
- Projected on **all four transports** (gRPC `Chunk`/`QueryHit`, REST DTOs, CLI
  DTOs, MCP carries it implicitly via the breadcrumb). `section_depth` is the
  leaf level; `len(section_context)` is NOT it.

## Non-goals

- Reconstructing literal `#` markers: the consumer composes `strings.Repeat("#",
  section_depth) + " " + section_context[len-1]`. (PDF/docx headings have no
  markers — level + text is the universal form.)
- A `metadata` map on `Chunk`: go-rag projects typed fields, not a string map.
  BL-005's `metadata["section_heading"]` shape is satisfied by the typed
  `section_context` + `section_depth` pair.

## Acceptance criteria (from BL-005)

- [x] A markdown chunk under an h2 carries `section_depth = 2` (and the h2 text
  in `section_context`).
- [x] A chunk before any heading carries `section_depth = 0` (and empty
  `section_context`).
- [x] `section_depth` is the **leaf level**, not the breadcrumb length — a doc
  with h1 → h3 yields depth 3 for the chunk under the h3 (length 2 ≠ level 3).
- [x] Surfaced on `Chunk` + `QueryHit` across gRPC / REST / CLI; round-trips
  through JSON (`section_level,omitempty`).
- [x] `make build && vet && go test -race ./... && lint` green.

## Tests

- `internal/pipeline/section_test.go` — `resolveBreadcrumb` now returns the leaf
  level; every existing assertion extended to check it (nesting depth, sibling
  reset, straddle rule, preamble/empty = 0, redaction offset alignment).
- `internal/pipeline/section_level_test.go` — `TestIngest_SectionLevel_Attached`
  (h1 → 1, h2 → 2, heading-less preamble → 0).
- `internal/model/model_test.go` — `TestChunk_SectionLevel_RoundTrip` (JSON
  round-trip + `omitempty` on zero).

## Files

- `internal/pipeline/section.go` — `resolveBreadcrumb` returns `([]string, int)`.
- `internal/pipeline/pipeline.go`, `internal/pipeline/workers.go` — thread
  `SectionLevel` onto ordinary + caption chunks.
- `internal/model/model.go` — `Chunk.SectionLevel`.
- `internal/engine/types.go`, `internal/engine/query.go` — `QueryHit.SectionLevel`.
- `proto/gorag.proto` (+ regenerated `proto/gen`) — `section_depth` on
  `Chunk` (18) + `QueryHit` (14).
- `internal/grpc/engine_adapter.go`, `internal/rest/{types,get_chunk,engine_adapter}.go`,
  `internal/cli/{chunk,query}.go` — project `section_depth`.
