# Data Model — Chunk Wikilink Metadata (BL-004)

> Phase 1 output for `/speckit-plan`. Entity deltas only — no new persisted structures beyond one additive field. Grounded in `internal/model/model.go`, `internal/reader/markdown.go`, and the spec 025 precedent. See [research.md](./research.md) for rationale and [contracts/api.md](./contracts/api.md) for the wire shape.

## Entity: `Chunk` (delta)

`internal/model/model.go:80` — one new non-identity sidecar field, joining `Poisoning` / `SectionContext` / `NearDup` / `Caption` / `Kind`.

```go
type Chunk struct {
    // ...existing identity, content, position, and sidecar fields unchanged...
    SectionContext []string `json:"section_context,omitempty"` // spec 025
    // Wikilinks are the Obsidian [[wikilink]] targets whose link text falls
    // inside this chunk's text range (spec 036 / BL-004), canonicalised by
    // linkTarget: alias (|) and anchor (#/block-ref) stripped, de-duplicated,
    // first-occurrence order. nil for chunks with no wikilinks and for chunks
    // ingested before this feature — treated as absent (never an error) at
    // retrieval (FR-008). Embeds (![[..]]) and note transclusions are excluded
    // (FR-003). A non-identity sidecar (like SectionContext): the chunk ID
    // folds text+mime+{doc,idx} only, and the span data is removed from document
    // metadata before GenerateID, so neither document nor chunk identity changes.
    // Surfaced on QueryHit and GetChunk across every transport (FR-009).
    Wikilinks []string `json:"wikilinks,omitempty"`
}
```

**Field rules**

| Property | Value |
|----------|-------|
| Type | `[]string` (canonicalised note names) |
| Ordering | document order, first occurrence; de-duplicated |
| Canonicalisation | `linkTarget`: strip `|alias`, strip `#anchor` / `#^blockid` |
| Included | `[[target]]`, `[[target\|display]]`, `[[target#heading]]`, `[[target#^id]]` → `target` |
| Excluded | `![[media]]`, `![[note]]` (embeds/transclusions stay in their own signals) |
| Absent when | source has no wikilinks **or** chunk was ingested before this feature |
| Absent semantics | `nil` → omitted in JSON (`omitempty`), empty list on the gRPC wire; retrieval never errors |
| Identity participation | **none** — not in chunk ID; transient spans stripped before document ID |
| Back-fill | via `Reprocess` (re-read source); no automatic rewrite |

## Transient value: `WikilinkSpan` (document metadata, not persisted)

`internal/reader/markdown.go` — a positional span emitted into the reader's document metadata map under `md["wikilink_spans"]`, consumed and dropped by the pipeline before identity/store. Mirrors `HeadingSpan` / `md["heading_spans"]` (spec 025).

```go
// WikilinkSpan is one Obsidian [[wikilink]] in the stripped text the chunker
// receives. Target is canonicalised by linkTarget; Offset is a byte index into
// the reader's returned (stripped) text — the same coordinate space the chunker
// and heading_spans use (the pipeline translates through redaction). Transient:
// consumed during chunking and removed from document metadata before
// identity/store, so it is never persisted (spec 025 R7 precedent).
type WikilinkSpan struct {
    Target string
    Offset int // byte offset into the reader's stripped text
}
```

Produced in `normalizeObsidian` alongside the existing transclusion collection: in the `reObsidianLink.ReplaceAllStringFunc` branch, record `(linkTarget(inner), matchOffset)` for each `[[…]]`. Match offsets come from `reObsidianLink.FindAllStringSubmatchIndex` over the post-embed normalised text (so `![[x]]` is not re-matched as `[[x]]`).

## Resolution rule (pipeline)

`internal/pipeline/*.go` — for each chunk the chunker emits (knowing `Chunk.StartCharIdx` / `EndCharIdx`):

```
chunk.Wikilinks = dedup(firstOccurrenceOrder(
    span.Target for span in wikilinkSpans
    if chunk.StartCharIdx <= span.Offset < chunk.EndCharIdx
))
```

Offset-**containment** (not start-offset): a wikilink is a point in the text; it belongs to the one chunk whose byte range contains it. (Contrast `SectionContext`, which uses the heading active at the chunk's *start*.) Because the chunker splits at paragraph/segment boundaries and a wikilink is inline within a paragraph, no link can straddle a chunk boundary — the containment rule assigns each link to exactly one chunk (FR-005).

After resolution, the pipeline **deletes `wikilink_spans`** from the document metadata map before `GenerateID` and store — exactly the existing `heading_spans` drop. This is the load-bearing identity-safety step (constitution Principle II; research R3).

## Entity: `QueryHit` (delta)

`internal/engine/types.go:56` — canonical projection source gains one field:

```go
type QueryHit struct {
    // ...existing fields...
    SectionContext []string
    // Wikilinks are the chunk's Obsidian [[wikilink]] targets (spec 036 / BL-004).
    // nil/absent for chunks with no wikilinks or pre-feature. Surfaced 1:1 by
    // every transport (FR-009).
    Wikilinks []string
}
```

Populated in `internal/engine/query.go` beside the existing `SectionContext` copy: `hit.Wikilinks = chunk.Wikilinks`. Also surfaced on the spec 035 `GetChunk` result (the chunk projection already carries every Chunk field).

## Identity & storage invariants (constitution Principle II)

- **Chunk ID** = `GenerateID(chunkText, mimeType, {document_id, chunk_index})`. Unchanged — `Wikilinks` is not an input.
- **Document ID** = `GenerateID(content, mimeType, metadata)`. The transient `wikilink_spans` is removed from `metadata` before this call, so the document ID is unchanged for the same content.
- **On-disk layout**: additive `omitempty` JSON field on prefix `0x03`. No new prefix, no key-construction change. No migration; `migrate.ExpectedVersion` unchanged (research R5).

## Validation rules (map to FRs)

- **FR-001/FR-005**: every `[[target]]` whose offset ∈ `[StartCharIdx, EndCharIdx)` appears, chunk-scoped.
- **FR-002**: `linkTarget` strips alias → target stored.
- **FR-003**: embeds/transclusions never enter `wikilinks`.
- **FR-004**: dangling targets included verbatim (no vault lookup).
- **FR-006**: deterministic — same text ⇒ identical `Wikilinks` (function of text + `linkTarget` + de-dup).
- **FR-007/FR-008**: non-markdown sources and no-link chunks ⇒ `nil` (omitted); always absent-never-error.
- **FR-010/FR-012**: identity unchanged; no migration (see invariants above).
