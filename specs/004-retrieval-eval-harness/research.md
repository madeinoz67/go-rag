# Phase 0 — Research & Decisions: Retrieval-Quality Evaluation Harness

> Resolves every NEEDS CLARIFICATION / open technical question before Phase 1
> design. Each entry: **Decision · Rationale · Alternatives considered.**
> Grounded in code read this session (`internal/engine`, `internal/index`,
> `internal/embed`, `internal/mcp`, `internal/cli`) and `RAG_BOOK_AUDIT.md` §1.6.

---

## D1 — How does eval drive the *real* retrieval path offline? (load-bearing)

**Decision:** Add an **additive, optional embedder injection** to `engine.Engine`
and drive eval through the canonical `engine.Query`.

- `Engine` gains an optional `embedder embed.Embedder` field.
- `NewWithEmbedder(cfg, db, em)` sets it; `NewWithDB` is unchanged.
- `engine.Query` (and the lazy ingest pipeline) use `e.embedderOrOllama()` which
  returns the injected embedder if set, else `embed.NewOllama(cfg…)` — **identical
  behavior for every existing caller** (CLI/REST/gRPC/MCP/daemon).
- The eval harness injects a deterministic pure-Go embedder (D2) so the same
  `engine.Query` code path runs with no Ollama and reproducible vectors.

**Rationale:** `engine.Query` currently hardcodes
`em := embed.NewOllama(e.cfg.OllamaURL, e.cfg.EmbeddingModel)` (engine/query.go:31).
Spec FR-007 requires eval to exercise the *same shared retrieval engine* as
REST/gRPC/MCP (cross-transport parity, spec 003), and FR-004/SC-004 require
offline reproducibility. The only way to satisfy both without forking the query
path is to make the embedder injectable — and Principle V ("extension by
interface") already backs this: `embed.Embedder` is the seam. The change is
purely additive (new optional field + fallback), so no working feature is
modified.

**Alternatives considered:**
- **fakeOllama HTTP server** (the existing `cli/commands_test.go` pattern): point
  `cfg.OllamaURL` at an `httptest.Server` returning deterministic vectors. No
  engine change. *Rejected:* it makes the production `go-rag eval` command spawn
  a fake HTTP server — a test-hack leaking into a first-class capability, and
  loopback-HTTP indirection for every query. Injection is cleaner and reuses the
  interface that already exists.
- **Eval builds retrieval directly** (like `cli/query.go` does with
  `index.NewRetrieval`): bypass `engine.Query`. *Rejected:* violates FR-007 (a
  parallel retrieval path could drift from the real one — exactly what eval is
  meant to prevent) and re-implements the rerank/threshold/collapse wiring.

---

## D2 — What deterministic embedder makes offline eval *meaningful*?

**Decision:** A pure-Go **feature-hashing ("hashing trick") vectorizer** with L2
normalization, fixed dimensionality (e.g. 256 dims), implemented in
`internal/eval/embedder.go` and satisfying `embed.Embedder`.

- Tokenize on word boundaries (lowercase, unicode-aware), hash each token into a
  bucket, add `+1` to that dim, L2-normalize the vector. Deterministic for a given
  input → identical vectors run-to-run, machine-to-machine, no network.
- Cosine over these vectors retrieves chunks that share tokens with the query, so
  the semantic/vector leg of hybrid retrieval behaves like a deterministic
  lexical-similarity signal. Combined with the existing BM25 FTS leg and RRF,
  retrieval produces **stable, meaningful rankings** — enough to detect real
  regressions in chunking, RRF weighting, rerank-fallback, collapse, and threshold.

**Rationale:** A constant vector (like the test helper `staticEmbed`) makes the
vector leg return arbitrary order — recall would be noise, useless for a gate. A
real-but-deterministic lexical vectorizer makes offline recall *interpretable*
while staying pure-Go and network-free. It is explicitly a **CI/mechanics**
embedder, not a substitute for a real semantic model (see D3).

**Alternatives considered:**
- **Constant vector** (`staticEmbed`): *rejected* — makes vector retrieval
  meaningless; the gate would fire on noise.
- **TF-IDF vectorizer**: slightly better semantics than hashing but requires a
  precomputed document-frequency table (corpus-dependent, complicates
  determinism/portability). *Deferred* — hashing is simpler and sufficient for a
  regression gate.
- **Real Ollama model offline**: impossible — Ollama is a network service by
  definition. Used only for the one-time baseline (D3).

---

## D3 — Two embedder modes: offline gate vs. real baseline

**Decision:** Eval supports two modes, selected by flag (`--embedder
auto|offline|ollama`, default `auto`):

- **offline (default, CI):** deterministic hashing vectorizer (D2). Reproducible,
  no network. This is what `make test-eval` and the CI gate use. Measures
  *mechanics* regressions (chunking, RRF, rerank-fallback, collapse, index rebuild).
- **ollama (baseline):** the real `embed.NewOllama` over the user's local Ollama.
  Used for the one-time *published baseline* (SC-003) — real semantic recall/MRR/
  NDCG against the golden corpus. Not reproducible across machines/models.
- **auto:** if a local Ollama is reachable AND not in CI, use ollama; else offline.

**Rationale:** SC-004 (no network by default) and SC-002 (regression detection)
demand an offline default; SC-003 (published baseline) demands a real-model run.
These are different jobs — one guards mechanics in CI, the other establishes
headline quality. Splitting them by flag is the simplest thing that does both.

**Alternatives considered:**
- **Offline only:** *rejected* — could never establish a real recall/MRR/NDCG
  baseline, so H07/H05/H10 quality wins could never be validated with real
  embeddings.
- **Ollama only:** *rejected* — non-reproducible, breaks CI/SC-004, and would
  silently skip the gate on machines without Ollama.

---

## D4 — IR metric definitions (hand-rolled, pure Go)

**Decision:** Implement exactly these, with these definitions, in
`internal/eval/metrics.go`. Each takes the ranked retrieved `chunk_id` list and
the set/grade of relevant chunk_ids for one query; averaged over the dataset.

- **Recall@k** = (# relevant in top-k) / (# relevant total). Relevant item absent
  from top-k counts as a miss in the numerator. If # relevant total is 0 for a
  query, the query is **excluded** from the average (FR-008), not counted as 0.
- **Precision@k** = (# relevant in top-k) / k. (k = retrieved count if fewer
  returned.)
- **MRR** = 1 / rank of the **first** relevant hit (0 if none in the retrieved
  list). Averaged over queries.
- **NDCG@k**: with binary relevance, DCG@k = Σ_{i=1..k} rel_i / log₂(i+1) where
  rel_i ∈ {0,1}; IDCG@k is DCG@k of the ideal (all-relevant-first) ordering;
  NDCG@k = DCG@k / IDCG@k (0 if IDCG is 0, i.e. no relevant items). Graded
  relevance supported by the same formula with rel_i = grade, but MVP labels are
  binary (D5).

**Tie-breaking:** when retrieved scores tie, break ties by chunk_id
(lexicographic) so values are deterministic run-to-run (spec edge case). `k`
cutoffs reported: 5 and 10 for recall, 5 for precision, 10 for MRR/NDCG pool.

**Rationale:** Book ch010 / App.C target thresholds (P@5 > 0.70, R@10 > 0.80,
MRR > 0.60, NDCG@10 > 0.75) are defined against exactly these standard formulas.
Hand-rolling avoids any dependency (Principle III); the math is <100 LOC and
trivially unit-testable with hand-computed expected values.

**Alternatives considered:**
- **A metrics library (e.g., a BEIR-style helper):** *rejected* — risks
  transitive CGo/C deps and a supply-chain surface for ~100 LOC of arithmetic.

---

## D5 — Golden dataset format & relevance model

**Decision:** Committed **JSONL** at `testdata/golden/v1.jsonl`, one record per
line:

```json
{"id":"q001","query":"how are chunks split?","relevant":["<chunk_id>","<chunk_id>"],"notes":"optional"}
```

- `relevant` is a list of **chunk_id** strings (SHA-256, content-addressed — the
  stable join key to a retrieved `QueryHit.ChunkID`).
- **Binary relevance** for v1 (a chunk_id is either relevant or not). The NDCG
  formula accepts grades, so graded labels can be added later as `{"grade":2}`
  without a schema break — but v1 ships binary (simplest thing that works).
- File is small, human-reviewable, diffs cleanly in PRs (spec US3 / book §9.6
  "dataset is code").

**Rationale:** chunk_id is content-addressed (Principle II), so labels are
portable: ingesting the same `testdata/golden/corpus/` into a throwaway vault
produces identical chunk_ids the labels refer to. JSONL is diff-friendly and
needs no parser dependency (stdlib `encoding/json` + `bufio.Scanner`).

**Alternatives considered:**
- **CSV:** *rejected* — queries contain commas/quotes; escaping is friction.
- **Embedded labels inside Pebble:** *rejected* — violates "golden set is code in
  git" (book §9.6) and the read-only/throwaway-vault discipline.
- **Graded relevance from day one:** *rejected* — binary is sufficient for an MVP
  baseline and gate; graded is additive later.

---

## D6 — Corpus handling: throwaway vault vs. live vault

**Decision:** Eval runs **read-only against a vault it is pointed at**
(`--db-path`, default the user's vault) for the measurement, AND can
**self-provision** a throwaway vault from `--corpus` (default
`testdata/golden/corpus/`) for a fully self-contained, reproducible run.

- Read-only mode: open the vault, run `engine.Query` per golden query, join hits
  to labels by chunk_id. Never writes (FR-006).
- Self-provision mode: ingest `--corpus` into a temp-dir vault (using the
  injected deterministic embedder), then measure — guarantees the chunk_ids the
  labels reference actually exist and the run is hermetic. The temp vault is
  removed on exit.

**Rationale:** chunk_id portability (D5) makes the labels valid in either vault.
Self-provision gives a reproducible default for CI/`make test-eval`;
read-only lets a maintainer measure their *real* corpus's quality. Both honor
FR-006 (no mutation of the user's data).

**Alternatives considered:**
- **Always self-provision:** *rejected* — would never measure the user's actual
  corpus quality (SC-003 baseline against real data).
- **Always live read-only:** *rejected* — not hermetic/reproducible for CI; a
  missing or differently-chunked live vault would produce misleading numbers.

---

## D7 — CI regression-gate semantics

**Decision:** `make test-eval` runs eval in **offline** mode against
`testdata/golden/v1.jsonl` and compares headline metrics (recall@10 primary,
MRR/NDCG@10 secondary) to a **committed baseline** (`testdata/golden/baseline.json`,
generated by `go-rag eval --record-baseline`). It fails (non-zero exit) when a
monitored metric drops more than `--tolerance` points (default 2.0 points)
versus baseline; it passes on equal-or-better and on changes within tolerance.

- Baseline is regenerated deliberately (a command), never silently, so a real
  improvement updates the baseline in the same PR that delivers it.
- Gate scopes to PRs touching `internal/chunk`, `internal/index`, `internal/rerank`,
  and hybrid/RRF weights (the retrieval-quality surface) — not every PR — so
  unrelated changes don't trip it (spec US2 acceptance #2).

**Rationale:** The book's "regression that almost shipped" (recall 78%→45% from a
model/boundary change, caught only by continuous eval) is the motivating failure.
A tolerance (not exact match) avoids flapping on offline-mode float noise; a
committed baseline avoids "did the gate or the code move?" ambiguity.

**Alternatives considered:**
- **Hard thresholds (P@5>0.70 etc. from the book):** *rejected* as the gate —
  those are *targets*, not gates; go-rag's deterministic-offline numbers won't
  match the book's real-model targets. Used as informational "target" lines in
  output, not pass/fail.
- **Exact-match baseline:** *rejected* — too brittle for float arithmetic and
  deterministic-but-order-sensitive ties.

---

## D8 — Synthetic query generation (US3, deferred-but-scoped)

**Decision:** US3's *optional* query generator is **out of MVP scope** but its
contract is fixed: a future `go-rag eval-gen --corpus <dir> --n 100` emits
*candidate* `{query, chunk_id}` pairs to stdout for **human triage** — humans
remain the source of truth for relevance labels. Nothing is auto-committed to
`v1.jsonl`.

**Rationale:** Avoids auto-label drift (the book's §9.1 warning that synthetic
labels corrode a golden set). Keeping the contract fixed means US3 lands later
without touching the metric/dataset code.

---

## Open questions after research

**None.** All NEEDS CLARIFICATION resolved. Proceeding to Phase 1 design.
