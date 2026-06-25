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


---

## T002 — pdfcpu API verification (spike, 2026-06-25)

**Resolves R1, R2, R5, R6 against the actual library.** pdfcpu version in use:
`v0.13.0`. All four extraction capabilities R1 assumed are present; two carry
design implications the plan must absorb. Verified from source
(`$GOMODCACHE/github.com/pdfcpu/pdfcpu@v0.13.0/pkg/api/extract.go`,
`.../pkg/pdfcpu/model/image.go`, `go doc` signatures).

### R1-verify — Metadata ✅ (cleaner than assumed)

`api.Properties(rs io.ReadSeeker, conf *model.Configuration) (map[string]string, error)`
returns the Info dictionary as a flat `map[string]string` — keys are the Info-dict
entries (`Title`, `Author`, `Subject`, `Keywords`, `Creator`, `Producer`,
`CreationDate`, `ModDate`, …). `conf == nil` → `model.NewDefaultConfiguration()`.

- `api.Keywords(rs, conf) ([]string, error)` — the Keywords field split into a list.
- `api.ExtractMetadata(rs, digestMetadata func(pdfcpu.Metadata) error, ...)` — raw
  metadata-dict readers (XML/XMP streams); not needed for US1 — `Properties` covers it.

**Implication for T006 (US1):** map the known Info-dict keys → `Document.Metadata`
(`title`, `author`, `subject`, `keywords`, plus dates). Absent keys are simply not in
the map → graceful omission for free (FR-001/FR-007, SC-005). No new types.

### R1-verify — Bookmarks / outline ✅ (with one caveat)

`api.Bookmarks(rs io.ReadSeeker, conf *model.Configuration) ([]pdfcpu.Bookmark, error)`
returns the outline tree. `pdfcpu.Bookmark`:

```go
type Bookmark struct {
    Title    string
    PageFrom int             // 1-based page number where this heading lives
    PageThru int             // reaches until before PageFrom of the next sibling
    Bold, Italic bool
    Kids     []Bookmark      // nesting = heading depth/level
    Parent   *Bookmark
}
```

**Caveat for T014 (US2-PDF):** bookmarks map to **page numbers, not byte offsets**.
`HeadingSpan.Offset` is a byte offset into the extracted `content` string. To produce
spans we must track per-page byte boundaries during extraction and walk the bookmark
tree (depth = `Level`). Two viable approaches for T014 to choose at implement time:
1. Track cumulative content length per page; place each bookmark title as a marker at
   its page's offset, then synthesize `HeadingSpan`s from the markers.
2. Prefer the **font-size heuristic** (from the T004 positioned-text helper) as the
   primary PDF heading signal — it yields offsets directly — and use bookmarks only as
   a tie-breaker/confirmation when present.

**Recommendation:** bookmarks are a free, high-precision signal when the PDF has an
outline; the font-size heuristic is the general fallback. T014 should try bookmarks
first (if `len(bms) > 0`) and fall back to font-size clustering.

### R2-verify — Tables & positioned text ⚠️ (the real risk, confirmed)

pdfcpu exposes **no positioned-text API**. `api.ExtractContent` →
`pdfcpu.ExtractPageContent(ctx, pageNr)` returns the **raw content stream** as
`bytes.NewReader(bb)` (the PDF text/graphics operators, not parsed text). The current
reader (`internal/reader/pdf.go`) already consumes this stream and regex-extracts
`(...)Tj` / `[...]TJ` show-text operators via `extractShowText`.

**Implication for T004 / T017 / T014:** the positioning data **is in the stream** —
`Tm` (text matrix: a b c d e f, where e=x, f=y), `Tf` (font name + size), `TD`/`Td`
(line/character offsets), `Tj`/`TJ` (text). pdfcpu does not interpret them; the T004
helper must parse the content stream itself (a small operator sequencer — extend the
existing regex approach to also capture the preceding `Tm`/`Tf`). This is feasible and
pure-Go, but it is the genuinely hard part of the feature:

- **US3 tables (T017):** cluster show-text fragments by `Tm`-derived (x, y) into rows
  (shared y-band) and columns (shared x-cluster), render as a Markdown table.
  Best-effort by construction (PDFs don't encode table structure). Complex tables
  (merged cells, rotated text) may fail — MUST fail gracefully (emit text as-is).
- **US2-PDF font-size heuristic:** bucket text by `Tf` size; the largest size(s) on a
  page → headings. Yields `HeadingSpan.Offset` directly.

No alternative pure-Go positioned-text library is license-compatible (Constitution
III). Confirmed: self-parsing the content stream is the only path.

### R5/R6-verify — Image extraction ✅ (ideal for captioning)

`api.ExtractImagesRaw(rs io.ReadSeeker, selectedPages []string, conf *model.Configuration) ([]map[int]model.Image, error)`
returns images **in-memory** (no disk write). `model.Image` (defined in
`pkg/pdfcpu/model/image.go:45`):

```go
type Image struct {
    io.Reader          // ← io.ReadAll(img) yields the image bytes (JPEG/PNG)
    Name      string
    FileType  string
    PageNr    int       // ← page position for caption splicing
    Width, Height int
    Size      int64
    Filter, DecodeParms string
    // ... (color space, mask, etc.)
}
```

**Implication for T024/T026 (US4):** `io.ReadAll(img)` gives the bytes to send to the
local vision model; `img.PageNr` gives the splice position; discard bytes after
captioning (data-model.md §3). No temp files, no disk I/O — `ExtractImagesRaw` (not
`ExtractImages`/`ExtractImagesFile`, which write to disk).

### Summary — downstream task adjustments

| Task | T002 verdict | Adjustment |
|------|--------------|------------|
| T006 (US1 metadata) | ✅ `api.Properties` map | None — cleanest story. |
| T014 (US2-PDF headings) | ✅ bookmarks + ⚠️ font-size | Try `api.Bookmarks` first (page→offset mapping), fall back to T004 font-size clustering. |
| T004 (positioned-text helper) | ⚠️ self-parse content stream | Extend `extractShowText`'s regex to also sequence `Tm`/`Tf`/`Td` operators. Pure-Go, no new dep. |
| T017 (US3 tables) | ⚠️ best-effort grid | Cluster by `Tm` (x,y); graceful fallback to flat text. Hardest task. |
| T024 (US4 image extract) | ✅ `api.ExtractImagesRaw` | `io.ReadAll(model.Image)` → caption bytes + `PageNr`. |

**No new dependency introduced.** Constitution III (pure-Go) holds — everything runs
through the existing `pdfcpu` + Ollama HTTP client. R1's "verify at implement time" is
now closed for metadata/bookmarks/images; the only open risk is table-detection
quality (R2), which is best-effort by design.

