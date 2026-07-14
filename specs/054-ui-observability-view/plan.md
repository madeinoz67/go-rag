# Plan — Observability View (spec 054)

> Companion to `spec.md`. Resolves the Open Questions; pins the data model, contracts, and
> build order. UI-only slice (no engine capability, no transport) — mirrors 047–053.

## Research findings (what already exists — do NOT rebuild)

- **Telemetry instruments** — `internal/observe/metrics.go`: `registerInstruments(m)`
  creates the OTel handles; write-side `RecordQuery`, `RecordIngest`, `RecordQueryResults`,
  `CacheHit`/`CacheMiss`, `PoisonFlagged`. Registered with `prometheus.DefaultRegisterer`.
- **`/metrics`** — `internal/observe/prometheus.go::MetricsHandler()` returns
  `promhttp.Handler()`; text exposition only. **No structured snapshot reader exists.**
- **Structured read path (the key enabler)** — because instruments register with the default
  registerer, `prometheus.DefaultGatherer().Gather()` returns `[]*dto.MetricFamily` directly.
  The UI handler projects the handful of `gorag_*` families to JSON. **No engine change.**
- **Audit** — `internal/audit/reader.go::Read(path, ReadOptions)` + `ReadOptions`
  (type filter, since/until window, limit) + `Event` (`event.go`: timestamp, type,
  query_hash/mode/k/hits/status | path/counts | transport). `Engine.AuditRead(vault, opts)`
  wraps it read-only (spec 049). Config: `EffectiveAuditLogEnabled`, `EffectiveAuditLogMaxBytes`.
- **Non-overlap with 049** — Bridge Ops owns `StatusInfo` health + a clamped recent-activity
  tail. 054 owns telemetry + the **filterable full-trail** audit browser. The recent-tail
  endpoint stays in 049; 054 adds a filterable page endpoint.
- **Shell** — 046 app shell, Alpine `goragApp`, 4-layer CSS, 045 Bearer guard, 052 vault
  picker. Sidebar item `"observability"` currently routes to `handlePlaceholder`; this slice
  replaces that with a real handler + Alpine view (mirrors how 047–053 displaced their
  placeholder slots).

## Data model (DTOs, UI-layer, `internal/ui`)

```
// GET /api/observability/metrics
type telemetryResponse struct {
    ProcessWide bool             `json:"process_wide"` // always true (one daemon, all vaults)
    Operations  []opStat         `json:"operations"`   // query, ingest (add/scan/reprocess/migrate)
    Cache       cacheStat        `json:"cache"`        // result + embedding hit/miss
    ErrorRate   float64          `json:"error_rate"`   // errors / total across ops
    Posture     postureNote      `json:"posture"`      // local-only, zero-egress label
    FreshAt     string           `json:"fresh_at"`     // gather timestamp
}
type opStat struct {
    Op       string  `json:"op"`        // "query" | "ingest:<kind>"
    Count    uint64  `json:"count"`
    P50/P95/P99 float64 `json:"p50"/"p95"/"p99"` // seconds, from histogram buckets; -1 if insufficient
    Errors   uint64  `json:"errors"`
}
type cacheStat struct { Result, Embedding hitMiss }

// GET /api/observability/audit?type=&since=&until=&limit=&offset=
type auditPageResponse struct {
    Events    []auditEventDTO `json:"events"`
    Type      string          `json:"type"`       // filter echo ("" = all)
    Since     string          `json:"since"`      // filter echo
    Limit     int             `json:"limit"`
    Offset    int             `json:"offset"`
    Truncated bool            `json:"truncated"`  // more rows exist beyond the page
    Enabled   bool            `json:"enabled"`    // false → "audit logging is off" state
}
// auditEventDTO projects audit.Event — query row carries QueryHash (NEVER raw text),
// ingest row carries Path + counts, auth-fail row carries Transport. No content fields.
```

Percentile computation: from each latency histogram's cumulative `_bucket` counts + total
count, locate the bucket where cumulative count crosses `rank × total` and interpolate
across the bucket boundary (standard Prometheus histogram quantile). With <2 samples or all
in one bucket, emit `-1` → UI renders `—`.

## Contracts (Bearer-guarded, vault-aware — mirror 053)

| Method | Route | Source | Notes |
|---|---|---|---|
| GET | `/api/observability/metrics` | `prometheus.DefaultGatherer().Gather()` | process-wide; no vault param (labelled) |
| GET | `/api/observability/audit` | `Engine.AuditRead(vault, opts)` | `?type=&since=&until=&limit=&offset=`; vault from `X-Go-Rag-Vault` |

Both wrapped in `s.guard(...)` (spec 045). No writes, no new engine methods, no transport
surface beyond the UI. Parity is UI↔`/metrics` (telemetry) and UI↔`Engine.AuditRead`
(audit) — asserted by tests.

## Implementation phases (each lands green: build + vet + test -race)

1. **Metrics handler + DTO** — `internal/ui/observability.go`: gather → project →
   `telemetryResponse`; percentile-from-buckets helper + unit tests (known histograms →
   known quantiles; insufficient-samples → -1). Remove `"observability"` from
   `placeholderViews`; fix the stale comment/spec-numbers there.
2. **Audit handler + DTO** — `internal/ui/observability_audit.go`: wrap
   `Engine.AuditRead`, map `audit.Event` → `auditEventDTO` (hash-only for query), echo
   filters, `enabled` from config. Tests: type/window filter, hash-only (no plaintext
   field), disabled-audit healthy state, empty-log healthy state, vault isolation,
   all-routes-guarded.
3. **Alpine view** — extend the embedded SPA: Observability panel = telemetry tiles +
   per-op sortable table + refresh (+ optional auto-poll toggle); Audit panel = type/window
   filters + bounded sortable table + older/newer. Reuse 046 components (read-only — no
   `confirmDialog`). Posture note footer.
4. **Sidebar wiring + parity** — register the two routes in `ui.go`; wire the sidebar entry
   to the new view (mirror 053). Telemetry-parity test (UI gather == `/metrics` scrape for
   the same families). Audit-parity test (UI == `Engine.AuditRead` direct).
5. **Verify** — `make build && make vet && make test -race` green; `make lint(0)`;
   Interceptor browser-verify US1 (telemetry after a workload) + US2 (filtered audit,
   DOM-grep for plaintext absence) on a real daemon.

## Quickstart (verify)

```bash
make build
DB=$(mktemp -d)
./bin/go-rag start --db-path "$DB" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 \
  --grpc-addr 127.0.0.1:17880 --ui-addr 127.0.0.1:17881
# login (spec 045), add a doc, run a few queries, then open the Observability view
./bin/go-rag stop --db-path "$DB"
```

## Out of scope (this slice)

- Trace browsing (no structured trace-read surface; traces → local file sink, spec 020).
- Persistent metric history across restarts (spec 020 out of scope).
- Editing config / retention from the UI (that is the Settings view, a later slice).
- Alerts (the operator's Prometheus handles alerts, spec 020).

## Risks

- **Metric-name drift** — the handler keys on `gorag_*` family names; if observe renames
  them, projection breaks. Mitigation: read the exact names from `observe/metrics.go` at
  implement time; a test asserts the families the handler expects are all present after a
  recorded op.
- **Percentile accuracy** — histogram-bucket quantiles are approximate. Mitigation: the
  `/metrics` scrape exposes the same approximation, so UI == `/metrics` holds exactly (SC-003
  is parity, not absolute accuracy).
- **Plaintext leakage** — a query row must never render raw text. Mitigation: the DTO
  carries only `QueryHash`; a DOM-grep test in US2 verification.
