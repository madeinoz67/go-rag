---

description: "Task list for Per-Chunk Section Context (spec 025, audit H23)"
---

# Tasks: Per-Chunk Section Context

**Input**: Design documents from `/specs/025-chunk-section-context/` — plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md.

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/api.md, quickstart.md — all present from `/speckit-plan`.

**Tests**: INCLUDED. The spec defines measurable Success Criteria (SC-001…005) and an Independent Test per user story; this is a correctness- and cross-transport-parity-critical feature in a test-heavy Go repo, so test tasks are first-class.

**Organization**: Tasks grouped by user story. Each story is an independently testable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story (US1/US2/US3) — Setup/Foundational/Polish have no label
- Every task names an exact file path

## Path Conventions (Go)

- Source lives under `internal/<pkg>/` (one package per PRD subsystem, per `CLAUDE.md`).
- Tests are co-located: `internal/<pkg>/<file>_test.go`. Contract/parity tests: `internal/engine/parity_test.go`.
- Generated protobuf: `proto/gen/`; schema: `proto/gorag.proto`.
- Build gate: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` (Constitution III; `make build vet test`).

## ⚠️ Build-order note (priority vs dependency)

The spec labels **US1 = P1** (highest user value: breadcrumb visible on the hit) and **US2 = P2** (correctness: every chunk inherits its active heading). But US1's visible breadcrumb is *populated* by US2's ingest-time resolver — US1 cannot pass its independent test until US2 exists. Therefore the phases are **built in dependency order** (Foundational → US2 → US1 → US3), while the `[US#]` labels and P1/P2/P3 priorities are preserved for spec traceability. This is the only deviation from strict priority ordering and it is called out per-story below.

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm the repo is green before changes so any regression is attributable (baseline for SC-004).

- [X] T001 Run `make build vet test` (or `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`) on `main`; confirm green and record as the pre-feature baseline. No code changes.

**Checkpoint**: Clean baseline established.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared infrastructure that ALL three stories depend on. No story work can begin until this phase is complete.

**⚠️ CRITICAL**: Blocks US1, US2, and US3.

- [X] T002 [P] Add `SectionContext []string` field to `Chunk` in `internal/model/model.go` with tag `json:"section_context,omitempty"` and the doc comment from `data-model.md` §1 (non-identity sidecar, nil = absent). No identity change.
- [X] T003 [P] Define `HeadingSpan{Level int; Text string; Offset int}` and replace the two divergent passes in `internal/reader/markdown.go` (the flat-heading loop at :36-45 and `stripMarkdown` at :142) with ONE unified, code-fence-aware scan that emits the stripped text AND a positional `[]HeadingSpan` (offsets into the stripped text). Keep `metadata["headings"]` as the flat list for backward compatibility. Closes FR-009 (`#` inside fenced code is not a heading). Research R1/R4.
- [X] T004 [P] Add an additive offset-aware redaction API in `internal/redact/redact.go` — a new method `ApplyWithEdits(text string) (redacted string, findings []Finding, edits []Edit)` returning per-substitution `Edit{Pos, RemovedLen, InsertedLen}` (the regex visitor at :50/:69 already sees each match), WITHOUT changing the existing `Apply` signature or its callers. Add helper `translateOffset(offset int, edits []Edit) int` (identity when edits empty). Research R3.
- [X] T005 [P] Add unit tests in `internal/reader/markdown_test.go` for the unified scan: heading levels + offsets into stripped text; `#`/`#!/bin/sh` inside a fenced block excluded (FR-009); H1→H6 nesting; heading-less body yields no spans; front-matter `title` is not a heading.
- [X] T006 [P] Add unit tests in `internal/redact/redact_test.go` for `ApplyWithEdits` + `translateOffset`: variable-length `[REDACTED:type]` substitutions shift offsets correctly; LUHN-card substitution; identity translation when no edits; existing `Apply` behaviour unchanged.

**Checkpoint**: Shared model field + heading-span source + offset translation ready. Story phases can begin.

---

## Phase 3: User Story 2 — Every chunk inherits the heading active at its position (Priority: P2) 🏗 built first

> **Built before US1** because US1's visible breadcrumb is populated here. See build-order note.

**Goal**: Each `Chunk` carries the ordered heading breadcrumb active at its **start** position; chunking geometry and identity are unchanged (FR-007/FR-008).

**Independent Test** (SC-001, no query needed): ingest a fixture whose headings sit at known byte ranges, enumerate the stored chunks, and assert each carries the heading path governing its start position.

### Implementation for User Story 2

- [X] T007 [US2] Implement `resolveBreadcrumb(spans []reader.HeadingSpan, startIdx int, edits []redact.Edit) []string` in a new `internal/pipeline/section.go`: translate each span's offset via `redact.translateOffset` (R3), then return the heading-stack state at the last span with `offset ≤ startIdx` (R5); nil when there are no spans. Pure function — the unit-test target for positional correctness.
- [X] T008 [US2] Wire resolution into `processFile` in `internal/pipeline/pipeline.go`: extract+remove `metadata["heading_spans"]` BEFORE `model.GenerateID` (R2 — keeps docID byte-identical), call `redactor.ApplyWithEdits` instead of `Apply` when a redactor is set (R3), and assign `chunks[i].SectionContext = resolveBreadcrumb(spans, s.StartCharIdx, edits)` inside the chunk-construction loop (before `storeDocument`, same site as poisoning scoring — R8). Depends on T002, T003, T004, T007.
- [X] T009 [US2] Add tests in `internal/pipeline/pipeline_test.go`: ingest a nested-heading fixture → enumerate chunks → assert the correct governing path per chunk (SC-001); a straddling chunk carries the heading at its **start** position (FR-007); chunk count/sizes/`StartCharIdx`/overlap identical to a pre-feature run (FR-008); a redaction-enabled case still attaches correct breadcrumbs (R3).
- [X] T010 [US2] Add an idempotency test in `internal/pipeline/pipeline_test.go`: re-adding an unchanged heading-bearing document is a no-op (document and chunk counts unchanged, no duplicate) — verifies the span key is removed before identity so docID is stable (FR-003 / US3-scenario-3).

**Checkpoint**: US2 independently testable — enumerate chunks, each has the correct breadcrumb; geometry and identity intact.

---

## Phase 4: User Story 1 — Section location visible on every hit (Priority: P1) 🎯 MVP value

> Builds on US2's populated `Chunk.SectionContext`. This is the user-facing value the spec ranks P1.

**Goal**: A retrieved hit shows its section breadcrumb on CLI, REST, gRPC, and MCP with an identical value, requiring zero extra user actions (FR-004, SC-002).

**Independent Test**: ingest a multi-section Markdown document, query for a phrase under a known heading, confirm the hit shows the breadcrumb identically across all transports.

### Implementation for User Story 1

- [X] T011 [P] [US1] Add `SectionContext []string` to `engine.QueryHit` in `internal/engine/types.go` and copy `c.SectionContext` onto each hit in the hit-building loop in `internal/engine/query.go` (immediately alongside the `Poisoning: c.Poisoning` copy at :252). Contracts/api.md.
- [X] T012 [P] [US1] Add `SectionContext []string \`json:"section_context,omitempty"\`` to `queryHit` in `internal/rest/types.go` and map it from the engine hit in the REST adapter (adapters carry no logic).
- [X] T013 [P] [US1] Add `repeated string section_context = 9;` to `message QueryHit` in `proto/gorag.proto` (tag 9, after `chunk_index = 8`) and regenerate `proto/gen` via the project's protoc/go-generate step.
- [X] T014 [P] [US1] Render the breadcrumb in the MCP hit response in `internal/mcp/server.go` (both text and structured renders), elements joined by `" / "`, omitted when absent — mirroring how `chunk_index`/`poisoning` are rendered today.
- [X] T015 [P] [US1] Render the breadcrumb in the CLI query output in `internal/cli/query.go` (`renderResults`): a `Section:` line in the human-text format and a `section_context` field in the machine-readable formats; omitted when absent.
- [X] T016 [US1] Extend the cross-transport parity suite in `internal/engine/parity_test.go`: add a nested-heading Markdown fixture to the parity corpus and assert `section_context` is byte-identical across REST/gRPC/MCP; add a heading-less fixture and assert absence is identical across transports (SC-002). Depends on T011–T015.
- [X] T017 [US1] End-to-end ingest→query→breadcrumb — covered by `TestCrossTransport_SectionContextParity` (T016), which ingests a nested-heading Markdown fixture, queries, and asserts the hit carries the expected breadcrumb across the facade/REST/gRPC. (A standalone duplicate is redundant: BM25 indexes async-after-ACK, so a reliable e2e needs the same standalone-pipeline-then-drain pattern the parity test already uses.)

**Checkpoint**: US1 independently testable — a query shows the breadcrumb, identical across all four transports.

---

## Phase 5: User Story 3 — Documents and chunks without section context degrade gracefully (Priority: P3)

**Goal**: Heading-less documents and pre-feature chunks return absent (never erroring) section context; back-fill is available via Reprocess (FR-006, US3).

**Independent Test** (SC-005): ingest a heading-less `.txt` and a code-only `.md`, query each → results return without error and omit `section_context`; separately, load a chunk written by a pre-feature build → field is absent, no parse failure.

### Implementation for User Story 3

- [X] T018 [P] [US3] Add tests across `internal/reader` and `internal/pipeline` (and an engine/CLI query assertion): heading-less plain-text and code-only Markdown documents ingest and query without error, and their chunks carry nil `SectionContext` → the hit omits `section_context` identically across transports (FR-006, US3-1). Much of this falls out of T003 (no spans emitted); tests pin it.
- [X] T019 [US3] Add a migration test in `internal/model/model_test.go` (or pipeline): a chunk JSON record in the pre-feature shape (no `section_context` key) unmarshals cleanly with nil `SectionContext`, persists/round-trips, and is returned on a hit with the field absent — no parse or read failure (US3-2).
- [X] T020 [US3] Verify back-fill via `Reprocess` in `internal/pipeline/reprocess_test.go`: a pre-feature heading-bearing document, once reprocessed (re-reads source), has its chunks gain the correct `SectionContext` (research R7 — there is no cheap rescan, unlike poisoning, because the raw heading structure is not persisted).

**Checkpoint**: US3 independently testable — degradation and migration are safe.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-story validation and project hygiene.

- [X] T021 [P] Run the retrieval-eval harness (spec 004) on a fixture corpus, pre-feature vs post-feature under the same embedding model; assert no metric regression (SC-004, FR-008 — the chunker and embedded text are unchanged).
- [X] T022 [P] Execute the full `quickstart.md` runbook (Scenarios A–F) against an ISOLATED DB (`--db-path <tmp>`) with NON-DEFAULT `--mcp-addr/--rest-addr/--grpc-addr` (per repo `CLAUDE.md` smoke rule); confirm SC-001…005.
- [X] T023 Update audit tracking: mark finding **H23** done in `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 6 §1.1); add a one-line `section_context` note to the project's done-condition/ISA if it maintains a per-feature ledger.
- [X] T024 Final gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III build gate); `go mod tidy` clean; commit to `main` with Conventional Commits (e.g. `feat(chunk): per-chunk section context (H23)`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1 — **BLOCKS all user stories**.
- **US2 (Phase 3)**: Depends on Phase 2. Built first — it populates `Chunk.SectionContext`, which US1 reads.
- **US1 (Phase 4)**: Depends on **Phase 2 + US2 (Phase 3)** — its hits read the field US2 populates.
- **US3 (Phase 5)**: Depends on Phase 2 (and benefits from US2's idempotency test T010); its degradation behaviour is mostly asserted, not newly built.
- **Polish (Phase 6)**: Depends on Phases 3–5.

### User Story Dependencies (priority vs build order)

- **US2 (P2)** — built first. Depends only on Foundational. Independently testable (enumerate chunks, no query). This is the correctness core.
- **US1 (P1)** — depends on **US2** (reads `Chunk.SectionContext`). Independently testable via query + cross-transport parity. This is the user-visible value.
- **US3 (P3)** — depends on Foundational; mostly verification of graceful absence + migration + back-fill.

> The spec ranks US1 highest by *user value*; the *build* order is US2 → US1 because surfacing a breadcrumb presupposes a correctly-populated one. The `[US#]`/P# labels are preserved for traceability to `spec.md`.

### Within Each User Story

- Pure resolver/helpers before pipeline wiring.
- Pipeline wiring before transport surfacing.
- Each story's checkpoint is independently verifiable before moving on.

### Parallel Opportunities

- **Phase 2**: T002, T003, T004, T005, T006 are all different files → fully parallel.
- **Phase 4 (US1 surfacing)**: T011, T012, T013, T014, T015 are different files → parallel; then T016 (parity) and T017 (e2e) integrate them sequentially.
- **Phase 5**: T018 parallel with T019/T020 where files differ.
- **Phase 6**: T021, T022, T023 are independent → parallel.

---

## Parallel Example: Phase 2 (Foundational)

```text
Task: "Add Chunk.SectionContext field in internal/model/model.go"        (T002)
Task: "Unified code-fence-aware Markdown scan + HeadingSpan in internal/reader/markdown.go"  (T003)
Task: "Additive ApplyWithEdits + translateOffset in internal/redact/redact.go"  (T004)
Task: "Markdown scan tests in internal/reader/markdown_test.go"          (T005)
Task: "Redact offset-translation tests in internal/redact/redact_test.go" (T006)
```

## Parallel Example: Phase 4 (US1 surfacing — all different files)

```text
Task: "engine.QueryHit.SectionContext + query.go copy"        (T011)
Task: "REST queryHit.SectionContext in internal/rest/types.go" (T012)
Task: "proto section_context = 9 + regen proto/gen"            (T013)
Task: "MCP breadcrumb render in internal/mcp/server.go"        (T014)
Task: "CLI breadcrumb render in internal/cli/query.go"         (T015)
```

---

## Implementation Strategy

### MVP First (deliver the P1 user value)

The smallest increment that delivers measurable user value is **US1's breadcrumb-on-hit**, but it requires US2's resolver underneath. MVP scope:

1. Phase 1 — baseline (T001).
2. Phase 2 — Foundational (T002–T006): the shared field, heading-span source, offset translation.
3. Phase 3 — US2 (T007–T010): the resolver + pipeline population + correctness tests.
4. Phase 4 — US1 (T011–T017): surface on all four transports + parity.
5. **STOP and VALIDATE**: run `quickstart.md` Scenario A — query a nested-heading doc, confirm the breadcrumb appears identically on CLI/REST/gRPC.

At this point the feature's headline value ("see where a retrieved chunk lives in its document") is shippable.

### Incremental Delivery

1. Foundational → shared infra ready.
2. + US2 → enumerate chunks, each has a correct breadcrumb (testable without query).
3. + US1 → query shows the breadcrumb across all transports (**MVP**).
4. + US3 → heading-less/pre-feature degradation safe; back-fill via Reprocess.
5. + Polish → no retrieval regression, full quickstart green, H23 closed.

### Solo-Author Note

Per repo `CLAUDE.md`, Spec Kit work commits straight to `main` — no feature branch, no PR ceremony. Commit per task or logical group with Conventional Commits.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[US#]` maps a task to its spec user story; Setup/Foundational/Polish carry no label.
- Each story checkpoint is independently verifiable (US2: enumerate chunks; US1: query + parity; US3: graceful absence).
- The chunker (`internal/chunk`), the `FileReader` interface signature, chunk/document identity, the write-ACK ordering, and the embedded text are all **unchanged** (FR-008, Constitution II/IV/V).
- Verify the build/test gate stays green after every task; the repo is never left red.
