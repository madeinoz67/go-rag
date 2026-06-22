# Quickstart: Validate Configurable RRF (H08)

> Runnable validation scenarios proving the feature works end-to-end. These are
> *validation* steps, not the implementation or test suite (those live in
> `tasks.md`). Fusion-only scenarios use the offline/deterministic embedder so no
> Ollama is required; real-corpus scenarios do.

## Prerequisites

```bash
make build                 # produces ./bin/go-rag
make test                  # go test -race -cover ./... must be green
```

Optional, for real-embedding scenarios: a local Ollama with an embed model
(e.g. `nomic-embed-text`) running on `http://localhost:11434`.

Use an **isolated DB** for every smoke (the default `dbPath` is the global vault
— per project CLAUDE.md, always pass `--db-path <tmp>`):

```bash
export TMPDB="$(mktemp -d)/test-vault"
./bin/go-rag init --db-path "$TMPDB" --model nomic-embed-text   # or offline embedder
# ingest a few docs so retrieval has a corpus
./bin/go-rag add --db-path "$TMPDB" <some-dir>
```

---

## Scenario 1 — Config-driven tuning (FR-001)

1. Set a non-default constant in config:
   ```bash
   ./bin/go-rag config set rrf_k 120 --db-path "$TMPDB"
   ./bin/go-rag config get rrf_k --db-path "$TMPDB"     # → 120
   ```
2. Run the same query twice — once at default, once at 120 — and confirm the
   ranking order shifts (larger `k` flattens the rank dominance).
   **Expected**: different hit ordering between `rrf_k=60` and `rrf_k=120` on a
   fixed corpus; both return valid, non-NaN scores.

## Scenario 2 — CLI flag override (FR-002)

1. `./bin/go-rag query "<q>" --db-path "$TMPDB" --rrf-k 30`
2. `./bin/go-rag query "<q>" --db-path "$TMPDB" --rrf-k 200`
3. `./bin/go-rag query "<q>" --db-path "$TMPDB"` (omit → config/default)
   **Expected**: distinct orderings for 30 vs 200; the omitted run matches the
   configured/default effective value.

## Scenario 3 — Invalid constant rejected (FR-005)

1. `./bin/go-rag query "<q>" --rrf-k 0`  → exits non-zero, error names the flag.
2. `./bin/go-rag query "<q>" --rrf-k -5` → exits non-zero.
3. `./bin/go-rag config set rrf_k -1 --db-path "$TMPDB"` → rejected by `Validate`.
   **Expected**: every explicit `≤ 0`/negative is rejected with a clear message;
   no query ever runs with an invalid constant.

## Scenario 4 — Non-hybrid is a no-op (Edge Cases)

1. `./bin/go-rag query "<q>" --db-path "$TMPDB" --mode keyword --rrf-k 999`
2. `./bin/go-rag query "<q>" --db-path "$TMPDB" --mode semantic --rrf-k 999`
   **Expected**: both succeed (no error); `rrf_k` has no effect on single-list
   modes. Ranking matches the same query without `--rrf-k`.

## Scenario 5 — Cross-transport parity (FR-003 / spec 003)

With the daemon up on isolated ports:
```bash
./bin/go-rag start --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 \
  --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880
```
Issue the same query with the same `rrf_k` over each transport (CLI, REST
`POST /v1/query`, gRPC `Query`, MCP `go_rag_query`) and compare the ranked
`chunk_id` order.
**Expected**: identical rankings across all four transports for a given
`rrf_k` (parity). Extend the existing cross-transport parity test to assert this.

## Scenario 6 — Retrieval-quality gate (SC-001)

1. **Before merge**, quantify the default change on the H02 harness:
   ```bash
   ./bin/go-rag eval --embedder offline --baseline testdata/golden/baseline.json
   ```
   The collapse to `k=60` is expected to breach the old tolerance — re-capture
   the baseline with the new default and confirm the gate is then green:
   ```bash
   ./bin/go-rag eval --embedder offline --baseline testdata/golden/baseline.json --record-baseline
   make test-eval
   ```
2. *(Optional, real embeddings)* Confirm direction on BEIR:
   ```bash
   ./bin/go-rag eval --benchmark scifact
   ```
   Report recall@5/10, MRR, NDCG@10 before vs after. The change ships only if
   retrieval quality is not regressed (or the regression is documented and
   accepted). See spec SC-001.

## Scenario 7 — Deterministic formula pin (FR-006)

A unit test asserts the exact fused score for a chunk at known ranks under a
known `k` (e.g. a chunk ranked #1 in both lists under `k=60` scores
`1/(60+1) + 1/(60+1) = 2/61`). This pins the documented formula and catches any
silent future drift. **Expected**: the golden-score unit test passes.
