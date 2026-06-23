# Phase 1 — Data Model: Observability (H17)

> Two in-process entities (MetricSample, TraceSpan) projected to `/metrics` and the local
> trace sink. No new persisted storage — metrics live in the OTel meter provider, traces
> in a capped in-process ring.

## Entities

### MetricSample  *(in-process — OTel meter provider)*

A named, labeled measurement aggregated in-process and projected to `/metrics` and
`status --metrics`. One source of truth: the meter provider.

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Prometheus-style `gorag_<name>` (see contracts/metrics.md) |
| `type` | enum `counter \| histogram \| gauge` | |
| `labels` | map | e.g. `{op, mode, status}`, `{cache}`, `{level}` |
| `value` | number | counter sum / histogram bucket counts / gauge point |

Aggregated by the OTel SDK (race-free across the async ingest workers + concurrent
queries). Histograms expose p50/p99 via bucket quantile estimation at read time
(`/metrics` and `status --metrics`).

### TraceSpan  *(in-process — capped ring)*

One timed operation within a trace.

| Field | Type | Notes |
|-------|------|-------|
| `trace_id` / `span_id` | string | OTel-assigned |
| `name` | string | `gorag.Query` / `gorag.Ingest` / sub-span (see contracts/metrics.md) |
| `start` / `duration` | time | wall-clock |
| `attributes` | map | op, mode, k, status, … |
| `status` | enum `ok \| error` | error spans carry a message |

Recent spans buffered in a capped ring (e.g. 256) backing an optional "last trace" view;
the full stream goes to the local stdout/file exporter (D2).

## Validation rules (from requirements)

- V1: every histogram bucket boundary MUST be positive + sorted (validated at instrument
  registration).
- V2: metric/trace writes MUST be race-free under concurrent queries + async ingest
  (OTel SDK guarantee; `-race` test).
- V3: the metric registry MUST be bounded — a fixed instrument set (no per-chunk or
  per-query cardinal labels); labels are low-cardinality (op/mode/status/level/cache).
- V4: trace ring MUST be capped; older spans evicted (bounded memory, FR-006).

## Config (FR-005 / D2)

`.go-rag/config.json` keys (all optional — safe defaults):

| Key | Default | Meaning |
|-----|---------|---------|
| `metrics_enabled` | `true` | expose `/metrics` on loopback |
| `otel_export` | `stdout` | trace exporter: `stdout` (local) \| `none` \| `otlp` (opt-in remote) |
| `otel_endpoint` | `""` | OTLP endpoint (used only when `otel_export=otlp`) |

`metrics_enabled=false` + `otel_export=none` ⇒ zero observability output (the off switch).

## State transitions

Metrics are monotonic (counters/histograms accumulate for the process lifetime; gauges
track current corpus size). Traces are append-then-evict (ring). Both reset on daemon
restart (local-first minimal; persisted history out of scope). No persistent state
machine — the only "state" is the in-process aggregation.

## Relationships

- `MetricSample` and `TraceSpan` are independent of the document/chunk model (orthogonal
  to Constitution II identity). They reference ops by label only, never by chunk content.
- H04/H06 tie-ins: `gorag_poison_flagged_total` and `gorag_cache_*` reuse counts already
  tracked by those features — the instruments read the same counters (no double-counting).
