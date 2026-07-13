# Implementation Plan: Quarantine Management View

**Branch**: `main` | **Date**: 2026-07-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/053-quarantine-management/spec.md`

## Summary

A dedicated Quarantine Management view for the poisoning-detection system (spec 019/H04). The
engine surface is **already complete** (`ListPoisoned` / `ReleaseChunk` / `ResetChunk` /
`RescanPoisoning`, all vault-aware post-052). This slice adds the **UI surface only**: a new
sidebar view ("Quarantine") that lists flagged chunks, shows the verdict detail (per-signal
scores + matched-phrase highlighting), and lets the operator release false positives. No new
engine capability, no new transport, no Node/build chain.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); vendored Alpine.js 3.14 (embedded).

**Primary Dependencies**:
- `internal/engine` — `ListPoisoned(vault) → []PoisonedChunk`, `ReleaseChunk(vault, chunkID)`,
  `ResetChunk(vault, chunkID)`, `RescanPoisoning(vault)`, `GetChunk(vault, chunkID)` (for full
  text in the detail view). All EXISTING, vault-aware.
- `internal/model` — `PoisonVerdict{Level, Score, Signals, MatchedPhrases}`,
  `PoisonSignals{Repetition, Stuffing, Instruction}`. EXISTING.
- `internal/ui` — the UI transport (spec 046); new `quarantine.go` handlers + Alpine view.
- `internal/auth` — spec 045 Bearer guard (unchanged).

**Storage**: None new. Reads the existing quarantine index (0x11) + chunk records.

**Testing**: `go test -race`; curl smoke for the new routes; Interceptor browser verify.

**Constraints**: read + confirmed-write; vault-aware; no Node; single binary; no new engine.

**Scale/Scope**: ~4–5 files. NEW: `internal/ui/quarantine.go` (+test). EDIT: `internal/ui/ui.go`
(routes), `internal/ui/web/{static/js/app.js, static/css/components.css, templates/index.html}`
(the Quarantine view + sidebar item).

## Constitution Check

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback UI in the existing binary; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | Read-only browse + confirmed release (via existing engine). No identity change. |
| III | Pure Go | **PASS** | stdlib + engine + model. No new dependency. |
| IV | Async-After-ACK Writes | **PASS** | Release/reset are bounded single-chunk operations (not ingest). The <10ms budget governs ingest; these are operator actions. |
| V | Extension by Interface, MCP-First | **PASS** | The engine surface is already on ALL transports (REST/gRPC/MCP/CLI). The UI is a 4th adapter over existing methods. No parity debt. |

**Storage discipline**: no on-disk change. **No migration, no ExpectedVersion bump.** Gate: PASS.

## Project Structure

```text
internal/ui/
  quarantine.go            # NEW — handleQuarantineList (ListPoisoned → DTOs),
                           #   handleQuarantineRelease (ReleaseChunk, confirmed),
                           #   handleQuarantineReset (ResetChunk), handleQuarantineRescan
  quarantine_test.go       # NEW — list/release/reset/rescan + guard + parity vs CLI
  ui.go                    # EDIT — register GET /api/quarantine/list,
                           #   POST /api/quarantine/{id}/release, /reset, POST /api/quarantine/rescan
  web/static/js/app.js     # EDIT — Quarantine view (Alpine): list, detail, release confirm
  web/static/css/components.css # EDIT — matched-phrase highlight colours per signal
  web/templates/index.html # EDIT — Quarantine sidebar item + view template + detail section
```

**Structure decision**: no new package. A new file (`quarantine.go`) inside the existing UI
transport, mirroring `documents.go` / `query.go` / `bridgeops.go`.
