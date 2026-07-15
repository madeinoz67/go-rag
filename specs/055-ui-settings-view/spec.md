# Feature Specification: Settings View — Effective Configuration (Slice 0)

**Feature Branch**: `main` (single-author repo — spec directory `055-ui-settings-view`; CLAUDE.md standing instruction: Spec Kit work commits to `main`, no feature branch)

**Created**: 2026-07-15

**Status**: Draft

**Input**: Settings management-console view — Slice 0 of a multi-slice arc. A read-only "Effective Configuration" panel that replaces the current Settings placeholder (declared in `internal/ui/placeholder.go` as "the next console slice, unblocked").

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See the effective retrieval + embedding configuration (Priority: P1)

The single operator opens the Settings view to see, at a glance, how queries are currently resolved and what the corpus is embedded with: the active embedding model and its vector dimensionality, the instruction-prefix convention (and, when set to automatic, the resolved convention), plus the retrieval defaults that govern every query — the result-fusion constant, default result depth, default retrieval mode, similarity threshold, and reranker candidate-pool size. These values exist today only inside the config file or behind status output; the Query view shows per-query effective values, but the source-of-truth defaults have no home in the console.

**Why this priority**: Retrieval and embedding behavior is the most-asked-about surface of the system. Surfacing the effective defaults is the core "what is my system set to do" answer, and the foundation every later Settings slice (transports, auth, editing) builds on.

**Independent Test**: Open Settings against a running daemon whose configuration is known; the retrieval and embedding sections show values that exactly match the status command and the Query view's reported effective mode/depth/pool.

**Acceptance Scenarios**:

1. **Given** a running daemon with a known embedding model and retrieval defaults, **When** the operator opens Settings, **Then** the embedding section shows the model, dimensionality, and effective prefix convention, and the retrieval section shows the fusion constant, default depth, mode, threshold, and pool.
2. **Given** a daemon whose prefix mode is "automatic", **When** the operator views the embedding section, **Then** both the configured mode and the resolved effective convention are shown.
3. **Given** a daemon using default (unset) retrieval values, **When** the operator views Settings, **Then** the effective defaults are displayed (not "unset" or blank).

---

### User Story 2 - See cache, chunking, and redaction configuration (Priority: P2)

The operator views the supporting configuration: the result and query-embedding caches (capacities, enabled/disabled state, and live hit/miss/size statistics that explain query cost), the chunking policy (chunk size, overlap, boundary mode, and whether section context is threaded into chunks), and the redaction policy (enabled/disabled and the count of active redaction patterns).

**Why this priority**: These knobs explain secondary behavior — why a query was cheap or expensive, how text was split, whether secrets are scrubbed. Important for trust and debugging, but not the headline retrieval answer.

**Independent Test**: Open Settings; the cache section's hit/miss/size matches the status command's cache line; chunking and redaction values match the config file.

**Acceptance Scenarios**:

1. **Given** a daemon with caching enabled and some queries already run, **When** the operator views the cache section, **Then** result-cache and embedding-cache capacities, enabled state, and hit/miss/size are all shown.
2. **Given** a daemon with caching disabled, **When** the operator views the cache section, **Then** the disabled state is shown clearly with zeroed statistics, without error.
3. **Given** a daemon with redaction enabled and N patterns configured, **When** the operator views the redaction section, **Then** the enabled state and the active pattern count N are shown.

---

### User Story 3 - Placeholder retired, no regression, read-only (Priority: P3)

The sidebar Settings item now opens a real panel instead of the "planned" marker; the Memory & Graph item continues to show its "blocked" placeholder unchanged; no previously-built view regresses into a placeholder; and the Settings view exposes no control that changes anything.

**Why this priority**: Hygiene and boundary-setting — confirms the slice replaced the right seam, did not disturb neighbors, and is honestly read-only (editing is a later slice).

**Independent Test**: The placeholder test set still passes with exactly one entry (`memory-graph`); Settings is absent from the placeholder map; the view contains no mutate controls.

**Acceptance Scenarios**:

1. **Given** the Settings view is built, **When** the operator clicks Settings in the sidebar, **Then** a real configuration panel renders (not the placeholder marker).
2. **Given** the built Settings view, **When** the operator surveys the Memory & Graph item, **Then** it still shows "blocked" and no built view (documents / query / operations / vaults / quarantine / observability) appears as a placeholder.
3. **Given** the Settings view, **When** the operator reviews every control, **Then** none mutate configuration, credentials, or stored data — all are display-only.

---

### Edge Cases

- What happens when a config value was never explicitly set (defaults)? → the effective default is shown, never "unset" or blank.
- What happens when caching is disabled? → the section renders with a clear disabled state and zeroed statistics, no error.
- What happens when the embedding prefix mode is "automatic"? → both the configured mode and the resolved effective convention are shown.
- What happens when the embedder is unreachable? → configured values still render; values that need a live embedder probe are shown as "unknown" rather than blocking the view (Slice 0 shows configured posture; live drift status stays owned by Bridge Ops 049).
- What happens on a bare / pre-init vault (loopback-bypass path)? → Settings still renders with defaults rather than erroring.
- What happens with very long model names or many redaction patterns? → layout does not overflow or break.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The console MUST provide a Settings view reachable from the sidebar, replacing the prior "planned" placeholder for Settings.
- **FR-002**: The Settings view MUST display the effective retrieval configuration: result-fusion constant, default result depth, default retrieval mode, similarity threshold, and reranker candidate-pool size.
- **FR-003**: The Settings view MUST display the effective embedding configuration: active embedding model, vector dimensionality, and the instruction-prefix convention and mode — showing the resolved convention when the mode is automatic.
- **FR-004**: The Settings view MUST display the effective cache configuration: result-cache and query-embedding-cache capacities, their enabled/disabled state, and live hit/miss/size statistics.
- **FR-005**: The Settings view MUST display the effective chunking configuration (chunk size, overlap, boundary mode, and whether section context is threaded) and the redaction configuration (enabled/disabled state and active pattern count).
- **FR-006**: The Settings view MUST be read-only — it MUST NOT expose any control that mutates configuration, credentials, or stored data.
- **FR-007**: Every displayed value MUST equal the running daemon's effective configuration (defaults where unset) and MUST be consistent with the status command and the Query view's reported effective values.
- **FR-008**: The Settings view MUST be guarded by the same authenticated single-operator session that guards the other console views.
- **FR-009**: The Settings view MUST degrade gracefully when optional subsystems are unavailable (cache disabled, embedder unreachable, bare vault) — rendering known configured values without blocking or erroring.
- **FR-010**: The Settings placeholder marker MUST be retired; the Memory & Graph placeholder MUST remain "blocked"; no built view MUST regress into the placeholder set.

### Key Entities *(include if feature involves data)*

- **EffectiveConfiguration**: a read-only projection of the running daemon's resolved, active configuration across retrieval, embeddings, cache, chunking, and redaction — reflecting defaults where the operator has not set explicit values. Slice 0 introduces no new persisted entity; it projects state the engine already holds.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the in-scope effective-configuration surface (retrieval, embeddings, cache, chunking, redaction) is visible in a single Settings view, without running a CLI command or opening a config file.
- **SC-002**: Zero discrepancies between values shown in Settings and values reported by the existing status command for the same running daemon, across the entire in-scope surface.
- **SC-003**: The Settings view loads and renders on par with the other read-only console views (e.g., Observability), within the console's established view-load budget.
- **SC-004**: The Memory & Graph placeholder remains "blocked" and no built view regresses to a placeholder — the placeholder set is exactly `{memory-graph}`.
- **SC-005**: No new on-disk data format is introduced — the view is read-only over existing state (no schema-version change, no migration).

## Assumptions

- Slice 0 is deliberately **read-only**. Credential management (API keys, sessions, admin), system-and-transports detail (daemon version/uptime, storage schema, bind addresses/ports, drift baseline, self-upgrade), and live config editing are **deferred to follow-up specs** — Settings Slice 1 (System & Transports), Slice 2 (Auth & Credentials), Slice 3 (Live Config Editing).
- The effective configuration is already held by the running engine and surfaced today by the status command and the per-query Query view; Slice 0 reuses that projection and introduces **no new engine capability** (UI-only), mirroring spec 054.
- The single-operator session auth (spec 045) guarding the other console views also guards Settings.
- Where a value is vault-sensitive, it is shown for the active vault context, consistent with the vault-aware engine surface (spec 052).
- Values are displayed, not edited, in Slice 0; a later slice adds live editing with apply/restart semantics.
- **Constitution compliance**: Principles I, III, V honored (local-first; pure-Go vendored SPA with no Node build chain; UI-only over existing interfaces); Principle IV (async-after-ACK) not engaged (read-only); **no on-disk layout change** (no migration, no `ExpectedVersion` bump).
