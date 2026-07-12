# Feature Specification: go-rag Management Console — Vaults View (Slice 5)

**Feature Branch**: `051-ui-vaults-view`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: *"Specify the Vaults view — the next sidebar view after Operations. A read-only view of the go-rag vaults on this machine (each a separate Pebble DB under ~/.go-rag/vaults/<name>): list vaults with per-vault identity (name, document count, embedding model, on-disk storage size, daemon-running state) and identify the active vault (the one this daemon is serving). Read-only initially, mirroring 047-049. Engine.ListVaults and the `go-rag vault list` CLI already exist; the UI reuses them."*

## Context & Background

Specs 046–050 built the console's read surfaces (Dashboard, Documents, Query, Operations) plus
the first write surface (Documents add/remove/reingest). **This spec replaces the Vaults
placeholder** (view 5 of the spec 046 sidebar) with a read-only view of the vaults on the
machine — the operator's answer to "what vaults do I have, how big is each, which one am I
serving, and what model does each use."

go-rag stores each vault as a separate Pebble DB under `~/.go-rag/vaults/<name>`; the daemon
serves exactly one (the active vault). The engine already exposes `Engine.ListVaults`
(returns each vault's name and document count) and the CLI `go-rag vault list` shows the
richer per-vault identity (name, docs, embedding model, daemon-running state, storage size).
The Vaults view is a read-only browser projection of that same surface, reusing the engine
in-process like every other console view.

The view reuses verbatim — and changes none of — the spec 046 shell, the Alpine `goragApp`
root, the 4-layer CSS, `go:embed` static serving, the loopback UI transport, and the spec 045
Bearer-session guard. It introduces **no new transport, no new storage, no new auth, no new
ingest logic, and no Node/build chain**. Plan confirms whether `Engine.ListVaults` (name + doc
count) suffices or a thin per-vault-detail accessor (model / storage / daemon state) is added.

A note on "active vault": the daemon serves exactly one vault per process. There is no live
vault-switching in this slice — the active vault is reported, not changed. (Switching would
restart the daemon against a different vault; that is a later, separately-specced concern.)

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - List vaults with their identity (Priority: P1)

An operator opens the **Vaults** sidebar item and sees every vault on the machine, each row
showing the vault's name, document count, embedding model, on-disk storage size, and whether
a daemon is running against it. This is the "what vaults exist and what state is each in"
view.

**Why this priority**: the gate to the rest of the view. Without a list of vaults, the active
indicator and detail have nothing to annotate.

**Independent Test**: On a machine with a known set of vaults, open Vaults; the row count and
per-row identity match `go-rag vault list` for the same machine.

**Acceptance Scenarios**:

1. **Given** one or more vaults on disk, **When** Vaults opens, **Then** every vault lists
   with name, document count, model, storage size, and daemon-running state.
2. **Given** the vault list, **When** compared to `go-rag vault list`, **Then** the names,
   counts, models, and states match.
3. **Given** the view is read-only, **When** the operator interacts with it, **Then** no vault
   is created, deleted, cleared, cloned, imported, or switched.

---

### User Story 2 - Identify the active vault (Priority: P1)

The vault this daemon is serving is clearly marked as **active** — distinct from any other
vaults that may be listed but are not served by this process. The operator can tell at a
glance which vault their console session is operating on.

**Why this priority**: without the active marker, the operator could mistake another vault for
the one they are querying/writing via the console — a real confusion risk now that the console
writes (spec 050).

**Independent Test**: Open Vaults; the vault matching the daemon's served vault (per
`go-rag status`) is marked active; no other vault is.

**Acceptance Scenarios**:

1. **Given** the daemon is serving vault X, **When** Vaults opens, **Then** vault X is marked
   active and no other vault is.
2. **Given** the active marker, **When** the operator checks `go-rag status`, **Then** the
   active vault matches the daemon's served vault.

---

### User Story 3 - Inspect a vault's detail (Priority: P2)

An operator can open a vault's detail to see a fuller snapshot: document count, embedding
model, storage size, daemon state, and — for the active vault — the live corpus stats
(documents, chunks, embeddings, embedding-complete flag) already on the Dashboard. For
non-active vaults, the detail shows the config/filesystem-derived identity (model, storage)
without claiming live counts it cannot read (a vault held by another daemon is locked).

**Why this priority**: useful for sizing and diagnosing vaults, but the list + active marker
(P1) already carry the essential information.

**Independent Test**: Open the active vault's detail; its live stats match the Dashboard; open
a non-active vault's detail; its identity (model/storage) renders and live counts are
presented honestly (the lock constraint is reflected, not hidden).

**Acceptance Scenarios**:

1. **Given** the active vault, **When** its detail opens, **Then** live corpus stats
   (documents/chunks/embeddings/complete) match the Dashboard / `go-rag status`.
2. **Given** a non-active vault, **When** its detail opens, **Then** its model and storage
   render; where a live count cannot be read (the vault is locked by another daemon), the
   detail says so plainly rather than showing a misleading zero.
3. **Given** the detail is read-only, **When** the operator interacts, **Then** no mutation.

---

### User Story 4 - Read-only, shell-consistent, and honest about switching (Priority: P2)

The Vaults view introduces no writes, no new authentication, no Node/build chain, and renders
inside the authenticated shell. It is honest that vault-switching is **not** a live action
here — the daemon serves one vault per process; switching means restarting against a different
vault, which is out of scope. It degrades gracefully on an empty vaults directory and on
vaults it cannot read. This is a constraint (mirroring spec 046/047/048/049/050 US4), proven
once so every later view inherits it.

**Why this priority**: not a feature but a hard invariant (read-only this slice; no live
switch; no Node; single binary). P2 because the view is functional before the invariant is
formally proven, but it must hold before the slice ships.

**Independent Test**: Inspect every network call the view issues — all are read-only; confirm
no create/delete/switch action is offered; confirm the view renders inside `goragApp` with no
full page reload; confirm no `package.json`/`node_modules`/build config is introduced.

**Acceptance Scenarios**:

1. **Given** the view in use, **When** its network calls are inspected, **Then** every call is
   a read-only request to a guarded `/api/*` route — no create / delete / switch.
2. **Given** the view, **When** checked, **Then** no live "switch vault" action is presented
   (switching is explicitly out of scope; the active vault is reported, not changed).
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced.
4. **Given** an empty vaults directory (no vaults), **When** Vaults opens, **Then** a healthy
   empty state renders (not an error).
5. **Given** a session that expires mid-view, **When** a fetch returns 401, **Then** the shell
   routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Single vault (the common case)** — the list shows one row, marked active.
- **Empty vaults directory** — a healthy empty state, not an error.
- **A vault held by another daemon (locked)** — its live doc count cannot be read; the view
  shows the model/storage it can derive and reflects the lock honestly (not a misleading 0).
- **A vault with no readable config** — model unknown; rendered as such, not a crash.
- **A vault whose data directory is missing/corrupted** — storage size 0 or unknown; rendered
  as such, not a crash.
- **Very large storage sizes** — human-readable units, no layout breakage.
- **The active vault also appearing in the list** — it is marked active and its live stats are
  available (no double-counting).
- **Mid-view session expiry** — graceful return to login.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST list every vault on the machine, each row showing name, document
  count, embedding model, on-disk storage size, and daemon-running state.
- **FR-002**: The view MUST clearly mark the **active vault** — the one this daemon is serving
  — distinct from all others.
- **FR-003**: The operator MUST be able to open a per-vault detail showing the active vault's
  live corpus stats (matching the Dashboard) and, for non-active vaults, the
  config/filesystem-derived identity with any unreadable live count reflected honestly.
- **FR-004**: The view MUST be strictly read-only — no create, delete, clear, clone, import,
  or vault-switch action; every network call is a read-only request to a guarded route.
- **FR-005**: The view MUST render inside the authenticated shell, gated by the existing spec
  045 / spec 046 Bearer guard, with no new authentication surface.
- **FR-006**: The view MUST ship inside the single binary via the existing embedded, vendored
  SPA — no Node / Vite / Tailwind build chain.
- **FR-007**: The view MUST render healthy states for an empty vaults directory and for
  vaults it cannot fully read (locked / missing config / corrupted) — no silent failures.
- **FR-008**: Vault identity shown MUST match `go-rag vault list` for the same machine
  (cross-surface parity — same vault/config surface the CLI uses).

### Key Entities *(include if feature involves data)*

- **Vault**: a named corpus — a separate Pebble DB under `~/.go-rag/vaults/<name>` with its
  own config (embedding model), document count, on-disk storage size, and daemon-running
  state.
- **Active Vault**: the single vault this daemon process is serving; reported (not switched)
  in this slice.
- **Vault Identity**: the per-vault metadata the view projects — name, document count,
  embedding model, storage size, daemon-running state, and active flag.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can open Vaults and see every vault on the machine within 1 second
  on loopback, each with name/count/model/storage/daemon-state.
- **SC-002**: 100% of operators can identify the active vault at a glance — it matches the
  daemon's served vault per `go-rag status`.
- **SC-003**: Vault identity shown matches `go-rag vault list` byte-for-byte — zero drift.
- **SC-004**: No write action (create/delete/clear/clone/import/switch) is possible from the
  Vaults view — verifiable by inspecting every network call.
- **SC-005**: The view introduces zero new build tooling — a single `make build` still
  produces one binary with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
  root, and spec 045 Bearer auth unchanged — exactly as specs 047–050 did.
- This slice is read-only; write-actions on vaults (create/delete/clear/clone/import) and
  live vault-switching are deliberately a later, separately-specced slice (the `go-rag vault`
  CLI already has the writes; switching means a daemon restart).
- `Engine.ListVaults` already exists (returns each vault's name + document count); the active
  vault's live stats come from `engine.StatusInfo` (already on the Dashboard). Plan confirms
  whether `ListVaults` alone suffices or a thin per-vault-detail accessor (model / storage /
  daemon state, as the CLI computes) is added.
- A vault held by another daemon is locked and its live count cannot be read; the view
  reflects that honestly rather than showing a misleading zero.
- Single-operator use; no multi-user or RBAC concerns (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- The view refreshes on demand (on view-entry / manual); live streaming is out of scope.

---

## Open Questions (to resolve in plan / tasks)

- **Per-vault identity source** — `Engine.ListVaults` returns name + doc count only; the
  model / storage / daemon-state the CLI shows come from opening each vault's config +
  `daemon.Status` + `dirSize`. Decide at plan: enrich `Engine.ListVaults` (add fields) vs a
  UI-internal handler that reads each vault's config (as the CLI does). Lean: enrich
  `ListVaults` to carry model/storage/daemon so every consumer benefits, mirroring how 049
  added `Engine.AuditRead`.
- **Locked-vault doc count** — `Engine.ListVaults` opens each vault to count docs; a vault
  locked by another daemon reads as 0. Decide whether to surface "locked" distinctly vs 0.
  Lean: distinct "locked/unavailable" state, not a misleading 0.
- **Active-vault detail depth** — whether the active vault's detail reuses the Dashboard's
  full `StatusInfo` projection or a vault-specific subset. Lean: reuse StatusInfo (the active
  vault IS the Dashboard's vault).
- **Switching affordance** — even though switching is out of scope, whether to show a disabled
  "switch (requires restart)" hint for non-active vaults (manages operator expectation) or
  omit entirely. Lean: omit this slice; note in a tooltip that switching restarts the daemon.
