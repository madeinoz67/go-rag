# Data Model — Bridge Ops View (Slice 3)

**Feature**: specs/049-ui-bridge-ops | **Date**: 2026-07-12

Phase 1 output. The Bridge Ops view introduces **no new persistent data** — it is a read-only
projection of `engine.StatusInfo` plus a bounded read of the audit log (via the new thin
`Engine.AuditRead` wrapper). This document defines the two route DTOs. Field names parallel the
engine surface (`internal/engine/types.go::StatusInfo`) and the audit package.

---

## Route table (new)

| Method | Path | Auth | Query | Returns | Maps to |
|--------|------|------|-------|---------|---------|
| GET | `/api/bridge-ops/stats` | `Server.guard` | — | `bridgeOpsStatsDTO` | `Engine.Status()` (in-process) + `WatchDirs` |
| GET | `/api/bridge-ops/activity` | `Server.guard` | `tail` (int, default 20, max 100), `type` (string, default `ingest`) | `activityResponseDTO` | `Engine.AuditRead` (in-process) |

No changes to existing routes. No engine / REST / gRPC / MCP / CLI / proto changes (the audit
read is the new `Engine.AuditRead` wrapper, UI-only consumer).

---

## `bridgeOpsStatsDTO`

Projects the operational subset of `StatusInfo` (omitting the corpus counts the Dashboard
already shows, except the backlog which is central here) + the watch configuration.

| Field | Type | Source (`StatusInfo` unless noted) | Notes |
|-------|------|--------------------------------------|-------|
| `vault` | string | derived | active vault name |
| `last_activity` | string | audit (newest event timestamp, RFC3339) | empty when no events |
| `backlog` | object | — | `{pending int (EmbedPending), failed int (EmbedFailed), complete bool (EmbeddingsComplete)}` |
| `drift` | object | — | `{verdict string (DriftVerdict), hard bool (HardDrift), version bool (VersionDrift), cause string, baseline {...}, live_ollama_ver string (LiveOllamaVersion)}` |
| `drift.baseline` | object | CorpusBaseline* | `{model, dim int, convention, ollama_ver, recorded_at}` |
| `subsystems.poisoning` | object | H04 | `{enabled bool (PoisoningEnabled), flagged int (PoisonFlagged), sources int (PoisonSources), phrases int (PoisonPhrases), threshold_sus float, threshold_qua float}` |
| `subsystems.enrichment` | object | spec 029 | `{enabled bool (EnrichmentEnabled), captioning bool (CaptioningEnabled), enriched_docs int (EnrichedDocs)}` |
| `subsystems.caches` | object | spec 016 | `{result CacheStatsDTO, embedding CacheStatsDTO}` |
| `subsystems.adaptive` | object | spec 024 | `{pool_size int (PoolSize), enabled bool (AdaptiveDepthEnabled), utilization object (PoolUtilization), near_dup_chunks int (NearDupChunks)}` |
| `watch` | object | `Config.WatchDirs` | `{dirs []string, scan_driven bool}` — `scan_driven` is always `true` this slice (no persistent watcher) |

`CacheStatsDTO` — `{enabled bool, size int, capacity int, hits int, misses int}` (projects
`engine.CacheStats`).
`PoolUtilization` projected as-is (aggregate pool-consumption signal).

The `drift.cause` is derived: "model" / "dimensionality" / "convention" / "ollama-version" /
"none" based on which drift flag/baseline mismatch is active — a one-line, act-on-able summary.

---

## `activityResponseDTO`

| Field | Type | Notes |
|-------|------|-------|
| `events` | []activityEventDTO | Most-recent first, bounded by `tail`. |
| `count` | int | `len(events)`. |

### `activityEventDTO`

Projects `audit.Event` (spec 021). The exact `audit.Event` field set is confirmed at implement
time; the DTO exposes the operator-relevant projection:

| Field | Type | Notes |
|-------|------|-------|
| `type` | string | Event type (`ingest`, etc.). |
| `timestamp` | string | RFC3339. |
| `summary` | string | Human-readable one-line summary (event-specific: document, outcome). |
| `outcome` | string | `success` / `failed` / `skipped` when the event carries one; empty otherwise. |

(Implement time: map `audit.Event` → `activityEventDTO` in `bridgeops.go`, field-parallel, same
as `chunkDTO`/`queryHitDTO` mirror their engine sources. If `audit.Event` lacks an explicit
outcome field, derive it from the event payload / type.)

---

## Validation rules (enforced at the UI handler)

- `tail` clamped to [0, 100]; default 20. 0 → return the bounded default (not unbounded).
- `type` validated against the audit event types (`ingest` / `query` / `auth-fail`); default
  `ingest`. Unknown → 400 `invalid type`.
- Missing/disabled audit log → `activityResponseDTO{events:[], count:0}` (healthy empty, not an
  error).
- Engine errors → `writeEngineErr` (existing helper, same package).

---

## State transitions

None. The view is stateless beyond per-request stats/activity reads. Client-side state is the
rendered snapshot + the refresh action. No persistence, no settings changes (read-only slice).
