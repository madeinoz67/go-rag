# Data Model — Per-Chunk Section Context (spec 025, audit H23)

> Phase 1 output for `/speckit-plan`. Defines the entities, fields, and state
> transitions this feature adds. Grounded in `internal/model/model.go`,
> `internal/reader/markdown.go`, `internal/pipeline/pipeline.go`,
> `internal/engine/types.go`. Design decisions live in `research.md` (R1–R8).

## Entity chain (unchanged shape)

```
Source 1:N Document 1:N Chunk 1:1 Embedding      (PRD §6, model.go:1-13)
```

This feature touches **one persisted entity** (`Chunk`) and adds **one
transient (non-persisted) value type** produced by the Markdown reader. No new
entity, no new Pebble key-space prefix, no new storage index.

---

## 1. `model.Chunk` — gains one field

`internal/model/model.go:71-89`.

Existing fields (unchanged): `ID, DocumentID, Content, ChunkIndex, TotalChunks,
StartCharIdx, EndCharIdx, PageNumber, PreviousChunkID, NextChunkID, TokenCount,
CreatedAt, Poisoning`.

**New field:**

```go
// SectionContext is the ordered heading breadcrumb active at the chunk's start
// position in the source document (top-level → governing heading), e.g.
// ["Operations", "Backups", "Retention"]. Derived positionally from the
// reader's heading structure during chunking (audit H23/spec 025). nil for
// chunks whose source has no headings and for chunks written before this
// feature — treated as absent (never an error) at retrieval (FR-006). A
// non-identity sidecar (like Poisoning): it does NOT participate in the chunk
// ID (model.GenerateID at pipeline.go:252 folds text+mime+{doc,idx} only).
// Surfaced on QueryHit across every transport (FR-004).
SectionContext []string `json:"section_context,omitempty"`
```

**Invariants / validation rules (from the spec FRs):**

| Rule | Source | How enforced |
|------|--------|--------------|
| Ordered path top→governing, not a flat list | FR-005 | Resolver (R5) builds from a heading stack |
| Deterministic for a straddling chunk = heading at **start** position | FR-007 | `last span with offset ≤ StartCharIdx` |
| Absent (nil) when the source has no headings | FR-006 | Reader emits no spans → resolver returns nil |
| Does not change chunk text/size/overlap/geometry | FR-008 | Field is additive; chunker untouched (R1) |
| `#` inside fenced code is not a heading | FR-009 | Unified code-fence-aware scan (R4) |
| Re-adding an unchanged file is a no-op | FR-003 | Sidecar, not identity; content-hash dedup intact (R2) |

**Why `[]string` with `omitempty` (not a pointer).** A nil slice serialises to an
**absent** JSON field (`section_context` omitted), which is exactly US3's
"absent/empty, not an error." A pointer-to-slice would add ceremony for no
benefit; the nil-vs-empty distinction is irrelevant because a heading-less
document and a one-heading document are both well-defined (the former omits, the
latter emits a one-element path — US1-scenario-3). `omitempty` matches the
existing `Poisoning` precedent's intent (absent when not applicable).

**Identity — explicitly out of scope.** `pipeline.go:252` is unchanged:
`cid := model.GenerateID(s.Text, doc.MimeType, map[string]any{"doc": docID, "idx": i})`.
`SectionContext` is assigned to the struct *after* `cid` is computed and does
not feed it (R2). Document identity (`model.GenerateID(content, mime, metadata)`,
`model.go:46`) is also unchanged because the pipeline removes the transient span
key from `metadata` before that call (see §3).

---

## 2. `HeadingSpan` — transient reader value type (NOT persisted)

A new unexported type, local to the reader (or a small shared `internal/reader`
type if the pipeline needs the name). It lives only for the duration of one
ingest; the pipeline consumes it and drops it. It is **never** written to Pebble.

```go
// HeadingSpan is one in-body Markdown heading with its position in the text the
// chunker will receive. Offset is a byte index into the STRIPPED text returned
// by the reader; the pipeline translates it into redacted-text space (research
// R3) before resolving breadcrumbs. Produced by the unified code-fence-aware
// scan that replaces the two divergent passes in markdown.go (research R1/R4).
type HeadingSpan struct {
    Level  int    // 1..6 (#..######)
    Text   string // heading text, markers stripped, trimmed
    Offset int    // byte offset into the reader's returned (stripped) text
}
```

**Carried through the reader metadata map** under a reserved key (e.g.
`"heading_spans"`), because the `FileReader.Read` signature is fixed
(`reader/reader.go:19-20` returns `(string, map[string]any, error)`) and changing
it would violate Constitution V (extension by interface — every reader would
need editing). The Markdown reader populates the key; every other reader does
not, so section context is simply absent for them (FR-006 graceful, no
synthesis — per the spec Assumptions).

**Code-fence awareness (FR-009).** The unified scan tracks `inCode` exactly as
`stripMarkdown` does today (`markdown.go:144-155`), so a `# comment` or
`#!/bin/sh` inside a fence is neither collected as a heading nor mis-offset.

---

## 3. Pipeline flow — where each value is set

`internal/pipeline/pipeline.go:206-301` (`processFile`). Annotated with the
feature's additions (★). Nothing in the existing order moves; two additive steps
are inserted at existing sites.

```
raw = ReadFile(path)                              # :207
ch  = ContentHash(raw)                            # :211
if ch seen: return SKIPPED                        # :214  (FR-003 dedup, unchanged)
content, metadata, _ = reader.Read(raw)           # :223
  ★ metadata["heading_spans"] = []HeadingSpan (Markdown only)   # R1/R4
  ★ extract+remove "heading_spans" from metadata BEFORE identity # R2 (docID stable)
docID = GenerateID(content, mime, metadata)       # :228  (byte-identical to today)
if redactor: content, edits = redactor.Apply(content)           # :231  (+edits, R3)
segs = splitter.Split(content)                    # :249  (unchanged chunker)
for i, s := range segs:
    cid = GenerateID(s.Text, mime, {doc, idx})    # :252  (SectionContext NOT in cid)
    chunks[i] = Chunk{ ...existing fields... }    # :253
  ★ translate heading spans stripped→redacted space via edits (R3, once)
  ★ chunks[i].SectionContext = resolveBreadcrumb(spans, s.StartCharIdx)  # R5
linked-list Previous/Next                         # :267
poisoning scoring (if detector)                   # :283
storeDocument(doc, chunks, ch)  → one Sync        # :294  (SectionContext rides the record)
```

**`resolveBreadcrumb(spans, startIdx) []string` (R5).** Sort spans by offset
(mostly already in order); walk, maintaining a heading stack; the breadcrumb is
the stack state at the last span with `Offset ≤ startIdx`. Returns nil if there
are no spans. This is the single correctness function and the unit-test target
for US2 (SC-001).

**`translateOffset(offset, edits) int` (R3).** Apply the cumulative
`(InsertedLen − RemovedLen)` delta for every edit at `Pos ≤ offset`. Identity
when `edits` is empty (redactor disabled — the default).

---

## 4. `engine.QueryHit` — gains one field (the canonical projection)

`internal/engine/types.go:52-69`.

```go
// SectionContext is the ordered heading breadcrumb active at this chunk's start
// position (top-level → governing heading), e.g. ["Operations","Backups",
// "Retention"]. nil/absent for chunks with no section context (heading-less
// source or pre-feature chunk). Surfaced 1:1 by every transport (FR-004).
SectionContext []string
```

Populated in the hit-building loop at `engine/query.go:243-253`, immediately
alongside the existing `Poisoning: c.Poisoning` copy (`query.go:252`):

```go
out = append(out, QueryHit{
    ...existing fields...
    Poisoning:      c.Poisoning,      // :252
    SectionContext: c.SectionContext, // ★ new — same one-line copy
})
```

The four transport projections of this field are specified in `contracts/api.md`.

---

## 5. State transitions & migration

| Chunk state | `SectionContext` | Retrieval behaviour |
|-------------|------------------|---------------------|
| Ingested pre-feature | `nil` (field absent in stored JSON) | Hit omits `section_context` — absent, no error (US3-2) |
| Heading-less source, ingested post-feature | `nil` (no spans emitted) | Hit omits `section_context` (US3-1) |
| Heading-bearing source, ingested post-feature | non-nil ordered path | Hit carries the breadcrumb (US1/US2) |

**Migration is additive and lazy.** Old chunk records are never rewritten; they
simply deserialize with a nil `SectionContext` (Go JSON: a missing field leaves
the slice nil — US3-2). An operator who wants section context on the back-catalog
runs `Reprocess` (re-reads the source, re-derives spans); there is **no** cheap
`Rescan` path, because the raw heading structure was not persisted (R7 — distinct
from `poison.RescanPoisoning`, which works only because its signal lives in the
persisted `Content`).

**Idempotency (FR-003).** Re-adding an unchanged file short-circuits at the
content-hash gate (`pipeline.go:214`) before the reader runs, so no duplicate
document or chunk is ever created. Because `SectionContext` is a non-identity
sidecar (R2) and the span key is removed before identity, neither document nor
chunk IDs change — the no-duplicate guarantee holds at both levels.

---

## 6. What does NOT change

- `Source`, `Document`, `Embedding` entities — untouched.
- Pebble key-space prefixes (`PrefixChunk` 0x03 etc.) — no new prefix, no new
  index (section context is not filterable in v1; it rides the chunk record).
- `internal/chunk` splitter — untouched (FR-008).
- `FileReader` interface signature — untouched (Constitution V).
- Write ACK path ordering and the single `Sync` per batch — untouched (the
  feature rides the existing record, R8).
- Embeddings / indexed text — untouched (H23 is structural-only; it does **not**
  change the text sent to the embedder, per the spec Assumptions).
