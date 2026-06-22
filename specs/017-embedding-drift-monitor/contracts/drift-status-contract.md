# Contract — Drift Readiness + Status Surface (H11)

> Phase 1 output. go-rag is a CLI + multi-transport server, so the public
> interface contract spans the health/readiness probe + the status surface across
> four transports. This fixes the exact surface for `/speckit-tasks`. The
> persisted corpus baseline is internal; the public additions are (1) readiness
> on the health probe, (2) baseline + drift fields in status.

## 1. Health / readiness probe (REST `/health`, gRPC health RPC, MCP `/mcp/health`)

The daemon's externally-observable posture under drift (clarification A — start degraded).

| Surface | Liveness (process up) | Readiness (can serve queries) |
|---------|-----------------------|-------------------------------|
| REST `/health` | HTTP **200** while the process is up (unchanged; does **not** 503 — avoids restart-loops if used as a liveness probe) | body `ready: false` on hard drift; `ready: true` otherwise |
| gRPC `Health` RPC (grpc-health-v1) | — (the process is the liveness unit) | `SERVING` when ready; **`NOT_SERVING`** on hard drift |
| MCP `/mcp/health` (unauthenticated daemon probe) | 200 while up | body carries `ready` + `drift_verdict` |

**REST `/health` body** (additive; existing fields retained):

```json
{
  "ok": true,                 // liveness (process alive + storage open) — unchanged
  "ready": false,             // H11: readiness — false on hard drift
  "storage_open": true,
  "embedder_reachable": true,
  "drift_verdict": "hard-drift"
}
```

**Semantics** (FR-004/FR-005/FR-011):
- Hard drift (model/dim/convention mismatch) → `ready=false`, gRPC `NOT_SERVING`. Mismatched queries
  are still refused by the existing H03 guard; `status`/`migrate`/`config` still work so the operator
  can remediate in place.
- Soft drift (Ollama-version change) → `ready=true`, `drift_verdict="version-warning"` (warns; queries
  served).
- Clean → `ready=true`, `drift_verdict="clean"`.
- Ollama unreachable → `ready` reflects model/convention verdict only; `live_version="unknown"`.

**Intentionally NOT done** (documented tradeoff, D4): `/health` does **not** return HTTP 503 on hard
drift — a 503 would risk restart-loops if `/health` is wired as a *liveness* probe. Readiness is
signalled via the body (`ready`) + the gRPC health RPC (`NOT_SERVING`). A future dedicated `/ready`
(503) endpoint is possible if a status-code-based REST readiness probe is needed (out of scope).

## 2. Status surface — baseline + drift fields (all four transports)

`Engine.Status()` returns the existing `StatusInfo` **plus** the H11 fields (data-model.md). Each
transport renders them.

| Transport | Wire |
|-----------|------|
| **CLI** `go-rag status` | Delegates to the daemon's MCP `go_rag_status` (when running); prints a `Baseline:` + `Drift:` section. When the daemon is stopped, computes locally (fresh engine) — baseline still shown from the persisted record. |
| **MCP** `go_rag_status` (`renderStatus`) | Appends `baseline: model=… dim=… conv=… ollama=…@<recorded-at>` and `drift: <verdict> (<reasons>)` to the status line. |
| **REST** status response JSON | Adds `corpus_baseline{model,dim,convention,ollama_version,recorded_at}`, `live_ollama_version`, `drift_verdict`, `hard_drift`, `version_drift`. |
| **gRPC** `StatusResponse` | Adds the same fields (proto regen — batch with any other proto change). |

**Parity invariant** (FR-009): the same baseline + verdict fields appear on every transport (all
converge on `Engine.Status`).

## 3. Behavioural contract (transport-invariant)

These hold identically over CLI/REST/gRPC/MCP — properties of the engine, not adapters:

1. **Boot detection** — hard drift is detected + logged at daemon boot, before any query (FR-004).
2. **Degraded, not crashed** — on hard drift the daemon stays up (liveness OK), readiness NOT READY,
   `status`/`migrate` work; queries refused by H03 (FR-004/FR-011).
3. **Soft = warn + serve** — Ollama-version change warns but queries succeed (FR-005).
4. **Baseline currency** — baseline written on first embed, refreshed on successful `migrate`,
   backfilled on first boot for a pre-H11 corpus (FR-001/FR-002/FR-007).
5. **Ollama-down safe** — boot succeeds with Ollama unreachable; version check skipped (FR-006).
6. **Offline/eval safe** — the deterministic offline embedder skips the version check (FR-010); recall
   unaffected (SC-006).

## 4. Out-of-contract (explicitly not exposed)

- No manual "reset baseline" command (migrate + backfill cover every real need; a reset knob is YAGNI).
- No continuous drift *scoring*/metric (a numeric drift score over time) — verdict is a discrete
  clean/hard/soft/unknown state, recomputed at boot/status/migrate.
- No auto-reindex on drift (the operator runs `migrate`; documented in the spec).
- No new Pebble key families beyond the single `PrefixCorpusMeta` (0x07) baseline record.
