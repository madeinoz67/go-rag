# Observability — Metrics & Tracing (H17 / spec 020)

go-rag exposes production observability via **OpenTelemetry** (metrics + traces),
matching the book §12.4 ("OTel spans on embed→retrieve→generate, p50/p99,
error-rate"). The decisive constraint is **Constitution I (air-gap)**: telemetry is
**local by default**; remote export is opt-in only.

## What's exposed

- **`/metrics`** (Prometheus text, loopback, scraped — never pushed): per-op latency
  (p50/p99 via histograms), op counts, error rate, and tie-in counters. See
  [`specs/020-observability/contracts/metrics.md`](../specs/020-observability/contracts/metrics.md)
  for the full `gorag_*` inventory.
- **Traces** (OTel spans): `gorag.Query` / `.Ingest` / `.Migrate` (+ attributes) to a
  **local** sink (stderr) by default.
- **`status`** (MCP): an `obs:` line reporting the metrics/traces state.

## Air-gap boundary (Constitution I)

- `/metrics` is a **pull** endpoint — go-rag makes **zero** outbound connections for it
  (your Prometheus scrapes it).
- Traces default to the **local stderr** exporter.
- **OTLP** remote export (the only telemetry egress) is constructed **only** when you
  set `otel_export=otlp` + `otel_endpoint`. Otherwise nothing dials out.
- An air-gap test (`TestObserve_AirGap_OTLPOptIn`) asserts: default config ⇒ zero
  outbound requests; OTLP ⇒ traffic only to the configured endpoint. Same posture as
  H04's threat-list import.

## Configuration (`.go-rag/config.json`)

| Key | Default | Meaning |
|-----|---------|---------|
| `metrics_enabled` | `true` | expose `/metrics` (loopback) |
| `otel_export` | `stdout` | trace exporter: `stdout` (local) \| `none` \| `otlp` (opt-in remote) |
| `otel_endpoint` | `""` | OTLP `host:port`; used only when `otel_export=otlp` |

## Using it

```bash
go-rag start ...                                  # daemon inits OTel (metrics on, traces→stderr)
curl -s 127.0.0.1:7879/metrics | grep gorag_     # scrape (e.g. query latency, op counts)
# Opt-in remote tracing (local collector):
go-rag config set otel_export otlp
go-rag config set otel_endpoint localhost:4318
```

## Architecture

`internal/observe` is the **only** package that imports the OTel SDK/exporters. The
engine calls the small helper API (`observe.StartSpan`, `RecordQuery`,
`MetricsHandler`) — keeping OTel vendor coupling in one place (Constitution V, mirrors
`internal/poison`). OTel is pure-Go, Apache-2.0 (Constitution III — user-authorized,
`CGO_ENABLED=0`).

## Latency targets (book App.C / §12.4)

Query p50 < 1s, p99 < 3s; error rate < 1%. go-rag's own budgets are tighter (<500ms
hybrid query), so these are loose ceilings. Instrumentation overhead is bounded
(<1%; spans ~µs, metric records atomic-friendly) and stays off the <10ms write-ACK
path (Constitution IV).
