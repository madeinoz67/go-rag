# Quickstart — FTS Snapshot Validation (H16)

> Phase 1 output. Runnable, end-to-end scenarios that prove the feature works.
> Implementation bodies belong in `tasks.md`, not here. All scenarios use an
> **isolated DB** (a temp vault or `--db-path <tmp>`) — never the user's live vault.

## Prerequisites

- Go 1.26+ toolchain; `CGO_ENABLED=0` builds cleanly (`make build`).
- No Ollama needed for the unit/transparency scenarios (they use deterministic/hashed embedders or
  keyword-only queries). The cold-start timing scenario is meaningful on a non-trivial corpus (a few
  hundred+ chunks).

```bash
make build   # → ./bin/go-rag
```

## Scenario 1 — Cold start loads the snapshot; results identical (FR-001/007, SC-001) 🎯 MVP

```bash
DB=$(mktemp -d)/vault
./bin/go-rag --db-path "$DB" init
./bin/go-rag --db-path "$DB" config set embedding_model nomic-embed-text   # if hybrid queries used
# Ingest a non-trivial corpus (many chunks):
./bin/go-rag --db-path "$DB" add <some-dir-with-docs>
./bin/go-rag --db-path "$DB" status        # embeddings complete

# A cold one-shot query — the engine seeds its FTS, writing a snapshot on Close:
./bin/go-rag --db-path "$DB" query "some keyword" --mode keyword --k 10   > hits_snapshot.txt

# Force the rebuild path (internal test escape) and diff — results MUST be identical:
GO_RAG_NO_FTS_SNAPSHOT=1 ./bin/go-rag --db-path "$DB" query "some keyword" --mode keyword --k 10 > hits_rebuild.txt
diff hits_snapshot.txt hits_rebuild.txt   # empty ⇒ transparent (SC-001)
```

**Expected**: the two hit lists are identical (transparency). The snapshot path is the fast one.

## Scenario 2 — Cold start with a snapshot is materially faster (SC-002)

```bash
# After Scenario 1 wrote a snapshot:
time ./bin/go-rag --db-path "$DB" query "some keyword" --mode keyword --k 10            # snapshot load
time GO_RAG_NO_FTS_SNAPSHOT=1 ./bin/go-rag --db-path "$DB" query "some keyword" --mode keyword --k 10  # rebuild
```

**Expected**: the snapshot-load cold start is materially faster than the rebuild (target ≥5× on a
multi-thousand-chunk corpus). On a tiny corpus the difference is negligible — use a real-sized corpus.

## Scenario 3 — The snapshot stays current after ingest/delete (FR-003, SC-003)

```bash
./bin/go-rag --db-path "$DB" query "uniquetermXYZ" --mode keyword --k 5   # no hits (term not in corpus)
echo "a document containing uniquetermXYZ for the snapshot currency test" > /tmp/new.txt
./bin/go-rag --db-path "$DB" add /tmp/new.txt                              # ingest → snapshot updated on Close
./bin/go-rag --db-path "$DB" query "uniquetermXYZ" --mode keyword --k 5   # cold start → MUST find it now
```

**Expected**: after the ingest (which refreshed the snapshot on Close), a fresh cold start finds the new
chunk — the snapshot stayed current. (Symmetric for delete: a deleted chunk is gone on the next cold
start.)

## Scenario 4 — A stale/corrupt/absent snapshot is rebuilt (never wrong) (FR-004/005/006, SC-004)

```bash
# Absent (pre-H16 vault): first cold start rebuilds + writes a snapshot.
rm -rf "$DB2"; DB2=$(mktemp -d)/vault
./bin/go-rag --db-path "$DB2" init
./bin/go-rag --db-path "$DB2" add <a-doc>           # no snapshot yet
./bin/go-rag --db-path "$DB2" query "term" --mode keyword   # rebuilds + (on Close) writes snapshot
# Corrupt the snapshot (out-of-band) then cold-start — MUST still return correct results:
# (locate the 0x06/"snapshot" key in the Pebble DB and corrupt it; or use the test that simulates it)
./bin/go-rag --db-path "$DB2" query "term" --mode keyword   # ignores corrupt snapshot, rebuilds, correct
```

**Expected**: a missing/corrupt/stale snapshot is detected and the FTS is rebuilt from chunks — correct
results, no error to the caller, and a fresh snapshot is written for the next start.

## Scenario 5 — Bulk ingest does not regress (FR-009, SC-007)

```bash
# Time a bulk ingest with the snapshot on vs a baseline (pre-H16) build:
time ./bin/go-rag --db-path "$DB" add <large-dir>      # marker bumps once; snapshot writes once on Close
```

**Expected**: bulk-ingest wall-time is not materially worse than the pre-H16 baseline (the marker bumps
once per session; the snapshot writes once on Close — no per-chunk write).

## Scenario 6 — Build + test gates green (constitution §Dev Workflow)

```bash
CGO_ENABLED=0 go build ./...
go vet ./...
go test -race -cover ./...   # incl. H16 tests: snapshot round-trip, transparency (load==rebuild),
                             # currency, staleness/corrupt/absent → rebuild, format-version, backward-compat
```

**Expected**: all green. The transparency test (load-snapshot vs forced-rebuild, diff the hits) and the
staleness test (corrupt/absent/out-of-band-change → rebuild + correct) are the highest-weight gates.

## Scenario 7 — No quality regression (FR-010, SC-006)

```bash
make test-eval   # H02 harness
```

**Expected**: recall@10 unchanged (the snapshot is transparent; the eval path loads it but gets identical
results).

## Done

When scenarios 1–7 pass with build/vet/test/eval green, H16 meets every FR and SC and is ready for the
audit-backlog checkbox.

> Note: `GO_RAG_NO_FTS_SNAPSHOT=1` is the **internal test escape** (contracts §4) — it forces the rebuild
> path so transparency/timing can be measured. It is not a documented user flag.
