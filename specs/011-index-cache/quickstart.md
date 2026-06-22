# Quickstart: Validate the Cached Index (H01)

> Runnable validation proving the cache works end-to-end. These are *validation*
> steps — implementation/test bodies live in `tasks.md`. Scenarios use a
> deterministic embedder (no real Ollama) unless noted.

## Prerequisites

```bash
make build                 # ./bin/go-rag
make test                  # go test -race -cover ./... must be green
```

Validation is driven through `internal/engine` tests against a temp DB with a
deterministic/fake embedder (the `openEngine` + `fastFakeOllama` harness already
in `internal/engine/parity_test.go`). No external service required for scenarios
1–5.

---

## Scenario 1 — Identical results, cache vs rebuild (FR-008, US3)

Embed `N` chunks; query once (seeds the cache) and capture the ranked hits. Query
again and assert the second result set is **identical** (same chunk IDs, order,
scores) to the first.

**Expected**: byte-for-byte identical results — the cache changes latency, not
output.

## Scenario 2 — Latency ratio: 2nd+ query ≪ 1st (SC-001, US1)

Against a corpus large enough that `LoadIndex` is measurably costly (thousands of
chunks, or a synthetic delay in the load path for the test), time the 1st query
(seeds) and the 2nd (reuses). Assert the 2nd completes in a small fraction of the
1st (e.g. < 5%), with identical results.

**Expected**: a dramatic, reproducible drop on the 2nd query; flat thereafter.
*(In the unit test this can be asserted structurally — e.g. a LoadIndex counter
shows 1 call across N queries — rather than by wall-clock, to avoid flake.)*

## Scenario 3 — Read-after-write: ingest then query (FR-003/004, US2)

Ingest a document; wait for embeddings to complete (`Status.EmbeddingsComplete`);
query for its content.

**Expected**: the document is returned by the very next query — no restart, no
manual flush. (This is the async-after-ACK correctness: the live index reflects
embedding completion.)

## Scenario 4 — Delete freshness: no phantom hits (FR-003, US2)

Delete a document via the delete path; query for its content immediately.

**Expected**: the deleted document's chunks no longer appear — the cache-aware
`DeleteDoc` removed them from the shared index.

## Scenario 5 — Migrate freshness

Re-embed the corpus (migrate) and query.

**Expected**: results reflect the new embeddings (the shared index was updated
through reprocess → `DeleteDoc` + re-ingest).

## Scenario 6 — Concurrency safety (FR-005/006, US3)

Run many concurrent queries while documents are being ingested in the background.

**Expected**: no errors, no panics, every query returns a self-consistent result
set. (Run under `-race`.)

## Scenario 7 — Seed-once (no thundering herd, FR-006)

Fire N concurrent first-time queries against a cold cache.

**Expected**: the seed (`LoadIndex`) runs **once**, not N times; all queries
reuse the single result. (Assert via a load counter.)

## Scenario 8 — End-to-end daemon latency (optional, real)

Start the daemon on an isolated DB with a populated vault; issue the same query
twice over REST/MCP/gRPC and observe the 2nd is far faster:

```bash
TMPDB="$(mktemp -d)/v"
./bin/go-rag init --db-path "$TMPDB" --model nomic-embed-text
./bin/go-rag add --db-path "$TMPDB" <large-corpus-dir>
./bin/go-rag start --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
# time the 1st vs 2nd identical query over any transport
```

**Expected**: the 2nd query returns in a small fraction of the 1st's time, with
identical results.
