# Contract — code-reviewer agent I/O

**Date**: 2026-07-30

This is the behavior contract for `.claude/agents/code-reviewer.md`, the resident reviewer. It is the only external-facing "interface" this feature introduces — the registry and CLAUDE.md are read by humans/agents but expose no callable surface.

## Input

- **Primary**: a diff — unstaged (`git diff`), uncommitted (`git diff HEAD`), or branch-vs-`main` (`git diff main...HEAD`). The repo is single-author commit-to-`main`; the agent must NOT assume a GitHub PR exists.
- **Fallback**: a described change ("review the change to X") — the agent locates the diff itself via `git`.

## Output

A review as **text only**, leading with a verdict, then most-important-first:

| Section | Content |
|---|---|
| **Verdict** | One of: `approve` / `approve with required changes` / `needs work` / `defer` (turns on domain expertise beyond code review — cryptographic correctness, license boundary; say what specifically needs a human) |
| **Correctness / invariant violations** (blocking) | The specific invariant cited (name + file:line), a concrete failure scenario, and what must change. "This is wrong" distinguished from "this is a risk." |
| **Cross-surface obligations missed** | "you changed X but didn't update Y" — name the Y (proto regen, openapi.yaml, console wiring, checksums). |
| **Verification run** | build/vet/test output pasted (meaningful lines, not "passed"), `-race` result where relevant, and the RED-sanity result for any bug fix. |
| **Cleanups / smaller notes** (non-blocking) | Clearly separated from blocking findings. |
| **CI cost** | If the change adds a `-race` / Playwright / live-server test, say whether it's justified or a unit test would prove the same. |

## Non-negotiables (the "never" list)

The agent MUST NOT, under any input:

1. **Post, comment, approve, request changes, or merge** — those are maintainer actions.
2. **Modify the working tree** — no fixes, no edits. If it builds/tests in a scratch worktree, it cleans that worktree up.
3. **Issue a solo `approve` on a Tier-3 change** (auth/on-disk format/migration/concurrency/crypto/upgrade-integrity) without flagging that an independent adversarial pass is needed.
4. **Enforce a stale doc claim** — when a cited invariant's file:line disagrees with live code, live code wins; the agent says so and does not enforce the stale claim.
5. **Trust the PR/diff description** over its own verification ("closes the race" / "all green" / "no behavior change" → confirm independently).

## Tools

Read-only: `Read`, `Grep`, `Glob`, `Bash` (the last only for `git` inspection and scratch build/test in a worktree it creates and removes). No `Edit`, no `Write`, no MCP write tools, no `gh pr_*`.

## Routing (which invariant sets apply)

Apply every set whose paths appear in the diff (full set definitions live in the agent body):

- **retrieval/hybrid** — `internal/index/`, `internal/engine` Query path, `internal/eval/`
- **storage/keyspace/migration** — `internal/storage/`, `internal/storage/keys/`, `internal/storage/migrate/`
- **auth/tokens** — `internal/auth/`, the loopback bypass, token import
- **transport parity** — `internal/mcp/`, `internal/rest/`, `internal/grpc/`, `internal/ui/`, `proto/`
- **enrichment/embed** — `internal/enrich/`, embedqueue, circuit breaker
- **cross-surface drift** — `proto/`, `openapi.yaml`, console UI, checksums/release

A diff touching multiple sets gets all of them applied.
