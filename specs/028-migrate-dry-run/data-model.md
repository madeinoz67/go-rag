# Data Model — Migration Dry-Run (H24, spec 028)

**Phase 1 output.** This feature adds **one new in-memory result type**
(`MigrationPlan`) and **one new read-only engine method** (`MigratePlan`). It
persists **nothing new** — no Pebble prefix, no on-disk shape change (FR-003). The
plan is computed on demand from two existing read-only corpus readers.

---

## 1. `MigrationPlan` — the preview result (NEW, in-memory only)

Returned by `Engine.MigratePlan`. Not persisted; computed per call from stored
metadata. Surfaces identically on CLI/REST/gRPC/MCP.

| Field | Type | Source | Purpose |
|-------|------|--------|---------|
| `TargetModel` | string | `cfg.EmbeddingModel` | the model a real migrate would re-embed onto |
| `Total` | int | `EmbeddingModelStats` sum | total tracked embeddings |
| `StaleTotal` | int | `EmbeddingModelStats`, `!= TargetModel` | count a real migrate would re-embed (FR-005 effort proxy; FR-008 = exactly what Migrate re-embeds) |
| `Sources` | `[]ModelCount` | `EmbeddingModelStats` | per stored model: `{Model, Count, Stale bool}` |
| `Dimensions` | `[]DimCount` | `CorpusProfile.DimCounts` | stored dimensionality distribution (R2) |
| `Consistent` | bool | `CorpusProfile.Consistent` | single model+dim (clean) vs mixed |
| `Estimate` | `Estimate` | derived | the labelled-approximate cost summary |

`ModelCount{ Model string; Count int; Stale bool }` — one row per stored model;
`Stale` is `Model != TargetModel`.

`DimCount{ Dim int; Count int }` — one row per distinct stored embedding length.

`Estimate{ StaleEmbeddings int; ModelChange bool; MixedCorpus bool; Note string }`
— the decision signal: how many embeddings to regenerate, whether the model
changes, whether the corpus is already mixed, and an explicit "estimate, not a
time guarantee" note (R4).

---

## 2. The two read sources (EXISTING, unchanged, metadata-only)

- **`pipeline.EmbeddingModelStats(db) map[string]int`** (`internal/pipeline/load.go`)
  — per-model embedding counts via a `PrefixScan` over the embeddings prefix
  (`0x04`). Read-only; never writes. Skips pre-tracking bare vectors (no model).
- **`engine.CorpusProfile(db) EmbeddingProfile`**
  (`internal/engine/embedding_profile.go`) — `{Total, MajorityModel, MajorityDim,
  DimCounts map[int]int, Consistent bool, …}` via a read scan. Read-only.

Neither constructs an `Embedder` or touches the network → FR-004 (succeeds with
no backend) holds by construction (R5).

---

## 3. Lifecycle / state transitions

```text
MigratePlan(ctx):                              # NEW — read-only
  stats  := EmbeddingModelStats(db)            # existing read
  prof   := CorpusProfile(db)                  # existing read
  return planFrom(stats, prof, cfg.EmbeddingModel)   # pure derive; no write, no embed

Migrate(ctx):                                  # REFACTORED — reuses the plan
  plan := MigratePlan(ctx)                     # FR-008: preview == execution set
  if plan.StaleTotal == 0: return zero summary
  flushCaches()                                # H06 — only on a REAL migrate
  ReprocessAll(ctx)                            # the actual re-embed
  refreshBaselineAfterMigrate(ctx)             # H11 — only on a REAL migrate
```

The dry-run is the **upper half** of `Migrate`, extracted. No state machine is
added; the only behavioural change is that `Migrate` now computes its plan via
`MigratePlan` (so preview and execution can never disagree — FR-008). The
dry-run path never reaches `flushCaches`/`ReprocessAll`/`refreshBaseline`, so the
index epoch, caches, and baseline are untouched (FR-003).

---

## 4. Validation rules (from requirements)

- **FR-001/FR-003** → `MigratePlan` calls only `EmbeddingModelStats` +
  `CorpusProfile`; conformance test asserts corpus/cache/baseline/epoch identical
  before/after.
- **FR-004** → `MigratePlan` constructs no `Embedder`; test runs it with the
  backend unreachable and asserts success + correct plan.
- **FR-005** → `MigrationPlan.Estimate` carries `StaleEmbeddings` +
  `ModelChange` + `MixedCorpus` + the "estimate" note.
- **FR-006** → `MigrationPlan` projected identically on CLI/REST/gRPC/MCP; parity
  test asserts byte-identical values.
- **FR-008** → `Migrate` reuses `MigratePlan`; the stale set the preview reports is
  exactly the set `Migrate` re-embeds (asserted by a plan-then-migrate test).
