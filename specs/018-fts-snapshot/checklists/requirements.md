# Specification Quality Checklist: Persistent FTS Index Snapshot (Fast Cold Start)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-06-23
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- **Pivoted 2026-06-23**: the feature changed from "snapshot the in-memory FTS" to "make the FTS a
  durable Pebble-backed inverted index, indexed async." The quality items above still pass (the spec is
  complete, testable, scope-bounded) — re-validated against the pivoted spec. **16/16 still passing.**
- **Zero [NEEDS CLARIFICATION] markers.** The pivot was a design decision grounded in MuninnDB research
  + a benchmark (Pebble prefix-scan query ~0.3 ms worst-case vs ~0.24 ms in-memory; durable store
  6.7 MB), recorded in the spec's Clarifications section.
- **Highest-risk item for plan**: the **async-FTS visibility window + the FTS rewrite** (the in-memory
  `*FTS` becomes a thin Pebble-backed adapter; `Index`/`Delete`/`Search` all change; the H01/H06 query-
  path + eval interactions). Plan must preserve BM25 transparency (FR-008) and the < 50 ms budget.
- **Constitution**: the pivot *improves* the check — Principle IV (BM25 indexing async post-ACK) passes
  by the letter (the current sync-FTS-in-`storeDocument` bent it).
- The plan/research/data-model/contracts/quickstart artifacts still describe the OLD snapshot design and
  are **stale** — regenerate via `/speckit-plan` against this pivoted spec.
- Items marked incomplete would require spec updates before `/speckit-plan`; none are incomplete.
