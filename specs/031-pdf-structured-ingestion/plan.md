# Implementation Plan: PDF Structured Ingestion

**Branch**: `031-pdf-structured-ingestion` (commits to `main` directly) | **Date**: 2026-06-25 | **Spec**: [spec.md](./spec.md)

**Input**: Enhanced PDF/DOCX/text ingestion — metadata extraction (title/author/keywords), document hierarchy (heading outline → section_context breadcrumbs for ALL formats), table extraction (structured text), and image/chart captioning (local vision model).

## Summary

The PDF reader flattens PDFs to plain text — losing tables, images, headings, and
metadata. This feature upgrades every reader to extract the full document structure:

1. **Metadata** (US1): pdfcpu reads the PDF Info dictionary (Title/Author/Subject/
   Keywords) → `Document.Metadata`. Pure extraction.
2. **Hierarchy** (US2, all formats): DOCX gets `pStyle` heading extraction (richest
   signal); PDF gets bookmark/outline + font-size heuristics; text gets pattern
   heuristics. All produce `[]HeadingSpan` → the chunker threads breadcrumbs
   (spec 025 parity, zero chunker changes).
3. **Tables** (US3): text-position grid detection from the PDF content stream →
   Markdown table text in the chunk content. Best-effort (PDFs don't encode table
   structure).
4. **Image/chart captioning** (US4): pdfcpu extracts embedded images; a background
   `Captioner` (local vision model, opt-in — the spec 029 enrichment pattern)
   generates descriptions; captions are spliced into the content before chunking.

US1–US3 are pure reader enhancements (no model). US4 is the model-dependent part
(background, opt-in, PRD N4). All four reuse existing surfaces (`content`,
`metadata["heading_spans"]`, `Document.Metadata`, `Chunk.SectionContext`).

**⚠️ Technical risk:** PDF table extraction (R2) is the hardest part — pdfcpu
cannot detect tables; the plan uses text-position heuristics from the content
stream. Simple tables (financial statements, spec sheets) should work; complex
tables (merged cells, rotated text) may fail gracefully. The pdfcpu API for
metadata/outline/images (R1) needs verification at implement time.

Full rationale: [research.md](./research.md) (R1–R6). Entity detail:
[data-model.md](./data-model.md). Validation: [quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: **pdfcpu** (existing, pure-Go — Constitution III) for PDF
text/metadata/outline/image extraction. No new dependency for US1–US3. US4
(captioning) reuses the local Ollama HTTP client (existing) with a vision model.

**Storage**: unchanged — no new prefix. All outputs flow through existing `content`
+ `metadata` surfaces.

**Testing**: `go test -race -cover ./...`. New: enhanced reader tests
(`internal/reader/pdf_test.go`, `docx_test.go`, `text_test.go`) for metadata,
heading spans, table detection. Captioner tests (fake captioner, like the fake
enricher). The retrieval-eval harness (spec 004) for no-regression.

**Constraints**: pure-Go (Constitution III), local-only (Constitution I), reader
stays a `FileReader` (Constitution V), captioning is background/opt-in (Constitution
IV, PRD N4). OCR is out of scope.

## Constitution Check

Constitution: `.specify/memory/constitution.md` v1.0.0. All five PASS.

| # | Principle | Verdict | Justification |
|---|-----------|---------|---------------|
| I | Local-First | ✅ PASS | All extraction is local; captioning uses local Ollama (vision model). No cloud. |
| II | Content-Addressed Identity | ✅ PASS | Metadata is stable document content (legitimately in GenerateID). Heading spans are non-identity (removed before GenerateID, like Markdown — spec 025). Table text + captions are content (part of the chunk text). |
| III | Pure Go | ✅ PASS | pdfcpu (existing). No new dependency for US1–US3. Captioning reuses the existing Ollama HTTP client. |
| IV | Async-After-ACK | ✅ PASS | US1–US3 are synchronous reader work (fast extraction, like today). US4 (captioning) is background, post-ACK (same pattern as enrichment). |
| V | Extension by Interface | ✅ PASS | Enhanced readers stay `FileReader`. The `Captioner` interface mirrors `Enricher`. Non-PDF readers unchanged (except DOCX/text heading extraction). |

## Project Structure

```text
specs/031-pdf-structured-ingestion/
├── spec.md, plan.md, research.md, data-model.md, quickstart.md

internal/
├── reader/
│   ├── pdf.go          # ENHANCED: metadata (Info dict) + outline/headings + table heuristics + image extraction
│   ├── docx.go         # ENHANCED: pStyle heading extraction
│   ├── text.go         # ENHANCED: heading pattern heuristics
│   └── *_test.go       # NEW tests for each enhancement
├── caption/            # NEW (US4): Captioner interface + local Ollama vision provider
├── pipeline/pipeline.go # Wire captioner (if bound) — splice captions into content pre-chunk
├── engine/engine.go     # Bind captioner when captioning_enabled
└── config/config.go     # captioning_enabled + captioning_model (default off)
```
