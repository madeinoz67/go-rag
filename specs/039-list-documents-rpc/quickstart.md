# Quickstart — ListDocuments (BL-007)

> Phase 1 validation guide for `/speckit-plan`. Runnable scenarios that prove the feature end-to-end. No implementation bodies — those live in `tasks.md`. References [data-model.md](./data-model.md) and [contracts/api.md](./contracts/api.md).

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (`CGO_ENABLED=0`).
- A throwaway vault (do **not** smoke-test against the global default vault — see project `CLAUDE.md`):
  ```bash
  export GR_DIR="$(mktemp -d)/vault"
  ./bin/go-rag init --db-path "$GR_DIR"
  ```

## Scenario 1 — `after` cursor returns only documents ingested since the cursor

**Setup.** Ingest a batch, capture the timestamp, ingest more:

```bash
mkdir -p "$GR_DIR/docs"
for i in $(seq 1 5); do echo "early document $i about tokens" > "$GR_DIR/docs/early-$i.txt"; done
./bin/go-rag add "$GR_DIR/docs" --db-path "$GR_DIR"
sleep 2
MID=$(date -u +%Y-%m-%dT%H:%M:%SZ)
for i in $(seq 1 3); do echo "later document $i about sessions" > "$GR_DIR/docs/later-$i.txt"; done
./bin/go-rag add "$GR_DIR/docs" --db-path "$GR_DIR"

./bin/go-rag documents list --db-path "$GR_DIR" --after "$MID" --format json | jq '.documents | length'
```

**Expected.** `3` — exactly the documents ingested after `MID`, in ascending `ingested_at` order. (contracts/api.md §Operation contract; FR-003.)

## Scenario 2 — `status=embedded` filters to fully-embedded documents

```bash
# Immediately after add, some docs may still be pending; list only embedded ones.
./bin/go-rag documents list --db-path "$GR_DIR" --status embedded --format json | jq '[.documents[] | {file: .file_path, status: .status}]'
```

**Expected.** Every returned document has `status == "embedded"` (none pending/error). (FR-004.)

## Scenario 3 — `after` + `status` combine with AND

```bash
./bin/go-rag documents list --db-path "$GR_DIR" --after "$MID" --status embedded --format json | jq '.documents | length'
```

**Expected.** Only documents that are BOTH ingested after `MID` AND embedded (the AND of Scenarios 1 and 2). (FR-004 AND semantics.)

## Scenario 4 — pagination composes with `after` + `status`

```bash
# Force small pages over the full filtered set.
PAGE1=$(./bin/go-rag documents list --db-path "$GR_DIR" --page-size 2 --format json)
echo "$PAGE1" | jq '.documents | length'          # → 2
TOK=$(echo "$PAGE1" | jq -r '.next_page_token')   # non-empty (more remain)
./bin/go-rag documents list --db-path "$GR_DIR" --page-size 2 --page-token "$TOK" --format json | jq '.documents | length'
```

**Expected.** Each page has ≤ `page_size` documents; concatenating all pages (echoing `next_page_token`) yields every matching document exactly once, in order; the last page's `next_page_token` is empty. (FR-006/FR-007.)

## Scenario 5 — invalid input is rejected

```bash
./bin/go-rag documents list --db-path "$GR_DIR" --page-size 999; echo "exit=$?"     # > 200
./bin/go-rag documents list --db-path "$GR_DIR" --status bogus; echo "exit=$?"      # unknown status
./bin/go-rag documents list --db-path "$GR_DIR" --after "yesterday"; echo "exit=$?" # not RFC3339
```

**Expected.** Each exits non-zero with a clear invalid-argument message (CLI); REST 400; gRPC `INVALID_ARGUMENT`. (FR-005; FR-003/FR-004 validation.)

## Scenario 6 — cross-transport parity

```bash
./bin/go-rag start --db-path "$GR_DIR" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2

# Same (page_size, after, status) over REST + CLI; diff the ordered document-id lists + next_page_token
curl -s "http://127.0.0.1:17879/v1/documents?page_size=10&after=$MID&status=embedded" | jq '[.documents[].id], .next_page_token'
./bin/go-rag documents list --db-path "$GR_DIR" --page-size 10 --after "$MID" --status embedded --format json | jq '[.documents[].id], .next_page_token'

./bin/go-rag stop --db-path "$GR_DIR"
```

**Expected.** REST and CLI return identical ordered document-id lists and the same `next_page_token`. gRPC + MCP agree (enforced in-code by `internal/engine/parity_test.go`). (FR-011.)

## Scenario 7 — empty result is not an error

```bash
FUTURE=$(date -u -v+1d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d tomorrow +%Y-%m-%dT%H:%M:%SZ)
./bin/go-rag documents list --db-path "$GR_DIR" --after "$FUTURE" --format json | jq '{docs: (.documents|length), next: .next_page_token}'
```

**Expected.** `{ "docs": 0, "next": "" }` — empty list, empty token, exit 0 (never an error). (Edge: `after` far in the future.)

## Build & test gates

```bash
make build          # CGO_ENABLED=0 — must succeed
make vet            # go vet ./...
make lint           # golangci-lint run (CI gate)
make test           # go test -race -cover ./...
```

**Must include:** `internal/engine/list_documents_test.go` (after/status/order/pagination/edges) and `internal/engine/parity_test.go` (ListDocuments identical across CLI/REST/gRPC/MCP).

## Constitution affirmation (for the PR)

Pure read over existing prefix `0x02`; no on-disk key-space layout change, no migration (`ingested_at` verified reliable — research.md R2), `migrate.ExpectedVersion` unchanged; pure Go, no new deps. Principles I–V all pass (see `plan.md` Constitution Check).
