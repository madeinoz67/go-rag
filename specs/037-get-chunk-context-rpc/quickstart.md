# Quickstart — GetChunkContext (BL-002)

> Phase 1 validation guide for `/speckit-plan`. Runnable scenarios that prove the feature end-to-end. No implementation bodies — those live in `tasks.md`. References [data-model.md](./data-model.md) and [contracts/api.md](./contracts/api.md).

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (`CGO_ENABLED=0`).
- A throwaway vault (do **not** smoke-test against the global default vault — see project `CLAUDE.md`):
  ```bash
  export GR_DIR="$(mktemp -d)/vault"
  ./bin/go-rag init --db-path "$GR_DIR"
  ```

## Scenario 1 — interior chunk returns a symmetric window (happy path)

**Setup.** Ingest a markdown document long enough to produce several chunks (so an interior chunk has neighbours both sides):

```bash
mkdir -p "$GR_DIR/docs"
{ echo "# Long Doc"; for i in $(seq 1 60); do echo "Paragraph $i about authentication tokens and session handling."; done; } > "$GR_DIR/docs/long.md"
./bin/go-rag add "$GR_DIR/docs/long.md" --db-path "$GR_DIR"
```

**Run.** Obtain an interior chunk id and fetch its context:

```bash
CID=$(./bin/go-rag query "authentication" --db-path "$GR_DIR" --mode keyword --json | jq -r '.hits[0].chunk_id')
./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window 2
# REST equivalent: curl "http://127.0.0.1:17879/v1/chunks/$CID/context?window=2"
```

**Expected.** Five chunks in document order with the target marked at `target_index=2` (2 predecessors, target, 2 successors); the parent document line is shown. (contracts/api.md §Operation contract.)

## Scenario 2 — document boundaries are tolerated

```bash
# First chunk: target_index=0, only successors
FIRST=$(./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window 5 --json | jq -r '.chunks[0].chunk_id')
./bin/go-rag chunk context "$FIRST" --db-path "$GR_DIR" --window 5
# Last chunk: target at the last index, only predecessors
```

**Expected.** The first chunk yields `target_index=0` with up to 5 successors and zero predecessors (no error). The last chunk yields the target at the final index with up to 5 predecessors. (FR-005.)

## Scenario 3 — `window=0` is equivalent to GetChunk

```bash
./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window 0 --json | jq '.chunks | length, .target_index'
```

**Expected.** `1` chunk, `target_index=0` — identical content to `go-rag chunk get "$CID"`. (FR-003.)

## Scenario 4 — window clamp + invalid argument

```bash
./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window 11; echo "exit=$?"
./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window -1;  echo "exit=$?"
```

**Expected.** Both exit non-zero with a clear "window must be 0–10" message (CLI); REST returns 400; gRPC returns `INVALID_ARGUMENT`. (FR-004.)

## Scenario 5 — not-found + invalid id

```bash
./bin/go-rag chunk context "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00" --db-path "$GR_DIR"; echo "exit=$?"
./bin/go-rag chunk context "   " --db-path "$GR_DIR"; echo "exit=$?"
```

**Expected.** Missing id → not-found (404 / `NOT_FOUND` / non-zero exit). Empty/whitespace id → invalid-argument (400 / `INVALID_ARGUMENT`), no lookup. (FR-006/FR-007.)

## Scenario 6 — cross-transport parity

```bash
./bin/go-rag start --db-path "$GR_DIR" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2

# Same (chunk_id, window) over REST + gRPC + CLI; diff the chunks/target_index/document
curl -s "http://127.0.0.1:17879/v1/chunks/$CID/context?window=2" | jq '.target_index, (.chunks | map(.chunk_id))'
./bin/go-rag chunk context "$CID" --db-path "$GR_DIR" --window 2 --json | jq '.target_index, (.chunks | map(.chunk_id))'

./bin/go-rag stop --db-path "$GR_DIR"
```

**Expected.** All transports return identical chunk-id lists, `target_index`, and document metadata for the same `(chunk_id, window)`. (FR-010.) Enforced in-code by `internal/engine/parity_test.go`.

## Scenario 7 — full metadata on every returned chunk (incl. Wikilinks)

```bash
cat > "$GR_DIR/docs/linked.md" <<'EOF'
# Auth
See [[authentication]] and [[JWT tokens]]. Surrounding prose for chunking.
More surrounding prose to ensure multiple chunks about sessions and tokens.
EOF
./bin/go-rag add "$GR_DIR/docs/linked.md" --db-path "$GR_DIR"
LCID=$(./bin/go-rag query "authentication" --db-path "$GR_DIR" --mode keyword --json | jq -r '.hits[0].chunk_id')
./bin/go-rag chunk context "$LCID" --db-path "$GR_DIR" --window 2 --json | jq '.chunks[] | {chunk_id, wikilinks, section_context}'
```

**Expected.** Every returned chunk carries `wikilinks` and `section_context` (spec 036 / spec 025 sidecars) — the window returns full chunks, identical to `GetChunk`. (FR-008.)

## Build & test gates

```bash
make build          # CGO_ENABLED=0 — must succeed
make vet            # go vet ./...
make lint           # golangci-lint run (CI gate)
make test           # go test -race -cover ./...
```

**Must include:** `internal/engine/get_chunk_context_test.go` (windowing: interior/first/last/single/`window=0`/`window>10`/orphan) and `internal/engine/parity_test.go` (GetChunkContext identical across CLI/REST/gRPC/MCP).

## Constitution affirmation (for the PR)

Pure read over existing prefix `0x03`; no on-disk key-space layout change, no migration, `migrate.ExpectedVersion` unchanged; pure Go, no new deps. Principles I–V all pass (see `plan.md` Constitution Check).
