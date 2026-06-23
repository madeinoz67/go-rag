# Data Model — Embedding Drift Monitoring + Version Pinning (H11)

> Phase 1 output. New in-process + persisted entities and the (additive) deltas to
> `HealthInfo` / `StatusInfo`. One new persisted record (single Pebble key); no
> change to existing document/chunk/embedding records.

## New persisted entity: `CorpusBaseline` (`internal/engine/baseline.go`)

One record per vault, under `PrefixCorpusMeta = 0x10`, fixed key (e.g. `0x10 || "default"`). The
authoritative snapshot of the embedding profile the corpus was built under.

| Field | Type | Meaning |
|-------|------|---------|
| `Model` | `string` | Embedding model name the corpus was built under (majority at write time) |
| `Dim` | `int` | Vector dimensionality at write time |
| `Convention` | `string` | Instruction-prefix convention in effect (e.g. `nomic`, `e5`, `""` for none) |
| `OllamaVersion` | `string` | Ollama server version at write time (`""` for offline/injected embedder) |
| `RecordedAt` | `time.Time` (UTC, RFC3339 in JSON) | When the baseline was last written/refreshed |

**Methods**: `LoadBaseline(db) (*CorpusBaseline, bool)`; `SaveBaseline(db, *CorpusBaseline)`; the
backfill constructor derives a baseline from `CorpusProfile(db)` + a live version + now.

**Validation**: `Model != ""` when written from a real embed (a baseline with an empty model is not
written); `RecordedAt` always set. No identity/hash impact (Principle II — the baseline is metadata
about the corpus, not a document).

**State transitions**:
```
(none) --first embed--> Baseline(model_A, dim, conv, ver, t1)
       --migrate to model_B--> Baseline(model_B, ..., ver', t2)   # refreshed on successful migrate
       --(pre-H11 corpus, first boot)--> backfill from CorpusProfile + live ver
```

## New in-process entity: `DriftVerdict` (`internal/engine/drift.go`)

The result of comparing the live profile + live Ollama version against the baseline (D6).

| Field | Type | Values |
|-------|------|--------|
| `Verdict` | `string` | `clean` \| `hard-drift` \| `version-warning` \| `unknown` \| `n/a` |
| `Hard` | `bool` | true for `hard-drift` (convenience for readiness) |
| `BaselineModel` | `string` | from the baseline (for status display) |
| `ConfiguredModel` | `string` | `cfg.EmbeddingModel` |
| `BaselineDim` / `LiveDim` | `int` | dimensionality comparison |
| `BaselineConvention` / `LiveConvention` | `string` | convention comparison |
| `BaselineVersion` / `LiveVersion` | `string` | ollama-version comparison (`"unknown"`/`""` possible) |
| `Reasons` | `[]string` | human-readable mismatch list (e.g. `model: nomic vs mxbai`) |

Computed by `Engine.computeDriftVerdict(ctx)`; cached on the engine (`driftVerdict` + `verdictMu`);
refreshed at boot (`serve.go`) and after `Migrate`.

## Delta: `HealthInfo` (`internal/engine/health.go`) — **+1 field, +semantics**

```
Ready          bool     // H11: readiness = OK && no hard drift (queries can be served). Distinct from OK (liveness).
DriftVerdict   string   // H11: the cached verdict ("clean"|"hard-drift"|"version-warning"|"unknown"|"n/a")
```

- `OK` (existing) = liveness (process alive + storage open) — **unchanged meaning**; stays true on drift.
- `Ready` = readiness — false on hard drift. `/health` returns HTTP 200 with this in the body; the gRPC
  health RPC maps `!Ready` → `NOT_SERVING`.

## Delta: `StatusInfo` (`internal/engine/types.go`) — **+fields**

```
CorpusBaselineModel       string    // H11: baseline model ("" if no baseline)
CorpusBaselineDim         int
CorpusBaselineConvention  string
CorpusBaselineOllamaVer   string
CorpusBaselineRecordedAt  string    // RFC3339
LiveOllamaVersion         string    // H11: live Ollama version ("unknown"/"" possible)
DriftVerdict              string    // clean|hard-drift|version-warning|unknown|n/a
HardDrift                 bool      // true on model/dim/convention mismatch
VersionDrift              bool      // true on ollama-version change (soft)
```

(The existing `EmbeddingModel`/`EmbeddingDrift`/`ConventionCounts` fields from H03/H07 remain — they
report intra-corpus drift; the H11 fields report corpus-vs-config + ollama-version drift. Orthogonal.)

## Delta: storage (`internal/storage/storage.go`) — **+1 prefix constant**

```
PrefixCorpusMeta byte = 0x10 // H11: corpus baseline metadata (single record)
```

(0x06 intentionally left free for H16's persistent index snapshot.)

## Relationships

- `Engine` **1—1** cached `DriftVerdict` + cached `liveOllamaVersion`.
- `CorpusBaseline` **1—1** vault (one record under `PrefixCorpusMeta`).
- `processJob` writes the baseline on first embed; `Migrate` refreshes it; `RefreshDriftVerdict`
  backfills it on first boot of a pre-H11 corpus.
- No relationship change to `QueryResult`, `Chunk`, `Document`, or the query cache (H06) — drift
  detection is read-only w.r.t. those.

## Validation rules (testable)

- A baseline exists after the first embedding (verifiable: `status` shows baseline fields + recorded-at).
- A baseline is refreshed after successful `migrate` (recorded-at advances; model = new configured).
- A pre-H11 corpus (no baseline) is backfilled on first boot (baseline appears without re-ingest).
- Hard drift (baseline model ≠ configured) at boot → `DriftVerdict="hard-drift"`, `Ready=false`,
  `OK=true`, logged at startup.
- Soft drift (ollama-version change) → `DriftVerdict="version-warning"`, `Ready=true`, queries succeed.
- Ollama unreachable at boot → `LiveVersion="unknown"`, boot succeeds, model/convention checks still run.
- Offline/injected embedder → version comparison skipped (`DriftVerdict` from model/dim/convention only
  or `n/a` if no baseline).
