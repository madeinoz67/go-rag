# CLI Command Contracts — Self-Upgrade

**Feature**: 034-cli-self-upgrade · **Spec**: [spec.md](../spec.md) · **Data model**: [data-model.md](../data-model.md)

go-rag is a CLI tool; its public contract is the command surface. This document specifies the
new `go-rag upgrade` command family and the on-open schema-migration contract. It references the
existing `go-rag version` and `go-rag migrate` commands, which are unchanged.

---

## `go-rag upgrade` — self-upgrade the binary

**Purpose**: resolve the latest release, fetch + verify the correct OS/arch asset, and atomically
replace the running binary. Schema migration is **not** triggered here — it runs lazily on next
open (see "On-open schema migration" below).

### Flags

| Flag | Type | Default | Behavior |
|------|------|---------|----------|
| `--check` | bool | false | Check-only: print current vs latest, exit `1` if an update is available, `0` if up-to-date. **No binary change, no identifying data sent.** (FR-005) |
| `--yes`, `-y` | bool | false | Skip the interactive confirmation prompt (non-interactive / CI). |
| `--rollback` | bool | false | Restore the prior binary from `{exe}.prev`. No network. Errors cleanly if no backup exists. (FR-004) |
| `--pre` | bool | false | Include pre-release versions. Excluded by default. (FR-009) |
| `--allow-downgrade` | bool | false | Permit "upgrading" to an older semver. Refused by default. (FR-009) |

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Up-to-date, or upgrade/rollback completed successfully |
| `1` | `--check` found a newer version (scripting signal), OR a non-fatal precondition (e.g. offline) |
| `≠0` (other) | Hard failure: download failed, checksum mismatch, verify failed, rename failed, permission denied |

### Output contract

- Always prints current version first.
- `--check`: prints `latest`, a one-line verdict ("up to date" / "vX.Y.Z available"), exits.
- Full upgrade: prints current→target, release-notes URL, then step lines
  (`Stopping daemon ✓` / `Downloading vX.Y.Z … ✓` / `Verifying ✓` / `Installing ✓` /
  `Restarting daemon ✓`), then a final confirmation.
- Daemon-running warning (FR-010): if the daemon is active, the upgrade stops it before the swap
  and restarts it after; this is printed as part of the step flow.
- Permission-denied on the install path ⇒ suggests `sudo` (or equivalent) in the error.

### Non-behaviors (explicit)

- **No background checks, no telemetry, no auto-update** (FR-006 / Principle I). The network is
  touched only on explicit `upgrade` / `--check`.
- **No package-manager delegation** (Principle III). Unlike MuninnDB (which detects Homebrew and
  runs `brew upgrade`), go-rag always self-updates via download + atomic rename.
- **No live daemon handoff** (FR-010). Stop → swap → restart.
- **Windows**: cannot self-replace a running executable; the command prints the asset URL and
  exits without modifying anything (known v1 limitation).

---

## `go-rag version` — unchanged, extended display

The existing `go-rag version` command (`internal/cli/commands.go`, `newVersionCmd`) gains a
companion role: it is the smoke-check the upgrade runs against a downloaded binary (R3), and it
reports the compiled-in version the upgrade compares against. **No flag/behavior change to
existing `version`** — it already prints the version string the upgrade needs.

---

## `go-rag migrate` — UNCHANGED (disambiguation)

The existing `migrate` command re-embeds the corpus onto a new embedding model (spec 028). It is
a **content** migration, on a different axis from schema migration. Schema migration is automatic
on store open and is **never** invoked via `migrate` (FR-016). This contract records the name
separation to prevent future collision.

---

## On-open schema migration (implicit contract)

Not a command — a behavior of store open, shared by every transport (CLI/MCP/REST/gRPC via the
engine).

**Contract**: opening the Pebble store runs the migration `Runner` before serving any operation.
Given stored schema version `S` and the binary's expected version `E`:

| Condition | Behavior |
|-----------|----------|
| `S == E` | Serve immediately (no-op). |
| `S < E` | Apply migrations `S+1 … E` in ascending order; fsync the version key after each; then serve. (FR-013) |
| `S > E` (store newer than binary) | **Refuse to serve** — clear error, do not misread, do not auto-downgrade. (FR-015 / R9) |
| key absent | `S = 0`; v1 (bootstrap) runs, writes the key; serve. (R7) |
| crash mid-migration | On next open, the un-advanced migration replays (idempotent); store never left half-migrated. (FR-014 / R8) |

**Observability**: when a migration runs on open, the binary prints/logs a one-line notice
(`migrating store schema v1 → v2…`) so the one-time cost is visible, not perceived as a hang
(edge case: cold-start budget exception).
