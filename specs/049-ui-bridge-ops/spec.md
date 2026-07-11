# Feature Specification: go-rag Management Console — Bridge Ops View (Slice 3)

**Feature Branch**: `049-ui-bridge-ops`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: *"Specify the Bridge Ops view — Slice 3 of the 8-view sidebar established in spec 046-ui-app-shell. It replaces the current Bridge Ops placeholder. Per spec 046's Slice Decomposition this is the 'go-rag-native half: ingest/watcher status' — a read-only OPERATIONAL view into how the engine's async ingest pipeline and file watcher are doing, going deeper than the Dashboard's corpus counts. An operator should be able to answer: is ingestion progressing, what's the embedder/embed backlog doing, is detection/enrichment healthy, what's drifted, and what ingest/reingest events have happened lately."*

## Context & Background

Slice 0 (spec 046) shipped the console app shell; Slices 1–2 (047 Documents, 048 Query)
shipped the corpus-browse and retrieval surfaces. **This spec replaces the Bridge Ops
placeholder (view 4)** with the operational-health surface — exactly as reserved in spec 046's
Slice Decomposition ("Slice 3 — Bridge Ops view (go-rag-native half: ingest/watcher status) →
spec 049").

The Dashboard (spec 046) already shows corpus **counts** — documents, chunks, embeddings,
model, reranker, completion flag. Bridge Ops goes deeper: it surfaces the **operational state
behind those counts** so an operator can tell whether the engine is healthy and making
progress, without opening the CLI. Concretely it answers: how big is the embedding backlog and
are any chunks failing; has the embedding model drifted; are poisoning detection and
enrichment on and what have they produced; what retrieval-cache and adaptive-pool behaviour is
in effect; and what ingest/reingest activity has happened recently.

Nearly all of this data **already exists** on `engine.StatusInfo` — the structured health view
the CLI `go-rag status` renders textually. Bridge Ops is a read-only browser projection of that
operational surface (the subset the Dashboard does not show), plus a recent-activity feed from
the engine's event/audit surfaces. The view reuses verbatim — and changes none of — the spec
046 shell, the Alpine `goragApp` root, the 4-layer CSS, `go:embed` static serving, the loopback
UI transport, and the spec 045 Bearer-session guard. It introduces **no new transport, no new
storage, no new auth, no new engine capability, and no Node/build chain**.

A note on "watcher": go-rag's file watcher (spec 007) is **scan-driven** — the daemon does not
run a persistent watcher, so there is no always-on watcher process to report a running state
from. Bridge Ops therefore shows the **configured watch directories** and **recent ingest
activity** rather than a live watcher feed. (A future always-on watcher would gain a live
status tile here; tracked in Open Questions, not this slice.)

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See pipeline health at a glance (Priority: P1)

An operator opens the **Bridge Ops** sidebar item and sees the engine's operational health in
one screen, going beyond the Dashboard's counts: the **embedding backlog** (chunks pending and
chunks permanently failed — spec 030), an **embedding-completion** indicator, the **drift
verdict** (clean / hard-drift / version-warning — is the live embedder still compatible with
the corpus), and a **last-activity** timestamp. This is the "is the engine healthy and making
progress, or is something stuck" view — the primary reason Bridge Ops exists.

**Why this priority**: the gate to the rest of the view. Without pipeline health, activity and
subsystem state have no context. This single story is a viable MVP.

**Independent Test**: On an isolated DB, ingest a corpus large enough that embedding is still
in progress; open Bridge Ops; the pending count is non-zero and matches the CLI's
`go-rag status` backlog; wait for embedding to complete; the pending count drops to zero and
the completion indicator flips to complete; force a drift condition (change configured model)
and confirm the drift verdict updates.

**Acceptance Scenarios**:

1. **Given** a corpus mid-embedding, **When** Bridge Ops opens, **Then** the pending backlog is
   shown and matches `go-rag status`, and the completion indicator shows "in progress".
2. **Given** a fully-embedded corpus, **When** Bridge Ops opens, **Then** pending is zero, the
   indicator shows "complete", and any permanently-failed count is shown distinctly.
3. **Given** a drift condition (model/dimension/convention mismatch, or Ollama-version change),
   **When** Bridge Ops opens, **Then** the drift verdict is shown plainly (clean / hard-drift /
   version-warning) with enough detail to act on it.
4. **Given** the view is read-only, **When** the operator interacts with it, **Then** no
   document is added, removed, reingested, re-embedded, or re-configured.

---

### User Story 2 - See recent ingest / reingest activity (Priority: P1)

An operator can see **what's happened lately**: a recent-activity list of ingest and reingest
events — what was added, changed, or removed, the outcome (success / failed / skipped), and
when. This is the "what has the engine been doing" view, drawn from the engine's existing event
and audit surfaces (specs 020 / 021 / 040). It lets an operator confirm a recent `add`/`scan`
landed and spot silent failures.

**Why this priority**: a health snapshot alone doesn't tell you whether the operation you just
ran worked. Without the activity feed the operator still needs the CLI to confirm.

**Independent Test**: Ingest several documents on an isolated DB (including one that will fail
embedding); open Bridge Ops; the activity list shows the recent ingest events in reverse-chron
order with outcomes and timestamps; the failed document is distinguishable from the successes;
counts and outcomes match the audit log.

**Acceptance Scenarios**:

1. **Given** recent ingest activity, **When** Bridge Ops opens, **Then** a recent-activity list
   renders in reverse-chronological order with event type, outcome, and timestamp.
2. **Given** a document whose embedding failed, **When** its activity entry renders, **Then**
   the failure is clearly distinguishable from successes (not silently omitted).
3. **Given** the activity list, **When** more events exist than shown, **Then** a bounded
   number renders (most-recent first) with no unbounded growth — older events are not loaded.
4. **Given** no recent activity (fresh vault), **When** Bridge Ops opens, **Then** a healthy
   empty state renders (not an error).

---

### User Story 3 - See subsystem states — detection, enrichment, cache, retrieval tuning (Priority: P2)

An operator can see, at a glance, the state of the optional subsystems that affect retrieval
quality and cost: **poisoning detection** (on/off, how many chunks flagged, threat-list size —
spec 019), **document enrichment** (on/off, how many docs enriched — spec 029), the
**retrieval caches** (enabled, size, hit/miss — spec 016), and **adaptive retrieval** (pool
ceiling, classifier posture, utilization — spec 024). This is the "what's configured and
active, and is it pulling its weight" view.

**Why this priority**: essential for tuning and for noticing a subsystem that's off when you
thought it was on, but the view is already valuable at P1 (health + activity) without it.

**Independent Test**: Toggle enrichment/poisoning on in config, re-open Bridge Ops; the
subsystem tiles reflect the new state (on, with non-zero produced counts after a re-ingest);
run queries and confirm the cache hit/miss counters advance; the states match `go-rag status`.

**Acceptance Scenarios**:

1. **Given** poisoning detection is enabled and has flagged chunks, **When** Bridge Ops opens,
   **Then** the detection tile shows on/off, the flagged count, and the threat-list size.
2. **Given** enrichment is enabled, **When** Bridge Ops opens, **Then** the enrichment tile
   shows on/off and the enriched-doc count; given it's disabled, **then** the tile shows off.
3. **Given** queries have run, **When** the cache tile renders, **Then** enabled state, size,
   and hit/miss counts are shown.
4. **Given** adaptive retrieval, **When** the adaptive tile renders, **Then** the pool ceiling,
   classifier posture, and utilization are shown.

---

### User Story 4 - The view is read-only, shell-consistent, and honest about the watcher (Priority: P2)

The Bridge Ops view introduces no writes, no new authentication, no Node/build chain, and
renders inside the authenticated shell using the established component system. It shows the
**configured watch directories** (spec 007 config) and clearly reflects that file watching is
**scan-driven, not always-on** — there is no live watcher process to report a running state
from in this slice. It degrades gracefully on a fresh/empty vault and on an unreachable
embedder. This is a constraint (mirroring spec 046/047/048 US4), proven once so every later
view inherits it.

**Why this priority**: not a feature but a hard invariant (read-only this slice; no Node;
single binary; honest watcher scope). P2 because the view is functional before the invariant is
formally proven, but it must hold before the slice ships.

**Independent Test**: Inspect every network call the view issues — all are read-only requests
to guarded routes; confirm the view renders inside `goragApp` with no full page reload; confirm
no `package.json`/`node_modules`/build config is introduced; on a fresh vault and with the
embedder down, confirm deliberate empty/error states (not crashes).

**Acceptance Scenarios**:

1. **Given** the view in use, **When** its network calls are inspected, **Then** every call is a
   read-only request to a guarded `/api/*` route — no create / update / delete.
2. **Given** configured watch directories, **When** Bridge Ops opens, **Then** the configured
   directories are listed and the scan-driven (not always-on) nature is reflected honestly.
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts are
   introduced.
4. **Given** a fresh/empty vault, **When** Bridge Ops opens, **Then** healthy empty/zero states
   render (not errors); given the embedder is unreachable, **Then** the drift/version signals
   degrade to "unknown" plainly, not a crash.
5. **Given** a session that expires mid-view, **When** a fetch returns 401, **Then** the shell
   routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Fresh / empty vault** — zero counts everywhere, "no recent activity" empty state, drift
  verdict "n/a"; no errors.
- **Embedding in progress** — non-zero pending backlog, "in progress" indicator; the count
  updates on refresh as the backlog drains.
- **Permanently-failed embeddings** — a distinct failed count, not hidden inside pending.
- **Embedder unreachable** (Ollama down) — drift/version signals degrade to "unknown" plainly;
  the backlog is still shown; no crash, no silent "all healthy".
- **Hard drift** (model/dim/convention mismatch) — verdict shown prominently so the operator
  knows queries may be degraded or refused.
- **No recent activity** — healthy empty activity feed, not an error.
- **Very large backlog** (thousands pending) — the number renders without layout breakage.
- **Many subsystems off** (default state — poisoning/enrichment/caching may be off) — each tile
  shows "off / disabled" cleanly, not as an error.
- **Watch directories configured but never scanned** — shown as configured with no last-scan
  activity.
- **Session expires mid-refresh** — graceful return to login.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST surface the embedding backlog — pending count and permanently-failed
  count (spec 030) — and an embedding-completion indicator, read-only.
- **FR-002**: The view MUST surface the drift verdict (clean / hard-drift / version-warning /
  unknown / n-a) with enough detail to act (which dimension changed — model, dimensionality,
  convention, or Ollama version), read-only (specs H11/H03/H07).
- **FR-003**: The view MUST surface a last-activity timestamp so the operator can see when the
  engine last did work.
- **FR-004**: The view MUST render a recent-activity list of ingest/reingest events
  (reverse-chronological, bounded) with event type, outcome (success/failed/skipped), and
  timestamp, drawn from the existing event/audit surfaces (specs 020/021/040).
- **FR-005**: Failed activity entries MUST be clearly distinguishable from successes — never
  silently omitted.
- **FR-006**: The view MUST surface the state of the retrieval-affecting subsystems read-only:
  poisoning detection (on/off, flagged count, threat-list size — spec 019), enrichment
  (on/off, enriched-doc count — spec 029), retrieval caches (enabled, size, hit/miss — spec
  016), and adaptive retrieval (pool ceiling, classifier posture, utilization — spec 024).
- **FR-007**: The view MUST list the configured watch directories (spec 007 config) and reflect
  that watching is scan-driven (not always-on in this slice).
- **FR-008**: The view MUST be strictly read-only — no add/remove/reingest/scan/scan-trigger or
  any state mutation; every network call is a read-only request to a guarded route.
- **FR-009**: The view MUST render inside the authenticated shell, gated by the existing spec
  045 / spec 046 Bearer guard, with no new authentication surface.
- **FR-010**: The view MUST ship inside the single binary via the existing embedded, vendored
  SPA — no Node / Vite / Tailwind build chain.
- **FR-011**: The view MUST render healthy states for a fresh/empty vault, an unreachable
  embedder, and all-subsystems-off — no silent failures.
- **FR-012**: Every count, verdict, and state shown MUST match `go-rag status` and the other
  transports byte-for-byte (cross-transport parity — same engine surface, as the Dashboard /
  Documents / Query views).

### Key Entities *(include if feature involves data)*

- **Pipeline Health**: the embedding backlog (pending, failed), completion state, and
  drift verdict — the engine's "is it healthy and progressing" rollup.
- **Activity Event**: one ingest/reingest/remove occurrence — its type, outcome, and timestamp;
  a bounded recent feed drawn from the engine's event/audit surfaces.
- **Subsystem State**: the on/off + produced-count + tuning knobs for each retrieval-affecting
  subsystem (poisoning detection, enrichment, caches, adaptive retrieval).
- **Watch Configuration**: the configured watch directories and the scan-driven (not always-on)
  nature of file watching in this slice.
- **Drift Verdict**: the corpus-vs-live compatibility assessment (model / dimensionality /
  convention / Ollama-version), distinct from intra-corpus mixed-record drift.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can open Bridge Ops and tell within 5 seconds whether the engine is
  healthy, in progress, or stuck — without opening the CLI.
- **SC-002**: 100% of the health counts, drift verdict, and subsystem states shown match
  `go-rag status` — zero drift between the view and the CLI.
- **SC-003**: An operator can confirm a recent ingest succeeded (or spot that it failed) from
  the activity feed alone, without reading logs.
- **SC-004**: A drift condition or a non-zero failed-embedding count is never silently hidden —
  it surfaces plainly on first open.
- **SC-005**: No write action is possible from the Bridge Ops view — verifiable by inspecting
  every network call the view issues.
- **SC-006**: The view introduces zero new build tooling — a single `make build` still produces
  one binary that serves the console with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
  root, and spec 045 Bearer auth unchanged — exactly as specs 047 and 048 did.
- This slice is read-only and adds **no new engine capability**. Nearly all operational data
  already exists on `engine.StatusInfo` (backlog, drift, poisoning, enrichment, caches,
  adaptive pool); the recent-activity feed draws from the existing event/audit surfaces (specs
  020/021/040). Plan confirms whether `StatusInfo` alone suffices or a thin read-only
  activity/accessor surface is needed, and over what window recent activity is available.
- The file watcher is **scan-driven, not always-on** — the daemon does not run a persistent
  watcher, so this slice shows configured directories + recent activity, not a live watcher
  feed. (An always-on watcher would gain a live tile in a future slice.)
- Single-operator use; no multi-user or RBAC concerns (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- The view refreshes on demand (manual / on view-entry); live streaming dashboards are out of
  scope for this slice.

---

## Open Questions (to resolve in plan / tasks)

- **Recent-activity source + window** — whether the activity feed reads the audit log (spec
  021), the in-memory event bus (spec 020), or the WatchDocuments surface (spec 040), and over
  what bounded window (last N events vs last N hours). Lean: bounded last-N from the most
  durable source the engine already exposes, no new persistence.
- **Activity feed without a new accessor** — whether `StatusInfo` + an existing audit/event
  read suffices, or a thin read-only recent-events accessor is needed (and, if so, whether it
  ships cross-transport like spec 035–039 or is UI-internal). Lean: reuse existing surfaces;
  UI-internal accessor only if needed.
- **Drift detail depth** — how much drift detail (just the verdict, or the full
  baseline-vs-live breakdown) to show at a glance vs behind an expand. Lean: verdict + one-line
  cause at a glance, full breakdown expandable.
- **Subsystem tiles vs unified table** — whether each subsystem gets its own tile (poisoning /
  enrichment / cache / adaptive) or they share a compact table. Lean: tiles for scannability,
  decided in plan against the style guide.
- **Always-on watcher** — out of scope here, but if a future spec adds a persistent watcher,
  its live status (running, last change, queue) lands in this view.
