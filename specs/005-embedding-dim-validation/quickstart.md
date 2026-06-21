# Phase 1 — Quickstart / Validation Guide: Embedding Mismatch Guard

> How to prove the guard works. This is a *run-and-observe* guide, not an
> implementation walkthrough — code lives in `tasks.md` (Phase 2). Contracts in
> [contracts/mismatch-guard.md](./contracts/mismatch-guard.md); entities in
> [data-model.md](./data-model.md); decisions in [research.md](./research.md).

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0`; `make build` → `./bin/go-rag`.
- A local Ollama with **two** embedding models of **different dimensionality**
  (e.g. `nomic-embed-text` 768-dim and `bge-m3` 1024-dim) for the live mismatch
  scenario. The unit tests need no Ollama (they inject deterministic embedders).

## Scenario A — Refuse a dimensionality mismatch (US1, the core P0)

1. Init + ingest under model A:
   ```bash
   ./bin/go-rag --db-path /tmp/h03 init --model nomic-embed-text
   ./bin/go-rag --db-path /tmp/h03 add <some-docs-dir>
   # wait for embeddings (check status)
   ```
2. Switch the configured model to B (different dim) **without re-indexing**, then
   query:
   ```bash
   ./bin/go-rag --db-path /tmp/h03 config set embedding_model bge-m3
   ./bin/go-rag --db-path /tmp/h03 query "some question"
   ```
**Expected outcome:** the query **fails** with a clear mismatch error naming the
stored model/dim vs the query model/dim — **no ranked results, no panic, no
plausible-but-wrong output.** Exit non-zero. (FR-001/FR-003/SC-001.)

## Scenario B — Refuse a same-dim different-model mismatch (US1)

Using two models with the **same** dimensionality but different names, repeat
Scenario A. **Expected:** still refused — same dimensionality does not make a
different model safe. (Acceptance US1 #2.)

## Scenario C — Happy path unaffected (US1 acceptance #3)

With model configured to match the stored corpus, query normally. **Expected:**
identical results and latency to before the guard (no false alarms; O(1) check).
(SC-004.)

## Scenario D — See drift in status before querying (US2)

After a partial migration (some vectors under A, some under B):
```bash
./bin/go-rag --db-path /tmp/h03 status
```
**Expected:** status reports the **stored** majority model + dim and, because the
corpus is mixed, a **drift flag** with per-model counts (e.g. "drift: nomic-embed-
text=120, bge-m3=3"). The operator sees the inconsistency without querying.
(FR-004/SC-002.)

## Scenario E — Graceful degradation mid-migration (US3)

Build a corpus where the majority is under model A and a minority under model B;
query under model A. **Expected:** results are drawn **only** from model-A vectors
(correct), model-B vectors are **skipped** with a logged count, and the query
**does not fail**. Querying under the minority model B is **refused** (it does not
match the majority). (FR-005/SC-003.)

## Scenario F — Cross-transport parity (FR-007/SC-006)

Issue the mismatched query from Scenario A over REST, gRPC, and MCP
(`go_rag_query`). **Expected:** all three refuse with the **same** mismatch
message text (only the wire shape differs). (Parity test.)

## Verification via the evaluation harness (SC-005, optional extension)

The spec-004 harness can be extended with a mismatch case: ingest the golden
corpus under one embedder, then evaluate under a different-dimensionality
deterministic embedder. **Expected:** the eval run reports the refusal (no
garbage metrics) — proving the guard is visible to a real retrieval-quality
workflow. (Extension, not part of the core deliverable.)

## Unit-test coverage (no Ollama needed)

- `internal/index`: `Vector.Query` skips+counts mismatched-length stored vectors;
  `cosine` never called on mismatched lengths.
- `internal/engine`: refuse on model mismatch; refuse on dim mismatch; `partial`
  verdict skips minority + counts; status drift flag set on a mixed corpus;
  empty-corpus query returns no results without error.

## Definition of done for the guard (maps to spec)

- Scenarios A–F behave as described (A–C need Ollama; D–F can be unit-tested).
- `make build && make vet && make test` green; new tests cover each verdict.
- No new third-party deps; no storage schema change; happy-path latency preserved.
