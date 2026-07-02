# Spec 042 — Chunk `extraction_quality` (BL-006)

**Spec Kit**: specify · **Backlog**: [BL-006](../../../docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md#bl-006) · **Date**: 2026-07-02

## Summary

Surface an `extraction_method` (`native` / `mixed` / `image`) and a 0-1
`extraction_quality` on every chunk, so the bridge can gate promotion of
low-quality text (sparse / image-only PDF pages) instead of treating all
extracted text as authoritative. Computed from **per-page text coverage** in the
PDF reader — no new detection deps.

## The OCR-limit caveat (read first)

go-rag is pure-Go and does **not** OCR (PRD §2.2 out-of-scope). So BL-006's
literal `"ocr"` quality bucket is **not truly detectable**: pdfcpu extracts
whatever text layer a PDF carries, and a scanned PDF that happens to ship a
machine text layer reads as `native`. This spec therefore reports an honest
**coverage-confidence** signal, not an OCR-quality verdict:

- A page that yielded substantial text → high confidence (`native`).
- A page that yielded little/no text → low confidence (`image` / sparse `mixed`).
- A doc with some text-bearing + some empty pages → `mixed`.

The bridge's actual gate — "don't promote sparse / image-only extraction" — is
satisfied. True OCR-quality detection waits for an OCR story (out of scope).

## Signal (per-document, MVP)

Computed in `PDFReader.Read` from `pageText` (the per-page extracted text) +
`pageOffsets` (page count). Per-page granularity is a documented refinement.

- `pages` = `len(pageOffsets)`; `pagesWithText` = count of pages with
  `len(pageText[p]) > 0`; `totalChars` = `Σ len(pageText[p])`; `avgPerPage` =
  `totalChars / pages`.
- Pure function `classifyExtraction(pages, pagesWithText, totalChars) → (method, quality)`:
  - `pages == 0` → `image`, `0.0` (degenerate — no chunks anyway).
  - `pagesWithText == 0` → `image`, `0.1` (all-image; no text layer → no chunks).
  - `pagesWithText < pages` → `mixed`, `0.3 + 0.5*(pagesWithText/pages)` (0.3–0.8).
  - all pages have text + `avgPerPage ≥ 200` → `native`, `0.95`.
  - all pages have text + sparse (`avgPerPage < 200`) → `mixed`, `0.5 + 0.4*(avgPerPage/200)` (0.5–0.9).
- Thresholds (200 chars/page, the 0.3/0.5/0.95 anchors) are heuristic + tunable
  constants — documented; not config-gated for MVP.

**Non-PDF readers** (markdown / docx / text): no coverage computation — the
pipeline defaults to `method = "native"`, `quality = 1.0` (clean text extraction).

## Contract

- Reader returns `md["extraction_quality"]` (float64) + `md["extraction_method"]`
  (string) for PDF; non-PDF omits them (pipeline defaults).
- `model.Chunk.ExtractionQuality float64` + `model.Chunk.ExtractionMethod string`
  — non-identity sidecars (stripped from metadata before `GenerateID`, like
  `heading_spans` / `page_offsets`). Pre-feature chunks read as `0` / `""` →
  consumer treats as default `native` / `1.0`.
- Proto (additive): `double extraction_quality = 19; string extraction_method =
  20;` on `Chunk`; same on `QueryHit` (15 / 16).
- Projected on gRPC / REST / CLI (MCP carries via its text render implicitly).

## Acceptance criteria (from BL-006, adapted to the OCR limit)

- [x] PDF chunks from a text-based PDF carry `extraction_quality ≥ 0.9` and
  `extraction_method = "native"`.
- [x] All non-PDF chunks carry `extraction_quality = 1.0`, `extraction_method = "native"`.
- [x] A PDF with mixed text/image pages classifies `mixed`; an all-image PDF
  classifies `image` (and yields no chunks — self-handling).
- [x] `extraction_quality` parses as `float64`; values stable across re-ingestion.
- [x] `make build && vet && go test -race ./... && lint` green.

## Tests

- `internal/reader/pdftext_test.go` — `classifyExtraction` pure-function cases:
  all-native (→ native/0.95), sparse (→ mixed/0.5-0.9), mixed pages (→ mixed),
  all-image (→ image/0.1), empty (→ image/0.0).
- `internal/pipeline/extraction_quality_test.go` — a non-PDF ingest → chunk
  carries `1.0`/`native` (the default path); model round-trip.
- Existing PDF fixture tests confirm the reader still extracts (no regression).

## Non-goals

- OCR / transcription-error detection (go-rag doesn't OCR).
- Per-page granularity (a doc-level signal for MVP; per-chunk-via-page-number is
  a refinement when mixed-PDF precision matters).
- A config knob for thresholds (constants for MVP; promote to config if needed).
