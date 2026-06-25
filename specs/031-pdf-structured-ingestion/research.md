# Research — PDF Structured Ingestion (spec 031)

**Phase 0 output.** Resolves the design decisions for enhanced PDF/DOCX/text
ingestion (metadata, hierarchy, tables, image/chart captions). Grounded in go-rag's
current reader architecture (`internal/reader/`) + pdfcpu's capabilities.

---

## R1 — What can pdfcpu do? (the library risk)

**Decision.** pdfcpu is the existing PDF library (PRD §9.2, pure-Go — Constitution
III). It's a **PDF manipulation** tool (split/merge/watermark/encrypt), not a
content-extraction library. The current reader uses only `api.ExtractContent`
(content-stream text via Tj/TJ operators). For this feature, the plan needs:
- **Metadata** (document Info dictionary — Title/Author/Subject/Keywords): pdfcpu's
  `api.Info` or the `core` package likely reads this. **Verify the API at implement
  time** — pdfcpu CAN inspect the Info dictionary (it's a core PDF structure).
- **Bookmarks/outline** (heading hierarchy): pdfcpu has `api.ListBookmarks` or the
  `core.Outline` API. **Likely available** — outlines are a core PDF structure.
- **Image extraction**: pdfcpu has `api.ExtractImages` (image extraction is a core
  manipulation feature). **Confirmed capability** — pdfcpu extracts embedded images.
- **Table detection**: pdfcpu **CANNOT** detect tables. Tables are not a PDF
  structural element — they're rendered as positioned text + lines. Table detection
  requires parsing the content stream's text positions + ruling lines. This is the
  **biggest technical risk** (see R2).

**Rationale.** pdfcpu is the sanctioned pure-Go PDF library. Adding a second PDF
library (e.g., unipdf) is AGPL/commercial (violates Constitution III). So the plan
works within pdfcpu's capabilities + adds heuristic layers where pdfcpu can't help.

---

## R2 — Table extraction (the hardest part)

**Decision.** Table detection from PDF content streams using **text-position
heuristics**: parse the content stream's text operators (Tj/TJ) with their
positioning (Tm/TD operators) to detect grid-aligned text (rows/columns). When a
region of text is positionally aligned in a grid (consistent X offsets = columns,
consistent Y offsets = rows), extract it as a table. This is **best-effort** — PDF
tables are inherently unreliable (the format doesn't encode table structure; they're
just positioned text). The extracted table is rendered as a Markdown table in the
chunk text. Documents where table detection fails produce the text as-is (today's
behaviour).

**Rationale.** No pure-Go library does reliable PDF table detection (the commercial
ones are AGPL/incompatible). The content stream has positioning data (text matrix
operators) — a heuristic grid detector can extract simple tables (financial
statements, spec sheets) with reasonable accuracy. Complex tables (merged cells,
rotated text, nested tables) may fail — acceptable for v1 (best-effort, graceful).

**Alternatives considered.**
- *A new pure-Go table-extraction library.* Rejected — none exists that's
  permissively licensed and reliable.
- *LLM-based table extraction (send the page image to the vision model).* Rejected
  for v1 — requires OCR/image rendering of pages (out of scope); too heavy for the
  common case. A future enhancement.
- *Skip table extraction entirely.* Rejected — tables carry the highest-density
  factual content (the #1 thing users search for in PDFs). Even best-effort is
  valuable.

---

## R3 — DOCX heading extraction (the easiest win)

**Decision.** The existing DOCX reader (`internal/reader/docx.go`) flattens to text
(no heading-style extraction — confirmed: no `pStyle`/`Heading` references). Enhance
it to parse Word's heading styles: DOCX's `document.xml` marks headings with
`<w:pPr><w:pStyle w:val="Heading1"/></w:pPr>` (Heading1 through Heading6). The
reader extracts these as positional `HeadingSpan` entries (the same mechanism the
Markdown reader uses), so the chunker threads them into `section_context`
breadcrumbs for free (spec 025 parity, zero chunker changes).

**Rationale.** DOCX heading styles are the **richest, most reliable** heading signal
of any format — they're explicit in the XML (not inferred). The DOCX reader already
parses the XML (it extracts text); adding heading-style detection is a small
enhancement (parse `pStyle` alongside text). This is the lowest-risk, highest-value
hierarchy extraction.

---

## R4 — Text heading heuristics (best-effort)

**Decision.** For plain-text files (`.txt`), detect headings via pattern heuristics:
- Lines in ALL CAPS (short, < 80 chars, no terminal punctuation).
- Lines followed by `===` or `---` underline patterns (Markdown ATX-style in .txt).
- Lines ending with `:` that are short (< 60 chars) and followed by indented content.
- Consistent indentation patterns (level 1 = no indent, level 2 = 4 spaces, etc.).

Output as `HeadingSpan` entries (level inferred from the pattern). **Best-effort,
lower confidence** than Markdown/DOCX — acceptable; text files without detectable
patterns produce no breadcrumbs (graceful).

**Rationale.** Plain text has no structural markup. The heuristics are common in
technical documentation (READMEs, notes, logs). Low risk (best-effort); the
HeadingSpan mechanism handles absent breadcrumbs cleanly.

---

## R5 — Image extraction + captioning (the model-dependent part)

**Decision.** pdfcpu's `api.ExtractImages` extracts embedded images from the PDF.
The images are extracted as raw bytes (JPEG/PNG). A **background captioning step**
(local multimodal model) generates a text description for each image. The caption is
inserted into the content at the image's position (so the chunker includes it in the
chunk text). The raw image bytes are discarded after captioning (the caption is the
searchable representation — image storage is out of scope).

**Captioning integration** — extends the spec 029 enrichment pattern:
- The reader extracts images + returns them as a side-list (e.g.,
  `metadata["images"]` = `[]ImageData{Position, Bytes}`).
- The pipeline's `processFile` passes the images to a **captioner** (a new
  `Captioner` interface, sibling of `Enricher`) — background, opt-in, local model.
- The captioner generates captions; the captions are spliced into the content at the
  image positions (before chunking, so the chunk text includes the caption).

**Chart captions** — the captioner is prompted to describe chart DATA (trend, key
values, comparisons), not just "a chart." The prompt distinguishes charts from
generic images (if the reader can classify the image type — heuristic or model-based).

**Rationale.** This follows the established pattern: the reader extracts (images as
data); a background model step generates (captions). The captioning is opt-in +
local-only (PRD N4, same exception as enrichment). The reader stays pure (no model
calls); the captioner is a separate background step.

---

## R6 — The vision/multimodal model for captioning

**Decision.** Image captioning requires a **multimodal vision model** (image-to-text)
in the local Ollama (e.g., `llava`, `llama3.2-vision`, `moondream`). The model is
configured via a new config key (`captioning_model`). If no vision model is
available, captioning is skipped gracefully (images extracted but not captioned — the
text/tables/hierarchy/metadata still extract). The specific model is a plan/config
decision.

**Rationale.** Text-only models (like llama3.1) cannot caption images. A vision
model is required. Ollama supports vision models; the operator pulls one. The
captioner interface mirrors the `Enricher` (spec 029) — a generation call per image,
background, circuit-breaker-guarded, bounded.
