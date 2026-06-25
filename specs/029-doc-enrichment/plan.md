# Implementation Plan: Document Auto-Tag & Summary Enrichment

**Branch**: `029-doc-enrichment` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/029-doc-enrichment/spec.md`. Adds a background, document-level enrichment step that tags and summarizes each ingested document via the local model, storing the result as a non-identity sidecar — so the existing-but-unpopulated tag filter becomes useful and each document gains a one-line summary. Granularity is per-document (one model call/doc), the cost profile that makes local enrichment viable.

## Summary

Today `Document.Metadata["tags"]` is read by the existing tag filter (spec 014)
but **nothing populates it**, so metadata-filtered retrieval — the single biggest
documented retrieval-quality lever — sits unused. This feature adds a background
`Enricher` (local-Ollama generation, distinct from the embedding `Embedder`) that,
after a document is durably ingested, produces a small tag set + a one-line summary
and stores them as a **non-identity `Document.Enrichment` sidecar** (mirroring the
per-`Chunk` sidecars `Poisoning`/`SectionContext`/`NearDup`). Tags reach the
existing filter via a **one-line bridge** (the tag resolver also reads
`Enrichment.Tags`), so `--tags` works with no query-surface change.

Design (full rationale [research.md](./research.md) R1–R7):

1. **`Document.Enrichment *EnrichInfo{Tags,Summary,Model,GeneratedAt,Status}`**
   — dedicated sidecar, NOT a `Metadata` key (identity-safe: `GenerateID` folds
   metadata, so the sidecar is a separate field, fixed once at ingest). R1.
2. **`Enricher` interface** (`Enrich(ctx, doc) (*EnrichInfo, error)`) + local
   Ollama generation provider (sibling of `embed.Embedder`, for generation). R3.
3. **Async-after-ACK** via `pipeline.SetEnricher(...)` → the existing `processJob`
   worker calls it per document (same pattern as near-dup clustering, spec 026). R2.
4. **Resilience** — circuit breaker (5 fails/30 s, MuninnDB-verified defaults) +
   `EnrichInfo.Status` (`enriched`/`failed`/`nothing-to-enrich`) for no-infinite-retry. R5.
5. **Opt-in** (`enrichment_enabled`, default off) via `EffectiveEnrichmentEnabled()`
   (mirrors `EffectivePoisoningEnabled`). R4.
6. **Surface** summary + status on status/hits (all transports); tags via the
   bridge; back-fill via a re-enrich pass. R6.

**PRD dependency:** enrichment is LLM generation, which revises PRD non-goal N4
("no LLM inference") **narrowly** — to "no LLM inference *except background,
local-only document enrichment*." Constitution-compatible; the PRD edit is a
tracked prerequisite (R7), not a constitution violation.

Entity/field detail: [data-model.md](./data-model.md). Wire contract:
[contracts/enrichment.md](./contracts/enrichment.md). Validation runbook:
[quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: none added. The Ollama generation provider reuses the
existing loopback HTTP client shape (different endpoint — `/api/generate` or
`/api/chat` — same base URL as embeddings). stdlib only; no new module.

**Storage**: unchanged — single Pebble instance. The `EnrichInfo` sidecar rides on
the existing document record (prefix `0x02`); **no new prefix**, no on-disk shape
change beyond the added JSON field.

**Testing**: `go test -race -cover ./...` (`make test`). Key suites: a new
`internal/enrich` package (enricher/provider unit tests with a fake model),
`internal/pipeline/*_test.go` (async-after-ACK enrichment + ACK-latency
unchanged), `internal/engine/*_test.go` (tag-filter bridge, identity-preserved,
status surfacing), the spec-004 retrieval-eval harness (no-regression with
enrichment off; tag-filter improvement when on), and cross-transport parity for
the summary field.

**Target Platform**: single statically-linked binary, local-first (Constitution I/III). No platform change.

**Project Type**: single-binary local RAG database + multi-transport daemon (CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution, unchanged): write ACK <10 ms — enrichment is
strictly post-ACK/background, so the budget is preserved (SC-003). Enrichment
latency itself is bounded by the local model + a circuit breaker (fast-fail on
failure); it does not gate ingest or query.

**Constraints**: non-identity sidecar (FR-002 — structural: separate field, not a
`Metadata` key), local-only (FR-005), opt-in/default-off (FR-006), graceful +
no-infinite-retry (FR-007/FR-009), async-after-ACK (FR-004).

**Scale/Scope**: one new sidecar type + field on `Document`, one new `Enricher`
interface + local provider, one new pipeline binding (`SetEnricher`) + async step
in `processJob`, a circuit breaker, a config gate, a tag-filter bridge, summary
surfacing on status/hits (4 transports), and a back-fill pass. Touches
`internal/{model,enrich,pipeline,engine,config,rest,grpc,mcp,cli}`, `proto/`.
Targets local <10K-doc corpora (one model call/doc).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All
five PASS — no violations, so the Complexity Tracking table is empty.

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | Enrichment uses the **bundled local model only** (FR-005) — the same loopback Ollama already used for embeddings, no cloud, no network egress. Single `CGO_ENABLED=0` binary, no new runtime dep. |
| II | Content-Addressed Identity | ✅ PASS | `Enrichment` is a **dedicated struct field on `Document`, not a `Metadata` key** (R1). `GenerateID(content, mime, metadata)` is unchanged — the sidecar never enters the identity hash. Re-add stays a no-op; chunk IDs/content hashes/vectors identical on vs off (SC-005). Same discipline as `SectionContext`. |
| III | Pure Go — No CGo | ✅ PASS | Pure-Go enricher/provider over the existing HTTP client; stdlib only, no new dependency. |
| IV | Async-After-ACK Writes | ✅ PASS | Enrichment runs on the background `processJob` worker **after** the <10 ms durable ACK (FR-004) — same async-after-ACK slot as embed/BM25/near-dup. The ACK budget is preserved (SC-003); the model call is off the hot path. |
| V | Extension by Interface, MCP-First | ✅ PASS | The `Enricher` interface mirrors `Embedder`/`FileReader` (extension by interface — R3); the summary is surfaced on MCP like every other field (FR-010). No core interface churn beyond the new sibling. |

**Post-design re-check** (after `data-model.md` / `contracts/enrichment.md`): no
new persisted entity/prefix, no on-disk shape change beyond the additive sidecar
field, no identity change, no ACK-path change, no new dependency, no cloud. The
five verdicts are unchanged. **Gate: PASS.**

> **PRD note (not a constitution item):** enrichment is LLM generation, which the
> PRD's N4 non-goal excludes. This feature revises N4 narrowly (background,
> local-only enrichment only). The constitution gate is unaffected (local-only
> honours Principle I). The PRD edit is a tracked implementation prerequisite (R7).

## Project Structure

### Documentation (this feature)

```text
specs/029-doc-enrichment/
├── spec.md              # Feature spec (/speckit-specify)
├── plan.md              # This file (/speckit-plan)
├── research.md          # Phase 0 — design decisions R1–R7
├── data-model.md        # Phase 1 — EnrichInfo sidecar + Enricher interface + bridge + lifecycle
├── quickstart.md        # Phase 1 — validation runbook (SC-001..005)
├── contracts/
│   └── enrichment.md    # Phase 1 — tags(unchanged surface) + summary/status surfacing + invariants
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

A new `internal/enrich` package (interface + local provider + circuit breaker),
additive sidecar + bridge, one pipeline binding + async step, and summary
surfacing across transports. No new `main`:

```text
internal/
├── enrich/                  # NEW: Enricher interface + local Ollama generation provider + circuit breaker (R3/R5)
├── model/
│   └── model.go             # Document.Enrichment *EnrichInfo sidecar (non-identity, R1)
├── pipeline/
│   ├── pipeline.go          # SetEnricher binding (mirrors SetDetector); processFile unchanged
│   └── workers.go           # async enrich step in processJob (after store; R2)
├── engine/
│   ├── (filter bridge)      # tag resolver reads Enrichment.Tags ∪ Metadata["tags"] (R1/US1)
│   ├── status.go            # summary + enrichment_status + aggregate enriched count
│   └── (back-fill)          # re-enrich pass (mirrors Reprocess/RescanPoisoning)
├── config/
│   └── config.go            # enrichment_enabled (default off) + enrichment_model + EffectiveEnrichmentEnabled() (R4)
├── rest/ ├── grpc/ ├── mcp/ ├── cli/   # summary/status surfacing (4 transports; tags ride existing filter)
proto/
└── gorag.proto              # summary + enrichment_status on the document/status message (+ regen)
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem (per
`CLAUDE.md`). The `Enricher` + provider + circuit breaker live in a new
`internal/enrich` package (keeps generation out of the pipeline/engine — Principle
V; the pipeline orchestrates, the provider owns the model call), mirroring how
`internal/embed` and `internal/rerank` isolate their concerns. The sidecar follows
the established `Poisoning`/`SectionContext`/`NearDup` discipline; the bridge is a
one-line extension of the existing tag resolver; surfacing is the standard
add-a-field-across-4-transports move. No new `main`, single binary entrypoint
(`cmd/go-rag`) untouched.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles. The
PRD N4 revision is a product-scope prerequisite (R7), not a constitution
violation, so it is not tracked here. This table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
