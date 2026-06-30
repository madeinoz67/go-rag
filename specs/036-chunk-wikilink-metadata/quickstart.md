# Quickstart — Chunk Wikilink Metadata (BL-004)

> Phase 1 validation guide for `/speckit-plan`. Runnable scenarios that prove the feature end-to-end. No implementation bodies — those live in `tasks.md`. References [data-model.md](./data-model.md) and [contracts/api.md](./contracts/api.md) for shapes.

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (`CGO_ENABLED=0`).
- A throwaway vault (do **not** smoke-test against the global default vault — see project `CLAUDE.md`):
  ```bash
  export GR_DIR="$(mktemp -d)/vault"
  ./bin/go-rag init --db-path "$GR_DIR"
  ```
- An Ollama runner reachable for embeddings (only needed for full ingest; the wikilink field is populated at read time, pre-embed).

## Scenario 1 — wikilinks appear on a query hit (happy path)

**Setup.** Ingest a markdown note with several link forms:

```bash
mkdir -p "$GR_DIR/docs"
cat >"$GR_DIR/docs/auth.md" <<'EOF'
# Authentication

See [[JWT tokens]] and [[RBAC]] for detail. The alias [[RBAC|role access]] is the same target.
An embed ![[diagram.png]] is not a link. Dangling [[phantom]] is kept verbatim.
EOF
./bin/go-rag add "$GR_DIR/docs/auth.md" --db-path "$GR_DIR"
```

**Run.** Query for a term in the note:

```bash
./bin/go-rag query "authentication" --db-path "$GR_DIR"
```

**Expected.** The hit covering the links renders a wikilinks line containing `JWT tokens`, `RBAC`, `phantom` — with `RBAC` appearing **once** (alias `[[RBAC|role access]]` canonicalised to `RBAC`, de-duplicated), the `![[diagram.png]]` embed **absent**, and the dangling `[[phantom]]` included verbatim. (contracts/api.md §Field contract.)

## Scenario 2 — cross-transport parity

**Run** the same query over all four transports against the same chunk and diff the `wikilinks` value:

```bash
# Start an isolated daemon (non-default ports, isolated DB)
./bin/go-rag start --db-path "$GR_DIR" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2

# REST
curl -s "http://127.0.0.1:17879/v1/query?q=authentication" | jq '.hits[0].wikilinks'
# gRPC (grpcurl) and MCP (JSON-RPC) equivalents; CLI:
./bin/go-rag query "authentication" --db-path "$GR_DIR"   # already shown in Scenario 1

./bin/go-rag stop --db-path "$GR_DIR"
```

**Expected.** All four return the identical `wikilinks` list for the same chunk (FR-009). This is enforced in-code by `internal/engine/parity_test.go` (extended for `wikilinks`).

## Scenario 3 — `GetChunk` carries wikilinks (spec 035 surface)

```bash
CHUNK_ID=$(./bin/go-rag query "authentication" --db-path "$GR_DIR" --json | jq -r '.hits[0].chunk_id')
./bin/go-rag chunk get "$CHUNK_ID" --db-path "$GR_DIR"
# REST equivalent: curl "http://127.0.0.1:17879/v1/chunks/$CHUNK_ID"
```

**Expected.** The fetched chunk carries the same `wikilinks` value as the query hit (FR-009 — the field rides on the chunk, not just the hit).

## Scenario 4 — chunk-scoped (FR-005)

**Setup.** A document where different paragraphs link to different notes:

```bash
cat >"$GR_DIR/docs/scoped.md" <<'EOF'
# Intro

This mentions [[alpha]] only here.

# Later

This section mentions [[beta]] only here.
EOF
./bin/go-rag add "$GR_DIR/docs/scoped.md" --db-path "$GR_DIR"
```

**Expected.** The chunk(s) covering "Intro" list `alpha` (not `beta`); the chunk(s) covering "Later" list `beta` (not `alpha`). No link appears on two chunks.

## Scenario 5 — non-markdown sources and empty case (FR-007/FR-008)

```bash
echo "plain text with no wikilinks at all" > "$GR_DIR/docs/notes.txt"
./bin/go-rag add "$GR_DIR/docs/notes.txt" --db-path "$GR_DIR"
./bin/go-rag query "plain text" --db-path "$GR_DIR"
```

**Expected.** The `.txt` hit omits the wikilinks line entirely (absent — never `null`/error). Same for a markdown chunk that simply has no `[[…]]`.

## Scenario 6 — pre-feature chunk degrades gracefully; back-fill via Reprocess

```bash
# A chunk ingested before this feature has Wikilinks == nil → omitted, no error
# (read an old chunk record → wikilinks absent). Back-fill by reprocessing:
./bin/go-rag reprocess "$GR_DIR/docs/auth.md" --db-path "$GR_DIR"
./bin/go-rag query "authentication" --db-path "$GR_DIR"
```

**Expected.** Before reprocess: wikilinks absent on the pre-feature chunk (no error). After `reprocess`: the chunk now carries the populated `wikilinks` value with its **chunk ID unchanged** (FR-010 — the field is non-identity).

## Scenario 7 — determinism (FR-006)

```bash
./bin/go-rag reprocess "$GR_DIR/docs/auth.md" --db-path "$GR_DIR"
./bin/go-rag reprocess "$GR_DIR/docs/auth.md" --db-path "$GR_DIR"
```

**Expected.** The `wikilinks` value for each chunk is byte-identical across both re-ingests (pure function of text + `linkTarget` + de-dup).

## Build & test gates

```bash
make build          # CGO_ENABLED=0 — must succeed
make vet            # go vet ./...
make lint           # golangci-lint run (CI gate; stricter than vet)
make test           # go test -race -cover ./...
```

**Must include:** `internal/reader/markdown_test.go` (wikilink grammar: alias, anchor, embed, dangling, de-dup, chunk-scope) and `internal/engine/parity_test.go` (`wikilinks` identical across CLI/REST/gRPC/MCP).

## Constitution affirmation (for the PR)

No on-disk key-space layout change. Additive `omitempty` JSON field on Chunk (prefix `0x03`); no migration; `migrate.ExpectedVersion` unchanged. Principles I–V all pass (see `plan.md` Constitution Check).
