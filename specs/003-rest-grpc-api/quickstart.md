# Quickstart: Multi-Transport Server APIs (Validation Guide)

**Feature**: 003-rest-grpc-api | **Date**: 2026-06-20

Runnable scenarios that prove the feature works end to end. This is a validation
guide — implementation steps live in `tasks.md`. References
[data-model.md](data-model.md) and [contracts/](contracts/) rather than
duplicating them.

## Prerequisites

- Go 1.26.4, `CGO_ENABLED=0` toolchain.
- A local Ollama with an embedding model (e.g. `nomic-embed-text`) at
  `http://localhost:11434`.
- An initialized go-rag database with at least one ingested file:
  ```bash
  go-rag init --model nomic-embed-text
  go-rag add ./some-docs/
  ```

## Build gate (run first, must be green)

```bash
make build     # CGO_ENABLED=0 → ./bin/go-rag
make vet
make test      # go test -race -cover ./...
```

`CGO_ENABLED=0 go build ./...` must succeed — confirms no CGo crept in via the
new gRPC dependency (Principle III).

## Scenario 1 — Start the multi-transport server

```bash
go-rag start --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 --grpc-addr 127.0.0.1:7880
```

**Expected**: `go-rag started (pid <n>)` and the daemon serves MCP/REST/gRPC on
7878/7879/7880. Verify each listener:

```bash
curl -s http://127.0.0.1:7879/health        # REST health → {"ok":true,...}
curl -s http://127.0.0.1:7878/mcp/health    # existing MCP health → ok
# gRPC health via grpcurl or the in-process grpc-go client (see Scenario 4)
```

A second `go-rag start` against the same DB **must fail** with a single-instance
error (FR-011).

## Scenario 2 — Transport equivalence (the core guarantee)

Run the same query over REST, gRPC, and MCP; assert identical structured results.

```bash
# REST
curl -s -X POST http://127.0.0.1:7879/v1/query \
  -H "Authorization: Bearer $(cat .go-rag/mcp.token)" \
  -d '{"query":"how does authentication work","k":5,"mode":"hybrid"}'
```

```bash
# gRPC (grpcurl against the grpc-go server)
grpcurl -plaintext -d '{"query":"how does authentication work","k":5}' \
  127.0.0.1:7880 gorag.Gorag/Query
```

**Expected**: identical `hits` (same chunk_ids, scores, file_paths) across REST,
gRPC, and MCP. The parity test in `internal/engine` automates this assertion
(research R6).

## Scenario 3 — Cross-transport read-after-write

```bash
# Ingest over REST
curl -s -X POST http://127.0.0.1:7879/v1/add \
  -H "Authorization: Bearer $(cat .go-rag/mcp.token)" \
  -d '{"path":"./new-docs/"}'
# → {"new":N,"skipped":M,"errors":0}   (ACKs immediately; indexes async)

# Immediately query over gRPC for content just added
grpcurl -plaintext -d '{"query":"<unique phrase from new-docs>"}' \
  127.0.0.1:7880 gorag.Gorag/Query
```

**Expected**: the newly added document is retrievable over gRPC right away
(FR-003). Re-adding the same path over any transport returns `new:0, skipped:N`
(idempotency, FR-007).

## Scenario 4 — Automated adapter + parity tests

```bash
go test -race ./internal/rest/...      # httptest against the REST adapter
go test -race ./internal/grpc/...      # in-process grpc-go client/server
go test -race ./internal/engine/...    # facade + cross-transport parity table
```

**Expected**: all green, including the parity test that runs each operation
through all three adapters and asserts identical structured results.

## Scenario 5 — Graceful shutdown

```bash
go-rag stop    # SIGTERM → drain → fsync → exit
```

**Expected**: in-flight requests drain, the process exits within the shutdown
budget (~5s, as in `serve.go`), and no data is lost; the next `start` reopens
cleanly (Pebble WAL recovery). Restart and confirm the corpus is intact via
`GET /v1/status`.

## Done when

- Scenarios 1–5 pass manually.
- `make build`, `make vet`, `make test` green with `CGO_ENABLED=0`.
- The parity test (Scenario 4) proves REST = gRPC = MCP for every shared
  operation.
