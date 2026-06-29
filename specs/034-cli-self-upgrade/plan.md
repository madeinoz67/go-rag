# Implementation Plan: CLI Self-Upgrade + Schema Migration

**Branch**: `034-cli-self-upgrade` (single-author repo; commits to `main`) | **Date**: 2026-06-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/034-cli-self-upgrade/spec.md`

## Summary

Add a `go-rag upgrade` command that atomically self-replaces the go-rag binary (resolve latest →
download → SHA-256 verify → rename), and introduce on-open Pebble schema migration so a new
binary safely upgrades an older store (numbered idempotent migrations + a global version key,
bootstrapping unversioned stores from v0). The mechanism is derived from MuninnDB's source
(`cmd/muninn/upgrade.go` + `internal/storage/migrate/migrate.go`), with two deliberate
strengthenings: cryptographic checksum verification inside the in-process upgrade (MuninnDB
verifies functionally), and no package-manager delegation (MuninnDB detects Homebrew — rejected
by go-rag's Principle III). Full grounding in [research.md](./research.md).

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`, pure Go — no CGo).

**Primary Dependencies**: existing only — cobra (CLI), pebble (KV), chromem-go (vectors). New
code is **stdlib-only** (`net/http`, `os`, `archive/tar`, `compress/gzip`, `crypto/sha256`,
`runtime`). No new third-party deps (the self-replace mechanism is ~40 lines of stdlib; a
`selfupdate` library is explicitly rejected per research R2).

**Storage**: single Pebble instance, prefix-partitioned (PRD §6.7). Adds one new **global meta
prefix `0xFF`** for the schema-version key — the first key outside the `0x01`–`0x0F` data range.

**Testing**: `go test -race -cover ./...`; new tests in `internal/upgrade` (mock release feed via
an injectable `latestVersionFn`, mirroring MuninnDB's test seam) and `internal/storage/migrate`
(crash-replay, idempotency, v0 bootstrap, refuse-newer-schema).

**Target Platform**: darwin/linux amd64+arm64 (full self-replace). Windows: prints asset URL,
no self-replace (known v1 limitation — OS locks running executables).

**Project Type**: CLI (single binary, multi-transport server).

**Performance Goals**: upgrade download bound only by network; `--check` returns in ≤3s (GitHub
API timeout). Migration-on-open is a one-time cost (exempt from the normal <1s cold-start budget,
surfaced to the user); re-open of a migrated store is a no-op.

**Constraints**: Local-First (Principle I) — network touched only on explicit `upgrade`/`--check`,
no telemetry/auto-update. Pure Go (Principle III) — no package managers. Durability (Principle II)
— migration crash-safety via idempotent steps + per-step `pebble.Sync`. Atomic binary replace
(same-directory `os.Rename`). Offline core ops unaffected.

**Scale/Scope**: ~2 new packages + CLI wiring; medium. No schema migration of existing data
content in v1 (the existing `migrate` re-embed command, spec 028, is unchanged — FR-016).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Upgrade is explicit/opt-in; no background checks, no telemetry (FR-006). Core ops stay fully offline. The single binary replaces itself — no second runtime. |
| **II. Content-Addressed Identity** | ✅ PASS | Downloaded binary verified by SHA-256 (FR-002, stronger than MuninnDB). Migration durability via idempotent steps + per-step fsync (FR-014/R8). |
| **III. Pure Go — No CGo, No Runtime** | ✅ PASS | `CGO_ENABLED=0`; stdlib-only new code; **no package-manager delegation** (deliberate divergence from MuninnDB's Homebrew path). |
| **IV. Async-After-ACK Writes** | ✅ PASS (unaffected) | Upgrade/migration do not touch the write-ACK path. Migration-on-open runs before serve, not on the write hot path. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Schema migration runs under the engine's store-open path, shared by all transports (CLI/MCP/REST/gRPC). `upgrade` is a CLI command; its discovery step mirrors the existing `version` command. |

**Post-design re-check**: PASS. The `0xFF` meta prefix is a documentation addition to PRD §6.7
(tracked in tasks.md), not a principle change. No violations → Complexity Tracking empty.

## Project Structure

### Documentation (this feature)

```text
specs/034-cli-self-upgrade/
├── plan.md              # This file
├── research.md          # Phase 0 — MuninnDB source findings (R1–R10)
├── data-model.md        # Phase 1 — entities, state machines, key-space impact
├── quickstart.md        # Phase 1 — runnable validation scenarios
├── contracts/
│   └── cli-commands.md  # Phase 1 — `go-rag upgrade` + on-open migration contract
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

go-rag is a single-binary Go project; this feature adds two packages and wires them into the
existing CLI/engine/daemon. No new `main` packages.

```text
cmd/go-rag/               # binary entrypoint (unchanged) — version injected via -ldflags
internal/cli/             # +upgrade.go: `go-rag upgrade` (--check/--yes/--rollback/--pre/--allow-downgrade)
internal/upgrade/         # NEW — release discovery, download, SHA-256 verify, atomic self-replace, rollback
internal/storage/         # +schema-version key (0xFF meta prefix); open path runs the migrator
internal/storage/migrate/ # NEW — Runner + numbered idempotent migrations (v1 bootstrap …)
internal/engine/          # wire migrate.Runner into store-open (migration-on-open, FR-013)
internal/daemon/          # stop→swap→restart coordination for `upgrade` (single-writer lock)
```

**Structure Decision**: Option 1 (single project). The two new packages mirror MuninnDB's layout
(`cmd/muninn/upgrade.go` + `internal/storage/migrate/`) and respect go-rag's 1:1
directory→PRD-subsystem mapping (CLAUDE.md architecture map). No second binary, no new entrypoint.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No constitution violations. The design complies with all five principles (see Constitution Check
above); the only cross-cutting change is documenting the new `0xFF` meta prefix in PRD §6.7
(a docs task, not a principle change).
