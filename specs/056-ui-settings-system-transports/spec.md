# Feature Specification: Settings — System & Transports (Slice 1)

**Feature Branch**: `main` (single-author repo — spec directory `056-ui-settings-system-transports`; commits to `main`, no feature branch)

**Created**: 2026-07-16

**Status**: Draft

**Input**: Settings view Slice 1 of the multi-slice arc — a read-only "System & Transports" panel showing system identity, transport posture, storage schema, and update availability. Continues spec 055 (Slice 0).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See system identity (Priority: P1)

The operator opens Settings → System & Transports and sees exactly what is running: the binary version, the daemon PID, the daemon uptime, the on-disk storage schema version, and the unified-store posture (one daemon serving all vaults). These answer "what am I running, and on what store" — the foundational system question, with no CLI needed.

**Why this priority**: System identity is the first thing an operator checks (version before anything else); it is the foundation of the panel.

**Independent test**: The version/PID/schema shown match `go-rag version` + `go-rag status`.

**Acceptance Scenarios**:

1. **Given** a running daemon, **When** the operator opens Settings → System & Transports, **Then** the binary version, daemon PID, uptime, schema version, and unified-store posture are all shown.
2. **Given** a daemon restarted with a new binary, **When** the operator views system identity, **Then** the version reflects the new build.
3. **Given** the storage schema, **When** the operator views the schema field, **Then** the current on-disk schema version is shown.

---

### User Story 2 - See transport posture (Priority: P1)

The operator sees the four transport bind addresses + ports (MCP, REST, gRPC, UI) and, for each, whether it is loopback-only or bound externally (spec 007). This is security-relevant — it confirms the vault is not accidentally exposed on the network.

**Why this priority**: Transport exposure is a security posture question; surfacing it is high-value and low-effort.

**Independent test**: The binds shown match the daemon's listening ports (`lsof`); loopback/external status matches config.

**Acceptance Scenarios**:

1. **Given** a daemon bound to loopback on all four transports, **When** the operator views transport posture, **Then** each transport shows its address + port + a "loopback" status.
2. **Given** a daemon with a non-loopback bind (`--bind-external`), **When** the operator views that transport, **Then** it is clearly flagged as external/exposed.
3. **Given** a transport disabled (empty addr), **When** the operator views posture, **Then** it shows "disabled" rather than an address.

---

### User Story 3 - Check update availability (Priority: P2)

The operator can check whether a newer go-rag release is available — the current version is always shown (local), and an explicit, operator-initiated action compares it to the latest release (spec 034). The check is never automatic, preserving the local-first air-gap (Constitution I — no egress without an explicit operator action, mirroring the existing `go-rag upgrade` command).

**Why this priority**: Useful but involves network egress; it must be opt-in, so it sits below the always-on local identity/posture.

**Independent test**: Current version shown without any network call; the check action returns the latest version + a newer-available flag.

**Acceptance Scenarios**:

1. **Given** the panel is open, **When** the operator views it, **Then** the current version is shown with no network activity.
2. **Given** the operator clicks "Check for updates", **When** the latest release is fetched, **Then** the latest version + a newer-available verdict are shown.
3. **Given** the update check cannot reach the release source, **When** the operator clicks check, **Then** the result shows "unknown" rather than erroring.

---

### Edge Cases

- What happens when the daemon is unreachable for live identity? → degrade gracefully (show the binary's static version; flag live fields unknown).
- What happens when the update check is offline / GitHub unreachable? → "unknown", never an error (FR-008).
- What happens when a transport binds non-loopback? → clearly flagged (security).
- What happens when the on-disk schema is newer than the binary expects? → flagged (refuse-newer posture, spec 034).
- What happens when uptime/PID are unavailable (e.g., engine-only, no daemon)? → show what is known, flag the rest unknown.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Settings view MUST include a System & Transports section (alongside Slice 0's Effective Configuration), reachable from the same sidebar item.
- **FR-002**: The panel MUST display system identity: binary version, daemon PID, daemon uptime, storage schema version, and the unified-store posture (one daemon, all vaults — spec 052).
- **FR-003**: The panel MUST display the four transport bind addresses + ports (MCP/REST/gRPC/UI) and each one's loopback-vs-external posture (spec 007).
- **FR-004**: The panel MUST always show the current version locally; an explicit operator-initiated "check for updates" action compares it to the latest release (spec 034). The check MUST NOT run automatically (no egress without an explicit operator action — Constitution I).
- **FR-005**: The panel MUST be read-only, with the sole exception of the explicit "check for updates" action (which performs a read against the release source, not a config mutation).
- **FR-006**: Every displayed local value MUST be consistent with `go-rag status` / `go-rag version`.
- **FR-007**: The panel MUST be guarded by the same authenticated single-operator session as the other console views (spec 045).
- **FR-008**: The panel MUST degrade gracefully — offline update-check ⇒ "unknown"; unreachable live identity ⇒ flag unknown — never an error.
- **FR-009**: The panel MUST NOT duplicate Bridge Ops (049: live health / embed-backlog / subsystem-tiles / drift / activity) or Observability (054: metrics / audit). Slice 1 owns system-identity + transports + storage-schema + update-status only.

### Key Entities *(include if feature involves data)*

- **SystemStatus**: a read-only projection of daemon identity (version/PID/uptime), transport posture (4 binds + loopback/external), storage schema, and update availability (current vs latest). No new persisted entity — projects state the daemon already holds.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the in-scope system/transport surfaces (identity, transports, schema) are visible in Settings without a CLI command.
- **SC-002**: Zero discrepancies between panel values and `go-rag status` / `go-rag version` across the local surface.
- **SC-003**: The update-check fires ONLY on explicit operator action (no automatic network egress — Constitution I verified).
- **SC-004**: The panel loads/renders on par with the other read-only console views.
- **SC-005**: No new on-disk data format is introduced (read-only over existing state — no schema-version change, no migration).

## Assumptions

- Slice 1 is **read-only** (system identity + transports + schema + update-check). Settings Slices 2 (Auth & Credentials) and 3 (Live Config Editing) remain the follow-ups.
- **Non-overlapping**: 049 Bridge Ops owns live operational health + drift; 054 Observability owns metrics + audit. Slice 1 owns system identity + transport posture + storage schema + update status.
- The update-check is an **explicit operator action** (mirrors `go-rag upgrade`); current version is local/read-only. This is the one sanctioned egress surface (a pre-existing operator utility, not a core operation), so Constitution I holds.
- Exact accessors (uptime surfacing, schema-version exposure, bind-posture projection) are grounded at the plan phase; UI-only wherever the daemon already exposes the value (mirroring spec 055 Slice 0).
- **Constitution compliance**: read-only (Principle IV not engaged); UI-only over existing interfaces (Principle V); no on-disk layout change (no migration, no `ExpectedVersion` bump); the update-check's opt-in egress is the documented operator-utility exception already established by `go-rag upgrade`.
