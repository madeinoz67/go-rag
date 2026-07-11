# Research — Bridge Ops View (Slice 3)

**Feature**: specs/049-ui-bridge-ops | **Date**: 2026-07-12

Phase 0 output. Resolves the spec's Open Questions and records the design decisions from
source-inspecting the engine/audit/dashboard surfaces. Every decision is grounded in code read
this session.

---

## Source grounding (verified before designing)

- `Engine.Status() *StatusInfo` — `internal/engine/types.go:134`. Already carries the operational
  depth: `EmbedPending`/`EmbedFailed` (spec 030), drift (H11/H03/H07 — `DriftVerdict`,
  `HardDrift`, `VersionDrift`, `CorpusBaseline{Model,Dim,Convention,OllamaVer,RecordedAt}`,
  `LiveOllamaVersion`, `ModelCounts`/`DimCounts`/`ConventionCounts`), poisoning (H04 —
  `PoisoningEnabled`, `PoisonThreshold{Sus,Qua}`, `PoisonFlagged`, `PoisonSources`,
  `PoisonPhrases`), enrichment (spec 029 — `EnrichmentEnabled`, `CaptioningEnabled`,
  `EnrichedDocs`), caches (spec 016 — `ResultCache`/`EmbeddingCache` of type `CacheStats`),
  adaptive retrieval (spec 024 — `PoolSize`, `AdaptiveDepthEnabled`, `PoolUtilization`),
  `NearDupChunks`.
- **Dashboard already projects a subset**: `internal/ui/dashboard.go::dashboardDTO` carries
  `Documents/Chunks/Embeddings/Dimensions`, `EmbeddingModel/Reranker/OllamaURL`,
  `EmbeddingsComplete`, `DriftVerdict`, `HardDrift`, `EmbedPending`, `EmbedFailed`,
  `EnrichmentEnabled`, `EnrichedDocs`, `Vault`. So backlog + verdict + enrichment state are
  already visible on the Dashboard.
- `audit.Read(path, opts audit.ReadOptions) ([]audit.Event, error)` — `internal/audit/reader.go`.
  `ReadOptions{Type, All, Tail, Since}`. `audit.DefaultPath(dbPath)`. Durable JSONL log (rotated).
- `go-rag audit` CLI — `internal/cli/audit.go::newAuditCmd`: `--tail N` (default 20),
  `--type query|ingest|auth-fail`, `--since 1h`, `--all`, `--format`. Calls `audit.Read` after
  resolving the path from `cfg.AuditPath`. **Audit-read is CLI-exposed; it is NOT on
  REST/gRPC/MCP** (a pre-existing spec 021 gap).
- `Engine.Events()` returns the in-memory event `Bus` (`Bus.Subscribe` is forward-only — no
  historical replay). So the bus cannot serve a recent-activity feed on page-load.
- `Config.WatchDirs []string` (default `["."]`) — `internal/config/config.go`. The daemon does
  **not** wire a persistent watcher (`internal/daemon` has no watcher references) — scanning is
  on-demand.

**Headline findings**: (1) the operational tiles need **no new engine accessor** — `StatusInfo`
already has everything; Bridge Ops differentiates from the Dashboard by surfacing what the
Dashboard omits (drift detail, poisoning/cache/adaptive subsystems, activity, watch config).
(2) The activity feed needs **one thin read-only engine wrapper** (`Engine.AuditRead`) over the
existing `audit.Read`; the bus is unsuitable (forward-only). (3) Audit cross-transport parity
is a pre-existing gap, not introduced here.

---

## R1 — Two routes, both in-process engine calls (resolves route shape)

**Decision**: `GET /api/bridge-ops/stats` (operational projection of `Engine.Status` +
`WatchDirs`) and `GET /api/bridge-ops/activity?tail=N&type=ingest` (recent events via
`Engine.AuditRead`). Both guarded by `Server.guard`. The UI calls the engine in-process — a 4th
adapter, not a REST proxy.

**Rationale**: Mirrors the established pattern (Dashboard calls `Engine.Status`, Documents calls
`Engine.ListDocuments`, Query calls `Engine.Query`). Stats is a read of `StatusInfo`; activity
is a read of the audit log via the new thin wrapper. Two routes keep the heavy `StatusInfo`
projection (cheap, engine-internal) separate from the bounded audit tail (file read) so each
can be refreshed independently.

**Alternatives rejected**: (a) one mega-route — rejected: couples a cheap status read to a
file-scan and forces one refresh cadence. (b) UI reads the audit file directly — rejected:
pushes filesystem knowledge into the UI layer, breaking the in-process-adapter invariant.

---

## R2 — Operational tiles reuse `StatusInfo`; differentiation is depth + subsystems (resolves Dashboard overlap)

**Decision**: US1/US3/US4 project `StatusInfo` directly. Bridge Ops does NOT re-list the
Dashboard's corpus counts; it shows the operational subset the Dashboard omits: **drift detail**
(baseline-vs-live, not just the verdict), the **subsystem tiles** (poisoning, caches, adaptive
pool, captioning, near-dup — none on the Dashboard), and **watch config**. The backlog
(pending/failed) repeats from the Dashboard intentionally — it is the centre of the operational
view and gives the activity feed its context.

**Rationale**: avoid duplicating the Dashboard; add what it lacks. An operator glances at the
Dashboard for counts, opens Bridge Ops for health/subsystems/activity.

**Alternatives rejected**: (a) duplicate the Dashboard counts in Bridge Ops — rejected: redundant.
(b) Move backlog off the Dashboard to Bridge Ops — rejected: the Dashboard's glance-value
includes "is embedding done"; don't take that away.

---

## R3 — Activity feed via a thin `Engine.AuditRead` wrapper (resolves OQ1/OQ2 — source + accessor)

**Decision**: Add `Engine.AuditRead(opts audit.ReadOptions) ([]audit.Event, error)` — a thin
read-only wrapper that resolves the audit path (`cfg.AuditPath` or `audit.DefaultPath(cfg.DBPath)`)
and delegates to `audit.Read`. The UI calls it in-process.

**Rationale**: the audit log is the only **durable, historical** record of ingest events. The
event bus is forward-only (subscribers get events after they subscribe — useless for a feed on
page-load). `WatchDocuments` (spec 040) is a stream, not a historical query. So audit is the
source. The engine owns its config/paths, so the wrapper lives on the engine (not the UI
layer), keeping the "UI talks to engine, not filesystem" invariant. The capability already
exists (`audit.Read` + `go-rag audit` CLI) — the wrapper makes it reachable in-process by the
UI; it is not a new operation.

**Alternatives rejected**: (a) the event bus — rejected: no historical replay. (b) UI reads the
file — rejected: layer violation. (c) `WatchDocuments` replay — rejected: it's a live stream,
not a bounded recent-events query.

---

## R4 — Activity is bounded: tail-N, type-filtered (resolves the window question)

**Decision**: `GET /api/bridge-ops/activity` returns the last N ingest events (default `tail=20`,
max e.g. 100), filtered to `type=ingest` by default. Mirrors `go-rag audit --type ingest --tail
20`. Older events are not loaded.

**Rationale**: a recent-activity feed is bounded by intent; an unbounded log dump is a
performance and UX hazard. `tail` + `type` match the CLI's proven shape (parity + familiarity).

**Alternatives rejected**: (a) time-window (`since=1h`) as the default — rejected: on a quiet
vault it returns nothing confusingly; tail-N always shows the latest N. (b) All event types —
rejected: ingest is the operational signal; query/auth-fail noise the feed (the `type` param
lets the operator broaden if desired).

---

## R5 — `WatchDirs` surfaced for the UI; honest scan-driven framing (resolves OQ watcher)

**Decision**: The stats DTO includes the configured `WatchDirs` (resolved from the engine's
config). The view presents them with an explicit "scan-driven, not always-on" framing — there
is no live watcher process to report a running state from in this slice.

**Rationale**: the daemon runs no persistent watcher, so the honest surface is the configured
directories + the fact that scanning is on-demand. Surfacing `WatchDirs` requires the engine to
expose them to the UI (a small additive read — resolved at tasks time as a `StatusInfo` field
addition vs. a config peek, both read-only).

**Alternatives rejected**: (a) claim a live watcher status — rejected: none exists; would be
dishonest. (b) Omit watch config entirely — rejected: it's a stated part of the view's scope and
the operator's legitimate question.

---

## R6 — Drift detail: verdict + cause at a glance, full baseline expandable (resolves OQ depth)

**Decision**: Show the drift verdict (clean / hard-drift / version-warning / unknown / n-a) and
a one-line cause (which dimension drifted — model / dimensionality / convention / Ollama
version) at a glance; the full baseline-vs-live breakdown (CorpusBaseline{*}, LiveOllamaVersion,
drift counts) is expandable behind the tile.

**Rationale**: the verdict + one-line cause is enough for the "is something wrong" glance; the
full breakdown is forensics for the operator who needs to act. Matches the Dashboard's glance
density while going deeper on demand.

**Alternatives rejected**: (a) full breakdown always-visible — rejected: noise for the common
clean case. (b) verdict only — rejected: the Dashboard already shows the verdict; Bridge Ops
must add the detail to justify itself.

---

## R7 — Subsystem tiles, not a table (resolves OQ tiles-vs-table)

**Decision**: Each subsystem (poisoning, enrichment, caches, adaptive retrieval) gets its own
tile — on/off state + the one or two numbers that matter — in a grid. Per `docs/style-guide.md`
(tile grid for scannable state).

**Alternatives rejected**: (a) one dense table — rejected: less scannable for on/off state at a
glance. (b) One tile per metric — rejected: too many tiles; group by subsystem.

---

## R8 — Refresh on view-entry + manual; no live streaming (resolves OQ live)

**Decision**: The view fetches stats + activity on entry and on explicit refresh. No SSE /
auto-polling / live streaming this slice.

**Rationale**: a manual refresh matches the "check on it" operational use; live streaming adds
complexity (SSE wiring, backpressure) disproportionate to a single-operator ops view. A future
always-on-watcher spec would be the place to introduce streaming.

**Alternatives rejected**: (a) auto-poll every Ns — rejected: pointless load on a quiet vault,
and the backlog drains in seconds-to-minutes, not sub-second. (b) SSE from the event bus —
rejected: the bus is forward-only and would only cover new events, not the initial feed.

---

## R9 — Audit cross-transport parity is a pre-existing gap, carried forward

**Decision**: 049 adds `Engine.AuditRead` + the UI consumer only. It does NOT add REST/gRPC/MCP
audit endpoints. The gap (`go-rag audit` CLI-only) originates in spec 021 and is documented in
the plan's Complexity Tracking as a follow-up.

**Rationale**: the UI consumes the engine in-process; full cross-transport audit parity is
orthogonal to a UI view and would balloon the slice (5-transport change + proto) for a read the
UI gets directly. The capability already exists; 049 doesn't invent it.

**Alternatives rejected**: (a) full parity in 049 — rejected: scope blow-up; not a UI concern.
(b) Block 049 until parity lands — rejected: the gap predates 049 and the UI is a legitimate
4th consumer.
