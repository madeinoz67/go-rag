# Phase 1 — Data Model: Retrieval Poisoning Defense (H04)

> One new entity (`PoisoningVerdict`), persisted as a field on the existing `Chunk` record
> plus a secondary quarantine-index key. No change to document/source identity (Constitution II).

## Entities

### PoisoningVerdict  *(new — lives on the Chunk record)*

The per-chunk injection-risk assessment. Pure deterministic function of chunk text
(Constitution II: same text → same verdict, re-score is a no-op).

| Field | Type | Notes |
|-------|------|-------|
| `level` | enum `clean \| suspicious \| quarantine \| released` | Derived from `score` vs thresholds; `released` is a user-asserted override terminal state (D6). |
| `score` | float 0..1 | Combined signal score. |
| `signals` | struct `{ repetition float; stuffing float; instruction float }` | Per-signal contribution (D1). Drives the "why was this flagged" view (FR-006/US2). |
| `matched_phrases` | []string | Instruction-phrase hits (empty if none). For transparency. |
| `scored_at` | (optional) | Provenance; verdict is deterministic so this is informational only. |

**Derivation rule**: `score = w_r·repetition + w_s·stuffing + w_i·instruction` (weights
default equal, configurable). `level`:
- `score < suspicious_threshold` → `clean`
- `suspicious_threshold ≤ score < quarantine_threshold` → `suspicious`
- `score ≥ quarantine_threshold` → `quarantine`
- user override → `released` (regardless of score; original score retained in `signals`)

### Chunk  *(existing — extended)*

| Field | Change |
|-------|--------|
| `Metadata` / new `Poisoning *PoisoningVerdict` | Verdict rides the chunk record (free batch write, D3). Nil only during the brief pre-score moment inside a store call; persisted value is always non-nil for a stored chunk. |

### QuarantineIndex entry  *(new — Pebble prefix `0x11`)*

| Field | Type | Notes |
|-------|------|-------|
| key | chunkID | Prefix `0x11`. Written only for chunks with `level ∈ {suspicious, quarantine}`. Deleted on `released`/`clean`. |
| value | serialized `PoisoningVerdict` | For O(quarantined) listing (FR-006/US2). |

**Open item (tasks)**: confirm `0x11` is free against `internal/storage` prefix constants;
adjust if taken.

### ThreatSource  *(new — persisted poisoning config, D12)*

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable source id (`builtin`, `user`, `community-owasp`, a URL hash, …). |
| `origin` | string | File path or URL the source was imported from. |
| `enabled` | bool | Toggle inclusion in the merged list (default true). |
| `version` | string | Source-supplied or content-hash version. |
| `fetched_at` | timestamp | When last imported (nil for `builtin`). |
| `phrases` | []string | Phrase/pattern entries (deduped within source). |

The **merged list** = union (deduped) of all enabled sources' phrases, recomputed on any
change. The merged list's content-hash is the rescan trigger key (FR-011). Stored under a
poisoning-config prefix (confirm next-free vs `0x11`).

## Validation rules (from requirements)

- V1: `score` MUST be in [0,1]; each signal in [0,1]. Out-of-range ⇒ scorer bug ⇒ clamp + log.
- V2: verdict MUST be deterministic — identical text ⇒ byte-identical verdict (Constitution II).
- V3: `released` MAY only be set by the explicit override op (D6), never by the scorer.
- V4: thresholds `suspicious_threshold < quarantine_threshold` (validated at config load).
- V5: detection disabled (`poisoning_enabled=false`) ⇒ no verdict computed; chunks treated as
  `clean` and fully retrievable (the configurable-off escape hatch, FR-010/Q2).

## State transitions

```text
                  ingest (score)
   ┌─────────────────────────────────────────────┐
   │                                             ▼
 [unscored] ──► [clean]                    [suspicious] ──► [quarantine]
   (transient)    │  retrievable               │ excluded       │ excluded
                  │                            │ (default)      │ (default)
                  │                            │                │
                  │          override (D6)     │                │
                  │   ◄────────────────────────┴────────────────┘
                  │   ◄─────────── override ──────────
                  ▼
              [released]   ◄── terminal until reset; original score retained
              retrievable
```

- `clean` / `released` → included in default query results.
- `suspicious` / `quarantine` → excluded from default results (Q1=A); included only when the
  request sets `include_quarantined` (D4).
- `released` is reached only via the override op; reset (back to scored level) is a separate
  admin action — non-destructive throughout (no content is ever deleted).

**Re-scan semantics (FR-011/D11)**: a background re-score recomputes `level` from the refreshed
`score`. The `released` state is **sticky** — a `released` chunk stays `released` across
rescans even if its refreshed score would now flag it (user override outranks the scorer); the
refreshed `score`/`signals` are still stored so the new threat assessment is visible. All other
states re-derive from score on rescan.

## Relationships

- `PoisoningVerdict` **1:1** `Chunk` (lives on the chunk record).
- `QuarantineIndex` **0..1** per `Chunk` (present iff level ∈ {suspicious, quarantine}).
- No relationship to `Source`/`Document` identity hashes — verdict is chunk-text-scoped only.
