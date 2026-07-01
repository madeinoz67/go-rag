# Quickstart — BatchGetChunks (BL-003)

> Phase 1 validation guide for `/speckit-plan`. Runnable scenarios that prove the feature end-to-end. No implementation bodies — those live in `tasks.md`. References [data-model.md](./data-model.md) and [contracts/api.md](./contracts/api.md).

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (`CGO_ENABLED=0`).
- A throwaway vault (do **not** smoke-test against the global default vault — see project `CLAUDE.md`):
  ```bash
  export GR_DIR="$(mktemp -d)/vault"
  ./bin/go-rag init --db-path "$GR_DIR"
  ```

## Scenario 1 — resolve a batch of live chunks (happy path)

**Setup.** Ingest a document long enough to produce several chunks, then collect its chunk ids:

```bash
mkdir -p "$GR_DIR/docs"
{ echo "# Long Doc"; for i in $(seq 1 60); do echo "Paragraph $i about authentication tokens and session handling."; done; } > "$GR_DIR/docs/long.md"
./bin/go-rag add "$GR_DIR/docs/long.md" --db-path "$GR_DIR"

IDS=$(./bin/go-rag query "authentication" --db-path "$GR_DIR" --mode keyword --json | jq -r '.hits[].chunk_id')
```

**Run.** Fetch the batch (CLI takes positional args):

```bash
./bin/go-rag chunk batch $IDS --db-path "$GR_DIR" --window 2 >/dev/null 2>&1 || true   # (no --window on batch; shown for tab-completion familiarity)
./bin/go-rag chunk batch $IDS --db-path "$GR_DIR" --format json | jq '.results | length, [.[] | .chunk_id]'
# REST equivalent: curl -s -X POST "http://127.0.0.1:17879/v1/chunks/batch" -H 'content-type: application/json' -d "{\"chunk_ids\":[$(echo "$IDS" | jq -R . | jq -sc .)]}"
```

**Expected.** `results` length equals the number of requested ids; each entry carries its `chunk_id` in request order, every chunk has full content, and the request order is preserved. (contracts/api.md §Operation contract.)

## Scenario 2 — a missing id yields a per-id error; the call still succeeds

```bash
LIVE=$(echo "$IDS" | head -1)
./bin/go-rag chunk batch "$LIVE" "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00" --db-path "$GR_DIR" --format json | jq '.results'
```

**Expected.** Two results in order: `[0]` the live chunk (full content + document), `[1]` `{ "chunk_id": "deadbeef…", "error": "not found" }` (empty chunk). The command exits 0 — the call does NOT fail for the missing id. (FR-003.) REST returns **200** (not 404) with the same in-band error.

## Scenario 3 — cap: > 100 ids is rejected

```bash
MANY=$(yes "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00" | head -101 | tr '\n' ' ')
./bin/go-rag chunk batch $MANY --db-path "$GR_DIR"; echo "exit=$?"
```

**Expected.** Non-zero exit with a clear "max 100 chunk_ids" message (CLI); REST 400; gRPC `INVALID_ARGUMENT`. No lookup performed. (FR-004.)

## Scenario 4 — empty list + empty/whitespace element are rejected

```bash
./bin/go-rag chunk batch --db-path "$GR_DIR"; echo "exit=$?"                       # empty list
./bin/go-rag chunk batch "$LIVE" "   " --db-path "$GR_DIR"; echo "exit=$?"          # whitespace element
```

**Expected.** Both exit non-zero with a clear invalid-argument message; REST 400; gRPC `INVALID_ARGUMENT`. (FR-005/FR-006.)

## Scenario 5 — duplicates are resolved positionally (no de-dup)

```bash
./bin/go-rag chunk batch "$LIVE" "$LIVE" --db-path "$GR_DIR" --format json | jq '.results | length, [.[] | .chunk_id]'
```

**Expected.** `results` length is 2; both entries carry the same `chunk_id` and the same chunk. No de-duplication. (FR-007.)

## Scenario 6 — cross-transport parity

```bash
./bin/go-rag start --db-path "$GR_DIR" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2

BODY="{\"chunk_ids\":[\"$LIVE\",\"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00\"]}"
# REST
curl -s -X POST "http://127.0.0.1:17879/v1/chunks/batch" -H 'content-type: application/json' -d "$BODY" | jq '[.results[] | {chunk_id, error}]'
# CLI
./bin/go-rag chunk batch "$LIVE" "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00" --db-path "$GR_DIR" --format json | jq '[.results[] | {chunk_id, error}]'

./bin/go-rag stop --db-path "$GR_DIR"
```

**Expected.** REST and CLI return identical per-position `{chunk_id, error}` lists for the same batch (live id resolves, missing id → "not found"). gRPC + MCP agree (enforced in-code by `internal/engine/parity_test.go`). (FR-010.)

## Scenario 7 — full metadata on every returned chunk (incl. Wikilinks)

```bash
cat > "$GR_DIR/docs/linked.md" <<'EOF'
# Auth
See [[authentication]] and [[JWT tokens]]. Surrounding prose for chunking.
More surrounding prose to ensure multiple chunks about sessions and tokens.
EOF
./bin/go-rag add "$GR_DIR/docs/linked.md" --db-path "$GR_DIR"
LIDS=$(./bin/go-rag query "authentication" --db-path "$GR_DIR" --mode keyword --json | jq -r '.hits[].chunk_id')
./bin/go-rag chunk batch $LIDS --db-path "$GR_DIR" --format json | jq '.results[] | .chunk | {chunk_id, wikilinks, section_context}'
```

**Expected.** Every returned chunk carries `wikilinks` and `section_context` (spec 036 / spec 025 sidecars) — the batch returns full chunks, identical to `GetChunk`. (FR-008.)

## Build & test gates

```bash
make build          # CGO_ENABLED=0 — must succeed
make vet            # go vet ./...
make lint           # golangci-lint run (CI gate)
make test           # go test -race -cover ./...
```

**Must include:** `internal/engine/batch_get_chunks_test.go` (order/missing/duplicates/cap/empty/orphan) and `internal/engine/parity_test.go` (BatchGetChunks identical across CLI/REST/gRPC/MCP).

## Constitution affirmation (for the PR)

Pure read over existing prefixes (`0x03`/`0x02`/source); no on-disk key-space layout change, no migration, `migrate.ExpectedVersion` unchanged; pure Go, no new deps. Principles I–V all pass (see `plan.md` Constitution Check).
