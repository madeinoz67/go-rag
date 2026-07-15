# Research — Settings View (Slice 0, spec 055)

> Phase 0 output. Resolves the spec's open technical questions by reading the real
> engine/config surface. The spec carried zero `[NEEDS CLARIFICATION]`; this file
> confirms the build is **UI-only** (a 5th adapter over surfaces that already exist)
> and nails the exact projection + the mirror pattern.

## R1 — Is the effective-configuration surface already projected by the engine?

**Decision: YES — `Engine.Status(vault) (*StatusInfo)` + `Config` cover ~all of it.**
No new engine capability is needed; the view is a read-only projection (UI-only),
exactly like spec 054 (Observability).

**Rationale (grounded):**
- `StatusInfo` (`internal/engine/types.go:140`) already carries: `EmbeddingModel`,
  `Dimensions`, `EmbeddingConvention` (stored majority), `ConfiguredPrefix`
  (auto|on|off), `QueryPrefix`/`DocPrefix` (resolved), `OllamaURL`, `Reranker`,
  `ResultCache`/`EmbeddingCache` (`CacheStats`: enabled/size/capacity/hits/misses),
  `PoolSize` (effective), `AdaptiveDepthEnabled`, `NearDupChunks`, and the drift
  fields.
- `Config` (`internal/config/config.go:19`) holds every knob: `RRFK`, `ChunkSize`,
  `ChunkOverlap`, `QueryCacheEnabled`/`QueryCacheResults`/`QueryCacheEmbeddings`,
  `PIIRedactEnabled`/`PIIPatterns`, `PoolSize`, `RerankModel`/`RerankCandidates`/
  `RerankRetryOnFailure`, `AdaptiveDepthEnabled`, `NearDupHamming`, `EmbeddingPrefix`.
- `Engine.Status` (`internal/engine/status.go:34`) computes these live per vault;
  the existing UI handlers (`handleDashboardStats`, `toBridgeOpsStats`,
  `handleObservabilityMetrics`) already consume it the same way.

**Alternatives considered:**
- *New `Engine.EffectiveConfig()` method* — rejected: `Status` + the public `Config`
  already expose everything; a new method would duplicate and risk drift. The UI
  handler reads `Engine.Status(vault)` + the engine's config the way 049/054 do.
- *Persist a settings snapshot* — rejected: violates "no new on-disk layout" and
  is unnecessary (values are live).

## R2 — Where do "default query depth / mode / threshold" come from? (spec FR-002)

**Decision: they are NOT operator-configurable config keys — they are built-in
behavior defaults applied per query request. The Settings retrieval section
therefore shows the actually-configurable retrieval knobs and does not invent
fictional default-k/mode/threshold config fields.**

**Rationale (grounded):** `Config` has no `QueryK`/`QueryMode`/`QueryThreshold`
fields; the only retrieval-shaped defaults in `config.go` are
`DefaultQueryCacheResults` and `DefaultPoisonThreshold*` (unrelated). `StatusInfo`
does not surface default k/mode/threshold either. Query depth/mode/threshold are
resolved per request inside `Engine.Query` and surfaced **per-query** by the Query
view (spec 048 "effective mode/k/threshold" transparency) — which is their correct
home.

**Plan-level refinement of FR-002:** the retrieval section displays `rrf_k`
(effective), `pool_size` (effective), reranker model + candidates + retry,
`adaptive_depth_enabled`, and `near_dup_hamming` — the real configurable surface.
Built-in query depth/mode/threshold are noted in the UI as "defaults applied per
query (see Query view)", not editable. This honors the spec's intent (effective
retrieval config visible) while staying truthful about what is configurable. Logged
as a Decisions row; spec FR-002 may be softened in a follow-up if desired.

## R3 — How is the redaction "active pattern count" obtained? (spec FR-005)

**Decision: `len(redact.DefaultPatterns(cfg.PIIPatterns))` — a pure function in
the already-shipped `internal/redact` package; no storage, no new capability.**

**Rationale (grounded):** `redact.DefaultPatterns(customPath string) []Pattern`
(`internal/redact/patterns.go:68`) merges the built-in curated set
(`builtinPatterns`) with an optional custom-patterns file. The count is `len(...)`
of that slice. When redaction is disabled (`PIIRedactEnabled=false`) the count is
0. The UI handler imports `internal/redact` (already a project package) and calls
it directly — read-only, no ingest coupling.

## R4 — What is the UI handler / route / auth pattern to mirror?

**Decision: copy the spec-054 Observability pattern verbatim** — a `Server` method
handler that calls `Engine.Status(vault)` (vault extracted per spec 052), projects
to a DTO, and `writeJSON`s; route registered in `internal/ui/ui.go` alongside the
other `/api/*` routes, guarded by the same Bearer middleware.

**Rationale (grounded):**
- Handlers: `Server.handleObservabilityMetrics` (`observability.go:148`),
  `Server.handleDashboardStats` (`dashboard.go:39`), `toBridgeOpsStats(vault)`.
- Auth: `Server.token` (`ui.go:42`), `New(... token ...)` (`ui.go:49`),
  `TestGuardedRoute_RequiresBearer` (`ui_test.go:171`),
  `TestObservabilityMetrics_401Unguarded` — the guard and its test pattern.
- Placeholder seam: `placeholderViews` (`placeholder.go:15`) currently lists
  `settings: planned`; remove that entry, keep `memory-graph: blocked`.
  `TestSidebar_ViewSet` (`ui_test.go:261`) pins the set — update its `want` map.
- Sidebar: `index.html` has the Settings nav button (8 items); wire it to the real
  Alpine view instead of the placeholder fetch. Add the view to `app.js`.

**Alternatives considered:** none — mirroring the established pattern is the
constitution Principle-V choice (UI-only over existing interfaces).

## R5 — Constitution compliance (pre-check)

All five principles hold; **no on-disk layout change**:
- I (local-first): read-only, no egress. ✓
- III (pure Go): vendored SPA, no Node build (`TestNoNodeArtifacts`). ✓
- IV (async-after-ACK): not engaged (read-only). ✓
- V (extension by interface): UI-only over `Engine.Status` + `Config` +
  `redact.DefaultPatterns`; no new engine method required. ✓
- Storage discipline: no new prefix, no migration, no `ExpectedVersion` bump. ✓
