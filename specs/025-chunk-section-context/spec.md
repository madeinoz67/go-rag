# Feature Specification: Per-Chunk Section Context

**Feature Branch**: `025-chunk-section-context`

**Created**: 2026-06-24

**Status**: Draft

**Input**: User description: "look at H23 in the backlog" — audit finding **H23**
(`RAG_BOOK_AUDIT_BACKLOG.md`, Phase 6, §1.1): *"Markdown structure destroyed before
chunking; no chunk-metadata. Thread the current heading into per-chunk metadata
during chunking; populate `section_context` from the reader's extracted
headings."* Explicitly deferred by **spec 013 (boundary-chunking)** as a
separate item; this spec consumes 013's chunker output and adds the structural
context 013 deliberately left out.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See where a retrieved chunk lives in its document (Priority: P1)

A user runs a query and receives ranked chunks. Today each hit is an island of
text — the user (or the downstream LLM consuming the hit) cannot tell which
section of the source document the text came from. After this feature, every hit
carries a **section location**: the heading breadcrumb that governs the chunk
(e.g. `Operations > Backups > Retention`). The user can immediately locate the
passage in the original document and cite it by section, not just by file.

**Why this priority**: This is the entire user-facing value of H23. Without a
visible section location, structural capture is invisible to the user. It is the
one story that, shipped alone, still delivers measurable benefit.

**Independent Test**: Ingest a multi-section Markdown document, query for a
phrase that lives under a known heading, and confirm the returned hit shows that
heading breadcrumb. Fully testable end-to-end with a single fixture document and
one query — no other story required.

**Acceptance Scenarios**:

1. **Given** a Markdown document with nested headings (`# Ops > ## Backups > ### Retention`) is ingested, **When** the user queries for text that appears in the Retention subsection, **Then** the returned hit shows the heading breadcrumb `Ops / Backups / Retention` (or equivalent ordered path) alongside the chunk text.
2. **Given** a hit is returned over any transport (CLI, REST, gRPC), **When** the user reads the result, **Then** the section location is present with an identical value across all three transports for the same chunk.
3. **Given** a hit whose source document has only a top-level heading, **When** displayed, **Then** the breadcrumb is the single governing heading (not empty, not the full flat heading list).

---

### User Story 2 - Every chunk inherits the heading active at its position (Priority: P2)

A user ingests a structured document. The document's heading structure is
already extracted by the reader today, but it is captured as a flat list and
flattened to plain text *before* the chunker sees it — so no chunk knows which
heading governs it. After this feature, the heading **active at each chunk's
position** is threaded onto that chunk during chunking, including correct
handling of chunks that bridge a heading boundary. The linked-list neighbours
(Previous/Next) and the per-chunk ordinal are unaffected.

**Why this priority**: This is the correctness backbone that makes US1
trustworthy. Without correct positional attachment, the breadcrumb in US1 would
be guessed or uniform. It is independently testable by inspecting the chunks
produced for a fixture document, without going through query.

**Independent Test**: Ingest a document whose headings and body are arranged so
each heading governs a known chunk range; enumerate the stored chunks and assert
each carries the heading governing its start position. No query or retrieval
needed.

**Acceptance Scenarios**:

1. **Given** a document with headings at known byte ranges, **When** it is chunked, **Then** every chunk whose start position falls under heading H carries H (and its ancestor headings) as its section context.
2. **Given** a single chunk whose text begins under heading A but continues past a later heading B into B's body (a straddling chunk), **When** section context is assigned, **Then** the chunk carries a single, deterministic value — the heading active at the chunk's **start** position — documented as the rule, not chosen arbitrarily per chunk.
3. **Given** the feature is enabled, **When** a document is chunked, **Then** chunk sizes, overlap, and the minimum-tail-merge guarantees established by spec 013 are unchanged (section capture does not disturb chunking geometry).

---

### User Story 3 - Documents and chunks without section context degrade gracefully (Priority: P3)

Not every document has headings (plain text, code-heavy Markdown, front-matter
only), and not every chunk in a vault was ingested after this feature exists.
These must not break ingestion or retrieval: a chunk with no detectable section
carries an absent/empty section context, and reading an older chunk that
pre-dates the feature returns no section context rather than erroring.

**Why this priority**: Robustness and migration safety. Lower priority than the
core value (US1) and correctness (US2) because the happy path must work first,
but required so the feature can ship without forcing a full re-ingest or
rejecting heading-less documents.

**Independent Test**: Ingest a heading-less `.txt` file and a code-only
Markdown file; query each; confirm results return without error and simply omit
the section location. Separately, read a chunk written by a pre-feature build
and confirm it loads with absent (not malformed) section context.

**Acceptance Scenarios**:

1. **Given** a plain-text or code-only document with no headings, **When** ingested and queried, **Then** ingestion succeeds and returned hits carry no section context (absent/empty), with no error.
2. **Given** a chunk persisted before this feature was enabled, **When** it is retrieved, **Then** the section context is absent rather than causing a read or parse failure.
3. **Given** an unchanged heading-bearing document already in the vault, **When** it is re-added, **Then** ingestion remains a no-op (no duplicate document or chunks) — idempotent identity is preserved.

---

### Edge Cases

- **Chunk straddles a heading boundary**: a chunk starting under one heading and continuing into the next. Resolved deterministically by the start-position rule (US2 scenario 2); documented, not configurable per chunk.
- **Document with zero headings** (plain text, code-fenced Markdown, front-matter only): no section context is synthesized; the field is absent, not an error (US3 scenario 1).
- **Front-matter "title" vs in-body heading**: a YAML front-matter `title` is document metadata, not a section heading; only in-body Markdown headings contribute to section context.
- **Lines beginning with `#` inside fenced code blocks** (comments, shebangs, heading-like text in code): must NOT be mistaken for headings. The reader's heading detection runs on the raw document; code-fence awareness must hold so a `# comment` or `#!/bin/sh` does not become a breadcrumb.
- **Deeply nested headings (H1→H6)**: the breadcrumb is the full ancestor path; behaviour for path length (full vs. capped) is a plan decision, but the value must remain an ordered path, not a flat list.
- **Heading appearing mid-chunk** because a chunk is larger than a section: covered by the straddle rule.
- **Obsidian syntax** (`[[wikilink]]`, `![[embed]]`): already normalised by the reader before chunking; must not pollute heading detection or the breadcrumb.
- **Re-ingestion after enabling the feature**: unchanged files remain a no-op (US3 scenario 3); whether section context participates in chunk identity or is a sidecar field is a plan decision, but the no-duplicate guarantee is a hard requirement (FR-003).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST associate each chunk with the **section context** that governs it — the heading (and its ancestor headings) active at the chunk's start position in the source document.
- **FR-002**: The system MUST derive section context from the heading structure **already extracted by the reader**, without invoking an LLM, a network call, or any cloud service (honours Local-First and Pure-Go).
- **FR-003**: The system MUST preserve idempotent ingestion (Constitution Principle II): re-adding an unchanged file remains a no-op and produces no duplicate documents or chunks once the feature is enabled.
- **FR-004**: The system MUST surface a retrieved chunk's section context on **every transport — CLI, REST, and gRPC — with an identical value** for the same chunk (cross-transport parity, consistent with existing hit fields).
- **FR-005**: The system MUST represent section context as an **ordered heading path** (breadcrumb from top-level to the governing heading), not a flat list of all document headings.
- **FR-006**: The system MUST ingest and query documents that contain no headings without error; for such documents and their chunks, section context is absent/empty rather than a failure.
- **FR-007**: The system MUST assign section context deterministically for a chunk that straddles a heading boundary, using the heading active at the chunk's **start position**, and this rule MUST be documented.
- **FR-008**: The system MUST NOT alter a chunk's retrievable text content, token count, size budget, or overlap as a side-effect of capturing section context — the chunking geometry established by spec 013 is preserved.
- **FR-009**: The system MUST NOT treat lines beginning with `#` inside fenced code blocks as headings when computing section context.

### Key Entities *(include if feature involves data)*

- **Chunk**: the retrievable text segment. Gains a section-context attribute describing where in the document's heading structure the chunk resides. Existing identity, linked-list, ordinal, and poisoning attributes are unchanged.
- **Section Context (Heading Path)**: an ordered breadcrumb of the headings active at a chunk's start position (e.g. `Operations → Backups → Retention`). May be absent for chunks whose source has no headings.
- **Document Heading Structure**: the in-body heading outline of a document, already extracted by readers today. This feature consumes it **positionally** during chunking, attaching the heading active at each chunk's location rather than storing a document-level flat list.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a Markdown fixture with at least three nested heading levels, 100% of produced chunks carry the correct governing heading path (verifiable by enumerating chunks against known heading/byte ranges).
- **SC-002**: A retrieved hit shows its section location with **zero additional user actions** on CLI, REST, and gRPC, and the value is identical across all three transports for the same chunk.
- **SC-003**: Re-adding an unchanged heading-bearing document is a no-op — document and chunk counts are unchanged (idempotency preserved, Constitution Principle II).
- **SC-004**: On the project's existing retrieval-eval harness (spec 004), overall retrieval metrics do **not regress** versus the pre-feature baseline — section capture must not perturb chunking or embeddings.
- **SC-005**: Documents with no headings ingest and query without error, and their hits carry no section context (absent), confirmed across all three transports.

## Assumptions

- **"Section context" means an ordered heading breadcrumb** active at the chunk's start position (top-level → … → governing heading), not the document's flat heading list. This is the natural reading of `section_context` in the backlog and the default; it is reflected in FR-005.
- **H23 captures, stores, and surfaces section context. It does NOT change the text sent to the embedder.** Prepending heading text to embedded content is a separate enrichment concern (it would change embeddings and force a re-embed) and is out of scope here.
- **Idempotent identity is a hard requirement (FR-003); the mechanism is a plan decision.** Whether section context participates in the chunk identity hash or is stored as a sidecar field is left to planning, but the no-duplicate-on-re-add guarantee is non-negotiable.
- **The heading structure is already extracted by the Markdown reader today** (as a flat list). Non-Markdown readers (PDF/DOCX/text) may not expose headings positionally; for readers that do not, section context is simply absent (graceful, per FR-006), never synthesised. Improving non-Markdown heading extraction is out of scope.
- **Structural enrichment only.** No LLM, no hypothetical-question generation, no summarisation — the PRD excludes LLM generation, and H23 is explicitly the no-LLM structural item (as distinct from the audit's separate P1 enrichment gap).
