# Quickstart — Pebble-backed Async FTS Validation (H16, pivoted)

> Phase 1 output. Runnable scenarios proving the pivot works. All use an
> **isolated DB** (a temp vault or `--db-path <tmp>`) — never the live vault.

## Prerequisites

- Go 1.26+ toolchain; `CGO_ENABLED=0` builds cleanly (`make build`).
- No Ollama needed for the transparency/unit scenarios (keyword-only queries).

## Scenario 1 — Cold start has no FTS rebuild (FR-005, SC-002) 🎯 MVP

```bash
DB=$(mktemp -d)/vault
./bin/go-rag --db-path "$DB" init
./bin/go-rag --db-path "$DB" add <some-dir>           # ingest (async FTS postings written)
sleep 2                                                 # let the async worker drain
# Cold start + keyword query — no re-tokenization:
time ./bin/go-rag --db-path "$DB" query "some keyword" --mode keyword --k 10
```

**Expected**: the query returns hits; the cold-start time is dramatically lower
than the pre-pivot rebuild (the FTS is read from Pebble, not reconstructed). On a
multi-thousand-chunk corpus the difference is seconds → sub-second.

## Scenario 2 — Transparency: identical results (FR-008, SC-001)

```bash
# Ingest a corpus, query with the Pebble-backed FTS:
./bin/go-rag --db-path "$DB" query "search term" --mode keyword --k 10 > pebble_hits.txt
# (The pre-pivot in-memory FTS would return the same hits — verified by the
# engine-level transparency test that builds both and diffs.)
```

**Expected**: the Pebble-backed hits are byte-identical to what the old in-memory
FTS would return (same chunk IDs, same BM25 order). The transparency test
(`TestFTS_Transparency_PebbleVsInMemory`) proves this directly.

## Scenario 3 — Postings stay current (async) (FR-003/004, SC-003)

```bash
./bin/go-rag --db-path "$DB" query "uniqueterm" --mode keyword   # no hits
echo "a document about uniqueterm" > /tmp/new.txt
./bin/go-rag --db-path "$DB" add /tmp/new.txt                     # ACK fast; async FTS write pending
sleep 2                                                            # wait for async drain
./bin/go-rag --db-path "$DB" query "uniqueterm" --mode keyword   # now finds it
```

**Expected**: after the async drain, the new chunk's keywords are findable. (If
queried immediately after ACK, before the drain, it may not be — eventual
consistency, same as vector search.)

## Scenario 4 — Pre-pivot vault migrates on first start (FR-006, SC-004)

```bash
# (Requires a pre-pivot binary to create a vault without 0x05 postings, OR
# simulate by deleting the 0x08 stats key from a vault.)
# On first start with the pivoted binary, the migration runs:
./bin/go-rag --db-path "$OLD_DB" query "term" --mode keyword
# → one-time backfill (same speed as old cold start), then subsequent starts are fast
./bin/go-rag --db-path "$OLD_DB" query "term" --mode keyword   # fast (no rebuild)
```

**Expected**: first start migrates (builds postings from chunks); second start is
fast (postings already on disk). No re-ingestion.

## Scenario 5 — Delete removes postings (FR-004)

```bash
./bin/go-rag --db-path "$DB" query "deletable" --mode keyword   # finds the doc
./bin/go-rag --db-path "$DB" scan                                # delete via watcher (or manual)
sleep 2
./bin/go-rag --db-path "$DB" query "deletable" --mode keyword   # gone
```

**Expected**: after the delete + async drain, the chunk's postings are removed and
a cold-start query no longer returns it.

## Scenario 6 — Build + test gates green

```bash
CGO_ENABLED=0 go build ./...
go vet ./...
go test -race -cover ./...   # incl. Pebble-backed FTS tests: Index/Search/Delete
                             # round-trip, prefix-scan query, transparency, cold-start
                             # no-rebuild, migration, delete
```

**Expected**: all green. The **transparency test** (Pebble vs in-memory, diff the
hits) and the **no-rebuild test** (cold start doesn't scan PrefixChunk for FTS)
are the highest-weight gates.

## Scenario 7 — No quality regression (FR-010, SC-005/006)

```bash
make test-eval   # H02 harness
```

**Expected**: recall@10 unchanged (the FTS backing changed, not the BM25 math).
Keyword query latency < 5 ms (benchmarked ~0.3 ms worst-case).

## Done

When scenarios 1–7 pass with build/vet/test/eval green, H16 (pivoted) meets every
FR and SC and is ready for the audit-backlog checkbox.
