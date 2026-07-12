# Contract: UI Bridge Ops Transport

**Feature**: specs/049-ui-bridge-ops | **Date**: 2026-07-12

The HTTP contracts for the Bridge Ops view's two backend calls. UI-transport routes
(`127.0.0.1:7881`, the spec 046 loopback 4th transport), guarded by the spec 045 Bearer session
via `Server.guard` — the same guard every other `/api/*` route uses. Both call the engine
in-process; neither proxies to REST.

Field-level definitions live in [data-model.md](../data-model.md). This contract fixes the wire
shape.

---

## `GET /api/bridge-ops/stats`

Operational health snapshot — embed backlog, drift (verdict + cause + baseline), subsystem
states (poisoning / enrichment / caches / adaptive), and configured watch directories.

**Auth**: `Authorization: Bearer <gorags_ session token>` (spec 045). Missing/invalid → 401.

**Response 200** — `application/json` (abbreviated; full shape in [data-model.md](../data-model.md)):

```json
{
  "vault": "default",
  "last_activity": "2026-07-12T13:10:08Z",
  "backlog": { "pending": 4706, "failed": 0, "complete": false },
  "drift": {
    "verdict": "clean", "hard": false, "version": false, "cause": "none",
    "baseline": { "model": "bge-small-en-v1.5-int8", "dim": 384, "convention": "auto",
                  "ollama_ver": "0.1.x", "recorded_at": "2026-07-10T03:02:00Z" },
    "live_ollama_ver": "0.1.x"
  },
  "subsystems": {
    "poisoning": { "enabled": true, "flagged": 0, "sources": 0, "phrases": 312,
                   "threshold_sus": 0.45, "threshold_qua": 0.7 },
    "enrichment": { "enabled": false, "captioning": false, "enriched_docs": 0 },
    "caches": {
      "result":     { "enabled": true, "size": 12, "capacity": 1000, "hits": 48, "misses": 12 },
      "embedding":  { "enabled": true, "size": 3,  "capacity": 256,  "hits": 2,  "misses": 0 }
    },
    "adaptive": { "pool_size": 60, "enabled": false,
                  "utilization": { "queries": 0, "avg_fetched": 0, "avg_kept": 0, "saturated": false }, "near_dup_chunks": 0 }
  },
  "watch": { "dirs": ["."], "scan_driven": true }
}
```

**Response 401** — `{"error":"unauthorized"}`. No 404 (the vault always has a status).

---

## `GET /api/bridge-ops/activity`

Recent audit events (bounded, type-filtered).

**Auth**: `Authorization: Bearer <gorags_ session token>`.

**Query**:
- `tail` — int, default 20, max 100 (clamped).
- `type` — event type filter, default `ingest`. One of `ingest` / `query` / `auth-fail`.

**Response 200** — `application/json`:

```json
{
  "events": [
    { "type": "ingest", "timestamp": "2026-07-12T13:10:08Z",
      "summary": "ingested docs/report.md (12 chunks)", "outcome": "success" },
    { "type": "ingest", "timestamp": "2026-07-12T12:55:01Z",
      "summary": "ingested docs/draft.md (embedding failed: ollama unreachable)", "outcome": "failed" }
  ],
  "count": 2
}
```

**Response 200 (no events / audit disabled)** — `{"events":[],"count":0}` (healthy empty, never
an error).

**Response 400** — `{"error":"invalid type"}` (unknown `type`).
**Response 401** — `{"error":"unauthorized"}`.

---

## Non-goals of this contract

- No write routes — both endpoints are `GET` (read-only).
- No streaming / SSE — manual refresh only (R8).
- No `POST /api/bridge-ops/scan` or any scan-trigger — write actions are a later slice
  (spec FR-008).
- No full audit-log dump — `tail` is bounded (R4).
