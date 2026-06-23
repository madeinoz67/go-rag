# Implementation Plan: Observability — Metrics, Latency & Tracing

**Branch**: `020-observability` *(single-author repo — commits directly to `main`)* | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature spec from `/specs/020-observability/spec.md` — backlog item **H17**. User direction: **use OpenTelemetry** (the standard), align metrics to best practice (Prometheus-format `/metrics`).

## Summary

Add a production-monitoring surface (book §12.4/§9.5): per-operation latency (p50/p99),
counts, and error rate as **OpenTelemetry metrics** exposed via a Prometheus-format
`/metrics` endpoint (loopback, scraped — not pushed), plus **OpenTelemetry traces**
(spans around `Query`/`Ingest`/`Migrate` + key sub-spans) to a **local** sink by default.
Remote export (OTLP) is **opt-in only** — zero telemetry egress by default (Constitution I,
mirroring the H04 threat-import boundary). One new pure-Go dependency family
(`go.opentelemetry.io/otel` + SDK/exporters — Apache-2.0, user-authorized).

## Technical Context

**Language/Version**: Go 1.22+ (pure Go, `CGO_ENABLED=0`).

**Primary Dependencies**: existing + **new** — `go.opentelemetry.io/otel`,
`otel/sdk`, `otel/sdk/metric`, `otel/exporters/prometheus` (`/metrics`),
`otel/exporters/stdout/stdouttrace` (local default), `otel/exporters/otlp/otlptrace`
(opt-in remote). All pure-Go, Apache-2.0 (Constitution III — user-authorized addition).

**Storage**: no new persistence — metrics are in-process (OTel meter provider + manual
reader for `status`); traces buffered in a capped local ring. (Persisted metric history
across restarts is out of scope.)

**Testing**: `go test -race -cover ./...`; an observe-package test asserting the
instruments are registered + a `/metrics` scrape returns the expected families; an
**air-gap test** (zero egress except explicit OTLP) reusing the H04 pattern; an
instrumentation-overhead check (<1%, no latency-budget regression).

**Target Platform**: single static binary, local-first.

**Project Type**: CLI + multi-transport daemon (MCP/REST/gRPC) over one Engine.

**Performance Goals**: instrumentation <1% overhead; existing budgets preserved
(<500ms hybrid query, <10ms write-ACK — Constitution IV).

**Constraints**: air-gap (no telemetry egress by default — Constitution I); bounded
observability memory; loopback-only `/metrics`.

**Scale/Scope**: local single-user daemon; metrics multi-writer (async ingest workers +
concurrent queries) — race-free aggregation via the OTel SDK.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First, Single-Binary | ✅ PASS | `/metrics` is **scraped** on loopback (not pushed); traces default to a **local** stdout/file exporter; OTLP remote export is **opt-in** only (a config flag). Zero telemetry egress by default — same air-gap posture as H04's threat-import. No cloud/managed monitoring. |
| II | Content-Addressed Identity | ✅ PASS (N/A) | Observability is orthogonal to document identity; no change to SHA-256 identity or change-detection. |
| III | Pure Go — No CGo, No Runtime | ✅ PASS (new dep, user-authorized) | `go.opentelemetry.io/otel` + SDK/exporters are **pure-Go, Apache-2.0** — permitted by III (which explicitly allows cobra/pebble/chromem-go). This is the first non-trivial new dep since chromem-go; **user-authorized** ("use oTel"). Logged, not a violation. Must `go mod tidy` cleanly with `CGO_ENABLED=0`. |
| IV | Async-After-ACK Writes | ✅ PASS | Instrumentation is bounded, non-blocking overhead (OTel spans are in-process, ~µs; metric records are atomic-friendly). It is observation-class, not indexing-class — stays off the ACK-critical path; <1% overhead (SC-004). |
| V | Extension by Interface, MCP-First | ✅ PASS | New self-contained `internal/observe` package owns all OTel wiring (engine imports it, not OTel directly where avoidable); `/metrics` + `status --metrics` surfaced consistently. |

**No violations → Complexity Tracking table intentionally empty.** Principle III is the
notable one: a new, user-authorized dependency — permitted (pure-Go, permissive), logged.

## Project Structure

### Documentation (this feature)

```text
specs/020-observability/
├── plan.md              # This file
├── research.md          # Phase 0 — OTel decision, exporter strategy, metric/span inventory
├── data-model.md        # Phase 1 — MetricSample / TraceSpan + state
├── quickstart.md        # Phase 1 — runnable validation (scrape /metrics, read a trace)
├── contracts/
│   └── metrics.md       # Phase 1 — metric + span inventory (the /metrics surface)
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/
├── observe/             # NEW — all OTel wiring (keeps OTel imports in one place)
│   ├── otel.go          #   TracerProvider + MeterProvider + exporter setup (local default, OTLP opt-in)
│   ├── metrics.go       #   instrument definitions (histograms/counters/gauges, gorag_* names)
│   ├── spans.go         #   StartSpan helper + span naming
│   └── prometheus.go    #   /metrics HTTP handler (OTel prometheus exporter)
├── engine/              # MODIFY — instrument Query/Add/Scan/Reprocess/Migrate (+ Query sub-spans)
├── rest/                # MODIFY — mount GET /metrics (loopback, unauth, like /health)
├── config/              # MODIFY — otel_export (none|stdout|otlp), otel_endpoint, metrics_enabled
├── cli/                 # MODIFY — status --metrics (read the meter); daemon boot wires/shuts observe
├── grpc/ , mcp/         # MODIFY — status --metrics projection (metrics summary in status)
└── daemon/              # MODIFY — init observe providers at boot; drain/shutdown on stop
```

**Structure Decision**: one new self-contained `internal/observe` package owns every OTel
import (provider/exporter setup, instrument defs, the `/metrics` handler). The engine
imports only `observe`'s small helper API (`observe.StartSpan`, `observe.RecordQuery(...)`),
keeping OTel vendor coupling in one place and the engine close to its current shape. Mirrors
how `internal/poison` isolated the detector. `/metrics` mounts on the existing REST loopback
mux alongside `/health` (no new port by default).

## Complexity Tracking

> Empty — no Constitution violations to justify. (Principle III's new OTel dep is
> permitted + user-authorized, logged in the Constitution Check above.)
