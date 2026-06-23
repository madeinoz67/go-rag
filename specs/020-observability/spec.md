# Feature Specification: Observability — Metrics, Latency & Tracing

**Feature Branch**: `020-observability` *(spec directory; per project convention this
work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-06-23

**Status**: Draft

**Input**: "next backlog item" → **H17** from `RAG_BOOK_AUDIT_BACKLOG.md` (Phase 5,
next open item after H04): *"No observability/metrics/tracing. OTel spans around
`Engine.Query`/`Ingest`/`Migrate`; expose `/metrics` on loopback; `status --metrics`."*
(Audit §1.8 / §1.7; book §12.4 — *"OTel spans on embed→retrieve→generate, p50/p99,
retrieval-precision, error-rate alerts"*; App.C — *p50 < 1s, p99 < 3s, error < 1%*.)

**Why this matters**: go-rag ships changes blind — `bench_test.go` measures ns/op in
isolation, but there is no per-operation latency, error-rate, or trace from the live
engine. An operator cannot tell whether a query is slow, where the time goes, or
whether errors are climbing. H17 adds the production-monitoring surface the book
treats as non-optional (§9.5).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Operator visibility into live performance (Priority: P1)

Stephen runs the go-rag daemon and wants to see its real-world performance — query
and ingest latency (p50/p99), error rate, and throughput — without installing a cloud
service or letting go-rag phone home. A local `/metrics` endpoint (scraped by his own
Prometheus, or read directly) and `status --metrics` give him a live, air-gapped view.

**Why this priority**: the core ops gap — without it, every change ships blind and
production problems are invisible. The book calls this non-optional.

**Independent Test**: run a small workload against a daemon, then scrape `/metrics`
and run `status --metrics`; confirm query/ingest latency percentiles, error rate, and
counts are present and plausible.

**Acceptance Scenarios**:

1. **Given** a daemon that has served several queries, **When** the operator scrapes
   `/metrics`, **Then** query latency (p50/p99), ingest latency, error rate, and op
   counts are returned in a standard text format.
2. **Given** the same daemon, **When** the operator runs `status --metrics`, **Then**
   a human-readable summary of the same metrics is printed.
3. **Given** a fresh daemon, **When** no operations have run, **Then** `/metrics`
   reports zero-valued (not absent) metrics — the surface is always present.

---

### User Story 2 - Localizing a slow or failing operation with traces (Priority: P2)

When a query is unexpectedly slow or an ingest fails, Stephen wants a **trace** —
timed spans across the operation's stages (query: embed → retrieve → rerank; ingest:
read → store → embed) — written to a **local** sink, so he can see where the time went
or where it broke. No trace data leaves his machine unless he explicitly turns that on.

**Why this priority**: metrics show *that* something is slow; traces show *where*.
Distinct value, builds on US1.

**Independent Test**: run a query; confirm a trace with sub-spans appears in the local
trace sink, each span timed.

**Acceptance Scenarios**:

1. **Given** a daemon with tracing to the local sink, **When** a query runs, **Then**
   a trace is emitted with spans for the query and its key sub-stages (embed, retrieve,
   rerank), each carrying a duration.
2. **Given** a failing ingest, **When** the operator inspects the local trace, **Then**
   the failing stage is identifiable from the span/error annotation.

---

### User Story 3 - Integration with existing local monitoring (opt-in remote) (Priority: P3)

Stephen already runs Prometheus and Jaeger locally. He wants to scrape go-rag's
`/metrics` and, optionally, forward traces to his local collector — but **only because
he explicitly configured it**. By default go-rag emits nothing to the network; remote
export is an explicit opt-in, exactly like the H04 threat-list import boundary.

**Why this priority**: rounds out integration; the air-gap default is the security
gate, not a nice-to-have.

**Independent Test**: with remote export unset, assert zero outbound connections; with
it explicitly configured to a local collector, confirm traces/metrics flow there only.

**Acceptance Scenarios**:

1. **Given** a daemon with default config, **When** it runs under a network monitor,
   **Then** there is **zero** telemetry egress (air-gap, Constitution I).
2. **Given** the operator has explicitly configured a remote exporter, **When** ops
   run, **Then** metrics/traces flow to that configured endpoint only — and stop the
   moment the config is removed.

---

### Edge Cases

- **Air-gap invariant**: zero telemetry egress unless remote export is explicitly
  configured — verifiable by a test asserting no outbound connections in steady state
  (Constitution I; same pattern as the H04 threat-import test).
- **Long-running daemon**: metrics must not grow unbounded — fixed-memory histograms/
  counters and a capped trace ring buffer; 10K queries must not inflate memory.
- **One-shot CLI**: a single `go-rag query` has no long-lived process — the daemon
  aggregates metrics; the one-shot CLI may emit a per-run trace to the local sink but
  does not expose `/metrics`.
- **Instrumentation overhead**: must stay well inside the latency budgets (<500ms
  hybrid query, <10ms write-ACK) — instrumented paths must not regress SC-006/eval.
- **Cold start / restart**: in-process metrics reset on restart by default (local-first
  minimal); a persisted snapshot is out of scope unless the operator needs continuity.
- **Concurrency**: metric updates from the async ingest workers + concurrent queries
  must be race-free (the daemon is single-writer for storage, but metrics are
  multi-writer).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST record latency for each `Query`, `Ingest` (Add/Scan/
  Reprocess/Migrate) operation as p50 and p99 percentiles, plus operation counts and
  an error rate, aggregated in-process.
- **FR-002**: The system MUST expose a `/metrics` endpoint on loopback serving the
  metrics in a standard text-scrape format (Prometheus), readable by a local
  collector or `curl`.
- **FR-003**: `status --metrics` MUST surface a human-readable summary (p50/p99 per
  op, error rate, counts) on every transport that surfaces status.
- **FR-004**: The system MUST emit distributed-tracing spans around `Query` (with
  sub-spans for embed, retrieve, rerank), `Ingest` (read, store, embed), and
  `Migrate`, written to a **local** sink (file/stderr) by default.
- **FR-005**: Remote metric/trace export (e.g. OTLP to a collector) MUST be **opt-in
  only** — disabled by default, enabled solely by explicit user configuration. There
  MUST be zero background telemetry egress otherwise (Constitution I, air-gap).
- **FR-006**: Metrics and traces MUST be **bounded** — fixed-memory aggregation and a
  capped trace ring buffer — so a long-running daemon's observability footprint does
  not grow unbounded.
- **FR-007**: Instrumentation overhead MUST stay under ~1% of operation latency and
  MUST NOT breach the existing budgets (<500ms hybrid query, <10ms write-ACK)
  (Constitution IV).
- **FR-008**: The system MUST document the metric/span inventory and the air-gap
  boundary (local-default, opt-in remote) so an operator knows exactly what is
  measured and what never leaves the host.

### Key Entities *(include if feature involves data)*

- **MetricSample**: a named, labeled measurement — latency histogram, counter, or gauge
  (labels: operation, mode, status). Aggregated in-process and projected to `/metrics`
  and `status --metrics`.
- **TraceSpan**: one timed operation within a trace (a top-level op or a sub-stage),
  with a name, start/duration, and optional error annotation; buffered in a capped
  ring and written to the local sink.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a workload, `/metrics` returns query p50/p99 latency, ingest
  latency, error rate, and op counts — verifiable by scraping the endpoint and
  asserting the values are present and in a plausible range.
- **SC-002**: A query produces a trace with sub-spans (embed/retrieve/rerank) in the
  local trace sink, each timed — verifiable end-to-end.
- **SC-003**: **Zero telemetry egress** in steady state — verifiable by a test
  asserting no outbound connections except when remote export is explicitly enabled
  (Constitution I).
- **SC-004**: Instrumentation adds <1% overhead to query latency and does not breach
  the <500ms hybrid / <10ms ACK budgets (Constitution IV); `make test-eval` recall@10
  unchanged.
- **SC-005**: A daemon serving ~10K queries holds observability memory bounded (no
  unbounded growth) — verifiable by a load test asserting stable memory.
- **SC-006**: `go build ./...`, `go vet ./...`, `go test ./...` green; no retrieval
  regression.

## Assumptions

- **Air-gap posture (established, not re-asked)**: observability is **local by
  default** — `/metrics` is scraped by the user's own collector, spans are logged
  locally; remote export (OTLP) is opt-in/explicit only. This mirrors the H04
  threat-import boundary and is mandated by Constitution I; the user has confirmed
  this posture twice. Correctable if the user wants push-by-default (which would
  require a Constitution I amendment).
- **Targets (book App.C / §12.4)**: query p50 < 1s, p99 < 3s; error rate < 1%. The
  existing budgets are tighter (<500ms hybrid), so these are loose ceilings.
- **Metrics format**: Prometheus text for `/metrics` (de-facto standard; whether via
  the OTel metrics SDK or a hand-rolled emitter is a PLAN decision per Constitution
  III — the OTel SDK is Apache-2.0 pure-Go and allowed, but the project's
  minimal-dependency ethos may favor hand-rolling, as spec 016 did for its cache).
- **Tracing**: spans around the three top-level ops + key sub-spans; local file/stderr
  sink by default; sampling off by default (low local volume).
- **In scope**: latency/count/error metrics, `/metrics`, `status --metrics`,
  local-default tracing, opt-in remote export, bounded footprint, overhead budget.
- **Out of scope**: a built-in dashboard UI, alerting (the user's Prometheus handles
  alerts), persistent metric history across restarts (local-first minimal), and any
  cloud/managed monitoring service (Constitution I).
- **Constitution gates**: I (air-gap — no telemetry egress by default), III (pure-Go
  deps only; OTel SDK is permitted, hand-roll is permitted), IV (overhead within
  latency budgets), V (surface consistently across transports).
