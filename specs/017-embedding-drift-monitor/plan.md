# Implementation Plan: Embedding Drift Monitoring + Version Pinning (H11)

**Branch**: `main` | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/017-embedding-drift-monitor/spec.md` (audit backlog item **H11**, P1 — Phase 4, second item, after the shipped H06).

## Summary

Layer a **proactive** embedding-drift detector on top of the shipped H03 (query-time guard) and H07 (per-embedding convention): persist a **corpus baseline** record `{model, dim, convention, ollama-version, recorded-at}` under a new Pebble prefix; on daemon boot (and on `status`) compare the live configured profile + live Ollama version against it; **start degraded** on hard drift (model/dim/convention) — readiness NOT READY, liveness OK, queries still refused by H03 — and **warn** on soft drift (Ollama-version change). Refresh the baseline on first embed + on successful `migrate`; backfill it for pre-H11 corpora. The novel value vs H03 is the Ollama-version pin (the book §4.6 silent-pooling-change failure, undetectable by H03) and boot-time detection. No new dependencies.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib (`net/http`, `encoding/json`, `sync`, `time`); existing `internal/engine`, `internal/storage`, `internal/embed`, `internal/config`, `internal/cli` (serve). **No new module dependencies.**

**Storage**: Pebble KV — **one new prefix**, `PrefixCorpusMeta = 0x10` (free; H16 reserves 0x06), holding a single fixed corpus-baseline record (JSON). No schema change to existing records; the baseline is a new corpus-level header, distinct from per-embedding provenance (H07's 0x04) and from user config (0x09).

**Testing**: `go test -race -cover ./...`; new tests (baseline write on first embed, refresh on migrate, backfill, boot drift verdict for hard/soft/clean/unreachable/offline, readiness flag, status surface). H02 eval gate — recall@10 unchanged (version check skipped on the offline embedder, FR-010/SC-006).

**Project Type**: CLI + multi-transport server. Touches: engine (baseline store/load, drift verdict, Health readiness, Status fields), embed (Ollama `/api/version` helper), storage (new prefix), pipeline (write baseline on first embed), ingest (refresh on migrate), cli/serve (boot check + log), and the four transport status surfaces.

**Performance Goals**: boot drift check adds one `GET /api/version` (short timeout, non-blocking on failure — mirrors the existing `embedderReachable` probe) and one baseline read; negligible vs the < 1s cold-start budget. `/health` reads a **cached** verdict (no per-probe fetch) so probes stay fast.

**Constraints**: Pure Go, no new deps; boot must succeed with Ollama down (version check skipped, FR-006); liveness must stay OK on drift (no restart loop); hard-drift = degraded + not-ready, soft-drift = warn + ready; offline/eval embedder skips the version check.

**Scale/Scope**: local single-user; one baseline record per vault.

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | All local; the Ollama version fetch is loopback (the configured Ollama URL). No new dependency; single binary unchanged. |
| II | Content-Addressed Identity | ✅ Pass | The baseline is corpus-level metadata about the embedding profile, NOT a document identity; no ContentHash/ID change. It's an additive header, not authoritative content. |
| III | Pure Go — No CGo/External Runtime | ✅ Pass | stdlib only (`net/http`, `encoding/json`); version fetch is a plain HTTP GET. |
| IV | Async-After-ACK Writes | ✅ Pass | The baseline write (first embed) happens on the async `processJob` worker; the boot check is read-only + a non-blocking probe. Neither touches the < 10 ms write-ACK. The migrate-refresh happens after `ReprocessAll` completes (post-ACK re-embed). |
| V | Extension by Interface, MCP-First | ✅ Pass | Drift state + baseline surface on all four transports' status (parity); `/health` readiness is exposed on REST + gRPC. The `Embedder` interface is unchanged (version is an ops/server concern, not an embedder-interface method — D2). |

**No violations.** (Re-check after Phase 1 design — still clean: one new Pebble prefix for a single metadata record, no new process/dependency, public-surface additions are an extended `HealthInfo`, extended `StatusInfo`, and the existing `/health` + gRPC health RPC semantics.)

## Project Structure

### Documentation (this feature)

```text
specs/017-embedding-drift-monitor/
├── plan.md                       # this file
├── research.md                   # Phase 0 — D1–D8 decisions resolved
├── data-model.md                 # Phase 1 — corpus baseline entity, HealthInfo/StatusInfo deltas, drift verdict
├── contracts/
│   └── drift-status-contract.md  # Phase 1 — /health readiness + status drift fields (4 transports)
├── quickstart.md                 # Phase 1 — runnable validation scenarios
└── tasks.md                      # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/storage/storage.go       # EDIT: PrefixCorpusMeta byte = 0x10 (new prefix constant)
internal/engine/
├── baseline.go        # NEW: CorpusBaseline struct; Load/Save/Backfill; the persisted {model,dim,convention,ollama-version,recorded-at}
├── drift.go           # NEW: DriftVerdict type; computeDriftVerdict(); engine caches the verdict; RefreshDriftVerdict()
├── health.go          # EDIT: HealthInfo gains Ready bool; Engine.Health reports liveness(OK)+readiness(Ready, from cached verdict)
├── status.go          # EDIT: StatusInfo gains baseline + live-version + drift fields; recomputes live verdict
├── types.go           # EDIT: StatusInfo/HealthInfo field additions; DriftVerdict/CorpusBaseline types (if not in their own files)
├── engine.go          # EDIT: Engine gains cached verdict + cached live-ollama-version; RefreshDriftVerdict on boot; Close clears cache
├── ingest.go          # EDIT: Migrate refreshes the baseline + verdict on successful completion
└── version.go         # NEW: ollamaVersion(ctx, baseURL) — GET /api/version (short timeout; "" for injected/offline embedder; "unknown" on error)
internal/pipeline/workers.go      # EDIT: processJob writes the baseline on the first successful embed (if absent)
internal/cli/serve.go             # EDIT: after engine construct, call RefreshDriftVerdict + log the verdict at boot
internal/cli/status.go            # EDIT: render baseline vs live + drift flags
internal/rest/{server,types}.go   # EDIT: /health readiness in body; status response gains drift/baseline fields
internal/grpc/engine_adapter.go + proto/ # EDIT: health RPC NOT_SERVING on hard drift; StatusResponse gains drift/baseline fields (regen)
internal/mcp/server.go            # EDIT: renderStatus appends baseline + drift verdict
```

**Structure Decision**: Four cohesive, independently-testable layers:

1. **Baseline persistence** (`baseline.go` + `storage` prefix) — a single corpus record; Load/Save/Backfill. Pure, no engine coupling beyond the DB handle.
2. **Drift verdict** (`drift.go` + `version.go`) — computes baseline-vs-live (hard/soft/clean/unknown); caches on the engine; the Ollama-version fetch is a standalone helper (server concern, not on the `Embedder` interface).
3. **Engine wiring** (`engine.go`, `health.go`, `status.go`, `ingest.go`, `workers.go`, `serve.go`) — boot computes + caches the verdict; `/health` reads it (fast); `Status` recomputes live; first-embed writes the baseline; migrate refreshes it; serve logs the verdict at boot.
4. **Transport exposure** (CLI/REST/gRPC/MCP + proto) — readiness on `/health` + gRPC health RPC; baseline + drift flags in status on all four transports.

**Highest-risk correctness item**: the **hard-vs-soft severity + the degraded-readiness contract** (D4/FR-004/FR-011). The boot check must (a) start the daemon (not exit) on hard drift, (b) mark readiness NOT READY while liveness stays OK, (c) keep working with Ollama down (skip version, still do model/convention). A regression test for each branch gates it.

## Complexity Tracking

*(Empty — Constitution Check passes on all five principles. No violations to justify.)*
