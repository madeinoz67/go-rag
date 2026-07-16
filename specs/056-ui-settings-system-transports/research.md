# Research — Settings: System & Transports (Slice 1, spec 056)

> Phase 0 output. Grounds the system-identity / transport / schema / update
> surfaces so the build stays UI-layer + read-only (mirroring spec 055 Slice 0).
> Two small UI-layer additions are required (version plumbing + startedAt); no
> engine/storage/migration change.

## R1 — System identity (version / PID / uptime): where does it live?

**Decision: PID + bind addresses come from the daemon pidfile surface; the binary
version is plumbed into the UI Server; uptime is derived from a serve-start
timestamp.**

**Rationale (grounded):**
- `daemon.Status(dbPath)` → `(running bool, pid int, addrs Addrs)` (`internal/daemon/lifecycle.go:134`) — reads the pidfile + addrs file + probes health. This is the SAME source `go-rag status` uses. The UI Server reaches it via `eng.Config().DBPath`.
- `daemon.ReadPID` / `ReadAddrs` / `Addrs` (`internal/daemon/pid.go`) — the underlying pidfile + addrs-file readers.
- Binary version is `main.version` (cmd package, set by `-ldflags -X main.version=…`) — NOT currently visible to `internal/ui`. It must be plumbed in (see Decision below).
- Uptime is not tracked anywhere. The serve process is the UI Server's lifetime, so a `startedAt time.Time` captured at UI Server construction ≈ daemon start.

**Version-plumbing decision:** add a UI Server field without breaking the existing
`New(eng, token)` signature used by tests — via a `NewWithVersion(eng, token, version)`
constructor (`New` defaults version to `"unknown"`). No package-level mutable state.
The `serve` command (which already receives version from `root.Execute`) calls
`NewWithVersion`.

**Alternatives considered:**
- *A new `Engine.SystemInfo()` method* — rejected: identity is daemon/process-level, not engine-level; the engine deliberately does not know its own PID/uptime/version. Reusing the daemon pidfile surface is correct.
- *Read version from the running daemon's status RPC* — rejected: the UI IS the daemon; reading its own pidfile + plumbed version is direct and adds no RPC.

## R2 — Transports + loopback posture

**Decision: bind addresses from `cfg` (MCPAddr/RESTAddr/gRPCAddr/UIAddr via
`eng.Config()`), cross-checked with `daemon.Addrs`; loopback/external posture via
`daemon.IsLoopbackBind` / `NonLoopbackBinds` / `ExternalBindWarning` (spec 007).**

**Rationale (grounded):** `internal/daemon/bind.go` already exports `IsLoopbackBind`,
`NonLoopbackBinds(addrs)`, and `ExternalBindWarning` — the spec 007 posture
machinery. A disabled transport (empty addr) shows "disabled".

## R3 — Storage schema version

**Decision: on-disk version via `migrate.readVersion(db)`; the binary's expected
version via `migrate.ExpectedVersion`. Flag when on-disk > binary (the refuse-newer
posture, spec 034).**

**Rationale (grounded):** `internal/storage/migrate/migrate.go` exports `readVersion`,
`writeVersion`, `schemaVersionKey`, and the `ExpectedVersion` constant (currently 4).
The unified-store posture (one daemon, all vaults — spec 052) is a fixed fact, surfaced
as a boolean.

## R4 — Update availability (operator-initiated egress)

**Decision: `upgrade.LatestVersion()` (GitHub release fetch) +
`upgrade.NewerVersionAvailable(current, latest)` behind an explicit operator action
`POST /api/settings/updates/check`. Never automatic — no egress without an explicit
operator click (Constitution I; mirrors the existing `go-rag upgrade` command).**

**Rationale (grounded):** `internal/upgrade/release.go::LatestVersion` +
`internal/upgrade/semver.go::NewerVersionAvailable(current, latest string) bool`
(clean signature; returns false for `dev`/empty/parse-failure → no false positives).
Response: `{current, latest, newer_available, checked_at}`; `latest="unknown"` when
the release source is unreachable (graceful, FR-008).

**Alternatives considered:**
- *Auto-probe latest on view load* — rejected: violates the local-first air-gap (Constitution I; egress must be operator-initiated).
- *Defer the update-check to a later slice* — rejected: it is in the ratified Slice 1 scope and the operator-initiated form is constitution-safe.

## R5 — Non-overlap with 049 (Bridge Ops) and 054 (Observability)

**Decision: Slice 1 owns system identity + transport posture + storage schema +
update status ONLY.** 049 owns live operational health (embed backlog, subsystem
tiles, drift verdict+baseline+cause, watch dirs, recent activity). 054 owns
metrics + audit. No field duplication — confirmed by reading `toBridgeOpsStats`
(049) which projects none of version/PID/uptime/schema/transports/update.

## R6 — Constitution compliance (pre-check)

All five principles hold; **no on-disk layout change**:
- I (local-first): read-only; the update-check is the one operator-initiated egress (the documented `go-rag upgrade` exception), never automatic. ✓
- III (pure Go): vendored SPA, no Node build; `CGO_ENABLED=0`. ✓
- IV (async-after-ACK): not engaged (read-only + one synchronous operator check). ✓
- V (extension by interface): UI-layer over existing `daemon`/`migrate`/`upgrade`/`config` surfaces; two small UI-local additions (version field + startedAt). ✓
- Storage discipline: no new prefix, no migration, no `ExpectedVersion` bump (the constant is READ, not changed). ✓
