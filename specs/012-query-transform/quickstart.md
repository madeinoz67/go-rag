# Quickstart: Validate the Query Transformation Seam (H05)

> Runnable validation proving the seam + normalization work. These are *validation*
> steps — implementation/test bodies live in `tasks.md`. Scenarios 1–4 use a
> deterministic embedder (no real Ollama); scenario 5 is the eval gate.

## Prerequisites

```bash
make build                 # ./bin/go-rag
make test                  # go test -race -cover ./... must be green
```

New validation lives in `internal/index/transform_test.go` (unit) and
`internal/engine` (in-package seam test), using deterministic/fake embedders — no
external service required for scenarios 1–4.

---

## Scenario 1 — Cosmetic variants retrieve identically (US1, SC-001)

A `normalizeQuery` unit test (and an engine-level check): the clean form and a
cosmetically-noisy form of the same query map to the same normalized string and
return the same ranked results.

- `normalizeQuery("Some Term") == normalizeQuery("  some   term ")`.
- Engine: query `"Some Term"` vs `"  some   term "` → identical hits/order on a fixed corpus.

**Expected**: identical normalized string + identical ranking (cosmetic equivalence).

## Scenario 2 — Idempotent + Unicode-safe (FR-007/FR-008)

- `normalizeQuery(normalizeQuery(q)) == normalizeQuery(q)` for several inputs.
- Non-ASCII preserved: `normalizeQuery("Café naïve")` lowercases accents correctly
  and does not drop CJK: `normalizeQuery("数据 检索")` keeps the characters (whitespace
  collapsed only).

**Expected**: idempotent; Unicode intact.

## Scenario 3 — Empty-after-normalize is handled, not embedded (FR-006)

- `normalizeQuery("   ")` (whitespace-only) yields empty → `Transform` returns an
  error → `Engine.Query` returns an error (the existing empty-query outcome), never
  a garbage embed.

**Expected**: error, no embed call, no crash.

## Scenario 4 — Custom transformer is honored (US2, SC-003)

An in-package `internal/engine` test sets `e.qTransformer` to a fake that appends a
synonym to every query (e.g. `"auth"` → `"auth credential"`). With a corpus that
contains `"credential"` but is a weak match for `"auth"` alone, the results change
when the fake transformer is wired (the synonym is searched).

**Expected**: the custom transformer's alteration is reflected in the results — the
seam is live.

## Scenario 5 — No regression on the H02 eval harness (US3, SC-002)

```bash
make test-eval
```

**Expected**: PASS — recall@10/MRR unchanged vs baseline (the harness queries are
already clean, so normalization is a no-op for them; the gate catches accidental
breakage).

## Scenario 6 — Cross-transport parity holds (US3 acceptance 2)

The existing cross-transport parity tests (spec 003, `internal/engine/parity_test.go`)
pass unchanged — the transform runs in the shared engine path, so CLI/REST/gRPC/MCP
stay identical.

**Expected**: parity tests green.

## Scenario 7 — End-to-end (optional, real model)

With a local Ollama + nomic-embed-text, ingest a small corpus and query a term in
different casings/with stray whitespace; confirm consistent results.

```bash
TMPDB="$(mktemp -d)/v"
./bin/go-rag init --db-path "$TMPDB" --model nomic-embed-text
./bin/go-rag add --db-path "$TMPDB" <dir>
./bin/go-rag query --db-path "$TMPDB" "Retrieval"
./bin/go-rag query --db-path "$TMPDB" "  retrieval  "
```

**Expected**: both return the same relevant hits.
