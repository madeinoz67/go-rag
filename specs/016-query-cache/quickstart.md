# Quickstart — Query Caching Validation (H06)

> Phase 1 output. Runnable, end-to-end scenarios that prove the feature works.
> Implementation bodies belong in `tasks.md`, not here. All scenarios use an
> **isolated DB** and non-default transport ports (per CLAUDE.md §Constraints —
> a bare `go-rag start` targets the user's live vault).

## Prerequisites

- Go 1.26+ toolchain; `CGO_ENABLED=0` builds cleanly (`make build`).
- A local Ollama with an embedding model pulled (e.g. `nomic-embed-text`). Not needed for scenarios 6–7 (eval/unit gates), which use deterministic embedders.
- **The cache is in-process / daemon-only** — scenarios 1–5 run against a started
  daemon (one-shot `go-rag query` calls each spin up a fresh engine and start
  cold). Start one on isolated ports, e.g.:
  `go-rag --db-path "$DB" start --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880`,
  then query over REST/MCP/gRPC; `go-rag --db-path "$DB" status` (which calls the
  daemon's `go_rag_status`) shows the cache line. Stop with `go-rag --db-path "$DB" stop`.

```bash
make build                 # → ./bin/go-rag
make vet                   # go vet ./... clean
```

## Scenario 1 — Repeated query is a cache hit (FR-001/008, SC-001/004) 🎯 MVP

```bash
DB=$(mktemp -d)/vault
./bin/go-rag --db-path "$DB" config set embedding_model nomic-embed-text   # if not set
./bin/go-rag --db-path "$DB" add <a-file-or-dir>
# wait for async embeddings to land:
./bin/go-rag --db-path "$DB" status        # EmbeddingsComplete: true

# Cold query (miss):
time ./bin/go-rag --db-path "$DB" query "some term" --k 5
./bin/go-rag --db-path "$DB" status | grep -A3 -i cache    # ResultCache Hits=0/1, Misses=1

# Repeat identical query (hit):
time ./bin/go-rag --db-path "$DB" query "some term" --k 5
./bin/go-rag --db-path "$DB" status | grep -A3 -i cache    # Hits incremented; dramatically faster
```

**Expected**: second query returns the **identical** hits; `status` shows ResultCache `Hits` incremented and the second wall-time is far lower. A diff of the two results' `chunk_id` lists is empty (transparency, SC-004).

## Scenario 2 — A corpus change evicts stale results (FR-003, SC-002)

```bash
./bin/go-rag --db-path "$DB" query "unique-new-term" --k 5          # miss (term not in corpus yet)
# add a document containing "unique-new-term":
./bin/go-rag --db-path "$DB" add <new-file>
./bin/go-rag --db-path "$DB" status                                  # wait for EmbeddingsComplete
./bin/go-rag --db-path "$DB" query "unique-new-term" --k 5          # MUST now return the new hit
```

**Expected**: the post-ingest query returns the new document (the epoch advanced; the stale pre-ingest entry was not served). Removing the doc (`scan` detects deletion, or re-add path) likewise evicts.

## Scenario 3 — `--no-cache` forces a fresh result (FR-010, SC-007)

```bash
./bin/go-rag --db-path "$DB" query "some term" --k 5                 # warm the cache
./bin/go-rag --db-path "$DB" query "some term" --k 5 --no-cache      # bypass serving
```

**Expected**: the `--no-cache` result equals the cached result (same hits), but `status` shows a `Misses` increment for that call (it did not serve from cache). Over REST/gRPC/MCP, the `no_cache`/`noCache` field produces the same fresh result (parity).

## Scenario 4 — Embedding reuse on a result miss (FR-004/005, SC-001)

Force a result-cache miss while keeping the query text identical (e.g. evict by filling the cache, or change `k`):

```bash
./bin/go-rag --db-path "$DB" query "some term" --k 5                 # embeds + caches vector
./bin/go-rag --db-path "$DB" query "some term" --k 6                 # different k → result miss, but same query text
```

**Expected**: the `--k 6` call recomputes results but does **not** re-embed (the query vector is served from the embedding cache). Observable via Ollama logs / `status` EmbeddingCache `Hits`.

## Scenario 5 — Migrate flushes both caches (FR-006, SC-003)

```bash
./bin/go-rag --db-path "$DB" query "some term" --k 5                 # warm both caches
./bin/go-rag --db-path "$DB" config set embedding_model <other-model>
./bin/go-rag --db-path "$DB" migrate                                 # re-embeds + flushes both caches
./bin/go-rag --db-path "$DB" status | grep -A3 -i cache              # both caches Size=0 (flushed)
./bin/go-rag --db-path "$DB" query "some term" --k 5                 # miss under the new profile
```

**Expected**: after `migrate`, both cache sizes are 0; the next query is a cold miss under the new embedding profile.

## Scenario 6 — Build + test gates green (constitution §Dev Workflow)

```bash
CGO_ENABLED=0 go build ./...
go vet ./...
go test -race -cover ./...          # incl. new cache tests: hit/miss, epoch invalidation,
                                    # capacity eviction, concurrency, nocache, rerank-failed-not-cached
```

**Expected**: all green. The dedicated **async-epoch regression test** (ingest → query → wait for the background vector-add → query again → assert the new epoch result, not the stale one) must pass — this is the highest-risk correctness gate (D2).

## Scenario 7 — No quality regression (FR-013, SC-006)

```bash
make test-eval                     # H02 harness; eval engine built with QueryCacheEnabled=false (D8)
```

**Expected**: recall@10 unchanged from baseline (cache disabled in the harness → measures pure retrieval; cached results are identical anyway).

## Done

When scenarios 1–7 pass with build/vet/test/eval green, the feature meets every FR and SC in the spec and is ready for the audit-backlog checkbox on H06.
