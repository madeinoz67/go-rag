# Tasks: Document Auto-Tag & Summary Enrichment

**Input**: Design documents from `/specs/029-doc-enrichment/` (the doc-level enrichment feature — background tags + summary via the local model).

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/enrichment.md, quickstart.md — all present.

**Tests**: INCLUDED — this feature's definition-of-done is verification: the identity-preservation guarantee (FR-002), the non-blocking ACK (FR-004), graceful model-down (FR-007), and the tags→filter payoff (SC-001) are all assertable. Tests are not optional add-ons.

**Organization**: US1 (auto-tags → existing filter) is the MVP; US2 (summary surfaced) is the triage payoff; US3 (resilient + back-fill) is the safety backbone. The sidecar + interface + config (Phase 2) underpin all three.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- File paths are project-relative (Go module `github.com/madeinoz67/go-rag`).

## Path Conventions (Go)

- Single binary, single entrypoint `cmd/go-rag` (untouched).
- New `internal/enrich` package (interface + provider + circuit breaker); additive sidecar on `Document`; one pipeline binding + async step; config gate; a filter bridge; summary surfacing on 4 transports + proto. No storage-prefix/on-disk-shape change.

## ⚠️ Build-order note (priority vs dependency)

This feature's value rests on a **non-identity guarantee** and an **async-after-ACK
guarantee**, so verification is part of the deliverable:

- The **PRD N4 revision (T002)** is a scope prerequisite — do it before the code
  lands, so the non-goal list stays honest.
- The **sidecar + interface + config (Phase 2)** must exist before any story.
- **US1** (tags→filter) is the MVP and proves the payoff; it depends on the
  provider (T007), pipeline binding (T008), and bridge (T010).
- **US2** (summary) and **US3** (resilience) are largely independent of each other
  after US1, and US3's circuit breaker (T014) wraps the provider call.

---

## Phase 1: Setup (Baseline)

**Purpose**: Record the pre-feature green baseline (the "nothing changed with enrichment off" claim is measured against it).

- [x] T001 Run `make build vet test` (and `make test-eval`) on `main` with enrichment off (the default); confirm green and record as the pre-feature baseline. No code changes.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The scope revision + the sidecar + the interface + the config gate + the identity guarantee. Blocks every story.

**⚠️ CRITICAL**: Blocks US1, US2, US3.

- [x] T002 Revise the PRD non-goal **N4** ("no LLM inference") narrowly to "no LLM inference **except background, local-only document enrichment**" in `PRD_RAG_Database.md`, with a one-line rationale. This is the scope prerequisite the plan flagged (research R7) — record it before the code lands.
- [x] T003 [P] Add the `EnrichInfo` type (`Tags []string`, `Summary string`, `Model string`, `GeneratedAt time.Time`, `Status string`) and the `Document.Enrichment *EnrichInfo` field (`json:"enrichment,omitempty"`) to `internal/model/model.go`. A **non-identity sidecar** — it MUST NOT enter `GenerateID` (the field is separate from `Metadata`; research R1, data-model §1).
- [x] T004 [P] Create `internal/enrich/enricher.go`: the `Enricher` interface (`Enrich(ctx, doc) (*EnrichInfo, error)`) — the document-level generation sibling of `embed.Embedder` — plus typed sentinels (`ErrNothingToEnrich`, a permanent-fail wrapper) so callers distinguish transient vs permanent failures (research R3, data-model §2).
- [x] T005 [P] Add the config gate to `internal/config/config.go`: `EnrichmentEnabled bool` (default false), `EnrichmentModel string`, and `EffectiveEnrichmentEnabled()` — mirroring `EffectivePoisoningEnabled()` (research R4).
- [x] T006 Add an identity-preservation test (`internal/model/model_test.go` or engine): assert `GenerateID`, document ID, chunk IDs, and content hash are byte-identical whether `Enrichment` is nil or populated — proving the sidecar never enters identity (FR-002 / SC-005). Depends T003.

**Checkpoint**: Sidecar + interface + config exist; identity is provably preserved; scope decision recorded.

---

## Phase 3: User Story 1 — Documents are auto-tagged, so the tag filter just works (Priority: P1) 🎯 MVP

**Goal**: Auto-tags land in the sidecar and flow through the existing `--tags` filter with no query-surface change; ingest ACK is unaffected.

**Independent Test**: Ingest with a fake enricher → the document gains `Enrichment.Tags` → `--tags <tag>` returns it; ACK latency unchanged (SC-001/SC-003).

### Implementation for User Story 1

- [x] T007 [US1] Create the local provider `internal/enrich/ollama.go`: implements `Enricher` via the local model's generation endpoint (reusing the existing loopback HTTP base, different endpoint from embeddings), producing tags + summary, returning `*EnrichInfo` or a typed error (research R3).
- [x] T008 [US1] Add `SetEnricher(e Enricher)` to `internal/pipeline/pipeline.go` (mirroring `SetDetector`/`SetRedactor`) and an async enrich step in `internal/pipeline/workers.go` `processJob` (after store/embed, per document) that calls the enricher and writes the `EnrichInfo` sidecar. Strictly post-ACK (research R2, data-model §4).
- [x] T009 [US1] Wire the enricher in `internal/engine/engine.go`: bind it to the pipeline only when `cfg.EffectiveEnrichmentEnabled()` (off → zero enrichment, byte-identical to today). Depends T005, T008.
- [x] T010 [US1] Add the tag-filter bridge: extend the tag resolver (the `tagsFromMetadata`/filter path in `internal/index/filter.go` or `internal/engine`) to read `Document.Enrichment.Tags` ∪ `Document.Metadata["tags"]`, so `--tags` consumes auto-tags with no new query field (research R1, data-model §3). Depends T003.
- [x] T011 [US1] Add pipeline + bridge tests (`internal/pipeline/*_test.go`, `internal/engine/*_test.go`): with a fake enricher, an ingested doc gains `Enrichment.Tags` and is returned by `--tags`; the <10 ms write ACK is unchanged with enrichment on (SC-001/SC-003). Depends T008, T010.

**Checkpoint**: Auto-tags reach the existing filter; ingest is non-blocking.

---

## Phase 4: User Story 2 — Every document has a one-line summary (Priority: P2)

**Goal**: The summary (and enrichment status) is surfaced wherever document metadata is shown, identically across all transports, omitted when absent.

**Independent Test**: An enriched doc shows a concise summary + status on status/hits across CLI/REST/gRPC/MCP; a too-short doc shows absent cleanly (SC-002).

### Implementation for User Story 2

- [x] T012 [P] [US2] Surface `summary` + `enrichment_status` (and the effective tag set) on the document/status view across all four transports: `internal/engine/status.go`, `internal/rest/`, `internal/grpc/engine_adapter.go`, `internal/mcp/server.go`, `internal/cli/`, and `proto/gorag.proto` (+ regen). Omitted when `Enrichment == nil` (contracts/enrichment.md §2). Depends T003.
- [x] T013 [US2] Add a status/parity test (`internal/engine/parity_test.go` or status_test): the summary + status appear identically across CLI/REST/gRPC/MCP and are omitted for unenriched docs (FR-010 / SC-002). Depends T012.

**Checkpoint**: Summary is visible everywhere, uniformly, gracefully absent when N/A.

---

## Phase 5: User Story 3 — Enrichment is resilient + back-fillable (Priority: P3)

**Goal**: A circuit breaker guards the model call; failures are marked (no infinite retry) and degrade gracefully; pre-feature docs back-fill on demand.

**Independent Test**: With the model unreachable, a doc still ingests/queries untagged, status reflects the failure, no infinite loop; a pre-feature doc back-fills (SC-004/SC-005).

### Implementation for User Story 3

- [x] T014 [US3] Add a circuit breaker to `internal/enrich` (opens after consecutive failures — MuninnDB-verified 5 fails / 30 s defaults, half-open probe) wrapping the provider call, so a down/misbehaving model fast-fails instead of stalling the worker (research R5).
- [x] T015 [US3] Implement graceful-fail + status in the pipeline enrich step: set `EnrichInfo.Status` (`enriched`/`failed`/`nothing-to-enrich`); permanent failures (bad output) are marked `failed` and not retried indefinitely; transient failures (model unreachable, circuit open, ctx cancelled) leave the sidecar nil for a later retry (research R5, data-model §4). Depends T008, T014.
- [x] T016 [US3] Add a back-fill re-enrich pass in `internal/engine` (mirrors `Reprocess`/`RescanPoisoning`) over docs with `Enrichment == nil` or `Status=="failed"`, plus an aggregate enriched-count in status (research R6). Depends T008.
- [x] T017 [US3] Add resilience + back-fill tests (`internal/pipeline`, `internal/engine`): model unreachable → doc still ingests/queries untagged, `Status` reflects failure, no infinite retry (SC-004); a pre-feature doc (nil sidecar) loads/queries and gains `Enrichment` after back-fill (SC-005). Depends T015, T016.

**Checkpoint**: Enrichment is safe to ship alongside an existing corpus and a flaky model.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Eval gate + final gate + ship.

- [x] T018 [P] Run `make test-eval` (spec 004 harness): with enrichment **off**, assert no regression vs the T001 baseline; document the tag-filter improvement available when **on** (SC-001 measured). No code unless a regression surfaces.
- [x] T019 [P] Run the full gate: `make build vet lint test` green; `CGO_ENABLED=0 go build ./...` succeeds (Constitution III); `go mod tidy` clean (no new dependency expected — the Ollama generation provider reuses the existing HTTP client).
- [x] T020 Final gate: commit to `main` with Conventional Commits (e.g. `feat(enrich): document auto-tag & summary enrichment (spec 029)`) and push (single-author repo — straight to `main`, per `CLAUDE.md`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — produces the baseline.
- **Foundational (Phase 2)**: After Setup. **BLOCKS** every story (sidecar/interface/config must exist; PRD N4 recorded).
- **US1 (Phase 3)**: Depends on Phase 2. MVP — ships alone.
- **US2 (Phase 4)**: Depends on Phase 2 (sidecar) only; independent of US1's pipeline work (different files: status/transports vs pipeline/filter).
- **US3 (Phase 5)**: Depends on US1's pipeline binding (T008); its circuit breaker (T014) wraps the provider.
- **Polish (Phase 6)**: Depends on all stories complete + green.

### User Story Dependencies

- **US1 (P1)**: After Phase 2. No dependency on US2/US3. (MVP.)
- **US2 (P2)**: After Phase 2 (sidecar). No dependency on US1/US3.
- **US3 (P3)**: After US1's pipeline binding (T008). Independent of US2.

### Within Each User Story

- Foundational sidecar/interface/config before any consumer.
- Provider (T007) before the pipeline binding that calls it (T008).
- Pipeline binding (T008) before the resilience layer that wraps it (T014/T015).

### Parallel Opportunities

- **Phase 2**: T003 (sidecar), T004 (interface), T005 (config) — different files, all `[P]`.
- **Phase 4**: T012 touches the status surface + 4 transports + proto as one uniform field-add (kept as one task for coherence; the per-transport edits within it are mechanical).
- After Phase 2, US1 (pipeline/filter) and US2 (status/transports) advance on largely disjoint files and can fan out.

---

## Parallel Example: After Phase 2

```bash
# US1 and US2 advance concurrently on disjoint files:
Task: "T007 [US1] local Ollama generation provider in internal/enrich/ollama.go"
Task: "T010 [US1] tag-filter bridge in internal/index/filter.go"
Task: "T012 [US2] summary/status surfacing across 4 transports + proto/"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (baseline) → Phase 2 (sidecar + interface + config + PRD N4 + identity test).
2. Phase 3 (US1 — provider + binding + bridge + tests).
3. **STOP and VALIDATE**: an ingested doc is auto-tagged and `--tags` returns it; the ACK is unchanged; identity is preserved.
4. At this point the retrieval-quality payoff is live; US2/US3 add triage value and safety.

### Incremental Delivery

1. Setup + Foundational → sidecar/interface/config exist; scope recorded; identity proven.
2. US1 (tags→filter) → test → metadata-filtered retrieval works with no manual tagging.
3. US2 (summary) → test → triage visibility across transports.
4. US3 (resilience + back-fill) → test → safe alongside an existing corpus + flaky model.
5. Polish → eval clean, committed to `main`.

### Solo-Author Note

Single-author repo, commits to `main` directly (per `CLAUDE.md`). The parallel
structure is for clarity and agent fan-out, not a team. In practice: Phase 1 → 2 →
3 → (4 ∥ 5) → 6.

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] label maps a task to its user story for traceability.
- **Identity is the load-bearing invariant** (FR-002): `Enrichment` is a struct
  field, never a `Metadata` key — if any task mutates `Document.Metadata` with
  enrichment data, stop (it would enter `GenerateID` and break idempotent ingest).
- Enrichment is **opt-in/default-off** — with it off, the system is byte-identical
  to today (zero model calls).
- The **PRD N4 revision (T002)** is a real prerequisite — the constitution gate
  passed (local-only), but the PRD non-goal list must be updated before shipping.
- Commit after each task or logical group; Conventional Commits to `main`.
