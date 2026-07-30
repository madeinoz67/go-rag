# Phase 0 Research — Repository Governance Standardization

**Date**: 2026-07-30 | **Status**: All unknowns resolved (spec issued no NEEDS CLARIFICATION; this consolidates the adaptation decisions).

## Method

The muninnd pattern was read from live files this session: `CLAUDE.md`, `.claude/agents/code-reviewer.md`, `docs/internals/keyspace-registry.md`. The go-rag target state was read from `CLAUDE.md`, `.specify/memory/constitution.md`, and `internal/storage/storage.go` (the `Prefix*` constants + `VaultScopedKinds` slice). Decisions below are grounded in those reads, not recalled.

## Decision 1 — Registry format: muninnd table + a Scope column

**Decision**: Mirror muninnd's keyspace-registry table (Prefix | Key shape after prefix | Value | Notes) but add a **Scope** column distinguishing **vault-scoped** (`kind | wsPrefix(8) | payload`) from **global** (`kind | payload`) kinds.

**Rationale**: In go-rag's v2.0 unified store (spec 052), the vault-scoped vs global distinction is load-bearing — it is exactly what the `PrefixScanByte`-on-a-vault-scoped-kind hazard turns on, and it is encoded as a literal slice (`VaultScopedKinds`) in `storage.go`. muninnd's registry buries scope in notes because nearly all its keys carry `ws`; go-rag's split is sharp enough to deserve its own column.

**Alternatives rejected**:
- *Copy muninnd verbatim, scope in Notes* — loses the single thing a go-rag reviewer most needs to see at a glance.
- *Two separate tables (vault-scoped / global)* — fragments the byte-ordering a reviewer uses to find free bytes.

## Decision 2 — Source of truth: `storage.go`, not the doc

**Decision**: The registry declares `internal/storage/storage.go` (`Prefix*` constants + `VaultScopedKinds`) as authoritative; the doc is a reference. A header states: where code and doc disagree, code wins and the reviewer flags the drift.

**Rationale**: This is muninnd's rule ("the docs can drift… live code wins") and it matches go-rag's constitution Storage-discipline rule, which already treats `storage.go` + `migrate.ExpectedVersion` as the layout authority. A doc that pretends to be authority will go stale and mislead.

**Alternatives rejected**:
- *Make the doc authoritative, generate code from it* — a code-generation pipeline for a single-author repo is over-build; would invert the constitution.

## Decision 3 — No programmatic disjointness test this spec (DEFERRED)

**Decision**: Do NOT add a `TestAll_NoDuplicateBytes`-style test (muninnd has one in `internal/prefix/prefix_test.go`). The registry is purely descriptive. Flag a follow-up issue for the test.

**Rationale**: (a) go-rag's prefixes are single-author and visually disjoint today (0x01–0x1B contiguous, no overlaps); (b) adding a test is a *code* change that belongs in its own spec, not a docs spec; (c) the constitution's Storage-discipline rule keys off `migrate.ExpectedVersion` and PRD §6.7, not off a disjointness test — so omitting it violates nothing. Visible deferral, not silent drop (recorded in spec Assumptions + a tasks.md follow-up).

**Alternatives rejected**:
- *Add the test now* — scope-creeps a docs feature into a code+migration change and risks the "sprawling PR" anti-pattern muninnd's principle 5 warns against.

## Decision 4 — CLAUDE.md: restructure in place, condense (don't duplicate) principles

**Decision**: Reorder existing CLAUDE.md content under muninnd's six-section shape — (1) what go-rag is + one-line promise, (2) architecture map, (3) core principles as the lens, (4) how we work, (5) the code-reviewer agent, (6) attribution. The principles section **condenses** the five from `constitution.md` and names that file authoritative; it does not reproduce them verbatim.

**Rationale**: muninnd's CLAUDE.md carries its principles inline because muninnd has no separate constitution file. go-rag *does* (`constitution.md`, v1.1.0) and its governance rule already says the constitution wins on principles. Duplicating verbatim would create a drift surface; condensing + linking preserves the single source of truth.

**Alternatives rejected**:
- *Reproduce the constitution verbatim in CLAUDE.md* — two authoritative copies of the principles; violates DRY and the constitution's own amendment discipline.
- *Leave CLAUDE.md as-is and only add the new sections at the end* — fails the "index" reshaping that makes the file scannable; the promise + principles would still be buried.

## Decision 5 — Reviewer agent: inline the protocol, don't reference a nonexistent rubric

**Decision**: The `code-reviewer.md` agent inlines its review protocol (operating rules, routing, output shape) directly in the agent body. It does **not** reference a `docs/internals/review-rubric.md` (muninnd's agent leans on one) because go-rag has no such file and authoring one is out of scope.

**Rationale**: muninnd's rubric is a gated protocol doc that took its own iteration. Pointing go-rag's agent at a nonexistent file would be a broken reference on day one. Inlining keeps the agent self-contained and shippable now; a rubric can be extracted later (the same way muninnd did it) without breaking the agent.

**Alternatives rejected**:
- *Author a review-rubric.md this spec too* — doubles the scope; the agent is useful without it.
- *Copy muninnd's rubric verbatim* — its invariant IDs (COG-/STO-/SEC-) are muninnd's; would mislead.

## Decision 6 — Reviewer invariant sets: adapted to go-rag, not muninnd's

**Decision**: The agent routes by six go-rag-specific invariant sets: **retrieval/hybrid** (BM25+vector+rerank RRF, eval harness), **storage/keyspace/migration** (prefixes, `VaultScopedKinds`, `migrate`), **auth/tokens** (spec 045: `gorag_` keys, bcrypt admin, `gorags_` sessions, loopback bypass guard), **transport parity** (MCP/REST/gRPC/UI over one engine; UI byte-parity), **enrichment/embed pipeline** (llama3.1, embedqueue, circuit breaker), and **cross-surface drift** (proto regen, openapi.yaml, console wiring, checksums for upgrade).

**Rationale**: muninnd's sets (engine/cognition, storage/keyspace, auth/transport/replication, surfaces, deps) map to muninnd's subsystems (replication, cognition, PAS). go-rag has neither replication nor cognition; it has retrieval hybridization and a 5-transport parity invariant that muninnd lacks. Copying muninnd's set names would produce an agent that mis-routes.

**Alternatives rejected**:
- *Use muninnd's set names verbatim* — would route a retrieval change to "cognition" (meaningless in go-rag) and miss the transport-parity invariant.

## Decision 7 — Spec number 059, not 058

**Decision**: Spec directory is `059-repo-standardization`.

**Rationale**: `PROJECTS.md` soft-reserves 058 for "Settings Slice 2b (sessions + admin reset, spec 058)". Using 058 here would collide when Slice 2b lands. 058/059 do not exist on disk; 059 is the next truly-free number after the reservation.

## Follow-ups flagged (not in scope)

- A programmatic prefix-disjointness test (Decision 3) — own spec, touches code.
- `docs/internals/invariants.md`, `decision-record.md`, `review-rubric.md` equivalents — the registry lands first; CLAUDE.md is shaped so these slots add without restructuring.
- Extracting the agent's inline protocol into a `review-rubric.md` once it stabilizes (Decision 5).

## Appendix — Locked prefix inventory (T003 output)

Single shared source US1 (registry) and US3 (agent routing) consume. Read from `internal/storage/storage.go` via Gortex `read` this session. This is the authoritative list; the registry (US1) renders it, the agent (US3) cites it.

**Vault-scoped kinds** (key shape `kind | wsPrefix(8) | payload`, ws = SipHash(vault name); the `VaultScopedKinds` slice, 19 entries): `0x01 Source, 0x02 Document, 0x03 Chunk, 0x04 Embedding, 0x05 FTSPosting, 0x07 FTSIndexed, 0x08 FTSGlobalSt, 0x0A SourceDocs, 0x0B DocChunks, 0x0C PathDoc, 0x0D ContentHash, 0x0E ChangeDetect, 0x0F Idempotency, 0x10 CorpusMeta, 0x11 PoisonQuar, 0x12 ThreatSrc, 0x13 NearDup, 0x14 EmbedQueue, 0x15 ImageCaption`.

**Global kinds** (key shape `kind | payload`): `0x09 Config, 0x17 AuthAPIKey, 0x18 AuthAdmin, 0x19 AuthSession, 0x1A VaultMeta, 0x1B VaultNameIndex, 0xFF` schema-version meta.

**Reserved**: `0x06` (FTS gap), `0x16` (BL-011 webhook per bridge backlog).

**Free for new storage prefixes**: `0x1C`–`0xFE` (excluding the global `0xFF`). Auth occupies 0x17–0x19 deliberately above the 0x16 reservation.
