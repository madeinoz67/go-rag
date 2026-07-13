# Feature Specification: Quarantine Management View

**Feature Branch**: `053-quarantine-management`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: *"A dedicated Quarantine Management surface for the poisoning-detection system (spec 019/H04). The engine already exposes ListPoisoned, ReleaseChunk, ResetChunk, RescanPoisoning — all vault-aware (spec 052). What's missing is the UI: the operator can see the flagged count but can't browse which chunks are flagged, inspect the signal breakdown, or release false positives. This fulfils the standing preference: a system with quarantine functionality MUST have a dedicated Quarantine Management section."*

## Context & Background

go-rag's poisoning detection (spec 019 / audit H04) quarantines chunks flagged as indirect
prompt-injection — they're excluded from query results by default (quarantine-by-default). The
Operations view (spec 049) shows the aggregate count (`PoisonFlagged: N`) but gives no way to
see **which** chunks are flagged, **why** each was flagged, or to **release** a false positive
back into the queryable pool.

The engine surface is **already complete** — `Engine.ListPoisoned(vault)`,
`Engine.ReleaseChunk(vault, chunkID)`, `Engine.ResetChunk(vault, chunkID)`, and
`Engine.RescanPoisoning(vault)` are all implemented, vault-aware (spec 052), and exposed on
REST / gRPC / MCP. The CLI has `go-rag poison list` / `go-rag poison release` /
`go-rag poison reset`. **What's missing is the browser UI** — the dedicated Quarantine
Management section the standing preference requires.

This spec adds that UI: a new sidebar view ("Quarantine") that lets the operator browse every
flagged chunk, inspect its verdict (the per-signal score breakdown + the matched phrases that
triggered the flag), see the flagged text highlighted, and release false positives with
confirmation. It reuses the spec 046 shell, the Alpine `goragApp` root, the 4-layer CSS, and
the spec 045 Bearer-session guard unchanged. No new engine capability, no new transport, no
Node/build chain.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse all flagged chunks (Priority: P1) 🎯 MVP

An operator opens the **Quarantine** sidebar item and sees every chunk currently flagged as
injection-poisoning in the active vault. Each row shows the chunk's document name, a text
preview, the verdict level (suspicious / quarantine), and the composite score. The list is
bounded (paginated or capped) so a large number of flagged chunks doesn't overwhelm. This is
the "what's been flagged" view — the operator can finally see the quarantine contents, not just
the count.

**Why this priority**: the gate. Without a list of flagged chunks, the verdict detail and
release action have nothing to operate on.

**Independent Test**: Ingest a document with injection-style content (e.g., "ignore all
previous instructions"); open Quarantine; the flagged chunk appears; the count matches
`go-rag poison list --vault default`.

**Acceptance Scenarios**:

1. **Given** one or more flagged chunks, **When** Quarantine opens, **Then** every flagged
   chunk lists with document name, text preview, verdict level, and score.
2. **Given** the list, **When** compared to `go-rag poison list`, **Then** the chunks and
   counts match.
3. **Given** no flagged chunks (clean corpus), **When** Quarantine opens, **Then** a healthy
   empty state renders (not an error).
4. **Given** the view is read-only for browsing, **When** the operator inspects, **Then** no
   mutation occurs.

---

### User Story 2 - Inspect a flagged chunk's verdict detail (Priority: P1)

An operator clicks a flagged chunk and a detail surface opens showing: the **full chunk text**
with the **matched phrases highlighted** (the specific phrases that triggered the detection),
the **per-signal score breakdown** (repetition, stuffing, instruction — the three detection
signals, each with its score and threshold), and the chunk's source document + section context.
This is the "why was this flagged, and is it a real threat or a false positive" view.

**Why this priority**: a list of flagged chunk IDs without the WHY is useless for triage. The
operator needs the evidence to decide release vs. keep-quarantined.

**Independent Test**: Open a flagged chunk; the matched phrases are highlighted in the chunk
text; the signal breakdown shows which signal(s) triggered (repetition/stuffing/instruction)
with their scores + thresholds; the detail matches the engine's PoisonVerdict.

**Acceptance Scenarios**:

1. **Given** a flagged chunk, **When** its detail opens, **Then** the full chunk text renders
   with the matched phrases highlighted.
2. **Given** the detail, **When** the signal breakdown is inspected, **Then** each signal
   (repetition, stuffing, instruction) shows its score + the threshold that was crossed.
3. **Given** the detail, **When** the matched phrases are checked, **Then** each phrase that
   triggered the flag is visible and highlighted in the chunk text.
4. **Given** the detail, **When** the source context is checked, **Then** the document name +
   section breadcrumb render (matching the Documents view).

---

### User Story 3 - Release a flagged chunk (Priority: P2)

An operator identifies a false positive and chooses **Release**. A confirmation dialog states
that the chunk will be un-flagged and re-enter the queryable pool (future queries may return
it). On confirm, the chunk is released via `Engine.ReleaseChunk`; it disappears from the
Quarantine list and the flagged count decrements. The operator can also **Reset** a chunk
(force a re-scan, restoring the engine's verdict) or trigger a **Rescan** of the entire vault
(re-evaluate all chunks under the current thresholds).

**Why this priority**: essential for managing false positives, but the browse + inspect (P1)
already let the operator triage; the release is the action on top.

**Independent Test**: Release a flagged chunk; it disappears from the Quarantine list; the
flagged count decrements; a query that previously excluded it now returns it (quarantine opt-in
no longer needed for that chunk).

**Acceptance Scenarios**:

1. **Given** a flagged chunk, **When** the operator chooses Release, **Then** a confirmation
   dialog states the effect; the action does not proceed without explicit confirm.
2. **Given** a confirmed release, **When** it completes, **Then** the chunk is gone from the
   Quarantine list, the count decrements, and the chunk re-enters the queryable pool.
3. **Given** a flagged chunk, **When** the operator chooses Reset, **Then** the chunk is
   re-scanned; if the verdict changes, the list updates accordingly.
4. **Given** the Quarantine view, **When** the operator triggers a vault Rescan, **Then** all
   chunks are re-evaluated; the list reflects the updated verdicts.

---

### User Story 4 - Vault-aware, shell-consistent, confirmed (Priority: P2)

The Quarantine view carries the vault parameter (from the shell vault picker, spec 052). All
operations (list, release, reset, rescan) target the selected vault. Destructive actions
(release, reset, rescan) require explicit confirmation. The view degrades gracefully on errors
and empty states. No new authentication, no Node/build chain.

**Why this priority**: not a feature but a hard invariant (vault-aware; confirmed destructive
ops; no Node; single binary). P2 because the view is functional before the invariant is formally
proven.

**Independent Test**: Switch vaults via the picker; the Quarantine list updates per-vault;
attempt a release without confirmation — it does not proceed; confirm no `package.json` /
`node_modules` introduced.

**Acceptance Scenarios**:

1. **Given** the vault picker, **When** the operator switches vaults, **Then** the Quarantine
   list reflects the selected vault's flagged chunks.
2. **Given** a destructive action (release, reset, rescan), **When** initiated, **Then** it
   does not proceed without explicit confirmation.
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced.
4. **Given** a session that expires mid-view, **When** a fetch returns 401, **Then** the shell
   routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Zero flagged chunks** (clean corpus) — a healthy empty state, not an error.
- **Chunk whose source document was deleted** — the chunk text + verdict still render (the
  verdict is chunk-scoped); the document link may be broken (graceful).
- **Chunk flagged by multiple signals** — all triggering signals shown, each with its score;
  the matched phrases from each signal highlighted (potentially overlapping).
- **Very long chunk text** — truncated preview with expand-to-full; matched phrases highlighted
  in both.
- **Released chunk that gets re-flagged on rescan** — re-appears in the list after a Rescan if
  the verdict is restored.
- **Rescan in progress** — the operator sees a loading state; the list updates when complete.
- **Mid-action session expiry** — graceful return to login.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST list every flagged (quarantined) chunk in the active vault, each
  showing document name, text preview, verdict level, and composite score.
- **FR-002**: The operator MUST be able to open a chunk's detail showing the full text with
  matched phrases highlighted, the per-signal score breakdown (repetition/stuffing/instruction
  + thresholds), and the source document context.
- **FR-003**: The operator MUST be able to release a flagged chunk (un-flag → re-enters the
  queryable pool) with explicit confirmation, via the existing `Engine.ReleaseChunk`.
- **FR-004**: The operator MUST be able to reset a chunk's verdict (force re-scan) and trigger
  a vault-wide rescan, via the existing `Engine.ResetChunk` / `Engine.RescanPoisoning`.
- **FR-005**: All operations MUST carry the vault parameter (from the shell vault picker).
  Results MUST be isolated by vault.
- **FR-006**: Destructive actions (release, reset, rescan) MUST require explicit confirmation.
- **FR-007**: The view MUST be gated by the existing spec 045 Bearer guard — no new auth.
- **FR-008**: The view MUST ship inside the single binary via the embedded vendored SPA — no
  Node/build chain.
- **FR-009**: The view MUST render healthy states for zero flagged chunks, errors, and
  mid-action loading — no silent failures.
- **FR-010**: Flagged chunks + verdicts shown MUST match `go-rag poison list` and the other
  transports (cross-surface parity).

### Key Entities

- **Flagged Chunk**: a chunk currently quarantined by the poisoning detector. Carries its text,
  source document, verdict (level + composite score), and per-signal breakdown.
- **Poison Verdict**: the detection result — level (suspicious/quarantine), composite score,
  per-signal scores (repetition, stuffing, instruction), matched phrases, and thresholds.
- **Matched Phrase**: a specific text fragment that triggered a detection signal. Highlighted in
  the chunk text so the operator can see exactly what the detector caught.
- **Release Action**: the operator's override — un-flag a false positive so it re-enters the
  queryable pool. Requires confirmation.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can open Quarantine and see every flagged chunk in the active vault
  within 1 second on loopback.
- **SC-002**: 100% of operators can identify WHY a chunk was flagged (the signal + matched
  phrases) from the detail view alone, without reading logs.
- **SC-003**: An operator can release a false positive and confirm it re-enters the queryable
  pool — verifiable by querying after release.
- **SC-004**: No release/reset/rescan completes without explicit operator confirmation.
- **SC-005**: The flagged-chunk list matches `go-rag poison list` — zero drift.
- **SC-006**: The view introduces zero new build tooling — a single `make build` still produces
  one binary with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
  root, spec 045 Bearer auth, and the spec 052 vault picker — unchanged.
- The engine surface is **already complete**: `Engine.ListPoisoned(vault)`,
  `Engine.ReleaseChunk(vault, chunkID)`, `Engine.ResetChunk(vault, chunkID)`,
  `Engine.RescanPoisoning(vault)` — all vault-aware, all on REST/gRPC/MCP/CLI. Plan confirms the
  exact return shapes (the PoisonVerdict fields available for the detail view).
- This slice adds the **UI surface only** — no new engine capability, no new transport surface.
- Single-operator use; no multi-user or RBAC (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- The matched-phrase highlighting is client-side (the verdict carries `MatchedPhrases`; the UI
  highlights them in the chunk text rendered from `Content`).

---

## Open Questions (to resolve in plan / tasks)

- **Matched-phrase highlight style** — how to highlight multiple (potentially overlapping)
  matched phrases in the chunk text. Lean: highlight each with a distinct background colour per
  signal (repetition = amber, stuffing = red, instruction = purple); overlaps blend.
- **Release vs Reset distinction in the UI** — Release permanently un-flags (the chunk stays
  queryable); Reset forces a re-scan (the verdict may be restored). Lean: both buttons, with
  tooltips explaining the difference.
- **Rescan progress** — vault-wide rescan can take time; whether to show a progress bar or a
  simple "scanning..." state. Lean: simple "scanning..." state with a refresh button.
- **Sidebar placement** — "Quarantine" as a new sidebar item (9th view, expanding the original
  8) or a sub-section of Operations (049). Lean: new sidebar item — it's a distinct workflow
  (triage/release), not a health metric.
