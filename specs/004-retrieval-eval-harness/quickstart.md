# Phase 1 — Quickstart / Validation Guide: Evaluation Harness

> How to prove the harness works end-to-end. This is a *run-and-observe* guide,
> not an implementation walkthrough — code lives in `tasks.md` (Phase 2).
> Contracts in [contracts/eval.md](./contracts/eval.md); entities in
> [data-model.md](./data-model.md); decisions in [research.md](./research.md).

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0` (pure-Go build, Principle III).
- A built binary: `make build` → `./bin/go-rag`.
- (Baseline-only) a local Ollama with an embedding model, for the *real* run. The
  default offline gate needs **no** Ollama.

## Scenario A — Run the offline eval (the default, no network)

This is the CI/`make test-eval` path: deterministic embedder, committed golden
set, hermetic throwaway vault.

```bash
make build
./bin/go-rag eval --embedder offline --format text
```

**Expected outcome**: prints `mode=offline`, a `queries: scored=N skipped=M`
line, the five metrics (recall@5, recall@10, precision@5, mrr, ndcg@10), and a
verdict line. **Run it twice** — the numbers MUST be byte-identical (SC-004,
reproducibility). Exit code `0`.

**Independently validates**: US1 (measure retrieval quality against a golden set)
+ FR-001/FR-002/FR-004 + SC-004.

## Scenario B — The regression gate passes/fails correctly

Using the committed baseline:

```bash
# Pass: unchanged retrieval → gate passes
./bin/go-rag eval --baseline testdata/golden/baseline.json --tolerance 2.0
echo "exit: $?"   # 0

# Fail: simulate a regression (e.g. run with a deliberately worse retrieval mode
#       or a degraded k), confirm the gate fires on recall@10 drop)
```

**Expected outcome**: an unrelated change (e.g. editing a doc comment) leaves the
gate passing; a change that lowers recall@10 beyond the tolerance fails with a
clear message naming the metric and delta, exit `1`. (US2 acceptance #1 & #2.)

**Independently validates**: US2 (regression gate) + FR-005/FR-009 + SC-002.

## Scenario C — Record/refresh the baseline

When a change legitimately improves retrieval, update the committed baseline in
the same PR:

```bash
./bin/go-rag eval --embedder offline --record-baseline
git diff testdata/golden/baseline.json   # review the delta deliberately
```

**Expected outcome**: `testdata/golden/baseline.json` is rewritten with the new
MetricSet; the diff is human-reviewable. Re-running Scenario B now passes against
the new baseline.

## Scenario D — Real-model baseline (one-time, informational)

Establish the headline recall/MRR/NDCG with a real embedding model (requires
local Ollama; not reproducible across machines):

```bash
./bin/go-rag eval --embedder ollama --format text
```

**Expected outcome**: real-model numbers for the golden corpus — the SC-003
published baseline. Compare against the book's targets (recall@10 > 0.80, MRR >
0.60, NDCG@10 > 0.75) as informational lines, not as the gate.

## Scenario E — Read-only guarantee (FR-006)

Confirm eval never mutates the measured vault:

```bash
# count keys before
./bin/go-rag eval --db-path <a-vault> --embedder offline
# count keys after — MUST be identical
```

**Expected outcome**: vault key-counts (documents/chunks/embeddings) identical
before and after. (Independently unit-tested by snapshotting key-counts in
`internal/eval`.)

## Scenario F — MCP tool parity (Principle V)

```bash
# tools/list now includes go_rag_eval (13 tools)
# tools/call go_rag_eval → returns the same metric numbers as the CLI text output
```

**Expected outcome**: `go_rag_eval` appears in `tools/list`, and calling it
yields the same numbers as `./bin/go-rag eval --format text` for the same inputs.
(US1 cross-transport; the MCP test count moves 12 → 13.)

## CI integration

Add `make test-eval` to `.github/workflows/ci.yml`, scoped to PRs touching the
retrieval-quality surface (`internal/chunk`, `internal/index`, `internal/rerank`,
hybrid/RRF weights). It runs Scenario A + Scenario B's gate, offline, on a clean
runner — no Ollama, no secrets, no network.

## Definition of done for the harness (maps to spec)

- A–F all pass as described, on a clean machine, offline by default.
- `make build`, `make vet`, `make test` stay green; new `internal/eval` tests cover
  metric correctness (hand-computed) + read-only + zero-network guarantees.
- `testdata/golden/v1.jsonl` and `baseline.json` are committed.
