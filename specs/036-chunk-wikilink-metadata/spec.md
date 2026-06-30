# Feature Specification: Chunk Wikilink Metadata — Expose Obsidian Wikilink Targets as a `Chunk.Wikilinks` sidecar

**Feature Branch**: `036-chunk-wikilink-metadata` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-06-30 · **Status**: Draft (reconciled with Phase 0 research, 2026-06-30)

**Input**: Fourth item (BL-004) of the go-rag ↔ MuninnDB bridge integration backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`), and the next open Stream-A item per the post-review map (`docs/RFC-bridge-muninndb/bridge-map-post-review.md` §7) now that BL-001 (`GetChunk`, spec 035) is implemented. *The go-rag-muninn bridge needs each chunk's `[[wikilink]]` targets exposed as a per-chunk field so it can write Obsidian backlinks as human-curated Hebbian edges into MuninnDB (pattern E2) without re-parsing markdown itself. The Obsidian-aware markdown reader already **parses** `[[wikilink]]` syntax during ingestion (via `reObsidianLink`) but currently substitutes only the display text and **discards the target** (only `![[note]]` transclusion targets are kept, as `transcludes`). BL-004 adds collection of the plain-wikilink target set — reusing the reader's existing `linkTarget()` canonicaliser, so no second parser — and exposes it as a dedicated non-identity `Chunk.Wikilinks []string` sidecar field, mirroring `SectionContext` (spec 025).*

> **Reconciled with `research.md` (Phase 0).** Three backlog assumptions were verified against code and corrected: (R1) plain wikilink targets are **not** collected today — collection must be added; (R2) `Chunk` has **no** metadata map — the value is a dedicated `omitempty` sidecar field, not a `metadata["wikilinks"]` key; (R5) this **is** new persisted state, but non-identity and backward-compatible (no migration). See `research.md` §Spec reconciliation.

**Why this item next (and not BL-002/003):** BL-001 (the reverse-lookup primitive) is done. Two P1 clusters remain unblocked: BL-002/003 (more chunk-fetch RPCs) and the metadata trio BL-004/005/006. The MuninnDB maintainer review (2026-06-30) explicitly named the **Obsidian wikilink → Hebbian-edge pipeline** "the best idea in the RFC" and bumped **BL-004** to the headline enabler, with the post-review map ordering `BL-004 → BL-005/006` as the next Stream-A work. BL-004 has no upstream dependencies. Size re-estimated **S → M** (dedicated field + collection logic + 5 transport projections + proto regen + parity tests; comparable to spec 025). *(If the intent was the literal sequential next item — BL-002 `GetChunkContext` — redirect; this spec is scoped to BL-004.)*

---

## Clarifications

### Session 2026-06-30

- Q: For `[[target#heading]]` / `[[target#^blockid]]`, keep the anchor fragment or strip it? → **A: Strip the anchor.** The stored target is the canonical note name (`[[a#h]]` → `a`, `[[a#^id]]` → `a`), aligning with the reader's `linkTarget()`. The Hebbian edge endpoint is a note, not a heading; preserving would fragment one note's backlinks across phantom `note#heading` entities, and makes alias/anchor de-dup possible in one canonical form.
- Q: Empty/absent contract — always `[]` (FR-008) or `omitempty`/absent? → **A: `omitempty`/absent (B).** `Wikilinks` is a dedicated `Chunk.Wikilinks []string` sidecar (R2); it is omitted/empty for no-link **and** pre-feature chunks. proto3 `repeated` cannot distinguish empty from absent, so cross-transport parity forces the `omitempty` convention used by every prior sidecar (`SectionContext`/`Poisoning`/`NearDup`/`Caption`). Consumers treat absence as "no edges"; staleness is handled by reprocess, not wire shape. (Was: FR-007/FR-008/US1-#5/US3 mandated always-present `[]` under the since-corrected metadata-map assumption.)
- Q: Wikilinks inside code blocks / inline code — collect or exclude? → **A: Exclude (A).** `[[x]]` inside fenced or inline code is not collected. Obsidian treats code as literal (no link), so a code-context `[[example]]` is not a real backlink; collecting it would pollute the Hebbian graph. The collection pass becomes code-fence-aware, reusing the reader's existing `stripMarkdownSpans` pattern.
- Q: Note-transclusion scope — should `![[Note]]` targets also appear in `Wikilinks`? → **A: Plain `[[wikilink]]` only (A).** Note-transclusions stay excluded from `Wikilinks`; they remain in their existing separate document-level `transcludes` signal. Keeps reference vs inclusion relations separable (the bridge may weight them differently) and is faithful to BL-004's explicit `[[wikilink]]` scope. Per-chunk transclusion attribution is a separate follow-up.
- Q: Path-prefixed targets — preserve `[[concepts/auth]]` or strip to basename? → **A: Preserve verbatim (A).** `[[concepts/auth]]` → `concepts/auth`. A path disambiguates distinct notes, and resolving it to a single note requires the vault index go-rag deliberately does not hold (FR-004); basename-stripping is a lossy resolution go-rag can't validate. Preserving is information-complete and reversible; the bridge normalises. Matches current `linkTarget()` behaviour. (Contrast anchor-strip: an anchor is a fragment of one note — safe to canonicalise away; a path identifies *which* note — needs resolution go-rag can't do.)

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A bridge client reads a chunk's wikilinks (Priority: P1)

The go-rag-muninn bridge syncs a document into MuninnDB. For each chunk it writes a memory record and, separately, wants to write Hebbian edges from that chunk's note to every other note it links to via `[[wikilinks]]` — Obsidian backlinks are explicitly human-curated graph edges, the highest-signal connection source the bridge has. To do that it needs the wikilink targets that appear in each chunk's text. Today the reader **parses** `[[wikilink]]` syntax but substitutes only the display text and discards the target — so the bridge would have to re-parse the markdown file itself to recover the targets, duplicating logic that already lives in go-rag and risking drift. Exposing a per-chunk `Wikilinks` list hands the bridge a value go-rag is best placed to compute, at the cost of one collection step reusing the reader's existing canonicaliser.

**Why this priority**: This is the unblocker for the maintainer's designated marquee feature — Obsidian backlinks → MuninnDB Hebbian edges (pattern E2). Without it, the highest-value bridge integration cannot be built without bridge-side markdown parsing. It is the single item that turns a parsed-but-discarded value into the input the bridge's edge-writing step consumes. Delivering this story alone is a viable MVP — any client reading a chunk gets its wikilink set.

**Independent Test**: Ingest a markdown document containing `[[authentication]]` and `[[JWT tokens]]`; fetch any chunk covering those links and assert `Wikilinks` contains both canonical targets. Ingest a chunk with no wikilinks and assert the field is absent/empty.

**Acceptance Scenarios**:

1. **Given** a markdown chunk whose text contains `[[target]]`, **When** a client reads the chunk, **Then** `Wikilinks` is present and contains the canonical target `target`.
2. **Given** an aliased link `[[target|display text]]` in a chunk, **When** the client reads the chunk, **Then** the stored target is `target` (the destination), not `display text`.
3. **Given** a wikilink to a file that does not exist in the vault (`[[phantom]]`), **When** the client reads the chunk, **Then** `phantom` is included verbatim — go-rag does not validate targets.
4. **Given** an embed `![[image.png]]` **or** a note-transclusion `![[Other Note]]` in a chunk, **When** the client reads the chunk, **Then** neither target is present in `Wikilinks` — embeds and transclusions are not reference edges (transclusions stay in the separate `transcludes` signal). *(Clarified 2026-06-30.)*
5. **Given** a chunk whose text contains no wikilinks, **When** the client reads the chunk, **Then** `Wikilinks` is **absent/empty** (omitempty — never an error); the consumer treats absence as "no edges."

---

### User Story 2 - The wikilink set is chunk-scoped and deterministic (Priority: P2)

A bridge building per-chunk edges needs the wikilinks that appear *in this chunk's text range*, not the union of the whole document — an edge should originate from the chunk that actually carries the link. And because chunk identity is content-addressed, the same chunk text must always yield the same wikilink set: re-ingesting an unchanged chunk must produce byte-identical `Wikilinks`, so edges are not spuriously rewritten on every sync. The reader resolves each link to a positional span (target + byte offset); the pipeline attributes each span to the chunk whose range contains it. Both properties hold by construction.

**Why this priority**: Correctness of the edge graph depends on chunk-scoping and determinism, but the MVP (Story 1 — "the value is present") is what unblocks the bridge. This story pins the two qualities that make the value trustworthy enough to write edges from. Independently testable: assert scope and determinism directly.

**Independent Test**: Split a document whose wikilinks cluster in different paragraphs; assert each chunk's `Wikilinks` contains only the targets in its own text. Ingest the same file twice; assert the `Wikilinks` list is identical across both ingestions for matching chunk IDs.

**Acceptance Scenarios**:

1. **Given** a document where `[[alpha]]` appears only in paragraph 1 and `[[beta]]` only in paragraph 2, **When** the document is chunked and read, **Then** the chunk(s) covering paragraph 1 list `alpha` (not `beta`) and vice versa.
2. **Given** the same markdown file ingested twice, **When** the resulting chunks for matching IDs are compared, **Then** `Wikilinks` is byte-identical.
3. **Given** a wikilink whose `[[` and `]]` would fall on different sides of a chunk boundary, **When** the chunker runs, **Then** the link is attributed to exactly one chunk (the one containing its opening `[[`) and never silently dropped — the chunker's paragraph-boundary splitting keeps inline links whole. *(Planner confirms against the actual split granularity.)*

---

### User Story 3 - Every transport sees the same field (Priority: P3)

A wikilink-bearing chunk fetched over gRPC, REST, MCP, or CLI must carry the identical `Wikilinks` value — the field is a dedicated sidecar projected from `engine.QueryHit` (and the spec 035 `GetChunk` surface) to all four transports, the same projection pattern as `SectionContext`. Non-markdown formats (PDF, docx, txt) never have wikilinks; the field is absent/empty for them, so consumer code does not branch on format.

**Why this priority**: Cross-transport parity is a universal invariant (Constitution Principle V). The field flows through one canonical engine type, so once Story 1 populates it, parity is mechanical — but it is the lowest-priority story because the resolution logic (Stories 1–2) is the hard part; projecting it is mechanical.

**Independent Test**: Fetch the same markdown chunk over gRPC, REST, MCP, and CLI; assert all four return the same `Wikilinks` list. Fetch a PDF chunk and a docx chunk; assert the field is absent/empty on both.

**Acceptance Scenarios**:

1. **Given** a markdown chunk with wikilinks, **When** it is fetched over gRPC, REST, MCP, and CLI, **Then** all four return the same `Wikilinks` value.
2. **Given** a chunk from a PDF, docx, or plain-text source, **When** it is read over any transport, **Then** `Wikilinks` is absent/empty.

---

### Edge Cases

- **Empty / no-link chunk:** `Wikilinks` is **absent/empty** (`omitempty`); never an error. (Clarified 2026-06-30 — was `metadata["wikilinks"] = "[]"`, always present.)
- **Pre-existing chunks (ingested before this feature):** the field is absent until the chunk is re-ingested/reprocessed — there is no automatic rewrite of stored chunks (see Assumptions). Absence is indistinguishable from "no links" (proto3 `repeated`), which is safe under content-addressing: the bridge treats absence as "write no edges" and reprocesses to populate.
- **Dangling links to non-existent notes:** included verbatim — go-rag does not resolve or validate targets against the vault file index.
- **Aliased links `[[target|display]]`:** the destination `target` is stored; the display alias is discarded.
- **Heading/block anchors `[[target#heading]]` / `[[target#^blockid]]`:** the `#anchor`/`#^blockid` fragment is **stripped** — the target is the canonical note name (`[[a#h]]` → `a`, `[[a#^id]]` → `a`), matching the reader's `linkTarget()`. *(Clarified 2026-06-30.)*
- **Path-prefixed targets `[[concepts/auth]]` / `[[folder/sub/Note]]`:** the path is **preserved verbatim** — `concepts/auth`, not `auth`. A path disambiguates distinct notes; basename-stripping is a resolution go-rag can't validate (FR-004). The bridge normalises into its own namespace. *(Clarified 2026-06-30.)*
- **Embeds `![[…]]` (image, PDF, **and note-transclusions**):** excluded from `Wikilinks` — they are not reference edges. Note-transclusions stay in the separate `transcludes` signal; per-chunk transclusion attribution is a separate follow-up. *(Clarified 2026-06-30.)*
- **Link split across a chunk boundary:** inline links live within a paragraph; the chunker splits at paragraph boundaries, so a link is wholly inside one chunk. The span offset is the index of the opening `[[`; the chunk containing `[[` also contains `]]`. Planner confirms the split granularity preserves this.
- **Duplicate target in one chunk** (`[[a]] … [[a]]`): de-duplicated, order-preserving on first occurrence (default; research R4. If the bridge should instead weight edges by mention count, that is an open alternative — see Q-queue).
- **Wikilink syntax inside a code span/fence:** `[[x]]` inside fenced or inline code is **excluded** — code is literal in Obsidian, so these are not backlinks. The collection pass is code-fence-aware, reusing the `stripMarkdownSpans` pattern. *(Clarified 2026-06-30.)*
- **Malformed syntax** (`[[]]`, `[[a|]`, unterminated `[[a`): default — emit non-empty canonical targets only (suppress any target that canonicalises to the empty string). *(Default; not yet confirmed as a question.)*

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: For every markdown chunk whose text contains Obsidian `[[target]]` wiki links, the system MUST populate a per-chunk `Wikilinks` list with the canonical target of every such link whose text falls within that chunk's range.
- **FR-002**: For aliased links `[[target|display]]` and anchored links `[[target#heading]]` / `[[target#^blockid]]`, the stored target MUST be the canonical note name — alias and anchor stripped, **path preserved verbatim** (per the reader's `linkTarget()`): e.g. `[[concepts/auth|JWT]]` → `concepts/auth`. Path components are NOT stripped (resolving them requires the vault index, which go-rag does not hold — FR-004). *(Path-preserve clarified 2026-06-30.)* *(Anchor-strip clarified 2026-06-30.)*
- **FR-003**: Embedded references `![[…]]` — both media embeds **and** note-transclusions — MUST be excluded from `Wikilinks`; they are not reference edges. Note-transclusion targets remain available via the reader's existing separate document-level `transcludes` signal. *(Transclusion scope clarified 2026-06-30.)*
- **FR-004**: Links to notes that do not exist in the vault MUST be included verbatim; the system MUST NOT validate wikilink targets against the vault file index.
- **FR-005**: The `Wikilinks` value MUST be chunk-scoped — only targets whose link text falls within the chunk's text range — never the document-level union.
- **FR-006**: The `Wikilinks` value MUST be deterministic: re-ingesting an unchanged chunk yields a byte-identical list.
- **FR-007**: For chunks from any non-markdown source (PDF, docx, txt, …), `Wikilinks` MUST be absent/empty — never an error. *(Clarified 2026-06-30 — was the literal string `"[]"`.)*
- **FR-008**: `Wikilinks` MUST be a dedicated `omitempty` sidecar field on `Chunk`; it is **absent** (omitted in JSON / empty on the gRPC wire) for no-link chunks **and** for pre-feature chunks. proto3 `repeated` cannot distinguish empty from absent, and cross-transport parity requires REST/CLI to match. Consumers treat absence as "no edges." *(Clarified 2026-06-30 — was "always present `[]` so consumers need no existence check," under the since-corrected metadata-map assumption.)*
- **FR-009**: `Wikilinks` MUST travel with the chunk across all four transports — gRPC, REST, MCP, and CLI — with identical values, projected from `engine.QueryHit` (and the spec 035 `GetChunk` surface), mirroring `SectionContext`. *(Constitution Principle V — Extension by Interface, MCP-First.)*
- **FR-010**: Exposing `Wikilinks` MUST NOT change the chunk's content-addressed `chunk_id`. Wikilinks are derived from the chunk text and stored as a non-identity sidecar field, which is not part of the identity hash. *(Constitution Principle II — Content-Addressed Identity; identity and change-detection hashes are distinct.)*
- **FR-011**: The feature MUST be pure Go with `CGO_ENABLED=0` and add no runtime dependencies — it reuses the reader's existing `reObsidianLink` detection and `linkTarget()` canonicaliser. *(Constitution Principle III.)*
- **FR-012**: The feature MUST NOT change the on-disk key-space layout — `Wikilinks` is a new additive `omitempty` field on the existing `Chunk` value (prefix `0x03`), so it triggers no schema migration and leaves `migrate.ExpectedVersion` unchanged. Existing chunks gain the field only when re-ingested/reprocessed (no automatic rewrite). *(Constitution — Storage discipline / schema-version compliance; additive-omitempty precedent per `Poisoning`/`SectionContext`/`NearDup`/`Caption`.)*
- **FR-013**: The wikilink target set MUST be produced by reusing the markdown reader's existing `reObsidianLink` detection and `linkTarget()` canonicaliser — BL-004 adds collection of an already-parsed syntax, not a second parser. The collection pass MUST be code-fence-aware (reusing the `stripMarkdownSpans` pattern) to exclude code-context matches.
- **FR-014**: `[[wikilink]]` syntax inside fenced code blocks or inline code MUST be excluded from `Wikilinks` — code is literal in Obsidian and these are not backlinks. The collection pass MUST track code-fence state. *(Clarified 2026-06-30.)*

### Key Entities *(include if feature involves data)*

- **Chunk**: the unit of retrieved text. It gains one new non-identity sidecar field, `Wikilinks []string` (`json:"wikilinks,omitempty"`), joining `Poisoning` / `SectionContext` / `NearDup` / `Caption` / `Kind`. The chunk's identity, text, and all other fields are unchanged.
- **Wikilink target**: the canonical destination of an Obsidian `[[target]]` link — alias (`|`) and anchor (`#heading` / `#^blockid`) stripped, **path preserved verbatim** (`[[concepts/auth]]` → `concepts/auth`), per the reader's `linkTarget()`. It is the endpoint the bridge writes as the target of a Hebbian edge; the bridge normalises paths into its own namespace.
- **WikilinkSpan (transient)**: a `{Target, Offset}` pair emitted by the reader into transient document metadata (`md["wikilink_spans"]`), consumed by the pipeline to resolve per-chunk `Wikilinks` by offset containment, and dropped before identity/store — mirroring `heading_spans` (spec 025). Not persisted.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Any client reading a markdown chunk over any transport receives a `Wikilinks` list that exactly contains the canonical Obsidian wikilink targets present in that chunk's text — enabling the bridge to write Hebbian edges with zero bridge-side markdown parsing.
- **SC-002**: The Obsidian-backlinks → MuninnDB-Hebbian-edges pipeline (pattern E2) can be implemented consuming `Wikilinks` alone — the bridge performs no markdown re-parse and no target validation.
- **SC-003**: The `Wikilinks` value is chunk-scoped and deterministic: 100% of re-ingested unchanged chunks produce byte-identical `Wikilinks`, and each edge originates from the exact chunk carrying the link.
- **SC-004**: Existing chunks keep their identities and the on-disk storage format is unchanged — the feature adds one non-identity `omitempty` sidecar field with no migration and no new runtime dependency.

---

## Assumptions

- **Collection of a parsed-but-discarded value.** The reader already parses `[[wikilink]]` via `reObsidianLink` and already has `linkTarget()`; BL-004 adds target collection in that pass (R1). It does not add a second parser (FR-013).
- **Dedicated sidecar, not a metadata-map key.** `Chunk` has no per-chunk metadata map; `Wikilinks` is a dedicated `omitempty` field, consistent with every prior reader-derived sidecar (R2).
- **New non-identity state, no migration.** A new persisted `Chunk` field **is** new state, but it is non-identity (excluded from the chunk ID; the transient span table is stripped from document metadata before `GenerateID`) and backward-compatible (old blobs decode to nil). Additive `omitempty` field growth on prefix `0x03` is not a key-space layout change — no migration, `migrate.ExpectedVersion` unchanged (R5).
- **Backfill via reprocess, not auto-rewrite.** Chunks ingested before this feature do not carry `Wikilinks` until re-ingested or reprocessed. There is no automatic rewrite (R6). Absence is indistinguishable from "no links" over the wire (proto3); content-addressing makes "treat absence as no edges, reprocess to populate" safe.
- **Empty/absent contract.** `omitempty`/absent — clarified 2026-06-30 (Q2).
- **Anchor fragments stripped.** Clarified 2026-06-30 (Q1).
- **Duplicate-target handling.** Default: de-duplicate within a chunk, first-occurrence order. Finalised in the plan phase and locked in tests.
- **Code-context exclusion.** Wikilinks inside fenced/inline code are excluded (code is literal in Obsidian); the collection pass is code-fence-aware, reusing `stripMarkdownSpans`. BL-004 does not add a second detector. *(Clarified 2026-06-30.)*
- **Vault is irrelevant to this field.** `Wikilinks` is per-chunk metadata computed from text; it has no vault parameter. (Consistent with spec 035: vault is connection-time, not per-call.)

---

## Research Note for Planner

Phase 0 (`research.md`) resolved the load-bearing questions. Remaining open items for `/speckit-clarify` then `/speckit-tasks`:

- **Resolved (see `research.md` R1–R7):** R1 collection must be added; R2 dedicated `Chunk.Wikilinks []string` sidecar; R3 reuse spec-025 span pipeline (offset-containment resolution, drop transient before `GenerateID`); R4 `linkTarget` strips alias+anchor; R5 no migration; R6 reprocess backfill; R7 gRPC `Chunk.wikilinks=17`, `QueryHit.wikilinks=13`, 5 projection points, `parity_test.go` extended.
- **Clarified this session:** anchor strip (Q1); `omitempty`/absent contract (Q2).
- **Still open (this clarify queue):** none — all five clarifications resolved (Q1 anchor→strip; Q2 empty→omitempty/absent; Q3 code-context→exclude; Q4 transclusion→plain-wikilinks-only; Q5 path→preserve verbatim). Each must be encoded as an acceptance test in `/speckit-tasks`.
- **Defaulted (not asked — clear best-practice answer):** duplicate-target → de-dup first-occurrence; malformed syntax → non-empty targets only; array cap → none; URL/external targets → include verbatim (FR-004); boundary straddle → opening-bracket offset; feature gate → always-on for markdown; observability → silent.
