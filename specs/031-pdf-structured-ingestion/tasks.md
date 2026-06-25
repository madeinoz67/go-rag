---

description: "Task list for PDF Structured Ingestion (spec 031)"

---

# Tasks: PDF Structured Ingestion

**Input**: Design documents from `/specs/031-pdf-structured-ingestion/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, quickstart.md, `.specify/memory/constitution.md` (v1.0.0)

**Tests**: plan.md explicitly requests enhanced reader tests + a fake-captioner test ("like the fake enricher"), and the constitution mandates `go test ./...` green on every change — **test tasks ARE included** (write test first, confirm it fails, then implement).

**Organization**: Tasks grouped by user story (US1 metadata, US2 hierarchy-all-formats, US3 tables, US4 image/chart captioning) so each story is independently implementable and testable. US1–US3 are pure reader extraction (no model); US4 is the model-dependent, background/opt-in story (PRD N4, spec 029 pattern).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1–US4). Setup/Foundational/Polish have NO story label.
- Exact file paths are included in every implementation task.

## Path Conventions

Go project — single module `github.com/madeinoz67/go-rag`, entrypoint `cmd/go-rag/main.go`, all subsystems under `internal/`. Readers live in `internal/reader/`; the new captioner package is `internal/caption/` (sibling of `internal/enrich/`).

## Constitution Anchors (v1.0.0)

Each story touches specific principles — called out inline so implement-time respects them:

| # | Principle | Where it bites this feature |
|---|-----------|------------------------------|
| II | Content-Addressed Identity | US1: PDF metadata is stable identity → MUST feed `GenerateID`. US2/US4: heading spans + captions are NON-identity (strip/splice, do not hash-alter identity the way metadata does). |
| III | Pure Go | All extraction via existing `pdfcpu`; captioning via existing Ollama HTTP client. No new dependency. |
| IV | Async-After-ACK | US4 captioning is strictly post-ACK, background, circuit-breaker-guarded. US1–US3 are synchronous reader work. |
| V | Extension by Interface | Enhanced readers stay `FileReader`; `Captioner` mirrors `Enricher`. |

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: De-risk the one library unknown (pdfcpu's extraction APIs — research.md R1) and assemble the test corpus every story needs.

- [X] T001 Confirm clean baseline — run `make build vet test` green on `main` before any change (gate: `Makefile`, `cmd/go-rag/main.go`)
- [X] T002 [P] pdfcpu capability spike — verify the APIs research.md R1 assumed (`api.Info`/Info dictionary for metadata, bookmark/outline list, `api.ExtractImages`); record concrete signatures + any gaps in `specs/031-pdf-structured-ingestion/research.md` (informs T004, T006, T014, T024)
- [ ] T003 [P] Assemble test fixtures under `internal/reader/testdata/031/` — PDF with document properties (title/author/keywords), PDF with a bookmark outline, PDF with a data table (incl. a multi-page table), PDF with embedded images/charts, DOCX using Word Heading1–6 styles, `.txt` with heading patterns (ALL CAPS / `===`/`---` underlines / `:` lines)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The one shared helper two PDF-side stories need. US1 and US4 do NOT depend on it.

**⚠️ CRITICAL**: T004 MUST be complete before the PDF portions of US2 (T014) and US3 (T017).

- [ ] T004 Add positioned-text content-stream helper in `internal/reader/pdf.go` — parse `Tj`/`TJ` text operators together with `Tm`/`TD` positioning operators into `[]positionedText{Text, X, Y, Font, Size}` (depends on T002's pdfcpu findings; consumed by US2 PDF heading heuristics T014 and US3 table detection T017)

**Checkpoint**: Shared PDF content-stream parser ready — US2/US3 PDF work can proceed.

---

## Phase 3: User Story 1 - PDF metadata is extracted (Priority: P1) 🎯 MVP

**Goal**: PDF document properties (title, author, subject, keywords, dates) populate `Document.Metadata` and are filterable via `--tags`.

**Independent Test** (SC-001): Ingest a PDF with properties set; `status` shows title/author; `query "<keyword>" --tags "<title>"` returns it. A PDF without properties ingests cleanly with file-path metadata only.

### Tests for User Story 1

> Write FIRST; confirm FAIL before implementation.

- [X] T005 [P] [US1] Test PDF metadata extraction (title/author/subject/keywords/dates present) + absent-properties graceful in `internal/reader/pdf_test.go`

### Implementation for User Story 1

- [X] T006 [US1] Implement Info-dictionary metadata extraction in `internal/reader/pdf.go` `Read()` — read Title/Author/Subject/Keywords/CreationDate/ModDate via pdfcpu and populate `Document.Metadata` (per T002 verified API)
- [X] T007 [US1] Make metadata identity-correct (Constitution II) — ensure the enriched `Document.Metadata` is populated BEFORE `GenerateID` is called in `internal/pipeline/pipeline.go`; note that previously-ingested PDFs will re-identity on re-process (acknowledged in spec.md edge case "Re-ingestion")
- [X] T008 [US1] Validate SC-001 end-to-end via `specs/031-pdf-structured-ingestion/quickstart.md` (isolated `--db-path`; `status` + `--tags` filter)

**Checkpoint**: User Story 1 fully functional and independently testable — this is the shippable MVP.

---

## Phase 4: User Story 2 - Document hierarchy for ALL formats (Priority: P1)

**Goal**: DOCX (Word heading styles — richest), PDF (outline or font-size heuristics), and text (pattern heuristics) all emit `[]HeadingSpan` so the existing breadcrumb machinery threads `Chunk.SectionContext` (spec 025 parity). Markdown unchanged.

**Independent Test** (SC-002): Ingest a structured DOCX/PDF; query a phrase under a known heading; the hit carries the correct section breadcrumb. Text is best-effort (breadcrumb present or gracefully absent).

### Tests for User Story 2

> Three formats → three independent test files, all parallelizable.

- [X] T009 [P] [US2] Test DOCX heading-style extraction (Heading1–6 → `[]HeadingSpan`) in `internal/reader/docx_test.go`
- [X] T010 [P] [US2] Test text heading-pattern heuristics in `internal/reader/text_test.go`
- [X] T011 [P] [US2] Test PDF heading extraction (bookmark outline primary; font-size fallback) in `internal/reader/pdf_test.go`

### Implementation for User Story 2

- [X] T012 [P] [US2] Implement DOCX heading extraction — parse `<w:pStyle w:val="HeadingN"/>` in `internal/reader/docx.go` (+ `internal/reader/docx_zip.go`), emit `metadata["heading_spans"]`, and strip spans before `GenerateID` (parity with `stripMarkdownSpans` in `internal/reader/markdown.go:181` — spans are NON-identity, Constitution II)
- [X] T013 [P] [US2] Implement text heading heuristics (ALL CAPS lines <80 chars no terminal punctuation; `===`/`---` underlines; short `:`-terminated lines; indentation levels) in `internal/reader/text.go` → `metadata["heading_spans"]`
- [X] T014 [US2] Implement PDF heading extraction in `internal/reader/pdf.go` — bookmark/outline via pdfcpu first; fall back to font-size/position clustering from the T004 positioned-text helper → `metadata["heading_spans"]` (depends on T004)
- [X] T015 [US2] Verify existing breadcrumb threading consumes the new spans — confirm `resolveBreadcrumb` in `internal/pipeline/section.go:29` is invoked for DOCX/PDF/text paths so each `Chunk.SectionContext` carries the heading path; validate SC-002 across all three formats via `quickstart.md`

**Checkpoint**: User Stories 1 AND 2 both work independently.

> **PDF font-size fallback deferred (T004):** T014 ships the bookmark path — `api.Bookmarks` → page-offset `[]HeadingSpan` (tree depth = level). This is the high-precision primary signal and spec-compliant: outline-less PDFs are gracefully absent (FR-007). The font-size fallback AND the T004 positioned-text content-stream parser are deferred to the US3 turn — both table detection and font-size headings share that parser, so it is built once there and reused.

---

## Phase 5: User Story 3 - Tables extracted as searchable text (Priority: P1)

**Goal**: PDF tables are detected from the content stream and rendered as structured (Markdown) table text inside the chunk content, preserving row/column context. Best-effort (PDFs don't encode table structure — research.md R2).

**Independent Test** (SC-003): Ingest a PDF with a known table; query a cell value; the hit returns the table context (rows/columns preserved, not garbled flowing text).

### Tests for User Story 3

- [ ] T016 [P] [US3] Test table grid detection → Markdown table render; multi-page table handling; no-table graceful (text passes through unchanged) in `internal/reader/pdf_test.go`

### Implementation for User Story 3

- [ ] T017 [US3] Implement table grid detection in `internal/reader/pdf.go` — detect positionally-aligned text (consistent X offsets = columns, consistent Y offsets = rows) from the T004 positioned-text helper; render the region as a Markdown table spliced into `content` at the table's position (depends on T004)
- [ ] T018 [US3] Handle multi-page tables in `internal/reader/pdf.go` — extract as one logical table, or split with explicit continuation markers (per spec.md acceptance scenario 3)
- [ ] T019 [US3] Validate SC-003 end-to-end via `quickstart.md` (query a cell value → structured table text returned)

**Checkpoint**: User Stories 1, 2, AND 3 all work independently.

---

## Phase 6: User Story 4 - Images and charts extracted and captioned (Priority: P2)

**Goal**: Embedded PDF images/charts are extracted (`api.ExtractImages`) and captioned by a local vision model (background, opt-in); the caption is spliced into chunk content so the image's visual content is searchable. Reuses the spec 029 enrichment pattern.

**Independent Test** (SC-004, SC-006): With captioning enabled + a vision model, query a phrase describing a chart → hit returns the caption. With captioning disabled, the PDF still ingests (text/tables/hierarchy/metadata extract; images skipped gracefully).

### Implementation for User Story 4

> Interface + config + provider are independent files → parallelizable; pipeline wiring depends on them.

- [ ] T020 [P] [US4] Create the `Captioner` interface + `ImageData{Position, Bytes}` type in `internal/caption/captioner.go` (sibling of `internal/enrich/enricher.go` — `Caption(ctx, imageBytes) (string, error)` + `Model()`)
- [ ] T021 [P] [US4] Add config keys `captioning_enabled` (default `false`) and `captioning_model` in `internal/config/config.go`
- [ ] T022 [US4] Implement the local Ollama vision caption provider in `internal/caption/ollama.go` — mirror the enrich Ollama provider's HTTP client; use a chart-data-aware prompt (describe trend/key values/comparisons, not just "a chart") (depends on T020)
- [ ] T023 [US4] Test the caption path — fake captioner (like the fake enricher), PDF image extraction, pipeline caption-splice-into-content, circuit-breaker trip, and graceful skip-when-disabled in `internal/caption/*_test.go` and `internal/reader/pdf_test.go` (depends on T020)
- [ ] T024 [US4] Implement PDF embedded-image extraction in `internal/reader/pdf.go` — `api.ExtractImages` → `metadata["images"] = []ImageData{Position, Bytes}` (per T002 verified API; bytes discarded after captioning)
- [ ] T025 [US4] Bind the captioner in `internal/engine/engine.go` — instantiate + inject only when `captioning_enabled` (default off), else nil (depends on T020, T021, T022)
- [ ] T026 [US4] Wire post-ACK captioning in `internal/pipeline/pipeline.go` — when a captioner is bound and images are present: for each image call `Caption` and splice the caption into `content` at the image position BEFORE chunking; reuse the spec 029/030 circuit-breaker primitive; MUST NOT block the write ACK (Constitution IV); skip gracefully when disabled or model unavailable (depends on T023, T024, T025)
- [ ] T027 [US4] Validate SC-004 + SC-006 end-to-end via `quickstart.md` (captioning on with a pulled vision model → chart queryable; off → images skipped, rest extracts)

**Checkpoint**: All four user stories independently functional.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: No-regression, edge-case hardening, docs, final gate.

- [ ] T028 [P] Run the retrieval-eval no-regression harness (spec 004 / finding H02) against re-ingested PDFs to confirm structured ingestion does not regress retrieval quality
- [ ] T029 [P] Edge-case hardening in `internal/reader/pdf.go` — scanned PDFs (little/no text layer → graceful, OCR explicitly out of scope), password-protected (skip/error as today), corrupted/partial (extract what's possible, log, don't fail whole doc), very-large many-image PDFs (captioning batched + circuit-breaker-bound)
- [ ] T030 [P] Docs — doc-comments on enhanced reader functions + the `Captioner` interface; note PDF structured-ingestion behavior + the captioning opt-in in README / PRD-relevant sections
- [ ] T031 Final gate — full `quickstart.md` run (SC-001 … SC-006) on an isolated DB; `make build vet test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III pure-Go build gate)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies. T002 (pdfcpu spike) and T003 (fixtures) run in parallel; T001 first.
- **Foundational (Phase 2)**: T004 depends on T002's pdfcpu findings. BLOCKS the PDF portions of US2 (T014) and US3 (T017).
- **User Stories (Phases 3–6)**: Each story depends only on Setup (+ T004 for PDF-side US2/US3). Stories are independently deliverable.
- **Polish (Phase 7)**: Depends on the stories being complete.

### User Story Dependencies

- **US1 (P1, MVP)**: Setup only. No dependency on other stories. Pure extraction.
- **US2 (P1)**: Setup + T004 (for PDF font-size fallback). DOCX (T012) and text (T013) need only Setup. Independent of US1/US3/US4.
- **US3 (P1)**: Setup + T004. Independent of US1/US2/US4.
- **US4 (P2)**: Setup only for extraction (T024); full story needs T020–T022 (caption package) + T025/T026 wiring. Independent of US1/US2/US3. Do last (model-dependent, opt-in).

### Within Each User Story

- Tests written FIRST and confirmed FAILING before implementation (TDD — plan.md + constitution mandate).
- Interface/types before provider before wiring (US4: T020 → T022 → T025 → T026).
- Reader extraction before pipeline integration.
- Validate the story's Success Criterion via `quickstart.md` before declaring the checkpoint.

### Parallel Opportunities

- **Phase 1**: T002 ∥ T003 (different files).
- **Phase 4 (US2) tests**: T009 ∥ T010 ∥ T011 (three separate `_test.go` files).
- **Phase 4 (US2) impl**: T012 ∥ T013 ∥ T014 (docx.go / text.go / pdf.go — T014 waits on T004 only).
- **Phase 6 (US4)**: T020 ∥ T021 (new caption package ∥ config); then T022, T024 can proceed in parallel once T020 is done.
- **Phase 7**: T028 ∥ T029 ∥ T030 (eval ∥ edge-cases ∥ docs — different concerns).

---

## Parallel Example: User Story 2

```bash
# All three US2 tests in parallel (independent _test.go files):
Task: "Test DOCX heading extraction in internal/reader/docx_test.go"        # T009
Task: "Test text heading heuristics in internal/reader/text_test.go"        # T010
Task: "Test PDF outline/font-size heading spans in internal/reader/pdf_test.go" # T011

# All three US2 reader implementations in parallel (after T004):
Task: "Implement DOCX pStyle heading extraction in internal/reader/docx.go" # T012
Task: "Implement text heading heuristics in internal/reader/text.go"        # T013
Task: "Implement PDF outline + font-size heading extraction in internal/reader/pdf.go" # T014
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (Setup) — fixtures + pdfcpu spike.
2. Complete Phase 3 (US1 metadata).
3. **STOP and VALIDATE** SC-001 independently.
4. Ship — every PDF now has real metadata, the cheapest/most-visible win.

### Incremental Delivery

1. Setup → foundation ready.
2. + US1 (metadata) → test → demo (MVP).
3. + US2 (hierarchy, all formats) → test → demo.
4. + US3 (tables) → test → demo.
5. + US4 (captioning, opt-in) → test → demo.
6. Polish → final gate.

Each story adds retrieval value without breaking earlier stories (US2–US4 only enrich `content`/`metadata` surfaces; identity changes are acknowledged and confined to re-process).

### Parallel Team Strategy

With multiple implementers after Setup + T004:

- Implementer A: US1 (metadata) → US3 (tables) — both pdf.go-centric.
- Implementer B: US2 DOCX + text (no pdfcpu dependency).
- Implementer C: US4 caption package + wiring.
US2-PDF (T014) and US3 (T017) share T004 — coordinate so T014's font-size clustering and T017's grid detection agree on the positioned-text representation.

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] labels map tasks to US1–US4 for traceability.
- **Smoke rule** (CLAUDE.md): daemon/CLI tests use an isolated `--db-path <tmp>` — never the global vault.
- **Commits to `main` directly** (single-author repo, Conventional Commits).
- **pdfcpu API risk** (research.md R1/R2): metadata/outline/images likely available; table detection is heuristic. T002 resolves R1 before PDF implementation; table detection (T017) is best-effort and MUST fail gracefully.
- Verify each test fails before implementing; commit after each task or logical group.
