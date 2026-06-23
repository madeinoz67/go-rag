# Phase 0 — Research: Observability (H17)

> Resolves the spec's open technical questions. The user direction — **"use OTel as
> it's a standard approach, align to best practice metrics"** — locks the primary fork.

## D1 — OTel vs hand-rolled (the user-locked decision)

**Decision**: adopt **OpenTelemetry** (`go.opentelemetry.io/otel`) for **both** traces
and metrics, with `/metrics` exposed via the OTel **Prometheus exporter**.

**Rationale (user direction + best practice)**: OTel is the CNCF standard for
vendor-neutral telemetry — one SDK for traces + metrics, the book §12.4 explicitly calls
for OTel spans, and the OTel Prometheus exporter yields a standard Prometheus-format
`/metrics` surface that any local collector scrapes. This is "best practice" in 2026:
unified OTel SDK + Prometheus-compatible scrape.

**Constitution III**: the OTel modules are **pure-Go, Apache-2.0** — permitted (the
constitution explicitly allows permissive pure-Go libs: cobra, pebble, chromem-go). This
is the first non-trivial new dep family since chromem-go; **user-authorized**. Must
`CGO_ENABLED=0 go build ./...` cleanly after `go mod tidy`.

**Alternatives rejected**:
- **Hand-rolled** (spec 016's no-dep ethos): rejected by the user ("use OTel"). OTel also
  gives correct histogram-bucket/p50-p99 math, semantic conventions, and exporter
  interop for free — non-trivial to hand-roll correctly.
- **`prometheus/client_golang` for metrics + OTel for traces**: considered (the
  historically-common Go split). Rejected to keep **one** telemetry SDK (simpler mental
  model, unified resource/service attribution, no two metric systems). The OTel
  Prometheus exporter gives the same Prometheus-format `/metrics`.

## D2 — Exporter strategy & the air-gap boundary (Constitution I — decisive)

**Decision**: telemetry is **local by default**; remote export is **opt-in**.

| Channel | Default | Opt-in |
|---|---|---|
| `/metrics` (metrics) | **on**, loopback, Prometheus-format, **scraped** (never pushed) | n/a (it's a pull endpoint; always local) |
| Traces | **stdout/file exporter** (local) | OTLP exporter to a user-named endpoint |
| Metrics (push) | off | OTLP exporter (opt-in) |

**Rationale**: OTel's *default* in most tutorials is an OTLP exporter (network egress) —
which would **violate** go-rag's air-gap (Constitution I: "no network egress for any core
operation"). So the default trace exporter is the **local stdout/file** exporter; the
OTLP exporter is constructed **only** when the user sets `otel_export=otlp` +
`otel_endpoint`. This is the same explicit-egress posture as H04's `threat import` — and
the air-gap test (D5) asserts zero outbound connections unless OTLP is explicitly enabled.

**`/metrics` is pull, not push**: a Prometheus-format endpoint scraped by the user's own
collector is local-first by construction (the user dials go-rag, not vice-versa). It stays
on loopback with the REST transport.

## D3 — Metric inventory (best-practice `gorag_*` names, OTel semantic conventions)

Instruments defined in `internal/observe/metrics.go`, exported via the OTel Prometheus
exporter (`gorag_<name>`). Best-practice Prometheus naming (snake_case, units suffix on
histograms, `_total` on counters):

| Metric | Type | Labels | Source |
|---|---|---|---|
| `gorag_query_duration_seconds` | histogram | mode, status | Query |
| `gorag_ingest_duration_seconds` | histogram | op (add\|scan\|reprocess\|migrate), status | Add/Scan/Reprocess/Migrate |
| `gorag_operations_total` | counter | op, status | every op |
| `gorag_query_results` | histogram | mode | Query (top-k returned) |
| `gorag_chunks_indexed_total` | counter | — | Ingest |
| `gorag_poison_flagged_total` | counter | level | H04 tie-in |
| `gorag_cache_hits_total` / `gorag_cache_misses_total` | counter | cache (result\|embedding) | H06 tie-in |
| `gorag_documents` / `gorag_chunks` | gauge | — | corpus size (updated on Status) |

Histogram buckets: tuned to the latency budgets (e.g. query `[0.005, 0.01, 0.025, 0.05,
0.1, 0.25, 0.5, 1, 2.5, 5]`s; ingest `[0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5]`s) so
p50/p99 land inside the book targets (p50<1s, p99<3s).

## D4 — Span inventory (OTel traces, local sink)

Tracer named `gorag`. Top-level spans carry op attributes; sub-spans localize cost:

- **Query** (`gorag.Query`, attrs: mode, k) → sub-spans: `embed` (query embedding),
  `retrieve` (FTS + vector + RRF fusion), `rerank` (when enabled). Error/fallback (H09
  RerankFailed) recorded as a span event/status.
- **Ingest** (`gorag.Ingest`, attrs: op) → sub-spans: `read` (FileReader), `store`
  (sync ACK), `embed` (async — span ends when the job drains).
- **Migrate** (`gorag.Migrate`).

Spans write to the **local** stdout/file exporter by default (D2). A capped in-process
ring of recent traces backs `status --metrics`'s "last trace" line (optional, US2 depth).

## D5 — Air-gap test (SC-003, reusing the H04 pattern)

A canary `httptest.Server` stands in for "the network." With default config, run
Query/Ingest/Migrate/Rescan + scrape `/metrics` and assert the canary receives **zero**
requests. With `otel_export=otlp` + `otel_endpoint=<canary>`, assert the canary receives
OTLP traffic **only**. This is the SC-003 proof and directly mirrors H04's
`TestThreat_Import_URL_AirGap`.

## D6 — `status --metrics` surface

`status --metrics` reads the OTel meter provider's manual reader (a one-shot collection)
and renders p50/p99 (computed from the histogram snapshot), op counts, and error rate as
text — identical numbers to what `/metrics` exposes (single source of truth: the meter
provider). Surfaced on CLI/MCP `status`; the REST/gRPC status DTOs gain an optional
metrics summary block.

## D7 — Daemon lifecycle

`internal/daemon` (or cmd boot) calls `observe.Init(cfg)` at startup (builds the
tracer/meter providers + exporters per config) and `observe.Shutdown(ctx)` on stop
(flushes batches). One-shot CLI commands (e.g. `go-rag query`) init a short-lived
provider that emits its trace to the local sink and exits (no `/metrics`, no daemon state).

## Resolved unknowns → spec FR mapping

| Spec item | Resolved by |
|---|---|
| FR-001 latency p50/p99 | D3 (histograms) + D6 |
| FR-002 `/metrics` | D2 (Prometheus exporter, loopback) |
| FR-003 `status --metrics` | D6 |
| FR-004 spans + sub-spans | D4 |
| FR-005 opt-in remote (air-gap) | D2 + D5 |
| FR-006 bounded | D3/D4 (in-process SDK; capped ring) |
| FR-007 overhead <1% | in-process OTel spans; benchmark SC-004 |

**All NEEDS CLARIFICATION resolved (the spec had none; the user direction resolved D1).**
Ready for Phase 1.
