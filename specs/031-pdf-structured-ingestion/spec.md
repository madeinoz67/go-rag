# Feature Specification: PDF Structured Ingestion

**Feature Branch**: `031-pdf-structured-ingestion`

**Created**: 2026-06-25

**Status**: Draft

**Input**: User description: "need to add pdf ingestion that includes images and tables, where these need to be captioned and metadata extraction; also document hierarchy extraction." Today the PDF reader flattens PDFs to plain text — losing tables (garbled into flowing text), images (invisible to search), document structure (headings/sections discarded), and metadata (only file format + page count extracted, not title/author/keywords). This feature upgrades the PDF reader to extract the full structure of a PDF: **metadata** (document properties), **document hierarchy** (heading outline → section breadcrumbs), **tables** (as searchable structured text), and **images** (extracted + captioned so their content is searchable). The reader remains a pure `FileReader` (extraction only); image captioning runs as a background local-model step (the same background-local-generation pattern as spec 029 enrichment — PRD N4 covers it).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - PDF metadata is extracted (Priority: P1)

A user ingests a PDF (a report, a paper, a spec). Today only the file format and page count are extracted — the document's title, author, subject, and keywords are discarded, so the metadata filter (`--source`/`--type`/`--tags`) and status can't use real document properties. After this feature, the PDF's document properties (title, author, subject, keywords, creation/modification dates) are extracted and populate the document's metadata — so filtering, display, and identity reflect the actual document, not just its file path.

**Why this priority**: The cheapest win (pure extraction, no model) and the most immediately visible — every PDF gains real metadata. The audit noted this as a known gap (PRD §8.1 spec'd title/author/subject/keywords; actual: only format + page_count).

**Independent Test**: Ingest a PDF with document properties set (title, author, keywords); confirm the metadata appears in the document record and is filterable via `--tags`.

**Acceptance Scenarios**:
1. **Given** a PDF with document properties (title, author, subject, keywords), **When** ingested, **Then** those properties populate the document's metadata.
2. **Given** a PDF without document properties, **When** ingested, **Then** metadata is gracefully absent (not an error; the document still ingests with file-path metadata only).
3. **Given** the extracted metadata, **When** the user filters by a keyword from the PDF's subject/keywords, **Then** the document is returned.

---

### User Story 2 - Document hierarchy is extracted for ALL formats (Priority: P1)

**Extended to every supported format**, not just PDFs. The hierarchy mechanism
(spec 025's `HeadingSpan` → breadcrumb threading) is format-agnostic — any reader
that identifies headings gets breadcrumbs for free:
- **DOCX**: Word heading styles (Heading 1–6) — the richest, most reliable signal.
- **PDF**: bookmark outline or font-size/position heuristics.
- **Text** (`.txt`): heuristic patterns (ALL CAPS, `===`/`---` underlines, lines ending `:`).
- **Markdown**: already shipped (spec 025) — unchanged.

A user ingests a structured PDF (a manual, a research paper, a contract). Today the heading structure is lost — the PDF is flattened to text, so `section_context` breadcrumbs (spec 025) are absent for PDF chunks, unlike Markdown where headings thread into every chunk. After this feature, the PDF's heading outline (from bookmarks/outline or font-size heuristics) is extracted and threaded into each chunk's `section_context` breadcrumb — so every PDF chunk carries its heading path, enabling section-aware retrieval and display.

**Why this priority**: Section context is a proven retrieval-quality feature (spec 025). Extending it from Markdown-only to PDFs is a direct parity win. Pure extraction (no model) if the PDF has an outline/bookmarks; font-size heuristics are a fallback.

**Independent Test**: Ingest a PDF with headings; query a phrase under a known heading; confirm the hit carries the correct section breadcrumb.

**Acceptance Scenarios**:
1. **Given** a PDF with a bookmark outline / heading structure, **When** ingested, **Then** each chunk carries a section breadcrumb reflecting its position in the document hierarchy.
2. **Given** a PDF without an outline (no bookmarks), **When** ingested, **Then** heading detection falls back to font-size/position heuristics, or is gracefully absent (no error).
3. **Given** section breadcrumbs on PDF chunks, **When** a user queries, **Then** the hit display shows the heading path (same as Markdown chunks, spec 025 parity).

---

### User Story 3 - Tables are extracted as searchable text (Priority: P1)

A user ingests a PDF with tables (financial statements, spec sheets, comparison matrices). Today tables are garbled into flowing text — rows/columns merge, numbers lose their column context, and table content is often unsearchable or misleading. After this feature, tables are extracted as structured text (a readable table format) and included in the chunk text — so table data is searchable and retrievable with its row/column context intact.

**Why this priority**: Tables carry high-density factual content (numbers, specs, comparisons) that is exactly what users search for. Losing them to text flattening is a major recall miss. Pure extraction (no model).

**Independent Test**: Ingest a PDF with a known table; query a value from the table; confirm it's returned and the surrounding chunk preserves the table structure.

**Acceptance Scenarios**:
1. **Given** a PDF containing a data table, **When** ingested, **Then** the table is extracted as structured text (preserving rows/columns) and included in the chunk content.
2. **Given** the extracted table text, **When** the user queries for a cell value, **Then** the hit returns the table context (not garbled flowing text).
3. **Given** a PDF with a table spanning multiple pages, **When** ingested, **Then** the table is extracted as one logical unit (or clearly split with continuation markers).

---

### User Story 4 - Images and charts are extracted and captioned (Priority: P2)

A user ingests a PDF with images and **charts** (bar charts, line graphs, pie charts,
diagrams, screenshots, photographs). Charts are the highest-value image type — they
carry structured data (trends, values, comparisons) that is exactly what users
search for, and a good chart caption describes the data, not just "a chart." Today images are invisible — their content is not extracted, not searchable, and not retrievable. After this feature, images are extracted and **captioned** (a text description generated by the local model), and the caption is included in the chunk text — so the image's visual content is searchable. Captioning uses the local model (background, opt-in, PRD N4 territory — the same background-local-generation exception as document enrichment, spec 029).

**Why this priority**: Images carry information that text alone misses (a chart's trend, a diagram's architecture, a screenshot's UI state). Captioning makes that information searchable. Secondary to the pure-extraction features (US1–US3) because it requires a model + is opt-in.

**Independent Test**: Ingest a PDF with a chart; with captioning enabled, query a phrase describing the chart's content; confirm the hit returns the caption.

**Acceptance Scenarios**:
1. **Given** a PDF with embedded images or charts, **When** ingested with captioning enabled, **Then** each image/chart receives a generated text caption (for charts, describing the data trend/values), and the caption is included in the chunk text.
2. **Given** captioning is disabled (or the captioning model is unavailable), **When** a PDF with images is ingested, **Then** the text + tables + hierarchy + metadata still extract correctly; images are simply skipped (no error).
3. **Given** a captioned image, **When** the user queries for the image's content (e.g., "revenue growth chart"), **Then** the hit returns the chunk containing the caption.

---

### Edge Cases

- **PDFs with no images/tables/headings** (pure text): ingest cleanly — all four features gracefully produce nothing extra.
- **Scanned PDFs** (image-only, no text layer): the text layer is absent; text extraction returns little/nothing for those pages. OCR is **out of scope** for this feature (a future capability).
- **Password-protected PDFs**: gracefully skipped or error (as today).
- **Corrupted/partial PDFs**: extract what's possible; log the issue; don't fail the whole document.
- **Very large PDFs** (many images): captioning is batched + bounded (circuit breaker, like enrichment); the reader doesn't block on captioning (it's background).
- **Tables spanning multiple pages**: extracted as one logical table or split with clear continuation.
- **Decorative images** (logos, dividers): captioned but the caption may be low-value ("a company logo"); acceptable — the caption is searchable but not prioritized.
- **PDFs with rich outlines vs. no outlines**: hierarchy extraction prefers the bookmark outline; falls back to font-size heuristics; or is absent (graceful).
- **Metadata absent**: PDFs without document properties — metadata is absent (not an error).
- **Captioning model unavailable**: images skipped gracefully (text/tables/hierarchy/metadata still extract).
- **Non-PDF formats**: this feature is PDF-reader-specific; Markdown/text/other readers are unchanged.
- **Re-ingestion**: existing PDFs (ingested with the old reader) have different content (more structure extracted) → different identity → re-process (like any reader change). Old PDFs still query correctly until re-processed.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The PDF reader MUST extract document properties (title, author, subject, keywords, creation/modification dates) from the PDF and populate the document's metadata. Absent properties are gracefully omitted (not an error).
- **FR-002**: EVERY supported document reader MUST extract heading hierarchy and thread it into each chunk's section breadcrumb (spec 025 parity): DOCX (Word heading styles — richest signal), PDF (bookmarks/outline or font-size heuristics), text (pattern heuristics). Markdown is unchanged (spec 025). Documents without detectable headings carry absent breadcrumbs (graceful).
- **FR-003**: The PDF reader MUST extract tables as structured text (preserving row/column structure in a readable format) and include them in the chunk content. Tables spanning pages are handled as one logical unit or clearly split.
- **FR-004**: The system MUST extract embedded images and charts from PDFs and generate text captions for them (via the local model), including the caption in the chunk text so the image's/chart's content is searchable. Chart captions SHOULD describe the data (trend, key values, comparisons), not just the chart type.
- **FR-005**: Image captioning MUST be opt-in and background (the same background-local-generation pattern as spec 029 enrichment). When captioning is disabled or the model is unavailable, images are skipped gracefully — text/tables/hierarchy/metadata still extract.
- **FR-006**: The PDF reader MUST remain a `FileReader` (the extension-by-interface pattern, Constitution V) — the enhanced extraction is internal to the reader; the interface is unchanged.
- **FR-007**: The extraction MUST be graceful: a PDF that lacks any of the four elements (metadata, hierarchy, tables, images) still ingests cleanly with the elements it has.
- **FR-008**: Non-PDF readers MUST be unchanged — this feature is PDF-specific.
- **FR-009**: Image captioning MUST use the local model only (no cloud — Constitution I, PRD N4 background-local-generation exception).
- **FR-010**: Captioning MUST NOT block the write ACK — it is strictly background/post-ACK (Constitution IV).

### Key Entities *(include if feature involves data)*

- **PDF Metadata**: the document's intrinsic properties (title, author, subject, keywords, dates). Extracted from the PDF's document dictionary. Populates the existing `Document.Metadata`. Part of the document's content-addressed identity (stable, not derived).
- **Section Hierarchy**: the PDF's heading outline, threaded into the existing `Chunk.SectionContext` breadcrumb (spec 025). Non-identity sidecar (like the Markdown reader's spans — removed before `GenerateID`).
- **Table Block**: a table extracted as structured text (a readable row/column format), included in the chunk content. Part of the chunk's text (searchable).
- **Image Caption**: a generated text description of a PDF image, included in the chunk text so the image's visual content is searchable. Generated by the local model (background, opt-in).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a PDF with document properties, the extracted metadata (title, author, keywords) appears in the document record and is filterable via `--tags`.
- **SC-002**: For a structured PDF (with headings), each chunk carries a section breadcrumb reflecting its position in the document hierarchy — identical in surface to Markdown chunks (spec 025 parity).
- **SC-003**: For a PDF with a data table, querying a cell value returns the hit with the table structure preserved in the chunk text (not garbled flowing text).
- **SC-004**: For a PDF with images (captioning enabled), querying a phrase describing an image's content returns the chunk containing the generated caption.
- **SC-005**: For a PDF with none of the four elements (no metadata, headings, tables, or images), ingestion completes cleanly with no errors — the text extracts as before.
- **SC-006**: With captioning disabled or the model unavailable, a PDF with images still ingests (text + tables + hierarchy + metadata extract; images skipped gracefully).

## Assumptions

- **Metadata + hierarchy + tables are pure reader enhancements** (no model needed). Image captioning is the only model-dependent part (background, opt-in — the PRD N4 background-local-generation exception, same as spec 029 enrichment).
- **The PDF reader uses pdfcpu** (already in the PRD §9.2 allowed-dependency list, pure-Go — Constitution III). Image extraction + outline/bookmark access + table detection are pdfcpu capabilities (or complementary pure-Go libraries). The specific library choices are plan decisions.
- **Image captioning requires a multimodal/vision model** in the local Ollama (e.g., a vision-capable model). The specific model is a plan/config decision. If no vision model is available, captioning is skipped gracefully (images are not captioned, but everything else extracts).
- **OCR is out of scope** — scanned PDFs (image-only, no text layer) are not OCRed. The reader extracts the text layer + embedded images; scanned pages without a text layer produce no text for that page. OCR is a future feature.
- **Section hierarchy extraction prefers the PDF's bookmark/outline** (if present); falls back to font-size/position heuristics; or is absent (graceful). The extraction quality depends on the PDF's structure (well-structured PDFs with outlines produce the best breadcrumbs).
- **Captioning is a background step** (like enrichment, spec 029) — the reader extracts images as data; a captioning step (local model) generates descriptions and writes them into the chunk text. This MUST NOT block the write ACK (Constitution IV).
- **Re-ingestion changes identity**: PDFs re-ingested with the enhanced reader produce different content (more structure) → different content-addressed identity. Existing PDFs must be re-processed (like any reader change) to gain the new extraction. Old PDFs query correctly until re-processed.
- **The enrichment feature (spec 029) is orthogonal** — enrichment tags + summarizes documents at the document level; image captioning is a reader-level extraction (pre-chunk, per-image). They compose: a PDF can be captioned (images) AND enriched (tags + summary).
- **Out of scope for v1**: OCR for scanned PDFs, image storage (images are captioned then discarded — the caption is the searchable representation), and non-PDF format enhancements.
