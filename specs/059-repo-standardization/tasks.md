# Tasks: Repository Governance Standardization (muninnd alignment)

**Input**: Design documents from `/specs/059-repo-standardization/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/agent-contract.md, quickstart.md

**Tests**: NOT requested. The spec specifies audit-based validation (read-both-files coverage, before/after content audit, agent smoke test, cross-reference check) — these appear as the final task in each story and in Polish, not as a TDD test phase. No `go test` is added; no binary, storage, or runtime change occurs.

**Organization**: Three independently-shippable P1 user stories (registry doc, CLAUDE.md restructure, reviewer agent). Each is its own phase; cross-references are wired in Polish.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the two missing directories and capture the baseline that the CLAUDE.md restructure must not lose.

- [x] T001 [P] Create `docs/internals/` and `.claude/agents/` directories (neither exists in go-rag today)
- [x] T002 [P] Capture the pre-restructure CLAUDE.md content inventory — the must-survive list from `specs/059-repo-standardization/quickstart.md` Check 2 (make commands, architecture table, multi-transport spec 003, daemon-restart, smoke-test-isolated-DB, kill-by-port, lint-before-push, console UI conventions, out-of-scope). This list is the acceptance baseline for US2.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Lock the single shared source that US1 (registry) and US3 (agent routing) both consume, so they agree.

**⚠️ CRITICAL**: US1 and US3 both depend on this inventory; complete it before either.

- [x] T003 Lock the authoritative prefix inventory from `internal/storage/storage.go` — the `Prefix*` constants (0x01–0x1B), the `VaultScopedKinds` slice (which kinds are vault-scoped vs global), reserved bytes (0x06, 0x16), and the 0xFF schema-version meta key. Record it (in research.md or a scratch note) as the single source both US1 and US3 reference. Read via Gortex `read`, not ad-hoc grep.

**Checkpoint**: Prefix inventory locked — US1, US2, US3 may now proceed (independently or in parallel).

---

## Phase 3: User Story 1 — Pebble keyspace registry (Priority: P1) 🎯 MVP

**Goal**: Ship `docs/internals/keyspace-registry.md` — the one file a reviewer/contributor reads to see every allocated prefix, its scope, and the live hazards, without grepping source.

**Independent Test**: Open the registry; every `Prefix*` constant in `internal/storage/storage.go` appears with the same byte, and the "Free bytes" + hazards sections answer allocation and risk questions without reading code (quickstart Check 1).

### Implementation for User Story 1

- [x] T004 [US1] Scaffold `docs/internals/keyspace-registry.md`: the purpose header, the source-of-truth statement (`internal/storage/storage.go` is authoritative; where code and doc disagree, code wins and the reviewer flags drift), and the **Storage prefixes table** (0x01–0x15) with columns Prefix | Constant | Scope (vault-scoped/global) | Key shape after prefix | Value | Notes. Use the T003 inventory.
- [x] T005 [US1] Add the **Global prefixes** section (0x09 Config, 0x17 AuthAPIKey, 0x18 AuthAdmin, 0x19 AuthSession, 0x1A VaultMeta, 0x1B VaultNameIndex, 0xFF schema-version meta), the **Reserved bytes** note (0x06 FTS gap, 0x16 BL-011 webhook per bridge backlog), and the **Free bytes** range for future prefixes, to `docs/internals/keyspace-registry.md`
- [x] T006 [US1] Add the **Live hazards** section to `docs/internals/keyspace-registry.md`: single-Pebble single-writer; the v2.0 unified-store key shape (`kind | wsPrefix(8) | payload`, ws = SipHash(vault name), spec 052); the `PrefixScanByte`-on-a-vault-scoped-kind danger (scans ALL vaults — see `internal/storage/db.go` PrefixScanByte comment); the deliberate auth-at-0x17+ reservation above the 0x16 webhook; the v3→v4 ws-prepend migration
- [x] T007 [US1] Validate registry coverage per `quickstart.md` Check 1 — every `Prefix*` constant in `internal/storage/storage.go` appears in the registry with a matching byte; reserved 0x06/0x16 and 0xFF are explicitly listed

**Checkpoint**: US1 complete — the registry stands alone as the highest-value defect-prevention artifact.

---

## Phase 4: User Story 2 — CLAUDE.md restructured to "constitution + index" (Priority: P1)

**Goal**: Reshape `CLAUDE.md` into muninnd's six-section form (what go-rag is → architecture map → principles → how we work → reviewer → attribution) **without losing any existing operational content**.

**Independent Test**: A reader given only the restructured CLAUDE.md can state go-rag's one-line promise, the five principles, and the verify-don't-assure protocol; and the before/after audit (quickstart Check 2) confirms nothing operational was dropped.

### Implementation for User Story 2

- [x] T008 [US2] Restructure the `CLAUDE.md` opening: a "What go-rag is" section (single-binary, local-first, pure-Go RAG database; the PRD one-line promise — *as frictionless as `git init; git add; git commit`*; retrieval-only, air-gapped), immediately followed by the **existing** directory→PRD architecture map (preserve the table verbatim — do not rewrite it)
- [x] T009 [US2] Add a **Core principles** section to `CLAUDE.md`: condense the five principles (Local-First/Single-Binary, Content-Addressed Identity, Pure-Go, Async-After-ACK, Extension-by-Interface) from `.specify/memory/constitution.md` into one line each, and **link `constitution.md` as authoritative** — do NOT reproduce the constitution verbatim (avoids a drift surface; see research Decision 4)
- [x] T010 [US2] Add the **"How we work"** section to `CLAUDE.md`: verify-don't-assume (confirm the commit, build+test the actual change, RED-sanity-check bug fixes, verify claims rather than trusting descriptions), **preserving** go-rag's existing standing instructions — daemon-restart-after-code-changes, smoke-test-on-isolated-DB, the kill-by-port cleanup line, lint-before-push + githooks
- [x] T011 [US2] Add the **code-reviewer agent** section (point at `.claude/agents/code-reviewer.md` and the global `/code-review` skill), the **`docs/internals/` index pointer** (name the keyspace registry; leave slots for future invariants/decision-record docs), and the **no-attribution rule** (no "Generated with Claude" in any PR/commit/issue/comment) to `CLAUDE.md`
- [x] T012 [US2] Run the before/after content audit per `quickstart.md` Check 2 — `git diff main -- CLAUDE.md` shows the new shape AND every must-survive item from T002 is still present; principles link (not duplicate) the constitution

**Checkpoint**: US2 complete — CLAUDE.md is the index that surfaces US1 and US3.

---

## Phase 5: User Story 3 — Resident code-reviewer agent (Priority: P1)

**Goal**: Ship `.claude/agents/code-reviewer.md` — a read-only resident reviewer that routes by go-rag's invariant sets, builds/tests the actual change, RED-sanity-checks fixes, and emits a review as text (never posts/edits/merges).

**Independent Test**: Invoke the agent against a sample unstaged diff; it returns a verdict + a blocking finding cited to an invariant and file:line + the build/test output it ran; `git status` is unchanged afterward (quickstart Check 3).

### Implementation for User Story 3

- [x] T013 [US3] Create `.claude/agents/code-reviewer.md` with YAML frontmatter (`name: code-reviewer`, a description matching muninnd's shape but scoped to go-rag, `tools: ["Read", "Grep", "Glob", "Bash"]`) and intro: it protects go-rag's promise and invariants, produces a review **as text only**, never posts/comments/approves/merges/edits the working tree, and follows the "live code wins over stale docs" rule. Source: `CLAUDE.md` + `docs/internals/` + `.specify/memory/constitution.md`
- [x] T014 [US3] Write the **operating rules** in `.claude/agents/code-reviewer.md`: confirm the commit before asserting (`git branch --show-current` / `git log`); build+test the actual change (`go build ./... && go vet ./... && gofmt -l .` + relevant `go test`, `-race` for storage/auth/migration/concurrency); RED-sanity-check every bug/race fix (prove the test fails without the fix); verify claims, never trust the diff description
- [x] T015 [US3] Write the **routing section** in `.claude/agents/code-reviewer.md` — six go-rag invariant sets (NOT muninnd's cognition/replication names): **retrieval/hybrid** (`internal/index/`, engine Query, `internal/eval/` — RRF, rerank, the eval harness); **storage/keyspace/migration** (`internal/storage/`, `keys/`, `migrate/` — cite the keyspace registry, prefix-disjointness, vault-scoped vs global); **auth/tokens** (`internal/auth/`, spec 045 — `gorag_` keys, bcrypt admin, `gorags_` sessions, loopback bypass guard); **transport parity** (`internal/mcp/`, `rest/`, `grpc/`, `ui/`, `proto/` — 5 adapters over one engine, UI byte-parity); **enrichment/embed** (`internal/enrich/`, embedqueue, circuit breaker); **cross-surface drift** (proto regen, `openapi.yaml`, console wiring, release checksums). Apply every set whose paths appear in the diff
- [x] T016 [US3] Write the **output-shape section** + **the never-list** in `.claude/agents/code-reviewer.md`: verdict (`approve` / `approve with required changes` / `needs work` / `defer`) then most-important-first — blocking correctness findings (cite invariant + file:line + failure scenario), cross-surface misses (name the Y), verification evidence pasted (build/vet/test/-race/RED-sanity output), non-blocking cleanups, CI-cost note. Never-list per `contracts/agent-contract.md`: no post/comment/approve/merge; no working-tree edits; no solo-approve on Tier-3 (auth/format/migration/concurrency/crypto/upgrade-integrity) without flagging an adversarial pass; no enforcing stale doc claims
- [x] T017 [US3] Run the agent smoke test per `quickstart.md` Check 3 — invoke against a sample unstaged diff (or a described past change); confirm verdict + ≥1 finding cited to file:line + pasted build/test output; `git status` unchanged (zero writes)

**Checkpoint**: US3 complete — the enforcer that makes the registry and principles bite at review time.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Wire the three artifacts to each other and confirm the cross-cutting rules.

- [x] T018 [P] Cross-reference integrity per `quickstart.md` Check 4 — `CLAUDE.md` names the registry + agent; `.claude/agents/code-reviewer.md` cites the registry + `constitution.md`; `docs/internals/keyspace-registry.md` names `internal/storage/storage.go` as source; **no phantom reference** to a nonexistent `review-rubric.md` (research Decision 5)
- [x] T019 [P] Confirm no "Generated with Claude" / Anthropic attribution in any produced content — the registry, CLAUDE.md additions, and the agent file (FR-011)
- [x] T020 Commit to `main` with Conventional Commits (`docs:` prefix) per the repo's single-author commit-to-main constraint

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — T001 and T002 run in parallel.
- **Foundational (Phase 2)**: T003 (lock prefix inventory) — blocks US1 and US3 (both consume it); US2 does not depend on it.
- **User Stories (Phase 3–5)**: Each depends only on T003 (US1, US3) or nothing (US2). The three stories touch **three different files** and can run fully in parallel.
- **Polish (Phase 6)**: T018/T019 depend on all three stories landing; T020 depends on everything.

### User Story Dependencies

- **US1 (registry)**: depends on T003 only. No dependency on US2/US3.
- **US2 (CLAUDE.md)**: no dependency on T003 or other stories — the restructure can proceed independently (it points at files that may not exist yet; cross-refs verified in T018).
- **US3 (agent)**: depends on T003 only. Cites the registry (US1) by path — works whether US1 has landed or not (the path is stable); cross-ref verified in T018.

### Within Each User Story

- Same-file tasks are sequential (no `[P]` within a story — one writer per file).
- Section-scaffold before section-fill before validation.
- The validation task (T007, T012, T017) closes each story.

### Parallel Opportunities

- **T001 ∥ T002** (Setup) — different concerns.
- **US1 ∥ US2 ∥ US3** — three different files, no cross-dependencies that block starting. If team capacity allows, all three run at once after T003.
- **T018 ∥ T019** (Polish) — different checks.
- Within a single story, tasks are sequential (one file each).

---

## Parallel Example: all three stories after T003

```bash
# Three different files — safe to run concurrently:
Task: "[US1] Scaffold docs/internals/keyspace-registry.md ..."
Task: "[US2] Restructure CLAUDE.md opening ..."
Task: "[US3] Create .claude/agents/code-reviewer.md frontmatter ..."
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Complete Phase 1 (Setup) + T003 (lock inventory).
2. Complete Phase 3 (US1 — the registry).
3. **STOP and VALIDATE**: run quickstart Check 1 — the registry covers every prefix from `storage.go`.
4. The registry now ships standalone defect-prevention value even before CLAUDE.md and the agent land.

### Incremental Delivery

1. Setup + T003 → inventory locked.
2. Add US1 (registry) → validate Check 1 → **MVP** (highest standalone value).
3. Add US2 (CLAUDE.md) → validate Check 2 → the index surfaces the registry.
4. Add US3 (agent) → validate Check 3 → the enforcer consumes both.
5. Polish → Check 4 + attribution + commit. Each story adds value without breaking the others.

### Parallel Strategy (single author, serial recommended)

Although the three stories are file-independent, this is a single-author repo — serial in priority order (US1 → US2 → US3) is the natural rhythm. The `[P]` markers exist for the day a second contributor arrives.

---

## Notes

- **No code, no tests, no migration, no daemon restart.** This feature is docs + agent scaffolding only. The binary and vault are untouched.
- `[P]` = different files, no dependencies. Within a story (one file) tasks are sequential.
- `[Story]` label maps each task to its user story for traceability.
- Validation is audit-based (the four quickstart checks), folded into each story's final task + Polish — not a TDD phase.
- Commit after each story checkpoint; final commit (T020) lands the cross-references.
