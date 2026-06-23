# Phase 1 — Quickstart: Score Calibration + Citation Contract (H21)

> Runnable validation that scores are calibrated + chunk_index is surfaced + the citation
> contract is documented. Run on an **isolated** DB.

## Scenario 1 — Top hit normalized to 1.0, monotonic (US1, FR-001, SC-001)

```bash
VAULT=$(mktemp -d); DB=$VAULT/vault
./bin/go-rag init --db-path "$DB" >/dev/null
./bin/go-rag add testdata/golden/corpus/ --db-path "$DB"
./bin/go-rag query "retrieval" --db-path "$DB" --k 5 --format json | jq '.[0].score'
# expect: 1.0 (or very close — the top hit is normalized to 1.0)
```

**Pass**: top hit's score = 1.0; subsequent scores ≤ 1.0, decreasing.

## Scenario 2 — Threshold on normalized scale (FR-002, SC-002)

```bash
./bin/go-rag query "retrieval" --db-path "$DB" --k 10 --threshold 0.5 --format json | jq 'length'
# expect: fewer hits (only those with normalized score ≥ 0.5)
```

**Pass**: threshold filters on the normalized [0,1] scale.

## Scenario 3 — chunk_index surfaced (US2, FR-004, SC-003)

```bash
./bin/go-rag query "retrieval" --db-path "$DB" --k 1 --format json | jq '.[0] | {chunk_id, chunk_index, document_id}'
# expect: chunk_index is present (an integer ≥ 0)
```

**Pass**: every hit carries `chunk_index`.

## Scenario 4 — Ranking order unchanged (FR-007, SC-006)

```bash
make test-eval   # recall@10 unchanged — normalization preserves order
```

**Pass**: eval green.

## Scenario 5 — Citation contract documented (FR-005, SC-005)

```bash
cat docs/citation-contract.md   # the contract is present + readable
```

**Pass**: the contract documents chunk_id as the anchor, chunk_index as ordinal, threshold
as relative-within-result.

## Done definition

All five scenarios pass + `go build/vet/test -race` green + no new go.mod dep.
