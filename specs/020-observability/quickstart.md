# Phase 1 — Quickstart: Observability (H17)

> Runnable validation that the observability surface works end-to-end. Implementation
> detail belongs in `tasks.md`; this is a run/validate guide. Run on an **isolated** DB
> (`--db-path <tmp>` + non-default transport addrs) — never against the live daemon.

**Prerequisites**: `make build` succeeds; a local Ollama with an embed model is needed
for ingest (the daemon path). `/metrics` and tracing work without Ollama (a query that
returns nothing still records a span + a `status=ok` sample).

## Scenario 1 — `/metrics` reports latency + counts after a workload (US1, FR-001/002, SC-001)

```bash
VAULT=$(mktemp -d); DB=$VAULT/vault
./bin/go-rag init --db-path "$DB" >/dev/null
./bin/go-rag add ./testdata/golden/corpus/ --db-path "$DB"
# start the daemon (REST loopback) on non-default addrs
./bin/go-rag start --db-path "$DB" --mcp-addr 127.0.0.1:17878 \
                   --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2
# drive a few queries
for q in retrieval search document; do
  ./bin/go-rag query "$q" --db-path "$DB" --rest-addr 127.0.0.1:17879 >/dev/null || true
done
# scrape /metrics
curl -s http://127.0.0.1:17879/metrics | grep -E "gorag_query_duration_seconds|gorag_operations_total"
```

**Pass**: `gorag_query_duration_seconds_bucket{...,mode="hybrid"}` and
`gorag_operations_total{op="query",status="ok"}` are present with non-zero counts.

## Scenario 2 — `status --metrics` shows the human summary (US1, FR-003)

```bash
./bin/go-rag status --metrics --db-path "$DB"
```

**Pass**: a `metrics:` line with query p50/p99, ingest p50/p99, counts, and cache hit% —
matching the values from `/metrics` (single source of truth).

## Scenario 3 — A query produces a local trace with sub-spans (US2, FR-004, SC-002)

```bash
# default trace exporter is local (stdout/file); run one query and read the trace sink
./bin/go-rag query "retrieval" --db-path "$DB" --rest-addr 127.0.0.1:17879 >/dev/null
# the daemon's trace sink (default: a local file or stderr) contains:
#   gorag.Query (mode=hybrid, k=5) → embed → retrieve → [rerank]
tail -20 "$VAULT/trace.log"   # or wherever the local stdout exporter writes
```

**Pass**: a `gorag.Query` span with timed `embed`/`retrieve` sub-spans appears in the
local sink. (No query text in the span.)

## Scenario 4 — Air-gap: zero egress by default; OTLP only when configured (US3, FR-005, SC-003)

```bash
# default config: no OTLP. Run ops under a connection monitor; expect zero outbound.
# (The automated test runs this under a canary httptest server — see TestObserve_AirGap.)
./bin/go-rag status --metrics --db-path "$DB"   # confirms config: otel_export=stdout (local)
# Opt in: set otel_export=otlp + otel_endpoint=<local collector>; traces then flow there ONLY.
```

**Pass (automated)**: `TestObserve_AirGap` — default config ⇒ canary receives **0**
requests; with `otel_export=otlp`+endpoint ⇒ canary receives OTLP traffic; remove the
config ⇒ traffic stops. Mirrors H04's `TestThreat_Import_URL_AirGap`.

## Scenario 5 — Overhead: no latency-budget regression (FR-007, SC-004)

```bash
go test -bench=. ./internal/observe/...     # instrumentation overhead microbench
make test-eval                              # recall@10 unchanged (SC-006)
go test -race -cover ./...                  # whole-suite + race green
```

**Pass**: instrumentation adds <1% to query latency; eval recall@10 unchanged; full
suite + race green.

## Done definition for this feature

All five scenarios pass + `go build ./...`, `go vet ./...`, `go test -race -cover ./...`
green + `go.mod` tidies cleanly under `CGO_ENABLED=0` (Constitution III: OTel is pure-Go)
+ the air-gap test green + metric/span inventory documented in `contracts/metrics.md`.
