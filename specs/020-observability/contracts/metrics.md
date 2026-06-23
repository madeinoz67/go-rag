# Phase 1 — Interface Contract: `/metrics` + trace inventory (H17)

> go-rag exposes observability as a **scraped** Prometheus-format `/metrics` endpoint
> (loopback, unauthenticated — like `/health`) plus a local trace stream. Remote export
> is opt-in. This contract fixes the metric + span names so a scraper/operator knows
> exactly what's exposed (cross-tool stable).

## 1. The `/metrics` endpoint

| Aspect | Value |
|---|---|
| Path | `GET /metrics` |
| Transport | REST loopback (mounted alongside `/health` in `internal/rest`) |
| Auth | none (loopback-only; same as `/health`) |
| Format | Prometheus text exposition (`# HELP`, `# TYPE`, samples) |
| Push? | **No** — scraped by the user's collector; go-rag never dials out for metrics |

Scraped like:
```bash
curl -s http://127.0.0.1:7879/metrics | grep gorag_
```

## 2. Metric inventory (Prometheus names — the stable contract)

All names are `gorag_*` (service namespace). Cardinality is bounded (labels are
low-cardinality enums).

| Metric | Type | Labels | Semantics |
|---|---|---|---|
| `gorag_query_duration_seconds` | histogram | `mode`, `status` | Query wall-clock; buckets tuned to p50<1s/p99<3s (D3) |
| `gorag_ingest_duration_seconds` | histogram | `op`, `status` | Add/Scan/Reprocess/Migrate wall-clock |
| `gorag_operations_total` | counter | `op`, `status` | Op count (op: query\|add\|scan\|reprocess\|migrate; status: ok\|error) |
| `gorag_query_results` | histogram | `mode` | top-k returned per query |
| `gorag_chunks_indexed_total` | counter | — | chunks stored |
| `gorag_poison_flagged_total` | counter | `level` | H04 tie-in (level: suspicious\|quarantine) |
| `gorag_cache_hits_total` | counter | `cache` | H06 tie-in (cache: result\|embedding) |
| `gorag_cache_misses_total` | counter | `cache` | H06 tie-in |
| `gorag_documents` | gauge | — | current document count |
| `gorag_chunks` | gauge | — | current chunk count |

`status=error` covers embed/rerank/retrieval failures (H09 RerankFailed is recorded as
`status=ok` + a span event, since results are still returned).

## 3. Trace inventory (OTel spans → local sink)

Tracer: `gorag`. Spans carry attributes; sub-spans localize cost.

| Span | Attributes | Sub-spans |
|---|---|---|
| `gorag.Query` | `mode`, `k` | `embed` (query embedding), `retrieve` (FTS+vector+RRF), `rerank` |
| `gorag.Ingest` | `op` | `read`, `store` (sync ACK), `embed` (async) |
| `gorag.Migrate` | — | — |

Errors/fallbacks recorded as span status (`error`) or events (e.g. H09 rerank-failed).
No query text or chunk content is ever put on a span (privacy + the existing "never log
query text" posture from H09).

## 4. `status --metrics` surface

CLI: `go-rag status --metrics` → reads the meter provider's manual reader → renders:

```
metrics: query p50=23ms p99=140ms (n=512, err=0.0%), ingest p50=8ms p99=61ms (n=44),
         chunks_indexed=318, poison_flagged=2, cache result=87%/13%
```

Same numbers as `/metrics` (single source of truth: the meter provider). MCP
`go_rag_status` gains the same line; REST/gRPC `StatusResponse` gain an optional
`metrics` block (the summary above, structured).

## 5. Air-gap boundary (Constitution I, FR-005)

- `/metrics` is a **pull** endpoint (scraped) — go-rag makes **zero** outbound
  connections for it.
- Traces default to the **local** stdout/file exporter.
- OTLP remote export (traces + metrics push) is constructed **only** when
  `otel_export=otlp` + `otel_endpoint` are set. Otherwise: no exporter that dials out.
- An air-gap test (SC-003) asserts zero outbound connections in steady state, and OTLP
  traffic only when explicitly configured — mirroring H04's `TestThreat_Import_URL_AirGap`.
