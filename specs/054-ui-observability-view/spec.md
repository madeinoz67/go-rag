# Feature Specification: Observability View

**Feature Branch**: `054-ui-observability-view` *(spec directory; per project convention
this work commits directly to `main` — single-author repo, no feature branch.)*

**Created**: 2026-07-14

**Status**: Draft

**Input**: Principal picked "Observability view (spec 054)" as the next console slice.
go-rag already records structured **telemetry** (spec 020: OTel instruments → `/metrics`
Prometheus; `status --metrics`) and a structured **audit log** (spec 021: local JSONL,
query SHA-256-hashed, reader with type/time-window filters, `Engine.AuditRead`).
**Bridge Ops (049)** projects live health (`StatusInfo`) + a small recent-activity tail.
What is missing is a dedicated Observability surface that renders the *telemetry*
(latency / throughput / error) in-browser and provides a *full filterable audit-log
browser* for security forensics. No new engine capability, no new transport, no Node
chain.

## Context & Background

Two observability data surfaces already ship but are **invisible from the console**:

1. **Telemetry (spec 020)** — `internal/observe` registers OTel instruments
   (`RecordQuery`, `RecordIngest`, `RecordQueryResults`, `CacheHit`/`CacheMiss`,
   `PoisonFlagged`) with `prometheus.DefaultRegisterer`; `observe.MetricsHandler()`
   serves `/metrics` as Prometheus text. There is no structured snapshot reader, but
   `prometheus.DefaultGatherer().Gather()` returns structured `MetricFamily` — so a
   UI-layer handler can project the `gorag_*` instruments to JSON with **zero engine
   change**.
2. **Audit log (spec 021)** — `internal/audit` writes a local, append-only JSONL trail
   (query events carry a SHA-256 **hash**, never plaintext; ingest events carry path +
   counts, no content; auth-fail events carry transport, no credential).
   `audit.Read(path, ReadOptions)` filters by event type + time window; the engine
   already wraps it as read-only `Engine.AuditRead(vault, opts)` (added in spec 049).

**Bridge Ops (049)** already shows a live health snapshot (`StatusInfo`: embed backlog,
drift verdict, subsystem tiles, watch dirs) and a small **recent-activity tail**
(`Engine.AuditRead`, clamped). The Observability view deliberately does **not** duplicate
that: 049 answers *"is the system healthy right now + what just happened?"*; 054 answers
*"how is it performing over time + what did it do (filterable)?"*. The split is
telemetry + historical forensics (054) vs live health + operational tail (049).

This spec adds the UI: a new sidebar view ("Observability", the existing placeholder
slot) with two panels — **Telemetry** (counts, latency percentiles, error rate, cache
hit/miss as tiles + a small table) and **Audit Log** (a filterable, bounded browser over
the full audit trail). It reuses the spec 046 shell, the Alpine `goragApp` root, the
4-layer CSS, the spec 052 vault picker, and the spec 045 Bearer-session guard — all
unchanged.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See live telemetry in-browser (Priority: P1) 🎯 MVP

An operator opens **Observability** and sees the daemon's real-world performance without
spinning up Prometheus or scraping `/metrics`: query and ingest **op counts**, **latency**
(p50 / p95 / p99), **error rate**, and **cache hit/miss** — as tiles plus a small
per-operation table. Values reflect the same instruments `/metrics` exposes. A manual
refresh re-reads; the surface always renders (zero-valued for a cold daemon, never an
error).

**Why this priority**: the core visibility gap. The telemetry exists but is only reachable
as Prometheus text; this is the "how is it performing" surface the operator actually opens.

**Independent Test**: run a small mixed workload (a few queries + an ingest) against a
daemon; open Observability; the counts, latency percentiles, error rate, and cache ratio
are present and plausible; the values agree with `curl /metrics` and `go-rag status
--metrics` (zero drift).

**Acceptance Scenarios**:

1. **Given** a daemon that has served several queries and one ingest, **When** the operator
   opens Observability, **Then** query/ingest op counts, latency percentiles (p50/p95/p99),
   error rate, and cache hit/miss render as tiles + a table.
2. **Given** the telemetry panel, **When** compared to `curl /metrics`, **Then** the counts
   and latency buckets agree (the UI is a projection, not a parallel source).
3. **Given** a freshly started daemon (no operations yet), **When** Observability opens,
   **Then** zero-valued tiles render with a "no data yet" note — never an error.
4. **Given** the panel, **When** the operator clicks refresh, **Then** the values re-read
   from the live instruments.

### User Story 2 - Browse the full audit log with filters (Priority: P1)

An operator opens the **Audit Log** panel and browses the entire audit trail — not just a
recent tail — filtered by **event type** (`query` / `ingest` / `auth-fail`) and a **time
window**, bounded/paginated so a large log does not overwhelm. Each row shows what spec 021
mandates: a query row shows the **query hash** (never plaintext) + mode + top-k + hit count
+ status + timestamp; an ingest row shows path + outcome counts (no content); an auth-fail
row shows transport + timestamp (no credential). This is the security-forensics view
distinct from 049's live activity tail.

**Why this priority**: a log you can only tail is half a feature (spec 021 US2 still had no
UI). The filterable browser is the triage surface for "what did go-rag do, and when".

**Independent Test**: generate one event of each type (run a query, ingest a file, trigger
an auth failure); open the Audit Log; filter to `query` — only the query row shows; filter
to the last hour — all three show; assert the query plaintext appears **nowhere** in the
rendered DOM (only its hash).

**Acceptance Scenarios**:

1. **Given** a populated audit log, **When** the operator opens the Audit Log, **Then** the
   most recent bounded page of events renders, newest first, each typed correctly.
2. **Given** the panel, **When** the operator filters to `ingest` events in the last 24h,
   **Then** only matching ingest records show (path + counts, no content).
3. **Given** a query event row, **When** inspected, **Then** it shows the SHA-256 hash, mode,
   top-k, hit count, status, and timestamp — and the raw query string appears **nowhere** in
   the DOM.
4. **Given** an auth-fail row, **When** inspected, **Then** it shows transport + timestamp
   only — never the rejected credential.
5. **Given** a large log, **When** browsed, **Then** results are bounded/paginated (a fixed
   page size + older/newer, or a time-window narrowing) — the view does not attempt to
   render unbounded history.

### User Story 3 - Posture + retention context surfaced (Priority: P2)

The Observability view surfaces the **air-gap + privacy posture** as on-screen context so
the operator trusts what they see: a note that metrics are local-only (scraped by the
operator's own collector; zero egress), that the audit log is local + append-only with
query text hashed, and the configured retention/size cap. This is not a control — it is the
trust label that makes the two panels legible.

**Why this priority**: rounds the view out; the data is already trustworthy, the label makes
that visible. Distinct, lower priority than the two data panels.

**Independent Test**: open Observability; the posture note renders (local-only metrics,
hashed queries, local append-only audit, retention cap value); the retention value matches
the effective config (`EffectiveAuditLogMaxBytes`).

**Acceptance Scenarios**:

1. **Given** the Observability view, **When** the posture note is inspected, **Then** it
   states metrics are local-only (zero egress), audit is local + append-only, and queries
   are hashed (not stored).
2. **Given** the note, **When** the retention value is checked, **Then** it matches the
   effective audit-log size cap from config.

### User Story 4 - Vault-aware, shell-consistent, guarded, no Node (Priority: P2)

The Observability view carries the vault parameter (from the shell vault picker, spec 052):
the Audit Log reads the **active vault's** audit path; telemetry is process-wide (one
daemon serves all vaults per spec 052) and labelled as such. The view is gated by the
existing spec 045 Bearer guard, reuses the 046 shell + Alpine root + 4-layer CSS unchanged,
degrades gracefully on errors and empty states, and introduces **no** Node/build chain.

**Why this priority**: a hard invariant (vault-aware; guarded; single binary), not a feature.
P2 because the view is functional before the invariant is formally proven.

**Independent Test**: switch vaults via the picker; the Audit Log updates per-vault; attempt
the view unauthenticated — it routes to login; confirm no `package.json` / `node_modules`
introduced.

**Acceptance Scenarios**:

1. **Given** the vault picker, **When** the operator switches vaults, **Then** the Audit Log
   reflects the selected vault's audit trail.
2. **Given** an expired session, **When** a panel fetch returns 401, **Then** the shell
   routes to login (no crash, no silent failure).
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced (single `make build`).
4. **Given** the view, **When** telemetry is process-wide, **Then** it is labelled as such
   (not per-vault), so the operator is not misled.

---

### Edge Cases

- **Cold daemon** (no operations yet) — zero-valued telemetry tiles with a "no data yet"
  note, not an error.
- **Audit logging disabled** (`audit_log_enabled=false`) — the Audit Log panel shows a
  healthy "audit logging is off" state with the config hint, not an error or empty-table
  confusion.
- **Missing/empty audit file** — healthy empty state (mirrors `audit.Read` and
  `TestAuditRead_MissingLogIsEmpty`).
- **Very large audit log** — bounded page + time-window filter; never unbounded render.
- **Histogram with too few samples** — percentiles render `—` / "insufficient data" rather
  than a misleading number.
- **One-shot CLI context** — N/A; the view is daemon-served (a one-shot `go-rag query`
  exposes no `/metrics`); the view is only reachable behind the running daemon.
- **Disabled metrics** (if observability is off) — the Telemetry panel shows the off-state
  note, not zeroes-that-look-like-zero-traffic.
- **Mid-action session expiry** — graceful return to login.
- **Query plaintext leakage** — a query row must never place the raw query in the DOM, even
  as a tooltip or data-attribute (privacy, spec 021 FR-002).

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST render telemetry — query/ingest op counts (by status), latency
  percentiles (p50/p95/p99), error rate, and cache hit/miss — sourced from the registered
  `gorag_*` instruments via `prometheus.DefaultGatherer().Gather()` (structured gather), not
  by scraping/parsing `/metrics` text.
- **FR-002**: The view MUST provide an Audit Log browser over the full trail, filtered by
  event type (`query`/`ingest`/`auth-fail`) and time window, bounded/paginated, via the
  existing read-only `Engine.AuditRead`.
- **FR-003**: A query audit row MUST display the SHA-256 **hash** of the query and MUST
  NEVER place the raw query string anywhere in the rendered DOM.
- **FR-004**: An ingest row MUST show path + outcome counts with **no** content; an
  auth-fail row MUST show transport + timestamp with **no** credential.
- **FR-005**: Audit reads MUST target the active vault's audit path (vault param from the
  shell picker); telemetry is process-wide and MUST be labelled as such.
- **FR-006**: The view MUST be gated by the existing spec 045 Bearer guard — no new auth.
- **FR-007**: The view MUST ship inside the single binary via the embedded vendored SPA — no
  Node/build chain.
- **FR-008**: The view MUST render healthy states for cold metrics, disabled audit, and an
  empty/missing log — no silent failures, no error storms on zero state.
- **FR-009**: Telemetry values shown MUST agree with `/metrics` (zero drift) — the UI is a
  projection of the same instruments, never a parallel computation.
- **FR-010**: The view MUST NOT duplicate Bridge Ops (049) — distinct surface (telemetry +
  historical forensics here vs live health + operational tail there). The recent-activity
  tail stays in 049; 054 owns the filterable full-trail browser.
- **FR-011**: All data tables in the view MUST have sortable column headers on meaningful
  columns (repo console convention).

### Key Entities

- **TelemetrySnapshot**: the projected metrics DTO — per-operation counts, latency
  percentile triple, error rate, cache hit/miss counts — read from the gathered
  `MetricFamily` set. Process-wide (one daemon, all vaults).
- **AuditEvent** (existing, spec 021): `{timestamp, type, ...}` — query carries
  `query_hash`; ingest carries path + counts; auth-fail carries transport. Rendered as rows.
- **AuditPage**: a bounded, filtered page of `AuditEvent`s plus the filter echo (type,
  window, page) — the browser's read model over `Engine.AuditRead`.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a mixed workload, Observability shows query/ingest counts + latency
  percentiles + error rate within 1s on loopback, agreeing with `curl /metrics`.
- **SC-002**: The Audit Log filters by type and time window; 100% of query rows show the
  hash, and the raw query string appears nowhere in the DOM (verifiable by DOM grep).
- **SC-003**: Zero drift between UI telemetry and a `/metrics` scrape for the same
  instrument set.
- **SC-004**: Disabled-audit and cold-metrics render healthy empty/off states, not errors.
- **SC-005**: No Node or front-end build artifacts; a single `make build` produces one
  binary.
- **SC-006**: `go build ./...`, `go vet ./...`, `go test -race ./...` green; no retrieval
  regression (`make test-eval` recall@10 unchanged).

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine
  `goragApp` root, spec 045 Bearer auth, and the spec 052 vault picker — unchanged.
- Telemetry is read in the **UI layer** via `prometheus.DefaultGatherer().Gather()` (the
  observe package already registers instruments with the default registerer). No engine
  change, no new transport. Plan pins the exact `gorag_*` metric names + the percentile
  computation from histogram buckets.
- The audit surface is **already complete**: `Engine.AuditRead(vault, opts)` +
  `audit.ReadOptions` (type filter, time window, limit). Plan confirms the exact
  `audit.Event` field shapes available for each row type.
- This slice adds the **UI surface only** — no new engine capability, no new transport
  surface, no new CLI command.
- Single-operator use; no multi-user or RBAC (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- Process-wide telemetry is correct under spec 052's one-daemon-many-vaults model; only the
  audit trail is per-vault.
- Trace browsing is **out of scope** for this slice (no structured trace-read surface
  exists; traces write to a local file sink per spec 020). Telemetry + audit only.

## Open Questions (to resolve in plan / tasks)

- **Percentile source** — compute p50/p95/p99 server-side from the Prometheus histogram
  cumulative buckets (standard quantile interpolation) in the UI handler, vs expose raw
  bucket counts and compute client-side. Lean: server-side — keeps the SPA dumb and the math
  in one tested place.
- **Telemetry refresh** — manual refresh button only, vs an optional auto-poll (e.g. every
  10s). Lean: manual refresh + a toggle for auto-poll off by default.
- **Audit pagination shape** — fixed page size (e.g. 50) with older/newer + a time-window
  picker, vs cursor/infinite scroll. Lean: fixed page + time-window filter (mirrors the
  `audit.ReadOptions` limit/since shape).
- **Sidebar slot** — "Observability" takes the existing placeholder sidebar entry (removing
  it from `placeholderViews`). Lean: yes — it is the designated slot; also fix the stale
  spec numbers / comment in `placeholder.go` while there.
- **Cache ratio prominence** — a primary tile or secondary. Lean: secondary tile (the Query
  view already surfaces per-query cache state).
