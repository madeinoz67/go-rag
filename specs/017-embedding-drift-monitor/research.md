# Research — Embedding Drift Monitoring + Version Pinning (H11)

> Phase 0 output. Resolves the Technical Context unknowns and the spec's open
> design questions (D1–D8) against the live codebase (read 2026-06-22). Each
> decision cites its evidence. The spec is the WHAT/WHY; this file is the HOW
> rationale that `/speckit-tasks` implements against.

## Context: what already ships (H11 layers on these, does NOT redo them)

- **H03 / spec 005** — `checkEmbeddingMismatch` (`internal/engine/query.go`) refuses a query whose
  model/dim/convention ≠ the corpus's stored majority (the per-embedding 0x04 records). Reactive
  (query-time). `CorpusProfile(db)` (`embedding_profile.go`) scans the 0x04 records for the majority
  model/dim/convention + drift counts — **reused for backfill (D5)**.
- **H07 / spec 008** — per-embedding convention provenance on the 0x04 record.
- **H06 / spec 016** — `Migrate` flushes the query caches; the daemon shares one engine across
  MCP/REST/gRPC (`serve.go` → `NewWithEngine`).
- **Health** (`health.go`) — `HealthInfo{OK, StorageOpen, EmbedderReachable}`; `OK = storageOpen`.
  REST `/health` + gRPC health RPC both call `Engine.Health` (parity).

## D1 — Corpus baseline: storage prefix + shape

**Decision**: a new Pebble prefix `PrefixCorpusMeta = 0x07` holding a **single fixed** record (key =
`0x07` + sentinel, e.g. `"default"`), JSON-encoded `CorpusBaseline`. Read/written via the existing
`db.{Get,Set}WithPrefix` helpers.

**Shape**: `{model, dim, convention, ollama_version, recorded_at}`.

**Rationale**: the baseline is corpus-level metadata, not a document and not user config — it deserves
its own prefix to keep it out of the `config get` namespace (0x09) and distinct from per-embedding
provenance (0x04). Prefix choice: 0x05/0x07/0x08 are free (storage.go: 0x01–0x04, 0x09–0x0F used);
**0x07** chosen, leaving 0x05/0x08 spare and **0x06 reserved for H16** (persistent index snapshot, per
its backlog note). Single record under the prefix (the prefix denotes the subsystem, matching the
codebase's "prefix = subsystem" idiom).

**Alternatives rejected**:
- Store under `PrefixConfig` (0x09) with a reserved key — pollutes the user-facing `config get` surface;
  conflates corpus state with user-editable config.
- Re-derive the majority on every boot (no persisted record) — no home for `ollama_version` (not stored
  anywhere today) and not point-in-time; defeats the "baseline snapshot" purpose.
- Per-embedding ollama-version on the 0x04 record — redundant with the model/convention already there
  and inflates every embedding record for a corpus-level fact.

## D2 — Ollama version fetch (and why it's not on the Embedder interface)

**Decision**: a standalone engine helper `ollamaVersion(ctx, baseURL) (string, error)` in
`internal/engine/version.go` that `GET {baseURL}/api/version` with a short timeout (mirrors
`embedderReachable` in `health.go`) and parses `{"version": "…"}`.

**Rationale**: the version is a property of the **Ollama server**, not of the `Embedder` interface
(whose contract is `Embed/Model/Dimensions`). Putting `Version()` on `Embedder` would force every
embedder (incl. the deterministic test/eval fake and the `recordingEmbed` doubles) to implement it and
would leak an HTTP/server concern into the embed abstraction. A free function over `baseURL` keeps the
interface closed (constitution Principle V: extension by interface — don't expand it needlessly).

**Failure modes**: unreachable / endpoint errors / non-200 → return `"unknown"` + nil error (the boot
must not fail on a version-fetch hiccup; FR-006). Empty base URL (injected/offline embedder path) →
return `""` (the caller treats `""` as "skip the version comparison", FR-010).

## D3 — Startup-check placement + when the verdict is computed

**Decision**:
- The verdict is computed by `Engine.computeDriftVerdict(ctx)` (`drift.go`): load baseline (D1) → live
  values (configured model; resolved convention from `cfg.Prefixer()`; live dim from
  `embedderOrOllama().Dimensions()`; live Ollama version via D2) → compare per D6.
- **`serve.go`** calls `eng.RefreshDriftVerdict(ctx)` once after constructing the engine (before serving
  listeners) and **logs the verdict** (the "loud at startup" signal, FR-004/FR-005).
- The engine **caches** the verdict (`driftVerdict` field, mutex-guarded) so `/health` reads it
  **without re-fetching** Ollama on every probe (probes must stay fast).
- **`Status()`** recomputes the verdict **live** (it already does non-trivial work + is called less
  often) so the detailed view reflects the current Ollama version.
- **`Migrate`** refreshes both the baseline and the cached verdict after successful re-embed.
- The one-shot CLI path (`go-rag status` against a stopped daemon, and `go-rag query`) constructs a
  fresh engine per process and computes the verdict on demand (no daemon needed to see drift in
  `status`).

**Rationale**: probes (`/health`) must be O(1) → read a cached verdict; `status` is the on-demand
detailed view → recompute live; boot + migrate are the natural refresh points. This avoids per-probe
HTTP to Ollama while keeping the operator-facing `status` fresh.

**Alternatives rejected**: compute on every `/health` (slow probes, Ollama fetch per probe); never cache
(stale `status` after a migrate until restart).

## D4 — Liveness vs readiness in `HealthInfo` + `/health` HTTP semantics

**Decision** (the clarification's locked posture A):
- `HealthInfo` gains **`Ready bool`** (readiness) distinct from **`OK bool`** (liveness). `Ready =
  OK && verdict != hard-drift`. `OK` stays "process alive + storage open" (unchanged liveness meaning).
- **REST `/health`**: HTTP **200** while the process is up (liveness — does **not** 503, to avoid
  restart-loops if an orchestrator wires `/health` as a *liveness* probe); the **body** carries
  `{ok, ready, storage_open, embedder_reachable, drift_verdict}` so a client/orchestrator reading the
  body sees readiness (FR-011).
- **gRPC health RPC** (grpc-health-v1 semantics): `SERVING` when ready, **`NOT_SERVING`** on hard drift
  — the canonical gRPC readiness channel, so gRPC clients deflect traffic.
- `mcp/health` (the daemon's startup probe target, unauthenticated) reports the same `ready` field.

**Rationale**: the clarification explicitly chose readiness-over-liveness to "avoid restart loops." A
503 on `/health` would re-introduce that risk for anyone using `/health` as a liveness probe. Keeping
`/health` = 200 (liveness) + a `ready` body field + gRPC `NOT_SERVING` gives an honest readiness signal
without the restart-loop footgun. (A future dedicated `/ready` endpoint returning 503 is possible if a
status-code-based REST readiness probe is needed — out of scope here; noted in contracts.)

**Decision on the verdict's effect on serving**: hard drift does **not** stop the daemon from serving
`status`/`migrate`/`config` (the operator needs those to remediate) — only query readiness is
withdrawn (and H03 refuses the mismatched queries regardless). So `/health` readiness is a query-served
signal, not a process-health signal.

## D5 — Baseline write / refresh / backfill timing

**Decision**:
- **Write (first embed)**: `processJob` (`workers.go`), after a successful embed, if no baseline exists
  yet, writes one from the embedder's model + the produced dim + the active convention + the engine's
  **cached live Ollama version** (captured at boot, D3) + a UTC `recorded_at` timestamp. (Async,
  post-ACK — Principle IV.)
- **Refresh (migrate)**: `Engine.Migrate` (`ingest.go`), after `ReprocessAll` succeeds, re-fetches the
  live Ollama version and rewrites the baseline with the new profile (post-migrate the corpus is uniform
  under the new model). Also refreshes the cached verdict.
- **Backfill (pre-H11 corpus)**: in the boot drift-check path (`RefreshDriftVerdict`), if no baseline
  exists but embeddings exist, derive the profile from `CorpusProfile(db)` (H03's majority scan) +
  cached live version + now, and persist it — **before** the comparison, so the first boot of an old
  corpus establishes a baseline rather than reporting "unknown". No re-ingestion (FR-007).
- **Empty corpus**: no baseline, no embeddings → verdict `clean`/`n/a` (nothing to compare); the
  baseline is written when the first embedding lands.

**Rationale**: the baseline must exist before it can be compared; backfill-on-first-boot makes H11
upgrade-safe for existing vaults without a re-ingest. Refresh-on-migrate keeps it current after the
operator's remedy. Using the cached boot-time Ollama version for the first-embed write avoids a
per-embed HTTP fetch and is accurate enough (the corpus is built under the currently-running server).

## D6 — Drift comparison rules (what's hard vs soft vs clean)

**Decision** (`computeDriftVerdict`):
- **model**: baseline.model vs `cfg.EmbeddingModel` → **hard** if differ (and baseline exists).
- **dim**: baseline.dim vs live `embedder.Dimensions()` → **hard** if differ AND live dim > 0 (dim is
  unknown until the first embed/response; skip when 0 — the model check already catches a swap at boot).
- **convention**: baseline.convention vs `cfg.Prefixer().Convention()` → **hard** if differ.
- **ollama-version**: baseline.ollama_version vs live version → **soft** (warn) if differ AND both are
  known/non-empty (version is `""` for offline/injected embedder → skip; `"unknown"` for unreachable →
  skip with a note).
- Precedence: **hard wins** over soft (if model mismatches AND version differs, report hard). Verdict
  values: `clean` | `hard-drift` | `version-warning` | `unknown` (Ollama unreachable and a comparison
  was attempted) | `n/a` (empty corpus / injected offline embedder with no baseline).

**Rationale**: matches the spec's severity split (FR-004 hard, FR-005 soft) and the offline/unreachable
edge cases (FR-006/FR-010). Hard-wins-over-soft keeps the operator-facing signal unambiguous.

## D7 — Ollama-version comparison granularity (the deferred clarify item)

**Decision**: **full version-string compare** — any difference is a soft warning.

**Rationale**: simplest, and conservative in the right direction — the book's §4.6 failure is a
library/server update silently changing pooling, which a *patch* bump could do. Ollama releases are
infrequent, so noise is low. The baseline records the exact string, so the comparison is exact.

**Alternative rejected**: compare major.minor only (ignore patch) — a patch *could* change behavior;
ignoring it would miss the exact failure the feature exists to catch. Revisit only if patch-noise
becomes a real complaint (then add a config knob).

## D8 — Concurrency & where state lives

**Decision**:
- `Engine` gains `driftVerdict DriftVerdict` + `verdictMu sync.RWMutex` + `liveOllamaVersion string`
  (cached at boot/refresh). `RefreshDriftVerdict(ctx)` computes + stores under the write-lock; `Health`
  reads under the read-lock (fast). `Migrate` calls `RefreshDriftVerdict` after refreshing the baseline.
- The baseline record is a single Pebble key — read/written atomically via the existing `db.Get/Set`
  (Pebble is the single writer; no concurrency concern beyond what the DB already guarantees).
- No interaction with the query-cache locks (H06) or the index epoch — drift detection is read-only
  w.r.t. those. No lock-ordering change.

**Rationale**: the constitution's single-writer/concurrent-reader model maps onto RWMutex around the
cached verdict; the baseline is a single KV record with no read-modify-write race (written from
processJob-once and migrate-serially).

---

## Summary of resolved decisions

| ID | Decision | One-line |
|----|----------|----------|
| D1 | Baseline storage | New prefix `0x07`, single fixed JSON record `{model,dim,convention,ollama-version,recorded-at}` |
| D2 | Ollama version fetch | Standalone `ollamaVersion(ctx, baseURL)` helper (not on the Embedder interface); `""` offline, `"unknown"` on error |
| D3 | Startup check | `serve.go` calls `RefreshDriftVerdict` + logs at boot; verdict cached; `Status` recomputes live; `Migrate` refreshes |
| D4 | Liveness vs readiness | `HealthInfo.Ready` distinct from `.OK`; `/health` = 200 + body `ready`; gRPC health RPC NOT_SERVING on hard drift (no 503 → no restart-loop) |
| D5 | Baseline timing | Written on first embed (async); refreshed on successful migrate; backfilled on first boot for pre-H11 corpora |
| D6 | Drift rules | model/dim/convention = hard; ollama-version = soft; hard-wins; clean/unknown/n/a verdicts |
| D7 | Version granularity | Full-string compare (any diff = soft warn) |
| D8 | Concurrency | RWMutex-cached verdict on the engine; single KV record; no lock-ordering change |

**No unresolved NEEDS CLARIFICATION.** Ready for Phase 1 design.
