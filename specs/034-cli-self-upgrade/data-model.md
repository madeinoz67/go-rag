# Data Model — CLI Self-Upgrade + Schema Migration

**Feature**: 034-cli-self-upgrade · **Date**: 2026-06-29 · **Spec**: [spec.md](./spec.md) · **Research**: [research.md](./research.md)

This feature adds two cooperating subsystems with distinct data shapes:
1. **Binary self-upgrade** — filesystem artifacts (no Pebble state).
2. **Schema migration** — one new Pebble key + an in-process migration registry.

---

## Entities

### 1. Release Candidate
A published go-rag release the user could move to. Built from the GitHub Releases API response.

| Field | Type | Notes |
|-------|------|-------|
| `version` | semver string | `tag_name` from `/releases/latest`, e.g. `v1.3.0` |
| `assetURL` | string | `releases/download/{version}/go-rag_{version}_{goos}_{goarch}.tar.gz` |
| `checksumsURL` | string | `releases/download/{version}/checksums.txt` |
| `expectedSHA256` | string (hex) | line for this asset in `checksums.txt`; absent ⇒ fatal (R3) |
| `prerelease` | bool | excluded unless `--pre` (FR-009) |
| `releaseNotesURL` | string | for display only |

**Validation**: semver-parseable; asset must exist for the host `(goos, goarch)`; checksum must
match after download or the upgrade aborts (FR-002).

### 2. Installed Binary
The go-rag binary currently on disk.

| Field | Type | Notes |
|-------|------|-------|
| `version` | semver string | compiled-in via `-ldflags`; `dev` if unbuilt (disables check) |
| `path` | string | `os.Executable()` after `EvalSymlinks` |
| `goos` / `goarch` | string | `runtime.GOOS` / `runtime.GOARCH` |

### 3. Backup Record (prior binary)
The immediately-previous binary retained across an upgrade.

| Field | Type | Notes |
|-------|------|-------|
| `path` | string | `{exe}.prev`, same directory as the active binary |
| `version` | semver string | the version it reports |
| `retention` | int | **N = 1** — only the immediately prior version is kept (R4) |

**Lifecycle**: created by `os.Rename(exe, exe+".prev")` immediately before the swap; overwritten
on the next upgrade; restored by `go-rag upgrade --rollback`.

### 4. Schema Version (Pebble, new key)
The store's on-disk schema version.

| Field | Type | Notes |
|-------|------|-------|
| key | `[]byte{0xFF, 's','c','h','e','m','a','_','v','e','r'}` | global meta prefix (outside `0x01`–`0x0F` data range) — R5 |
| value | uint64, big-endian (8 bytes) | last successfully applied migration version |
| durability | `pebble.Sync` | written only after a migration's `Up` succeeds (R6) |

**Bootstrap rule**: absence of the key ≡ version `0` (R7). The v1 migration writes this key.

### 5. Migration (in-process registry type)
A single versioned schema step.

| Field | Type | Notes |
|-------|------|-------|
| `Version` | int | monotonically increasing; applied in ascending order |
| `Description` | string | human label, logged on apply |
| `Up` | `func(*pebble.DB) error` | **must be idempotent** (R8 — replayed after a crash) |

### 6. Migration Replay Journal (conceptual, not a new key)
Not a separate entity — the **combination** of (a) the version key and (b) idempotent `Up`
functions IS the journal. A crash mid-`Up` leaves the version un-advanced, so the next open
re-runs the same migration from a consistent Pebble state. (R8 — this replaces the spec's
"Migration Snapshot / Pebble Checkpoint" assumption; a Checkpoint is an optional escape hatch
for a non-idempotent step, not the default.)

---

## Relationships

```
Installed Binary ──upgrades to──▶ Release Candidate
       │
       └──backs up as──▶ Backup Record (N=1, {exe}.prev)

go-rag store (Pebble)
       │
       ├── 0xFF|schema_ver ──read on open by──▶ Migration Runner
       │                                          │
       │                                          ├── applies──▶ Migration v1 (bootstrap)
       │                                          ├── applies──▶ Migration v2 …
       │                                          └── each Up fsyncs the version key
       │
       └── (existing prefixes 0x01–0x0F unchanged by v1)
```

The **binary upgrade** and **schema migration** are decoupled by design: upgrade swaps the
binary only; migration runs lazily when the (new) binary next opens the store (FR-013). This is
what makes binary rollback clean (R4) — the binary can be swapped without touching data.

---

## State Transitions

### Binary upgrade state machine (`go-rag upgrade`)
```
        ┌─────────┐  no newer   ┌──────────────┐
        │  CHECK  │ ──────────▶ │ UP-TO-DATE   │  (exit 0)
        └────┬────┘             └──────────────┘
             │ newer available
             ▼
        ┌─────────┐  --check    ┌──────────────┐
        | RESOLVE │ ──────────▶ │ CHECK-ONLY   │  (exit 1, no change)
        └────┬────┘             └──────────────┘
             ▼
        ┌─────────┐  fail       ┌──────────────┐
        │DOWNLOAD │ ──────────▶ │ ABORT        │  (current binary untouched, exit ≠0)
        └────┬────┘             └──────────────┘
             ▼
        ┌─────────┐  mismatch   ┌──────────────┐
        │ VERIFY  │ ──────────▶ │ ABORT        │  (temp removed, exit ≠0)
        │(sha256) │             └──────────────┘
        └────┬────┘
             ▼
        ┌─────────┐
        │ BACKUP  │  rename exe → exe.prev
        └────┬────┘
             ▼
        ┌─────────┐
        │  SWAP   │  atomic rename temp → exe
        └────┬────┘
             ▼
        ┌─────────┐
        │ RESTART │  daemon stop→swap→restart (if running)
        └────┬────┘
             ▼
        ┌─────────┐
        │  DONE   │  (exit 0)
        └─────────┘
```
Windows: `SWAP` is unreachable — at `RESOLVE` the command prints the asset URL and exits
(known v1 limitation, R2).

### Schema migration state machine (on store open)
```
   OPEN STORE (single-writer lock held)
        │
        ▼
   READ 0xFF|schema_ver        ──missing──▶ version = 0
        │
        ▼
   version == binary expected?  ──yes──▶ SERVE (no-op, idempotent)
        │ no
        ▼
   for each Migration where Version > current, ascending:
        │   run Up(db)           ──error──▶ ABORT OPEN (clear error, FR-015)
        │   fsync version = m.Version
        ▼
   SERVE
```
Crash at any point ⇒ on next open, the un-advanced migration replays (idempotent). The store is
never left half-migrated and readable at a prior state (FR-014 / SC-007).

---

## Validation Rules (traceable to spec)

- **VR-1** (FR-002/R3): `expectedSHA256` mismatch ⇒ ABORT before SWAP; temp file removed.
- **VR-2** (FR-003/R2): SWAP is a single `os.Rename` in the binary's directory; no intermediate
  state where the binary is missing.
- **VR-3** (FR-009): target version ≤ current ⇒ refuse, unless `--allow-downgrade`.
- **VR-4** (FR-012/R5): schema-version key is global, `0xFF`-prefixed, 8-byte big-endian uint64.
- **VR-5** (FR-013/R6): migrations run only when `Version > current`, ascending, before serve.
- **VR-6** (FR-014/R8): each `Up` is idempotent; version advanced with `pebble.Sync` only on
  success.
- **VR-7** (FR-015/R9): stored version > binary's max known ⇒ refuse to serve (no silent
  misread, no auto-downgrade).
- **VR-8** (FR-016): schema migration is on-open only; the existing `migrate` command (re-embed,
  spec 028) is untouched and distinct.

---

## Key-space impact

**New prefix**: `0xFF` — reserved for global store-meta (schema version). This is the first
go-rag key outside the `0x01`–`0x0F` data range documented in PRD §6.7. **PRD §6.7 and the
constitution's storage-discipline note must be updated** to record `0xFF` as the meta prefix
(this is a documentation task in `tasks.md`, not a code risk — `0xFF` is unused today).
