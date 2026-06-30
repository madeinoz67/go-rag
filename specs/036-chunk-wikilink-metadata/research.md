# Research — Chunk Wikilink Metadata (BL-004)

> Phase 0 output for `/speckit-plan`. Every question grounded in source read this session (`internal/reader/markdown.go`, `internal/model/model.go`, `internal/engine/types.go`, `proto/gorag.proto`, `internal/rest/types.go`) and in the direct prior art: spec 025 (chunk-section-context, audit H23). No `NEEDS CLARIFICATION` remains.

## Spec reconciliation (read first)

The spec (`spec.md`) carried three assumptions from the backlog's BL-004 entry. Phase 0 verified each against code; **two are wrong, one is imprecise**. The design in `plan.md` / `data-model.md` / `contracts/api.md` follows the corrected understanding. A follow-up `/speckit-clarify` should sync `spec.md`:

| Spec claim | Verified finding | Disposition |
|-------------|------------------|-------------|
| "the reader already computes the wikilink set — serialise an existing value, no new computation" (Assumptions; FR-013) | The reader *parses* `[[wikilink]]` (`reObsidianLink`) but **discards the target** — it only does display substitution. The only set it returns is `transcludes` = `![[note]]` targets (R1). | **Add** plain-wikilink collection in the `reObsidianLink` callback (R1). FR-013's intent ("don't write a second parser") still holds — reuse `linkTarget()`. |
| "`metadata["wikilinks"]` is an entry in the existing `Chunk.metadata` map all transports serialise" (FR-009; Key Entities) | `Chunk` has **no `Metadata` map** — that field is on `Document` only (R2). Per-chunk reader-derived values are dedicated `omitempty` sidecar fields. | **Replace** with a dedicated `Chunk.Wikilinks []string` field (R2/R3). |
| "no new stored state … no schema migration" (FR-012; Assumptions) | A new persisted Chunk field IS new stored state, but it is **non-identity and backward-compatible** (R5). No migration is still correct — but for the additive-omitempty precedent, not because there is no new field. | **Keep** the no-migration conclusion; **correct** the rationale (R5). |
| Anchor-fragment default "preserve `#anchor` verbatim" (Edge Cases; Assumptions) | `linkTarget()` **already strips** alias (`\|`) and anchor (`#`) → canonical note name (R4). | **Align** with `linkTarget`: strip alias + anchor (R4). |

---

## R1 — Does the reader already collect `[[wikilink]]` targets?

**Finding.** `normalizeObsidian(s) (string, []string)` (`internal/reader/markdown.go:84`) runs two regex passes:

```go
s = reObsidianEmbed.ReplaceAllStringFunc(s, func(m string) string {   // ![[x]]
    inner := strings.TrimSpace(m[3 : len(m)-2])
    if isMediaEmbed(inner) { return inner }                            // media: token only
    transcludes = append(transcludes, linkTarget(inner))               // note transclusion: collected
    return linkDisplay(inner)
})
s = reObsidianLink.ReplaceAllStringFunc(s, func(m string) string {    // [[x]]
    return linkDisplay(m[2 : len(m)-2])                                // target DISCARDED
})
return s, transcludes
```

- `reObsidianEmbed` (`![[…]]`) collects **note-transclusion** targets into `transcludes` (media embeds are kept as tokens, not collected).
- `reObsidianLink` (`[[…]]`) substitutes the display text and **discards the target**. No wikilink set is returned.

The reader's docstring even says so: *"`[[Note]]` wikilink → the link's display text (alias/heading aware)"* — it keeps display text, not targets.

**Decision.** Add target collection to the `reObsidianLink` branch: capture `linkTarget(inner)` into a `wikilinks` slice (with byte offset — see R3). Reuse `linkTarget()`; do **not** introduce a second parser (honors FR-013's intent). Media embeds and note-transclusions remain out of scope for `wikilinks` (FR-003): plain `[[wikilink]]` only, mirroring the backlog.

**Rationale.** Minimal, surgical change at the one place the syntax is already recognised. `transcludes` (note-transclusions) stays a separate signal — a transclusion is an inclusion, not a reference edge, and BL-004 scopes itself to wikilinks.

**Alternatives rejected.**
- *Serialise `transcludes` as the wikilink set.* Rejected — wrong content (transclusions, not wikilinks) and BL-004 explicitly excludes embeds.
- *A second regex pass over the stripped text.* Rejected — `reObsidianLink` already fires once; a second pass is duplicate work and risks drift (FR-013).

---

## R2 — Is there a `Chunk.metadata` map, or a dedicated field?

**Finding.** `Chunk` (`internal/model/model.go:80`) has **no `Metadata` map**. Its fields are: identity (`ID`, `DocumentID`), content/position (`Content`, `ChunkIndex`, `TotalChunks`, `StartCharIdx`, `EndCharIdx`, `PageNumber`, `PreviousChunkID`, `NextChunkID`, `TokenCount`, `CreatedAt`), and a family of **non-identity sidecar fields**: `Poisoning *PoisonVerdict`, `SectionContext []string`, `NearDup *NearDupInfo`, `Caption *CaptionInfo`, `Kind string`. The `Metadata map[string]any` at `model.go:32` belongs to **`Document`**, not `Chunk`.

**Decision.** Add a dedicated sidecar field — `Wikilinks []string` with `json:"wikilinks,omitempty"` — directly mirroring `SectionContext` (spec 025). It joins the established non-identity sidecar family.

**Rationale.** This is the repo's one pattern for reader-derived, per-chunk, cross-transport data. Every prior attribute (poisoning verdict, section breadcrumb, near-dup, caption, kind) is its own `omitempty` field — never a metadata-map key. Following it keeps serialization, parity tests, and the "non-identity sidecar" invariant uniform.

**Alternatives rejected.**
- *Add a `Metadata map[string]any` to `Chunk` and store under `"wikilinks"`.* Rejected — introduces a whole new per-chunk map (a larger contract change, new identity/participation questions) where a single typed field suffices, and breaks consistency with the five existing sidecars.
- *Store on `Document.Metadata`.* Rejected — BL-004 is explicitly **chunk-scoped** (FR-005); a document-level value would force the bridge to re-derive per chunk.

---

## R3 — How is a reader-derived value made chunk-scoped, non-identity, and cross-transport?

**Finding.** Spec 025 (audit H23) already solved this end-to-end for `SectionContext`. The reusable pipeline:

1. **Reader** emits a **transient positional span table** as document metadata: today `md["heading_spans"]` ([]`HeadingSpan{Level,Text,Offset}`), where `Offset` is a byte index into the stripped text the reader returns. The table is "consumed + dropped by the pipeline before identity/store" (`markdown.go:46`).
2. **Pipeline** knows each chunk's byte range from the chunker (`Segment.StartCharIdx` / `Chunk.StartCharIdx`-`EndCharIdx`). It resolves the per-chunk value from the spans and assigns it to the chunk field. `SectionContext` uses the *start-offset* rule (the heading active at the chunk's start).
3. **Identity safety:** the span table is removed from document metadata **before** `GenerateID` (which hashes the metadata map), and the sidecar field does not participate in the chunk ID — so neither identity changes (constitution Principle II).
4. **Transport:** the field lives canonically on `engine.QueryHit`; every transport (REST/gRPC/CLI/MCP) projects it; `parity_test.go` asserts identity.

**Decision.** BL-004 reuses this pipeline verbatim with one difference in the resolution rule:

- Reader emits `md["wikilink_spans"]` = `[]WikilinkSpan{Target string, Offset int}` (offset = byte index into stripped text, computed the same way `stripMarkdownSpans` computes heading offsets).
- Pipeline resolves each chunk's `Wikilinks` by **offset containment** — a span belongs to the chunk whose `[StartCharIdx, EndCharIdx)` contains `span.Offset` — de-duplicated, first-occurrence order. (Containment, not start-offset: a wikilink is a point in the text, not a section that governs a range.)
- Pipeline drops `wikilink_spans` from document metadata before `GenerateID`/store (mirror the `heading_spans` drop).
- Canonical field on `engine.QueryHit`; project to all four transports + `GetChunk`; extend `parity_test.go`.

**Rationale.** One pattern, already proven and heavily tested. The only new logic is offset-containment resolution (a few lines), which is strictly simpler than `SectionContext`'s stack algorithm.

**Alternatives rejected.**
- *Whole-document wikilink set on the chunk.* Rejected — violates FR-005 (chunk-scoped).
- *Persist the span table.* Rejected — R7 of spec 025 established that positional reader signals are deliberately not persisted; back-fill is via `Reprocess`.

---

## R4 — Wikilink grammar: alias, anchor, embeds, duplicates, code-context?

**Finding.** The reader already has the exact extractor BL-004 needs: `linkTarget(inner)` (`markdown.go:118`) strips both alias (`|`) and anchor (`#`) → canonical note name. `linkDisplay(inner)` does the opposite for display. `isMediaEmbed(inner)` (`markdown.go:131`) classifies media by extension (`.png/.jpg/.jpeg/.gif/.bmp/.svg/.webp/.pdf`); `.md`/`.txt` are treated as note transclusions.

**Decision.**

| Syntax | Result | Source |
|--------|--------|--------|
| `[[target]]` | `"target"` | `linkTarget` |
| `[[target\|display]]` | `"target"` (alias stripped) — **FR-002** | `linkTarget` |
| `[[target#heading]]` | `"target"` (anchor stripped) | `linkTarget` — **confirmed by clarification Q1, 2026-06-30** |
| `[[target#^blockid]]` | `"target"` (block-ref stripped) | `linkTarget` |
| `![[image.png]]` / `![[note]]` | excluded — **FR-003** | not in the `reObsidianLink` pass; embeds/transclusions stay separate |
| duplicate `[[a]] … [[a]]` in one chunk | `"a"` once, first-occurrence order | de-dup at resolution (R3) |
| `[[concepts/auth]]` / `[[folder/sub/Note]]` (path-prefixed) | `"concepts/auth"` — **path preserved verbatim** | `linkTarget` keeps path; basename-stripping is resolution go-rag can't do (FR-004) — **clarified Q5, 2026-06-30** |
| wikilink inside a fenced/inline-code context | **excluded** | collection pass is code-fence-aware (reuses `stripMarkdownSpans`) — **clarified Q3, 2026-06-30** |

**Rationale.** `linkTarget` is the canonicaliser the codebase already uses for transclusion targets; using it for wikilinks keeps one definition of "target" and satisfies FR-002/FR-003 for free. Stripping the anchor (not preserving it) matches the bridge's need — the Hebbian edge endpoint is the *note*, not a heading within it — and matches existing behaviour, so no grammar divergence is introduced.

**Note (code-context) — REVISED by clarification Q3 (2026-06-30).** `reObsidianLink` is a plain regex and is **not** code-fence-aware, so a `[[not-a-link]]` inside a fenced/inline-code block *would* match. Research's original disposition was to keep that behaviour; **clarification Q3 reversed it: EXCLUDE code-context matches** — Obsidian treats code as literal (no link), so a code-context `[[example]]` is not a real backlink and would pollute the Hebbian graph. Implementation: the collection pass becomes code-fence-aware, reusing the reader's existing `stripMarkdownSpans` pattern (run wikilink detection with `inCode` state). Encoded as FR-014 in the spec.

**Alternatives rejected.**
- *Preserve `#anchor` (spec's stated default).* Rejected — diverges from `linkTarget`, and the edge endpoint should be the note.
- *Validate targets against the vault index.* Rejected — FR-004 forbids it; the reader has no vault view at read time.

---

## R5 — Does adding a Chunk field require a schema migration?

**Finding.** Chunk records are JSON-marshaled blobs under Pebble prefix `0x03`. Go's `encoding/json` is tolerant: an old blob missing the new field decodes to a nil slice (no error); a new blob with the field simply has extra data old code ignores. Every prior sidecar (`Poisoning`/`SectionContext`/`NearDup`/`Caption`/`Kind`) was added this way with `omitempty` and **no migration**.

The spec-034 schema-evolution rule keys on the key-space *layout* — "a new/retired prefix, value encoding, or key construction." An additive `omitempty` JSON field on an existing prefix changes none of those: same prefix, same key construction, backward-compatible decode.

**Decision.** **No migration.** `migrate.ExpectedVersion` is unchanged. The PR affirms: *no on-disk key-space layout change*. See `plan.md` Complexity Tracking.

**Rationale.** There is no v(n)→v(n+1) transform to perform — old data is already valid under the new shape. A no-op migration would add registry ceremony with zero behavioral difference.

**Alternatives rejected.**
- *Add a marker migration + bump `ExpectedVersion`.* Rejected as default (ceremony). Available as a fallback if a reviewer wants strict registry parity — but it transforms nothing.

---

## R6 — Pre-feature chunks and back-fill?

**Finding.** Spec 025 R7 set the precedent: a reader-derived positional signal is **not persisted** (only `Chunk.Content` is), so it cannot be recomputed from stored data the way `Poisoning` can (`engine.RescanPoisoning` re-scores from `Chunk.Content`). Back-fill is therefore via **`Reprocess`** (re-read the source file, re-derive spans), not a cheap rescan.

**Decision.**
- `Chunk.Wikilinks` is `nil` (→ omitted via `omitempty`) for (a) sources with no wikilinks and (b) chunks written by a pre-feature build. Retrieval returns **absent**, never an error (FR-008).
- Back-fill is an operator action: `Reprocess` the path. Consistent with every other reader-derived attribute (`pipeline.go` applies reader changes to the back-catalog only via re-ingest).
- Consumers treat absence as "no wikilinks" (the bridge writes no edges for that chunk).

**Rationale.** Honest about the cost; matches spec 025 R7 exactly.

---

## R7 — gRPC / REST / proto field tags and projection points?

**Finding.** Read this session:

- `engine.QueryHit` (`internal/engine/types.go:56`) — canonical; fields end with `EnrichmentStatus`. `SectionContext` is at line 77.
- gRPC `QueryHit` (`proto/gorag.proto`) — tags 1–12 used (`enrichment_status = 12`). **Next free = 13.**
- gRPC `Chunk` (`proto/gorag.proto:170`, defined by spec 035 for `GetChunkResponse`) — tags 1–16 used (`created_at = 16`). **Next free = 17.**
- REST `queryHit` (`internal/rest/types.go:23`) — `SectionContext` at line 33 with `json:"section_context,omitempty"`.

**Decision.** Two additive proto fields, both `repeated string`:

```proto
// message QueryHit (proto/gorag.proto)
repeated string wikilinks = 13; // spec 036 / BL-004: Obsidian [[wikilink]] targets in this chunk (absent = none/pre-feature)

// message Chunk (proto/gorag.proto:170)
repeated string wikilinks = 17; // spec 036 / BL-004
```

Projection points (canonical → transports), mirroring `SectionContext`:

| Layer | File | Change |
|-------|------|--------|
| Engine (canonical) | `internal/engine/types.go` | `QueryHit.Wikilinks []string` |
| Engine (projection) | `internal/engine/query.go` | copy `chunk.Wikilinks → hit.Wikilinks` (beside the `SectionContext` copy) |
| Engine (GetChunk) | `internal/engine/get_chunk.go` (spec 035) | surface `Chunk.Wikilinks` on the returned chunk |
| gRPC | `proto/gorag.proto` + `proto/gen` + `internal/grpc` | `Chunk.17`, `QueryHit.13`; regenerate; wire mapping |
| REST | `internal/rest/types.go` | `queryHit.Wikilinks` + the GetChunk chunk shape, `json:"wikilinks,omitempty"` |
| CLI | `internal/cli/query.go` + the `chunk get` command | render wikilinks |
| MCP | `internal/mcp/server.go` | include wikilinks in the hit + `go_rag_get_chunk` tool |
| Parity | `internal/engine/parity_test.go` | assert `wikilinks` identical across CLI/REST/gRPC/MCP |

`proto3` repeated-field semantics already give the absent/empty behaviour (unset → nothing on the wire → clients receive an empty list). See `contracts/api.md` for the full wire contract.

**Rationale.** This is the four(+1)-projection template spec 025 R6 documented for `SectionContext`; reusing it keeps parity-test coverage uniform and makes the field available everywhere a chunk surfaces (query hits **and** `GetChunk`).

---

## Summary of decisions

| # | Question | Decision |
|---|----------|----------|
| R1 | Wikilink set already collected? | No — **add** collection in the `reObsidianLink` branch, reusing `linkTarget`. |
| R2 | Map key or dedicated field? | Dedicated `Chunk.Wikilinks []string` sidecar (no per-chunk map exists). |
| R3 | Chunk-scoped + non-identity + cross-transport? | Reuse the spec 025 span pipeline; resolve by **offset containment**; drop transient before identity. |
| R4 | Grammar? | `linkTarget` rules: strip alias + anchor; exclude embeds/transclusions; de-dup, first-occurrence order. |
| R5 | Migration? | **No** — additive `omitempty` field, backward-compatible. |
| R6 | Old chunks / back-fill? | `nil` → absent (never error); back-fill via `Reprocess`. |
| R7 | Proto/transport tags? | gRPC `Chunk = 17`, `QueryHit = 13`; 5 projection points; extend `parity_test.go`. |
