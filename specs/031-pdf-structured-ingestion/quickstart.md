# Quickstart — PDF Structured Ingestion (spec 031)

**Phase 1 output.** Validates the four enhancements (metadata, hierarchy, tables,
image/chart captions) + backward compatibility. Every scenario maps to a Success
Criterion in [spec.md](./spec.md).

> **Smoke rule:** use an isolated DB (`--db-path <tmp>`). Image captioning needs a
> local vision model (e.g. `llava`); the pure-extraction checks (US1–US3) need none.

## Build gate
```bash
make build vet test
```
**Expected:** green. Non-PDF readers unchanged; existing tests pass.

## SC-001 — Metadata extracted
```bash
go run ./cmd/go-rag add report.pdf --db-path "$DB"
go run ./cmd/go-rag status --db-path "$DB"   # title/author appear
go run ./cmd/go-rag query "keyword" --tags "report-title" --db-path "$DB"  # filterable
```
**Pass:** PDF document properties (title, author, keywords) appear in metadata + are filterable.

## SC-002 — Hierarchy for all formats
```bash
# DOCX (richest signal — Word heading styles)
go run ./cmd/go-rag add manual.docx --db-path "$DB"
go run ./cmd/go-rag query "configuration" --db-path "$DB"
# EXPECT: hit carries section: Installation / Configuration / Network

# PDF (bookmarks or heuristics)
go run ./cmd/go-rag add paper.pdf --db-path "$DB"
go run ./cmd/go-rag query "methodology" --db-path "$DB"
# EXPECT: hit carries section breadcrumb

# Text (heuristics)
go run ./cmd/go-rag add notes.txt --db-path "$DB"
# EXPECT: best-effort breadcrumbs or absent (graceful)
```
**Pass:** DOCX/PDF chunks carry section breadcrumbs (spec 025 parity); text is best-effort.

## SC-003 — Tables searchable
```bash
go run ./cmd/go-rag add financials.pdf --db-path "$DB"
go run ./cmd/go-rag query "revenue 2024" --db-path "$DB"
# EXPECT: hit returns the table context (row/column preserved, not garbled)
```
**Pass:** querying a table cell value returns the structured table text.

## SC-004 — Images + charts captioned
```bash
# With captioning enabled (captioning_model=llava)
go run ./cmd/go-rag add report_with_charts.pdf --db-path "$DB"
go run ./cmd/go-rag query "revenue growth trend" --db-path "$DB"
# EXPECT: hit returns the chunk containing the chart's caption
```
**Pass:** querying chart content returns the generated caption.

## SC-005 — Graceful (no images/tables/headings)
```bash
go run ./cmd/go-rag add plain.pdf --db-path "$DB"   # pure text, no structure
# EXPECT: ingests cleanly, no errors; text extracts as before
```

## SC-006 — Captioning off → images skipped
```bash
# captioning disabled
go run ./cmd/go-rag add report_with_charts.pdf --db-path "$DB"
# EXPECT: text + tables + hierarchy + metadata extract; images skipped (no error)
```
