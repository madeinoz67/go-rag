# Quickstart — Reranker Error Surfacing (H09)

> Phase 1 validation guide. Runnable scenarios that prove the feature works end-to-end.
> Implementation steps live in `tasks.md`; this is a *how to see it working* guide.
> Contract details: [contracts/query-response.md](contracts/query-response.md).
> Data shape: [data-model.md](data-model.md).

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0`.
- A built binary: `make build` → `./bin/go-rag`.
- An **isolated** vault (never target the default global vault — see CLAUDE.md "Smoke-test
  the daemon"). All commands below use `--db-path` on a temp dir.
- Optionally a local Ollama with a rerank model for the happy-path check; for the
  failure scenarios you do **not** need a working reranker.

## Setup — seed a small corpus

```bash
VAULT=$(mktemp -d)
./bin/go-rag add --db-path "$VAULT" /path/to/some/markdown/or/text/files
# confirm there is something to query
./bin/go-rag status --db-path "$VAULT"
```

## Scenario 1 — Rerank failure degrades gracefully + flags + logs (US1, FR-001/002/003/004)

Point the reranker at a dead endpoint so the rerank call fails, then query.

```bash
# rerank_model set, but the URL is unreachable → rerank error
go-rag config set --db-path "$VAULT" rerank_model "bge-reranker-base"
go-rag config set --db-path "$VAULT" ollama_url "http://127.0.0.1:9/nope"

# CLI: expect results on stdout AND a warning on stderr
./bin/go-rag query --db-path "$VAULT" --format json "search terms" 2>err.txt
#   err.txt contains: "warning: reranking failed; results are in fallback order ..."
#   the application log contains: "rerank failed: model=... candidates=N scores=M err=..."
#   stdout still contains a valid JSON result array (fallback-ordered)
```

**Pass when:** results are returned (not empty, not an error), the stderr warning is
present, the log line is present, and the log line contains **no** query text.

## Scenario 2 — Cross-transport parity (US2, FR-004, SC-003)

With the same dead-reranker vault, issue the same query over every transport and confirm
each reports the failure:

```bash
./bin/go-rag start --db-path "$VAULT" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
# wait for "daemon started", then:

# REST
curl -s http://127.0.0.1:17879/v1/query -d '{"query":"search terms","k":5}' | jq .rerank_failed
#   → true

# gRPC (via grpcurl, or the project's grpc client test)
grpcurl -plaintext -d '{"query":"search terms","k":5}' 127.0.0.1:17880 gorag.Gorag/Query | jq .rerankFailed
#   → true

# MCP (JSON-RPC tools/call go_rag_query) — response text begins with:
#   "⚠ reranking failed; showing fallback-ordered results ..."
```

**Pass when:** all three transports report the failure (REST `"rerank_failed":true`,
gRPC `rerankFailed:true`, MCP warning line). Repeat with a *healthy* reranker (or
`no_rerank=true`) → all three report **no** failure.

## Scenario 3 — Retrieval failure on the rerank path propagates as an error (FR-009, SC-006)

This exercises the sibling swallow. With a failing index/corpus state on the rerank path
(the unit test in `internal/index/retrieval_test.go` simulates this directly via a
retrieval that returns an error), a query must return a **non-nil error**, not silent
empty results. Verify via the unit test rather than a live repro (the condition is
internal):

```bash
go test ./internal/index/ -run SearchWithRerank -race -v
#   expect: a retrieval-error case returns (nil, false, err) — not (emptyHits, false, nil)
go test ./internal/engine/ -run Query -race -v
#   expect: engine.Query surfaces that error
```

**Pass when:** the retrieval-error test case asserts a propagated error.

## Scenario 4 — Optional retry recovers then degrades (US3, FR-006)

```bash
go-rag config set --db-path "$VAULT" rerank_retry_on_failure true
# against a flaky/empty reranker: query observes one retry; on 2nd failure → fallback + flag
go test ./internal/index/ -run RerankRetry -race -v
```

**Pass when:** with retry on, a transient failure is retried once (success →
`RerankFailed=false`; 2nd failure → `RerankFailed=true`); with retry off (default), no
retry occurs.

## Regression — no quality regression on the happy path (SC-005)

```bash
make test-eval
#   expect: recall@10 unchanged vs baseline when the reranker is healthy
```

**Pass when:** the eval harness reports no recall@10 regression attributable to this
change (the success path is observability-only).

## Cleanup

```bash
./bin/go-rag stop --db-path "$VAULT"   # or kill the background daemon by PID
rm -rf "$VAULT"
```
