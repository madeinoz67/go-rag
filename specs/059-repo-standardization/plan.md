# Implementation Plan: Repository Governance Standardization (muninnd alignment)

**Branch**: `main` (single-author repo) | **Date**: 2026-07-30 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/059-repo-standardization/spec.md`

## Summary

Bring muninnd's three-piece governance scaffolding to go-rag, adapted to go-rag's domain: (1) a Pebble keyspace registry under `docs/internals/` listing every allocated prefix with vault-scoped vs global scope and the live hazards; (2) a restructure of `CLAUDE.md` into muninnd's "constitution + index" shape (what go-rag is → architecture map → principles as lens → working protocol → reviewer section → attribution), preserving all existing operational content; (3) a resident read-only `code-reviewer` agent under `.claude/agents/` that routes by go-rag's invariant sets. **No binary, storage-layout, or runtime change** — purely docs + agent scaffolding. `internal/storage/storage.go` remains the source of truth; the registry is a reviewer's reference that declares "code wins."

## Technical Context

**Language/Version**: Go 1.22+ project (the subject being documented). The three deliverables are Markdown + agent frontmatter/prose — no Go code is written or modified this feature.

**Primary Dependencies**: Pebble KV (the keyspace being registered). No new dependencies introduced.

**Storage**: Pebble (documented, not modified). The registry is descriptive of the existing layout in `internal/storage/storage.go`; it changes nothing on disk.

**Testing**: Audit-based validation, not executable tests — (a) read-both-files prefix-coverage check, (b) CLAUDE.md before/after content audit, (c) reviewer-agent smoke test (verdict + zero writes), (d) cross-reference check. No `go test` additions this feature.

**Target Platform**: macOS dev (single-operator). Artifacts are repo-portable Markdown/YAML-frontmatter.

**Project Type**: Documentation + agent scaffolding for a Go single-binary local RAG database.

**Performance Goals**: N/A (documentation).

**Constraints**: `internal/storage/storage.go` is the source of truth; the registry MUST declare code-wins on drift; single-author commit-to-`main`; no "Generated with Claude" attribution in any produced content.

**Scale/Scope**: 3 artifacts — 1 new doc (`docs/internals/keyspace-registry.md`), 1 restructure (`CLAUDE.md`), 1 new agent (`.claude/agents/code-reviewer.md`).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status: PASS — no violations, no Complexity Tracking rows.**

Checked against `.specify/memory/constitution.md` (v1.1.0):

| Principle / Rule | Result | Reasoning |
|---|---|---|
| I. Local-First, Single-Binary | N/A | No binary or runtime change. Artifacts are Markdown. |
| II. Content-Addressed Identity | N/A | No ingest/identity change. |
| III. Pure-Go, No CGo | N/A | No new dependencies; no code added. |
| IV. Async-After-ACK Writes | N/A | No write-path change. |
| V. Extension by Interface, MCP-First | N/A | No new formats/providers/transports. |
| Storage discipline (key-space layout change → migrate + bump + PRD 6.7) | **PASS** | The feature *documents* the existing layout; it does not add/retire a prefix, change value encoding, or change key construction. No migration, no `ExpectedVersion` bump, no PRD §6.7 edit. |
| Compliance (key-space-touching PR states schema-version impact) | **PASS** | `internal/storage/storage.go` is read, not modified. Schema-version impact = none. |
| Code-navigation (tokensave/gortex) | Honored | Plan uses Gortex `read`/`search` for source, not ad-hoc grep on indexed code. |

**Post-Phase-1 re-check**: the design (Phase 1 below) introduces no code, no on-disk layout change, and no new dependency — the gate still passes.

## Project Structure

### Documentation (this feature)

```text
specs/059-repo-standardization/
├── plan.md              # This file
├── research.md          # Phase 0 — adaptation decisions
├── data-model.md        # Phase 1 — prefix entity + invariant-set model
├── quickstart.md        # Phase 1 — validation runbook
├── contracts/
│   └── agent-contract.md  # Phase 1 — reviewer agent I/O contract
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

No source-code tree change. Three repository files are touched — two new, one restructured:

```text
docs/internals/
└── keyspace-registry.md   # NEW — Pebble prefix registry (US1)

.claude/agents/
└── code-reviewer.md       # NEW — resident read-only reviewer agent (US3)

CLAUDE.md                  # RESTRUCTURE — muninnd "constitution + index" shape (US2); preserve all content
```

**Structure Decision**: Documentation-only. `docs/internals/` is created (it does not yet exist in go-rag); it mirrors muninnd's `docs/internals/` slot so future internal docs (invariants, decision-record, review-rubric equivalents) land without restructuring. The `.claude/agents/` directory is created (go-rag has `.claude/` but no `agents/` subdir today).

## Complexity Tracking

> Not filled — Constitution Check has no violations to justify.
