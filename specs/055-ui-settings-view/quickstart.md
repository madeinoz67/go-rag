# Quickstart — Settings View (Slice 0, spec 055)

> Runnable validation that the slice works end-to-end. Mirrors the 054
> Observability quickstart shape.

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`
- A scratch vault (do NOT point at the global vault): `export GR=/tmp/gorag-settings-$$`
- Optional: local Ollama + an embedding model, for non-zero embedding values.

## Setup

```sh
mkdir -p $GR
./bin/go-rag init --db-path $GR
# seed one doc so embedding/dim are non-zero (optional, needs Ollama):
#   ./bin/go-rag add --db-path $GR README.md
./bin/go-rag start --db-path $GR \
  --mcp-addr 127.0.0.1:17878 \
  --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 \
  --ui-addr 127.0.0.1:17881
```

The first login creates the admin user; capture the Bearer session token.

## Validate US1 + US2 (parity)

1. `curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:17881/api/settings | jq`
   → returns grouped `retrieval` / `embeddings` / `cache` / `chunking` / `redaction`.
2. `./bin/go-rag status --db-path $GR` → every JSON value matches the status output
   (SC-002: zero discrepancies). In particular: `rrf_k`, `pool_size`, cache caps +
   hit stats, embedding model/dim/convention, chunk size/overlap.
3. Browser (Interceptor): open the console → Settings → a real panel renders (NOT
   the "planned" placeholder) with the five sections; Memory & Graph still shows
   "blocked".

## Validate US3 (boundary)

4. `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:17881/api/settings`
   (no bearer) → `401`.
5. `go test ./internal/ui/ -run 'Settings|Sidebar|Placeholder' -race` → green; the
   placeholder set is exactly `{memory-graph}`.

## Teardown

```sh
for p in 17878 17879 17880 17881; do lsof -ti :$p | xargs kill -9; done
rm -rf $GR
```
