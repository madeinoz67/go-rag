# Research — Per-Chunk Section Context (spec 025, audit H23)

> Phase 0 output for `/speckit-plan`. Resolves every NEEDS CLARIFICATION and
> design decision against the live codebase (read in full this session). Every
> claim cites `file:line`. The companion `data-model.md`, `contracts/api.md`,
> and `quickstart.md` build directly on these decisions.

---

## R1 — Where is heading structure lost today, and what is the precise insertion point?

**Finding.** Structure is destroyed in **two separate places**, and the chunker
never sees it:

1. `internal/reader/markdown.go:36-45` — `MarkdownReader.Read` collects every
   `#`-prefixed line into `headings []string` (a **flat list**, no level, no
   byte offset), then **discards the markers** by returning
   `stripMarkdown(body)` (`markdown.go:53`).
2. `internal/chunk/chunk.go:121` `Split` operates on that stripped string and
   emits `Segment{Text, StartCharIdx, EndCharIdx, TokenCount}` (`chunk.go:22-27`)
   whose indices are into the **stripped text** — the heading markers are gone,
   so no segment can know which heading governs it.

The pipeline wires them together at `internal/pipeline/pipeline.go:223-264`:
`reader.Read` → `splitter.Split(content)` → a `model.Chunk` is built per
`Segment`. The linked-list (`pipeline.go:267-274`) and poisoning scoring
(`pipeline.go:283-291`) are both attached **in this same loop, before
`storeDocument`** (`pipeline.go:294`). That is the proven insertion site for a
new per-chunk attribute — exactly how `Poisoning` was added (spec 019).

**Decision.** Thread section context in at the reader→chunker seam:
- The Markdown reader emits, alongside the stripped text, a **positional
  heading-span table** (heading level + text + offset into the returned
  stripped text). See R4/R8 for why this replaces the current flat list.
- The pipeline resolves each `Segment`'s breadcrumb from that table
  (R3/R5) and writes it onto the `Chunk` in the existing construction loop.
- The chunker itself is **untouched** (FR-008: chunking geometry unchanged).

**Rationale.** This is the only seam where both the heading structure and the
chunk positions are visible to one component (the pipeline). Every prior
per-chunk attribute (linked list, poisoning) was attached here.

**Alternatives rejected.**
- *Store a document-level flat heading list and guess at query time* — already
  what `metadata["headings"]` is today; it cannot answer "which heading governs
  *this* chunk" (the whole point of H23). Rejected.
- *Change the chunker to be heading-aware* — violates FR-008 (geometry frozen
  by spec 013) and couples two concerns. Rejected.

---

## R2 — Does section context participate in identity? How is FR-003 preserved?

**Finding.** Identity today:

- **Document** — `model.GenerateID(content, mimeType, metadata)`
  (`model/model.go:46-59`): SHA-256 over content + mime + the **sorted metadata
  map**. The flat `headings` list is already part of this hash (it lives in
  `metadata`).
- **Chunk** — `pipeline.go:252`:
  `cid := model.GenerateID(s.Text, doc.MimeType, map[string]any{"doc": docID, "idx": i})`.
  Chunk identity folds in **text + mime + {doc, idx} only** — not arbitrary
  metadata, and not `Poisoning` (the existing sidecar).
- **Dedup short-circuit** — `pipeline.go:211-216`: `ContentHash(raw)` (raw bytes
  only, `model.go:64-68`) is checked **before the reader even runs**. An
  unchanged file returns `SKIPPED` regardless of any metadata change.

**Decision.** Section context is a **non-identity sidecar**, mirroring
`Chunk.Poisoning *PoisonVerdict` (`model.go:88`) exactly:
- It does **not** enter the chunk `cid` computation.
- The reader's positional span table is carried under a reserved metadata key
  (e.g. `heading_spans`) and **extracted-and-removed by the pipeline before
  `GenerateID` is called** (`pipeline.go:228`), so the document identity is
  **byte-identical to today** for Markdown documents — zero identity churn,
  zero forced re-embed. (The flat `metadata["headings"]` list is left as-is.)

**Rationale.** This makes the feature purely additive: section context cannot
change any document or chunk ID, so re-adding an unchanged file is a no-op at
*both* the content-hash gate and the identity level. FR-003 / US3-scenario-3
hold trivially. It also matches the one precedent in the codebase (`Poisoning`).

**Alternatives rejected.**
- *Fold section context into chunk identity* — would change `cid` whenever a
  heading is edited, forcing re-embed of unchanged text and breaking the
  "re-embed under a new model without duplicate documents" property
  (`model.go:62-63`). Rejected.
- *Persist the span table on the Document* — it is only needed at chunk time;
  persisting it bloats the 0x02 record for no query-time benefit. The pipeline
  consumes and drops it (R8). Rejected.

---

## R3 — How do heading offsets and chunk `StartCharIdx` share a coordinate space? (redaction)

This is the central design constraint and the one genuine subtlety.

**Finding.** Redaction runs **before** chunking and is **variable-length**:
- `pipeline.go:231-233` redacts `content` after identity, before
  `splitter.Split(content)` at `pipeline.go:249`. So `Segment.StartCharIdx`
  (`chunk.go:121`) is an index into the **redacted** text.
- `internal/redact/redact.go:50-53` replaces each secret with
  `[REDACTED:<type>]` (`placeholder`, `redact.go:62-64`) — a **different length**
  than the matched secret. LUHN-card redaction (`redact.go:67-77`) likewise
  substitutes a fixed placeholder for a variable-length number.

So if the reader records heading offsets in **stripped-text** space and the
chunker records `StartCharIdx` in **redacted-text** space, the two coordinate
systems diverge by the cumulative length delta at every redaction site.
Resolving "the heading active at the chunk's start" by comparing these offsets
directly would be wrong whenever a secret was masked before the chunk.

**Decision (recommended).** Make the redactor **offset-aware** additively, and
translate the heading-span offsets through it:

1. `redact.Scanner.Apply` gains an **additional return value** — a sorted slice
   of edits `{Pos, RemovedLen, InsertedLen}` — which it already visits inside
   `ReplaceAllStringFunc` (`redact.go:50`, `redact.go:69`). Existing callers
   ignore the new value (backward compatible; `pipeline.go:232` is a 2-return
   call today).
2. The pipeline calls `Apply` once (as today), receives the redacted text **and**
   the edit list, then translates each heading span's stripped-text offset into
   redacted-text space with an O(edits) `translateOffset` scan.
3. Result: heading spans and `Segment.StartCharIdx` are now in the **same**
   (redacted) coordinate space → the resolver in R5 is exact.

When the redactor is disabled (the default — `pipeline.go:57` `redactor` is
`nil` unless configured), there are no edits and translation is the identity
function. The common case costs nothing.

**Rationale.** This is the only approach that keeps a single coordinate space
**without reordering the pipeline**. Redaction-before-storage is a privacy
invariant (stored `Chunk.Content` must be redacted) and redaction-before-identity
is a Constitution-II invariant (`pipeline.go:229-230`); neither may move.
Chunking geometry (FR-008) and chunk identity (FR-003) are both left byte-for-byte
unchanged.

**Alternatives rejected.**
- *Reorder to split-then-redact* — would store unredacted `Chunk.Content`
  (privacy regression) and compute `cid` over unredacted text (identity churn).
  Rejected.
- *Split twice (once on stripped text for context, once on redacted for storage)*
  — the two splits can produce different segment counts when redaction shifts a
  span across the `MinTokens` merge threshold (`chunk.go:211-219`), making
  1:1 breadcrumb↔chunk alignment impossible. Fragile. Rejected.
- *Re-detect headings on the redacted text* — headings survive stripping as
  plain lines (`markdown.go:156-158` strips `#` but keeps the text), but
  locating them by text is ambiguous on duplicate headings and breaks if
  redaction touches a heading. Fragile. Rejected.

---

## R4 — Does current heading detection respect fenced code blocks? (FR-009)

**Finding. No.** This is a real pre-existing gap that H23 must close.

- The heading-detection loop (`markdown.go:37-42`) scans the raw `body`
  line-by-line with `strings.TrimLeft(line, "#")` and **no code-fence state**.
  A line like `# comment`, `#!/bin/sh`, or heading-like text inside a ```` ``` ````
  block is wrongly appended to `headings`.
- `stripMarkdown` (`markdown.go:142-168`) **does** track `inCode`
  (`markdown.go:144-155`). So today the two passes **disagree** about code
  fences — a heading can be "detected" yet not appear in the stripped text at the
  expected place, or appear without its marker.

**Decision.** Replace the two divergent passes with **one unified, code-fence-aware
scan** that produces both (a) the stripped text and (b) the positional
heading-span table of R1, with offsets taken from the stripped-text builder
length at the moment each heading's text is written. Code-fence state is tracked
once, consistently. This fixes FR-009 *and* yields offsets that are inherently
aligned with `stripMarkdown`'s output (then translated per R3).

**Rationale.** One pass, one truth. It removes the disagreement, satisfies FR-009,
and is the natural source of the R1 span table. Edge cases (`[[wikilink]]`,
`![[embed]]`, front-matter `title`) are already normalised before this point
(`markdown.go:47-51`, `markdown.go:29-34`) and are unaffected.

---

## R5 — How is the breadcrumb represented, and how are nesting + straddling handled? (FR-005, FR-007)

**Decision.**
- **Wire shape:** an **ordered path, top-level → governing heading**, modelled as
  `[]string` (e.g. `["Operations", "Backups", "Retention"]`). Display joins with
  `" / "` → `Operations / Backups / Retention` (matches US1 acceptance wording).
  This is an ordered path, **not** the document's flat heading list (FR-005).
- **Nesting (stack algorithm):** walk heading spans in offset order maintaining a
  stack of `(level, text)`; on a level-`L` heading, pop entries with `level >= L`
  then push; the breadcrumb for any position is the current stack top→bottom.
  Sibling sections reset correctly; H1→H6 nesting yields the full ancestor path.
- **Straddle (FR-007 / US2-scenario-2):** a chunk starting under heading A but
  running past heading B into B's body carries **A** — the heading active at the
  chunk's **start** position. Implemented as: the breadcrumb for a chunk is the
  stack state at the **last heading span whose offset ≤ `Segment.StartCharIdx`**.
  Deterministic, documented, not configurable per chunk.
- **Path-length capping:** **no cap by default** (full ancestor path is cheap and
  correct). The edge case lists capping as a plan decision; it is deferred (a
  future `max_section_depth` config), not needed for v1.

**Rationale.** The stack algorithm is the standard, obviously-correct way to
maintain "the heading active here." Tying the chunk's value to its start offset
makes the straddle rule a single, testable line.

**Alternatives rejected.**
- *Flat list of all document headings* — explicitly prohibited by FR-005.
- *Store only the deepest governing heading* — loses the breadcrumb (US1 wants
  `Ops / Backups / Retention`, not just `Retention`). Rejected.

---

## R6 — Across how many transports must section context surface, and where? (FR-004)

**Finding.** The engine is the single source of truth; every transport is a thin
projection of `engine.QueryHit` (`internal/engine/types.go:52-69`). The
`Poisoning` field already round-trips through **four** projection points, which
is the exact template for `SectionContext`:

| Layer | File:line | Projection |
|------|-----------|------------|
| Engine (canonical) | `engine/types.go:52`, built at `engine/query.go:243-253` | `QueryHit.SectionContext []string` (copied from `c.SectionContext`, alongside `Poisoning: c.Poisoning` at `query.go:252`) |
| REST | `rest/types.go:23` `queryHit` | `SectionContext []string \`json:"section_context,omitempty"\`` |
| gRPC | `proto/gorag.proto:77` `QueryHit` | `repeated string section_context = 9;` (next free tag after `chunk_index = 8`) → regenerate `proto/gen` |
| CLI | `cli/query.go:123` `renderResults` | render the breadcrumb line per hit |
| MCP | `mcp/server.go` (hit render) | include the breadcrumb in the text/structured hit |

**Decision.** Surface `section_context` on **all four** — CLI, REST, gRPC, **and
MCP** — with an identical value for the same chunk (FR-004). The spec names three
(CLI/REST/gRPC); MCP is added at ~zero cost because it already projects every
`QueryHit` field, and the architecture's parity guarantee
(`engine/types.go:1-6`) is "every adapter invokes the same methods." Omitting MCP
would be the one transport that silently drops the field.

**Rationale.** Cross-transport parity is a first-class, heavily-tested invariant
in this repo (`internal/engine/parity_test.go` is 36 KB). Adding the field in one
canonical place (`engine.QueryHit`) and projecting it everywhere is how
`Poisoning` and `ChunkIndex` (spec 021/023) were done. The plan's tasks will
extend `parity_test.go` to assert `section_context` is identical across
transports (SC-002).

---

## R7 — How do heading-less / pre-feature chunks degrade, and can context be back-filled? (FR-006, US3)

**Finding.** `Poisoning` set the migration pattern: a pointer field, `nil` on
pre-feature chunks, "treated as clean at retrieval" (`model.go:84-88`), and
**back-fillable** by `engine.RescanPoisoning` (`engine/poison.go:120-168`) which
re-scores every stored chunk **from its persisted `Content`** without re-reading
the source file.

Section context **cannot** follow that back-fill path: the raw document's heading
structure is **not persisted** (only `Chunk.Content` is, post-strip-post-redact).
The headings were discarded at `markdown.go:53` and never stored positionally
(R2 deliberately does not persist the span table).

**Decision.**
- `Chunk.SectionContext` is `nil` (→ omitted in JSON via `omitempty`) for (a)
  heading-less documents (plain text, code-only Markdown, front-matter-only) and
  (b) chunks written by a pre-feature build. Retrieval returns **absent** context,
  never an error (FR-006 / US3-scenarios-1&2). Loading an old chunk record simply
  leaves the new field zero — Go JSON unmarshalling of a missing field is a nil
  slice, not a parse failure (US3-scenario-2).
- **Back-fill is via `Reprocess`** (re-read the source file, re-derive spans),
  **not** a cheap rescan. This is documented in `quickstart.md`. It is consistent
  with every other reader-derived attribute: a reader change applies to the
  back-catalog only through re-ingest (`pipeline.go:129-132` TODO note).

**Rationale.** Honest about the cost. The poisoning rescan works only because its
signal is derivable from persisted text; section context's signal (positional
headings) is gone post-ingest. US3 only requires *graceful absence*, which a nil
slice provides; back-fill is an operator action, not a feature requirement.

---

## R8 — Where does resolution run, and is it ACK-path-safe? (Principle IV)

**Finding.** The synchronous ACK path is `processFile` → `storeDocument`
(`pipeline.go:294`, one `Sync` per batch). `Poisoning` scoring was placed
**on the ACK path** (`pipeline.go:277-291`) on the explicit reasoning that
heuristic text-scoring is "validation-class work (cost ≈ the SHA-256 content hash
already computed here; no I/O)" and "rides the chunk record — zero extra fsync."

**Decision.** Section-context resolution runs at the **same site** — inside
`processFile`, after `splitter.Split` (`pipeline.go:249`) and before
`storeDocument` (`pipeline.go:294`), in the chunk-construction loop. It is:
- **O(headings + chunks)** of pure string/offset work per document;
- **no I/O, no allocation beyond the breadcrumb slices**;
- **rides the existing chunk record** in the existing `Sync` — zero added fsync,
  zero added ACK latency.

The redactor offset translation (R3) is likewise O(edits) and runs once per
document on the ACK path, next to the existing `redactor.Apply` call
(`pipeline.go:232`).

**Rationale.** Bit-for-bit the same cost class as poisoning scoring, which was
already judged ACK-safe. The `<10ms` write budget (Constitution IV) is
unaffected. Embedding/indexing remain async-after-ACK, untouched.

---

## Summary of decisions

| # | Decision | Honours |
|---|----------|---------|
| R1 | Thread context at the reader→pipeline seam; chunker untouched | FR-008 |
| R2 | Non-identity sidecar on `Chunk` (like `Poisoning`); span key extracted before `GenerateID` | FR-003, Constitution II |
| R3 | Additive redactor offset-edit map; translate heading offsets into redacted space | FR-001/007, Constitution IV, privacy invariant |
| R4 | One unified code-fence-aware scan replaces the two divergent passes | FR-009 |
| R5 | `[]string` ordered breadcrumb via a heading stack; straddle = start-position rule | FR-005/007 |
| R6 | Surface on CLI + REST + gRPC + MCP from one canonical `QueryHit` field | FR-004 |
| R7 | `nil`/absent for heading-less & pre-feature chunks; back-fill via Reprocess | FR-006, US3 |
| R8 | Resolve on the ACK path in `processFile`; rides the chunk record | Constitution IV |

No NEEDS CLARIFICATION remains. No constitution violation is introduced (see
`plan.md` → Constitution Check).
