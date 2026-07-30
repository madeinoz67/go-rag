# Feature Specification: Repository Governance Standardization (muninnd alignment)

**Feature Branch**: `main` (single-author repo — spec directory `059-repo-standardization`)

**Created**: 2026-07-30

**Status**: Draft

**Input**: User description: *"Review CLAUDE.md and standardise for the project along the lines of the muninn project, also look at adding a code reviewer agent just like muninnd repo. Also in the muninn repo I added a pebble registry to the internal docs, add likewise."*

## Context

This is a **developer-facing governance standardization**, not an end-user feature. The "users" are human contributors and AI agents working in the go-rag repo. The muninnd repo has matured a scaffolding pattern — a CLAUDE.md that is an index to deep internal docs, a resident code-reviewer agent, and a Pebble keyspace registry — that prevents whole classes of defect (prefix collisions, cross-surface drift, description-trusting reviews). go-rag has grown to comparable complexity (52 storage prefixes, 5 transports, auth, enrichment, upgrade/migration) but has not yet adopted that scaffolding. This spec brings the three artifacts over, adapted to go-rag's domain.

The three deliverables are **independently shippable** but mutually reinforcing: the registry is the substance, the CLAUDE.md is the index that surfaces it, and the reviewer agent is the enforcer that cites both.

**Authoritative sources read this session**: muninnd `CLAUDE.md`, `.claude/agents/code-reviewer.md`, `docs/internals/keyspace-registry.md`; go-rag `CLAUDE.md`, `.specify/memory/constitution.md`, `internal/storage/storage.go`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Pebble keyspace registry doc (Priority: P1)

A contributor (human or AI) about to add a new Pebble key type — or a reviewer checking a PR that touches storage — needs **one place** that lists every allocated prefix, its key shape, whether it is vault-scoped or global, and the live hazards, without grepping `internal/storage/storage.go` and reconstructing the layout mentally. muninnd calls this "the single most important artifact for preventing a whole class of bug"; go-rag has the prefixes in code comments but no consolidated registry.

**Why this priority**: It is the highest standalone-value artifact and the substance the other two deliverables index and enforce. go-rag already had a near-collision-shaped reservation (auth deliberately placed at 0x17+ to dodge the 0x16 webhook reservation) — exactly the class of risk a registry makes visible.

**Independent Test**: Open `docs/internals/keyspace-registry.md`; every `Prefix*` constant in `internal/storage/storage.go` appears in the registry with the same byte value, and the registry names the source-of-truth file plus the test that guards disjointness (or notes its absence).

**Acceptance Scenarios**:

1. **Given** the registry is published, **When** a contributor needs to allocate a new prefix, **Then** the "Free bytes" section tells them which bytes are unclaimed and the rule says "add a row here and keep it disjoint."
2. **Given** a reviewer reading a storage-touching PR, **When** they check the registry, **Then** they can see at a glance which kinds are vault-scoped (`kind | wsPrefix(8) | payload`, 0x01–0x15) vs global (0x09, 0x17–0x1B, 0xFF) and the danger of `PrefixScanByte` on a vault-scoped kind.
3. **Given** the registry's hazard list, **When** someone proposes reusing a vault name, **Then** the doc reflects go-rag's actual reuse semantics (SipHash-prefix deterministic) rather than a guessed invariant.

---

### User Story 2 — CLAUDE.md restructured to "constitution + index" (Priority: P1)

A new contributor (or a fresh AI agent session) opening the repo needs CLAUDE.md to answer, in order: **what go-rag is** (one-line promise + the subsystems behind it), the **core principles** that are the lens for every change, **how we work** (verify-don't-assume, RED-sanity, the daemon-restart and smoke-test-on-isolated-DB standing instructions), and **where the deep references live** (`docs/internals/`, the PRD, the constitution). The current CLAUDE.md is a strong operational file (commands, architecture map, constraints, console conventions) but is not shaped that way: it buries the "what is this" framing, omits the principles (they live only in `constitution.md`), and has no verify-don't-assume protocol or pointer to internal docs (because none exist yet).

**Why this priority**: CLAUDE.md auto-loads for every agent session and every contributor; it is the highest-leverage surface. It is also the index that makes US1 and US3 discoverable.

**Independent Test**: A reader who never opens another file can state go-rag's one-line promise, name the five core principles, and list the three working-protocol invariants — and the file points them to `docs/internals/` and the constitution for depth.

**Acceptance Scenarios**:

1. **Given** the restructured CLAUDE.md, **When** an agent starts a session, **Then** the first sections frame what go-rag is, its promise, and the architecture map, before any commands.
2. **Given** the principles section, **When** a change is proposed, **Then** the five principles (Local-First, Content-Addressed Identity, Pure-Go, Async-After-ACK, Extension-by-Interface) are visible in CLAUDE.md as the lens, with `constitution.md` named as the authoritative source.
3. **Given** the "how we work" section, **When** a reviewer needs the working protocol, **Then** verify-don't-assume, RED-sanity, the isolated-DB smoke-test rule, kill-by-port cleanup, and lint-before-push are all present, alongside the existing daemon-restart standing instruction.
4. **Given** existing go-rag-specific content (make commands, multi-transport spec 003, console UI sort/no-cache conventions, out-of-scope), **When** the restructure lands, **Then** none of it is lost — it is reorganized under the new shape, not deleted.

---

### User Story 3 — Resident code-reviewer agent (Priority: P1)

A maintainer about to open a PR (or an agent finishing a change) wants a **resident reviewer** that reviews the diff for correctness and for adherence to go-rag's invariants — retrieval (BM25+vector+rerank RRF), storage/keyspace, auth/tokens (spec 045), transport parity (MCP/REST/gRPC/UI), enrichment/embed pipeline, and upgrade/migration — builds and tests the actual change, RED-sanity-checks bug fixes, and produces a review as text without ever posting, approving, or merging. go-rag currently has no `.claude/agents/` directory at all.

**Why this priority**: It is the enforcer that makes the registry and the CLAUDE.md principles bite at review time. muninnd's version is the model; it needs adapting to go-rag's invariant sets.

**Independent Test**: Invoke the `code-reviewer` agent against an unstaged diff; it returns a verdict (approve / approve-with-changes / needs-work / defer) with blocking findings cited to specific invariants and file:lines, plus the build/test evidence it ran, and it does not modify the tree or post anything.

**Acceptance Scenarios**:

1. **Given** the agent is installed at `.claude/agents/code-reviewer.md`, **When** it is invoked on a storage-touching diff, **Then** it routes to the storage/keyspace invariant set, cites the keyspace registry, and checks for prefix-disjointness and vault-scoped vs global mistakes.
2. **Given** a bug-fix diff, **When** the agent reviews it, **Then** it RED-sanity-checks the new test (shows it fails without the fix) rather than trusting the description.
3. **Given** any diff, **When** the agent finishes, **Then** it produces a review as text only — it never calls a write/post/merge tool — and it cleans up any scratch worktree it built.
4. **Given** a diff that touches multiple surfaces (e.g. a new engine method exposed across transports), **When** the agent routes, **Then** it applies every matching invariant set and flags cross-surface drift (proto regen, openapi.yaml, console UI wiring).

---

### Edge Cases

- What happens when the registry and `storage.go` disagree (a prefix added in code but not the doc, or vice versa)? The doc must declare **code is the source of truth** and the reviewer must flag drift rather than enforce a stale claim (mirroring muninnd's "live code wins" rule).
- What happens when a contributor uses an existing tool (the global `/code-review` skill) instead of the resident agent? The CLAUDE.md section should point to both; the resident agent is preferred for go-rag-specific invariant routing.
- What about the reserved 0x06 and 0x16 bytes and the 0xFF meta key? The registry must list them as reserved/allocated so nobody reuses them — 0x16 is reserved for the BL-011 webhook, 0xFF is the schema-version meta key (spec 034).
- Single-author repo, commits straight to `main` — the reviewer agent must still work against an unstaged/uncommitted diff, not assume a PR exists.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The project MUST ship `docs/internals/keyspace-registry.md` listing every Pebble prefix constant currently defined in `internal/storage/storage.go` (0x01–0x1B), plus the 0xFF schema-version meta key, with for each: the byte, the constant name, the key shape after the prefix, vault-scoped vs global, and a one-line note.
- **FR-002**: The registry MUST mark the reserved bytes (0x06, 0x16) and the allocated non-storage bytes (0x17–0x1B auth/vault-registry, 0xFF schema version), and MUST name a "Free bytes" range for future prefixes.
- **FR-003**: The registry MUST state that `internal/storage/storage.go` is the source of truth and that the doc is a reviewer's reference; where code and doc disagree, code wins and the reviewer flags the drift.
- **FR-004**: The registry MUST include a go-rag-specific "Live hazards" section covering at minimum: single-Pebble single-writer; the v2.0 unified-store key shape (`kind | wsPrefix(8) | payload`, ws = SipHash(vault name), spec 052); the `PrefixScanByte`-on-a-vault-scoped-kind danger (scans all vaults); the deliberate auth-at-0x17+ reservation above the 0x16 webhook; and the v3→v4 ws-prepend migration.
- **FR-005**: CLAUDE.md MUST be restructured to lead with (a) what go-rag is + its one-line promise, (b) an architecture map, (c) the five core principles as the lens for every change (referencing `constitution.md` as authoritative), (d) a "how we work" protocol, (e) the code-reviewer agent section, (f) attribution rule — while preserving all existing operational content (commands, multi-transport, console UI conventions, constraints, out-of-scope).
- **FR-006**: CLAUDE.md MUST surface the working protocol: verify-don't-assume (confirm the commit, build+test the actual change, RED-sanity-check bug fixes, verify claims rather than trusting descriptions) in addition to go-rag's existing standing instructions (daemon-restart, smoke-test-on-isolated-DB, kill-by-port cleanup, lint-before-push).
- **FR-007**: CLAUDE.md MUST index `docs/internals/` (the keyspace registry and any future internal docs) and the existing `docs/` references, so the file is an index to depth, not the depth itself.
- **FR-008**: The project MUST ship `.claude/agents/code-reviewer.md` — a resident reviewer agent with read-only tools (`Read`, `Grep`, `Glob`, `Bash`) that produces a review as text and never posts, approves, requests changes, merges, or modifies the working tree.
- **FR-009**: The reviewer agent MUST route by what the diff touches, with invariant sets adapted to go-rag: retrieval/hybrid (BM25+vector+rerank RRF, eval), storage/keyspace/migration, auth/tokens (spec 045), transport parity (MCP/REST/gRPC/UI), enrichment/embed pipeline, upgrade/self-replace, and cross-surface drift (proto regen, openapi.yaml, console wiring).
- **FR-010**: The reviewer agent MUST cite `docs/internals/` (the keyspace registry) and `constitution.md` as its source of truth, and MUST flag when a cited invariant's file:line disagrees with live code (live code wins).
- **FR-011**: No "Generated with Claude" / Anthropic attribution MUST appear in any PR body, commit message, issue, or code comment produced by these artifacts (mirroring muninnd's rule).

### Key Entities *(include if feature involves data)*

- **Key-space prefix**: a single byte allocating a slice of the shared Pebble keyspace to one logical data type; the registry's primary entity. Attributes: byte value, owning package, key shape, vault-scoped vs global, allocation source (spec/PRD/audit reference).
- **Vault-scoped kind** vs **Global kind**: the two key-shape families under the v2.0 unified store (spec 052); the registry distinguishes them per row.
- **Reviewer invariant set**: a named bundle of review checks the agent applies when a diff touches its files (e.g. STO-/SEC-/drift- equivalents for go-rag).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A reviewer or contributor can determine the next free Pebble prefix and the hazards of an existing one by reading exactly one file (`docs/internals/keyspace-registry.md`), with zero grep over source.
- **SC-002**: Every `Prefix*` constant in `internal/storage/storage.go` is represented in the registry with a matching byte value (100% coverage, verified by a read of both).
- **SC-003**: A fresh agent session, given only the restructured CLAUDE.md, can correctly state go-rag's one-line promise, the five core principles, and the verify-don't-assume protocol — measured by a reader-quiz during plan/review, not by assertion.
- **SC-004**: The `code-reviewer` agent, run against a sample storage-touching diff, returns a verdict with at least one invariant cited to a file:line and the build/test evidence it ran, and makes zero writes to the repo.
- **SC-005**: No existing operational content in CLAUDE.md is lost in the restructure (commands, multi-transport note, console UI conventions, constraints, out-of-scope) — verified by a before/after content audit.
- **SC-006**: The three artifacts are mutually cross-referenced: CLAUDE.md names the registry and the agent; the agent's prompt cites the registry and constitution; the registry names the source-of-truth code file.

## Assumptions

- go-rag is and remains a **single-author, commit-to-`main`** repo (per current `CLAUDE.md` constraint); the reviewer agent is invoked locally against unstaged/uncommitted diffs, not GitHub PRs. The muninnd agent's PR-language is adapted to "the change"/"the diff."
- The **constitution (`constitution.md`) remains the authoritative source** for the five principles; the restructured CLAUDE.md surfaces a condensed version and points to it, rather than duplicating or superseding it. On conflict, constitution wins (matching the existing governance rule).
- The registry's **source of truth is `internal/storage/storage.go`** (the `Prefix*` constants and the `VaultScopedKinds` slice). This spec does NOT add a programmatic disjointness test (muninnd has `TestAll_NoDuplicateBytes`; go-rag currently has none) — that is a *follow-up*, flagged in the spec, not a deliverable, because go-rag's prefixes are single-author and visually disjoint today.
- The reviewer agent is **advisory and read-only**, mirroring muninnd: it never posts/approves/merges/edits. It is a productivity tool, not a CI gate.
- Future `docs/internals/` additions (invariants.md, decision-record.md, review-rubric.md equivalents) are **out of scope** for this spec — only the keyspace registry ships now; CLAUDE.md is written so those slots can be added without restructuring again.
- The "no attribution" rule applies to content produced by these artifacts; it does not require rewriting existing historical commits.
