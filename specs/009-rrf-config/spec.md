# Feature Specification: Configurable Reciprocal Rank Fusion (RRF) Constant

**Feature Branch**: `009-rrf-config` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-22

**Status**: Draft

**Input**: User description: "work on the next backlog item" → resolved to **H08**
from `RAG_BOOK_AUDIT_BACKLOG.md` (first unchecked Phase 1 item):
*"RRF weights hardcoded + asymmetric-k formula unreviewable. Move k to config
(or derive from Mode), expose via flag; document the asymmetry as intentional OR
collapse to single k=60."* Source detail: `RAG_BOOK_AUDIT.md` §1.3 (P1) and §1.4
(P1), backlog row H08.

**Audit trail** (source: truth grounding in current code):
- `internal/index/retrieval.go:34` — `Retrieval` struct holds `kVec`, `kFTS`,
  `poolSize` as unexported ints.
- `internal/index/retrieval.go:55` — `NewRetrieval` hardcodes `kVec: 40, kFTS: 60,
  poolSize: 60`.
- `internal/index/retrieval.go:231` — `reciprocalRankFusion(vectorHits, ftsHits,
  kVec, kFTS)` fuses with an **asymmetric per-list k**: vector contributes
  `1/(kVec+rank+1)`, FTS contributes `1/(kFTS+rank+1)`. With `kVec=40 < kFTS=60`
  the vector list is silently up-weighted relative to FTS.
- `internal/config/config.go` — `Config` has no RRF fields; the `Get(key)`
  accessor switch has no `rrf_*` cases.
- `internal/cli/query.go` — query flags are `Query / K / Mode / NoRerank /
  Threshold`; no RRF knob.
- `internal/engine/query.go:17` — `Engine.Query(QueryRequest)`; `QueryRequest`
  has no field to carry an RRF constant, so a per-call override cannot reach the
  fuser today.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Tune retrieval quality per corpus without editing code (Priority: P1)

A user indexing a noisy corpus (e.g., chat logs, transcripts) finds the default
hybrid ranking under- or over-weights one retrieval list. They want to adjust the
RRF smoothing constant from the persisted config or a one-off flag — without
rebuilding the binary or touching source — and see the effect on results.

**Why this priority**: This is the core value of H08. The audit (§1.4 P1) cites
the retrieval book: "fusion weights tunable per corpus." Today the constant is
opaque and frozen in source, so no corpus-specific tuning is possible. Making it
configurable is the primary remediation.

**Independent Test**: Set the RRF constant to a non-default value in
`.go-rag/config.json`, run `go-rag query "<q>"`, and confirm the ranking order
changes vs. the default (verifiable on a fixed golden corpus via the H02 eval
harness in `specs/004-retrieval-eval-harness`).

**Acceptance Scenarios**:

1. **Given** a vault with a known set of documents, **When** the user sets a
   larger RRF constant in config and reruns the same query, **Then** the fused
   ranking shifts toward flatter/even weighting of the two lists (the constant
   dampens rank dominance).
2. **Given** the default config (no `rrf_k` key), **When** the user runs a hybrid
   query, **Then** ranking uses the new single-`k=60` default (standard RRF).
   This is a deliberate default change from today's asymmetric `kVec=40`/`kFTS=60`
   — documented in the README/changelog and measured on the H02 golden dataset
   before merge (SC-001).
3. **Given** an out-of-range constant (≤0), **When** config is loaded, **Then**
   validation rejects it with a clear error rather than producing NaN scores.

---

### User Story 2 - One-off tuning from the CLI flag (Priority: P2)

A user experimenting interactively wants to try several RRF constants on the same
query without editing config between runs — a `--rrf-k` (or per-list equivalent)
flag on the `query` command that overrides config for that single invocation.

**Why this priority**: Lower friction iteration; complements US1. The flag is the
"expose via flag" half of the backlog item. Without it, tuning requires editing
JSON per attempt.

**Independent Test**: Run `go-rag query "<q>" --rrf-k <n>` for two different `n`
values and confirm distinct result orderings; confirm omitting the flag falls
back to the config/default value.

**Acceptance Scenarios**:

1. **Given** the query command, **When** the user passes the RRF flag, **Then**
   that value overrides config for this call only and is reflected in the
   returned ranking.
2. **Given** the flag is omitted, **When** the query runs, **Then** the effective
   constant equals the config value (or default), never zero/empty.

---

### User Story 3 - Deterministic, documented fusion formula (Priority: P2)

A reader of the code or docs (or an AI agent consuming go-rag) needs to know
exactly how the two ranked lists combine and *why* the constant is what it is.
Today the asymmetric `kVec=40 / kFTS=60` is undocumented and its effect (silent
FTS/vector weighting) is unreviewable.

**Why this priority**: This is the "unreviewable" word in the audit title. The
formula must be stated once, in one place, with the chosen semantics justified.

**Independent Test**: A reader finds the fusion formula and the rationale for the
constant in the retrieval package doc and/or README; a unit test pins the exact
score a chunk earns from a given rank under the documented formula.

**Acceptance Scenarios**:

1. **Given** the source/docs, **When** a reader looks for the fusion formula,
   **Then** they find a single canonical statement of `score(d) = Σ 1/(k + rank)`
   with `k` defaulting to 60, plus the rationale: standard RRF per the retrieval
   book §6.6; the prior asymmetric per-list `kVec`/`kFTS` was undocumented and is
   removed.
2. **Given** a chunk ranked #1 in both lists under a known constant, **When** the
   fuser runs, **Then** its fused score equals the value the documented formula
   predicts (golden unit test).

---

### Edge Cases

- **Zero or negative constant**: MUST be rejected at config-load / flag-parse
  time (division by zero or sign flip would corrupt every score). See FR-005.
- **Non-hybrid mode**: the RRF constant only affects `ModeHybrid`. In
  `ModeKeyword` / `ModeSemantic` only one list is used, so the constant is inert
  — this MUST be a no-op, not an error (do not reject a configured constant just
  because the current query is single-list).
- **Rerank interaction**: `poolSize` governs how many candidates enter rerank
  (H09). Changing the RRF constant does not change `poolSize`; rerank still
  operates on the full pool then truncates to `k`. The two are independent knobs.
- **Default-change blast radius**: collapsing to single `k=60` changes the
  default ranking for every existing vault. This is intentional and MUST be
  (a) called out in the README/changelog and (b) measured with the H02 eval
  harness before merge (see SC-001). If the measurement shows a retrieval
  regression, the default `k` is re-tuned — the asymmetric two-constant formula
  is **not** reinstated.
- **Cross-transport parity**: REST / gRPC / MCP must expose the same override
  surface so a query over any transport honours the configured/flagged constant
  (spec 003 parity contract, FR-002/003 of that spec).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The single RRF smoothing constant `k` MUST be readable from
  persisted config (`.go-rag/config.json`) under the key `rrf_k`, defaulting to
  **60** when the key is absent (the retrieval book's canonical RRF constant).
- **FR-002**: The `query` command MUST accept a `--rrf-k` flag that overrides the
  RRF constant for a single invocation; when the flag is absent the config (then
  default) value MUST apply.
- **FR-003**: The override MUST flow through `Engine.Query`'s request to the
  fuser, so the same override is honoured whether the call originates from the
  CLI, REST, gRPC, or MCP (cross-transport parity).
- **FR-004**: The fusion formula and the rationale for the chosen constant(s)
  MUST be documented in exactly one canonical place (package doc of
  `internal/index` and/or README), with no contradictory statements elsewhere.
- **FR-005**: A constant ≤ 0 MUST be rejected (config validation or flag parse)
  with a clear error; the system MUST never compute scores from an invalid
  constant.
- **FR-006**: The fusion behaviour MUST be pinned by a deterministic unit test
  that asserts the exact fused score for a chunk at known ranks under a known
  constant — so future formula changes are caught, not silent.
- **FR-007 (RESOLVED — Option A: collapse to standard single-k RRF)**: The
  asymmetric per-list constants (`kVec`/`kFTS`) are **removed**. Reciprocal rank
  fusion collapses to ONE symmetric constant `k` (default 60) using the book's
  standard formula `score(d) = Σ 1/(k + rank)` with rank 1-based (first hit =
  rank 1). This removes the opaque asymmetry the audit flagged and matches the
  retrieval book §6.6. The old per-list form `1/(kVec+rank+1)` / `1/(kFTS+rank+1)`
  and its off-by-one are replaced by the literature-standard single-k form.
  *(Decision: A, recorded 2026-06-22.)*

### Key Entities *(include if feature involves data)*

- **RRF constant `k`**: the single symmetric smoothing parameter fed to
  reciprocal rank fusion (default 60). Lives in config as `rrf_k`; overridable
  per-query via the `--rrf-k` flag and the `QueryRequest` field. Replaces the
  prior asymmetric `kVec`/`kFTS`.
- **Fusion formula**: `score(d) = Σ 1/(k + rank)` (standard single-k RRF, rank
  1-based). Deterministic function of rank + `k`; produces the dimensionless
  score later normalized/truncated by rerank and `collapseByDoc`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On the H02 eval harness (`go-rag eval`, `specs/004-retrieval-eval-
  harness`), the default ranking change introduced by this feature (if any — none
  under the "retain asymmetric defaults" option; a measurable shift under the
  "collapse to k=60" option) is quantified on the committed golden dataset:
  recall@5, recall@10, MRR, NDCG@10 are reported before vs. after. The change
  ships only if it does not regress retrieval quality vs. the current baseline
  (or the regression is documented and accepted).
- **SC-002**: A user can change the effective RRF constant via config or flag and
  observe a ranking change on a fixed query — verifiable end-to-end on the CLI
  with a deterministic golden corpus (no network, no Ollama required for the
  fusion-only assertion).
- **SC-003**: The fusion formula and constant rationale are findable in one
  canonical documentation location; a reviewer can answer "what is the RRF
  constant and why" without reading fusion source code.
- **SC-004**: Cross-transport parity holds — a query with the same override
  produces identical rankings over CLI, REST, gRPC, and MCP (parity is asserted
  by an existing or new cross-transport test).

## Assumptions

- **H02 eval harness is operational** (`specs/004-retrieval-eval-harness`) and
  will be used to measure any default-ranking change (SC-001). This is the
  backlog's stated precondition: "Use H02 as the measurement harness to prove
  each retrieval change helps before merging it."
- **`poolSize` is out of scope** for this spec. It is a separate candidate
  count (H22 territory: reranker pool-size tuning) and stays at its current
  default. Only the RRF smoothing constant(s) are made configurable here.
- **No new storage prefix / no schema migration.** The constant is configuration
  and request-state, not persisted per-document data, so Constitution Principle
  II (content-addressed identity) and the single-Pebble-keyspace discipline are
  untouched.
- **Backward compatibility (config schema only):** existing configs without an
  `rrf_k` key MUST keep loading — the absent key resolves to the default `k=60`.
  The *ranking* default does change (asymmetric `kVec=40`/`kFTS=60` → single
  `k=60`); this is the intended remediation, documented and measured (SC-001),
  not a bug. There is no data migration: the constant is config + request state,
  not persisted per-document data.
- **Constitution Principle V (extension by interface / MCP-first):** the
  configured/flagged constant MUST be reachable from every transport, so the MCP
  tool surface gains the override alongside CLI/REST/gRPC.
