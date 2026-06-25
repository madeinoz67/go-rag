# Specification Quality Checklist: Crash-Safe Background Embedder (spec 030)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-06-25
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — names concepts only (background embedder, pending-embed state, circuit breaker, micro-batch, transports, status); flags the flag-vs-scan mechanism explicitly as a plan decision, not a prescription
- [x] Focused on user value and business needs — framed per stakeholder (crash-safety, throughput/resilience, compatibility/observability)
- [x] Written for non-technical stakeholders — avoids Go/Pebble/struct detail
- [x] All mandatory sections completed — User Scenarios, Requirements, Success Criteria, plus Edge Cases / Key Entities / Assumptions

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — the one genuine open decision (flag-byte vs missing-embedding-scan) is recorded as a plan decision in Assumptions, with the spec requiring the outcome (crash-safe/idempotent) rather than the mechanism
- [x] Requirements are testable and unambiguous — each FR maps to ≥1 acceptance scenario or SC
- [x] Success criteria are measurable — SC-001..005 verifiable via kill-restart, ACK-latency, backend-down, batch-count, before/after identity
- [x] Success criteria are technology-agnostic — "backend", "embedding", "status", "transports" as surfaces, not code
- [x] All acceptance scenarios are defined — US1×3, US2×3, US3×3
- [x] Edge cases are identified — 9 incl. crash-mid-embed, backend-down, bulk, pre-existing orphans, permanent fail, idempotency, concurrency, Migrate interaction
- [x] Scope is clearly bounded — out-of-scope list (hot-swap, plugins, model change); distinct-from-Migrate stated
- [x] Dependencies and assumptions identified — 5 assumptions incl. the mechanism-is-plan-level, Migrate-distinction, constitution fit

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows — crash-safety (US1), decoupled/batched/resilient (US2), compatible/observable (US3)
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- This adopts the MuninnDB embedder-as-retroactive-processor model (verified: `cmd/muninn/server.go` → `NewRetroactiveProcessor(..., DigestEmbed)`; `internal/plugin/retroactive.go` — initial-scan recovery, scan-by-flag, circuit breaker, cross-doc micro-batching). Grounded in source read this session.
- The headline gap this closes is go-rag's crash-recovery hole: the in-memory embed queue (`chan job`, `pipeline.go`) is lost on SIGKILL, orphaning durably-stored chunks (0x03) without vectors (0x04) — silently invisible to semantic search until re-ingest. Confirmed in `pipeline.go`/`workers.go`/`load.go`.
- Constitution-compatible: embeddings are core (in-scope, no PRD N4 issue), still async-after-ACK (Principle IV preserved), local + pure-Go (I/III), identity untouched (II).
- Items all complete; ready for `/speckit-clarify` (if desired) or directly `/speckit-plan` (which will run the Constitution Check + resolve the flag-vs-scan mechanism in research.md).
