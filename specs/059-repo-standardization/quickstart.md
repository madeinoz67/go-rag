# Quickstart — Validation runbook

**Date**: 2026-07-30

This feature ships docs + agent scaffolding, so validation is audit-based, not `go test`. Run these four checks after implementation; all must pass. No build step — nothing compiled changed.

## Prerequisites

- The three artifacts exist: `docs/internals/keyspace-registry.md`, `.claude/agents/code-reviewer.md`, restructured `CLAUDE.md`.
- `internal/storage/storage.go` is unchanged by this feature (it is the source the registry is read from).

## Check 1 — Registry coverage (FR-001, SC-001, SC-002)

**Goal**: every `Prefix*` constant in code appears in the registry with the same byte.

```bash
# 1. List the constants + bytes from code:
grep -E 'Prefix[A-Za-z]+\s+byte' internal/storage/storage.go
# 2. Confirm each byte (0x01–0x1B) and 0xFF appears in the registry:
grep -oE '0x[0-9A-F]{2}' docs/internals/keyspace-registry.md | sort -u
```

**Pass**: every constant's byte from step 1 is present in step 2's set; reserved bytes 0x06/0x16 and the 0xFF meta key are explicitly listed as reserved/allocated. A reviewer can find the next free prefix from the registry alone.

## Check 2 — CLAUDE.md before/after content audit (FR-005, FR-006, SC-005)

**Goal**: the restructure lost nothing operational and gained the new shape.

Compare against the pre-restructure CLAUDE.md (git):

```bash
git diff main -- CLAUDE.md
```

**Pass**: the diff shows the new six-section shape (promise → architecture map → principles → how-we-work → reviewer → attribution), AND every one of these survives (search the new file):

- `make build` / `make test` / `make vet` / `make lint` / `make tidy`
- the directory → PRD architecture table
- the multi-transport (spec 003) paragraph
- "Restart the daemon after code changes" standing instruction
- "Smoke-test the daemon on an isolated DB" + the kill-by-port line
- "Lint before push" + the githooks note
- the Console UI conventions (sortable tables, no-cache static assets)
- Out of scope for v1 (with the spec 046 web-UI amendment)

And the principles section **links** `constitution.md` rather than reproducing it verbatim.

## Check 3 — Reviewer agent smoke test (FR-008, FR-009, SC-004)

**Goal**: the agent returns a verdict with cited evidence and makes zero writes.

Invoke the resident agent against a small unstaged diff (or a no-op diff describing a real past change), then verify:

1. It returns a verdict line (`approve` / `approve with required changes` / `needs work` / `defer`).
2. Any blocking finding cites an invariant + a `file:line`.
3. It pastes the build/vet/test output it ran.
4. `git status` after the run is unchanged — **zero writes** to the working tree.

**Pass**: all four. If it edited the tree or attempted a post/merge, that is a contract failure (see `contracts/agent-contract.md`).

## Check 4 — Cross-reference integrity (FR-007, FR-010, SC-006)

**Goal**: the three artifacts point at each other and at the source of truth.

```bash
# CLAUDE.md names the registry + the agent:
grep -E 'docs/internals/keyspace-registry|\.claude/agents/code-reviewer' CLAUDE.md
# The agent cites the registry + constitution:
grep -E 'keyspace-registry|constitution' .claude/agents/code-reviewer.md
# The registry names storage.go as source of truth:
grep -E 'internal/storage/storage.go' docs/internals/keyspace-registry.md
```

**Pass**: all three grep sets return matches; no broken/phantom references (e.g., the agent must not point at a nonexistent `review-rubric.md` — see research Decision 5).

## Expected outcome

All four checks green ⇒ the feature meets its success criteria. No `go test`, no migration, no daemon restart required — the binary and vault are untouched.
