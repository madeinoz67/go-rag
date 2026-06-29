# Phase 0 Research — CLI Self-Upgrade + Schema Migration

**Feature**: 034-cli-self-upgrade · **Date**: 2026-06-29 · **Spec**: [spec.md](./spec.md)

> **Directive (from spec)**: derive the concrete binary self-upgrade AND schema-migration
> mechanism from the **MuninnDB source repository** (`github.com/scrypster/muninndb`), then
> reconcile against the go-rag spec and constitution. This document is the primary-source
> grounding for every design decision below.

## Sources examined (MuninnDB, primary)

| File | What it reveals |
|------|-----------------|
| `cmd/muninn/upgrade.go` | In-process `muninn upgrade` command: discovery, semver, atomic self-replace, daemon stop/restart, `--check` |
| `install.sh` | Shell installer: GitHub `/releases/latest`, SHA-256 vs `checksums.txt` (fatal on mismatch), `mv` to `/usr/local/bin` |
| `internal/storage/migrate/migrate.go` | Schema-migration runner: numbered steps, global version key, per-step fsync, v0 bootstrap |
| `internal/storage/migrate/v1_embed_dim.go`, `v2_rel_entity_index.go` | Concrete numbered migration steps (idempotent) |
| `internal/storage/snapshot.go` | "Snapshot" = Pebble point-in-time **read** snapshot (NOT a backup copy) |
| `internal/storage/vault_lifecycle.go` | Range-tombstone vault deletion + cache eviction patterns |
| `docs/key-space-schema.md` | Prefix-partitioned key space; per-concern version markers (0x17, 0x1B, 0x1D); "migrations must be idempotent" rule |

---

## R1 — Version / release discovery

**Decision**: Resolve the latest release via the GitHub Releases API
(`https://api.github.com/repos/madeinoz67/go-rag/releases/latest`), parse `tag_name`. Compare
against the compiled-in current version with a strict semver parser.

**Rationale** (from `upgrade.go` `latestVersionDefault` + `newerVersionAvailable`):
- MuninnDB hits `api.github.com/.../releases/latest` with a **3 s timeout** and parses
  `tag_name`.
- `parseSemver` strips a leading `v`, drops `-prerelease`/`+build` suffixes, parses
  major.minor.patch; `newerVersionAvailable` returns **false on any parse error** to avoid
  false-positive upgrades.
- A `dev` build (no compiled version) short-circuits the check — skip, don't error.
- Network failure is **non-fatal**: report "could not reach GitHub," do not imply up-to-date.

**Alternatives considered**:
- *Self-hosted update manifest* — rejected: re-introduces cloud infra, violates Local-First
  ethos (Principle I) for no gain over GitHub Releases, which already hosts the assets.
- *Polling a `latest` redirect* — GitHub's API gives the tag + asset list in one call; cheaper.

**go-rag specifics**: current version comes from the existing `go-rag version` command
(`internal/cli/commands.go`, `newVersionCmd`). Inject via `-ldflags` at build (the standard Go
pattern), so a release build carries its tag.

---

## R2 — Atomic self-replacement strategy

**Decision**: Port MuninnDB's `selfUpdate` sequence verbatim, adapted to go-rag:
resolve `os.Executable()` → `EvalSymlinks` → download asset → write to a **temp file in the same
directory as the running binary** → verify → `os.Rename(temp, exe)` → advise daemon restart.

**Rationale** (from `upgrade.go` `selfUpdate`):
- Temp file is created with `os.CreateTemp(filepath.Dir(exe), ...)` — **same directory is
  mandatory**: `os.Rename` is only atomic within a single filesystem. Renaming across
  filesystems (e.g. `/tmp` → `/usr/local/bin`) silently becomes copy+delete and is NOT atomic.
- `os.Rename` over a running Unix executable is safe: the kernel keeps the old inode mapped for
  the running process; new invocations get the new binary. This is the atomic primitive.
- `EvalSymlinks` first — handles Homebrew-style symlinked installs so the rename lands on the
  real file, not the symlink.
- On any failure (download, verify, rename), the temp file is `os.Remove`d and the current
  binary is left byte-identical. No partial state.

**Per-OS edge cases** (from `upgrade.go`):
- **Unix (darwin/linux)**: full self-replace works.
- **Windows**: the OS locks a running executable — `os.Rename` over it fails. MuninnDB gives up
  on Windows self-replace and just opens the browser to the release page. **go-rag decision**:
  same — on Windows, print the asset URL and instruct manual replacement (do not attempt a
  broken rename). Track as a known v1 limitation.

**Daemon coordination** (from `selfUpdate`): the daemon is **stopped before** the swap and
**restarted after** — there is no live handoff. MuninnDB stops the daemon (up to 15 s graceful
wait for Pebble flush + WAL sync, then force-kill), waits 200 ms for the OS to release the
Pebble `LOCK` file, then swaps, then restarts. go-rag has the same Pebble single-writer lock
(spec 003), so the identical stop→swap→restart sequence applies. (Live daemon handoff remains
out of scope, per FR-010.)

**Alternatives considered**:
- *Move-after-exit / batch script* (common Windows workaround) — rejected for v1 complexity;
  revisit only if Windows self-upgrade becomes a requirement.
- *`github.com/minio/selfupdate` library* — viable, but the mechanism is ~40 lines of stdlib
  (`os.Executable` + `os.CreateTemp` + `os.Rename`); a dependency adds supply-chain surface for
  no functional gain (Principle III favours minimal deps). **Decision: implement in-house,
  stdlib-only.**

---

## R3 — Integrity / trust model

**Decision**: go-rag verifies the downloaded binary with **SHA-256 against a published
`checksums.txt`** (fatal on mismatch), AND runs a functional `version` smoke-check on the
extracted binary before the rename. This is **strictly stronger than MuninnDB's in-process
path**.

**Rationale**:
- MuninnDB's **`install.sh`** does real checksum verification: fetch
  `releases/download/{tag}/checksums.txt`, `sha256sum` the download, **fatal abort on
  mismatch**, warn-and-continue only if no checksum is published or no tool is available.
- MuninnDB's **`selfUpdate`** does NOT checksum — it verifies by executing `<tmp> version` and
  checking the output contains the expected tag (`verifyBinary`). This is a functional check,
  not cryptographic.
- go-rag's constitution **Principle II (content-addressed identity)** and the air-gapped ethos
  argue for the stronger guarantee: a binary that will own the user's vault should be
  cryptographically verified, not just run to see if it prints a version string. So go-rag
  adopts the `install.sh` checksum discipline **inside** the in-process upgrade (MuninnDB splits
  it across two paths; go-rag unifies them).

**Asset / checksum format**: the existing `.github/workflows/release.yml` CI publishes `go-rag-{tag}-{goos}-{goarch}.tar.gz` (Unix; `.zip` on Windows)
assets plus a `checksums.txt` (lines of `<sha256>  <asset>`). The upgrade fetches the checksums
file, matches the asset line, compares. A missing/mismatched checksum is fatal (do not install).

**Alternatives considered**:
- *Signed releases (cosign/gpg)* — stronger but adds tooling + key-distribution burden for a
  single-author local tool; defer. The release pipeline can add a signature later without
  changing the upgrade contract (it already fetches a sidecar file).
- *Checksum-only via `version` smoke test* (pure MuninnDB selfUpdate) — rejected as too weak
  for go-rag's content-addressed posture.

---

## R4 — Binary rollback / safety

**Decision**: Retain the prior binary as `go-rag.prev` (next to the active binary) before the
rename; `go-rag upgrade --rollback` swaps it back. Keep exactly one previous version (N=1).

**Rationale**:
- MuninnDB's `selfUpdate` keeps **no** prior-binary backup — it relies on atomic rename + the
  release archive being re-downloadable. Rollback there = re-run `muninn upgrade` to an older
  tag, or re-run `install.sh`.
- go-rag's spec (FR-004) requires a one-command rollback. Cheapest robust design: before
  `os.Rename(tmp, exe)`, first `os.Rename(exe, exe+".prev")` (preserving the old binary), then
  `os.Rename(tmp, exe)`. Both renames are atomic and same-directory. `--rollback` reverses it
  (`exe` → `exe.broken`, `exe.prev` → `exe`). Retention = 1 (the immediately prior version);
  older versions are not kept (disk + simplicity).

**Alternatives considered**:
- *N-version retention* — rejected: marginal value, complicates the rollback contract.
- *No backup, re-download on rollback* (MuninnDB model) — rejected: FR-004 wants offline
  rollback; re-download needs network and the exact old tag.

---

## R5 — Schema-version tracking

**Decision**: A **single global schema-version key** in the Pebble store, holding the
last-applied migration version as a big-endian uint64. Absence of the key ≡ version 0.

**Rationale** (from `migrate/migrate.go`):
- MuninnDB uses `migrationVersionKey = []byte{0xFF, 'm','i','g','_','v','e','r'}` — a **global**
  key (not vault-scoped), distinct from per-concern markers. It stores the version as a
  big-endian uint64.
- `readMigrationVersion` returns **0 on `pebble.ErrNotFound`** — this IS the v0→v1 bootstrap:
  a pre-versioning store reads as version 0, so every registered migration runs from the start.

**go-rag key placement** (resolving the spec's open question): go-rag's key space
(PRD §6.7) allocates `0x01`–`0x0F`. MuninnDB reserves a high byte (`0xFF`) for
out-of-band meta. **Decision**: follow MuninnDB — reserve a **global meta prefix `0xFF`** for
go-rag's schema-version key (`0xFF | "schema_ver"`), keeping it outside the data prefix range
so it never collides with future data prefixes and is clearly "store meta, not vault data."
(Introducing it is itself the v0→v1 migration — see R7.)

- *Alternative rejected*: the `0x09` config KV — that prefix is for user config, mixing
  immutable store-meta with user-config invites confusion; a dedicated meta prefix is cleaner
  and matches MuninnDB.

**Per-vault vs global**: go-rag uses ONE Pebble instance for all vaults (unlike MuninnDB's
workspace-prefix vault isolation within one DB). A single global schema version is therefore
correct — the schema is a property of the store, not per-vault. (MuninnDB's per-concern markers
like `0x17`/`0x1B` exist because it evolves subsystems independently; go-rag starts with one
linear version and can add per-concern markers later if a subsystem diverges.)

---

## R6 — Migration step registry

**Decision**: Port MuninnDB's `Runner` pattern — a registry of
`Migration{Version int; Description string; Up func(*pebble.DB) error}`, sorted ascending,
applied only where `Version > current`.

**Rationale** (from `migrate/migrate.go` `Runner.Run`):
- Migrations are registered in any order; `Run` sorts by `Version`.
- For each migration with `Version > current`: run `Up(db)`, then **durably write the new
  version with `pebble.Sync`** (fsync) before proceeding to the next.
- Multi-version jumps are handled naturally — all pending migrations apply in ascending order.
- Returns `(applied count, first error)`; on error, stops (the failing migration's version was
  NOT advanced, so a retry resumes there).

**Concrete migration files in MuninnDB**: `v1_embed_dim.go`, `v2_rel_entity_index.go` — each a
small, focused, idempotent transform over Pebble keys. go-rag's first migration (`v1`) will be
the bootstrap that writes the schema-version key itself (R7).

---

## R7 — The v0 → v1 bootstrap (chicken-and-egg)

**Decision**: The release that introduces schema versioning ships migration **v1 = "write the
schema-version key = 1"** (and any key-layout normalization needed). On open, a store with no
schema-version key reads as version 0 → v1 runs → the key is written → the store is at v1.

**Rationale** (from `migrate.go`): `readMigrationVersion` treats a missing key as version 0, so
the very first migration runs against every existing store automatically. This is exactly
go-rag's situation on the release that adds versioning — no special-casing needed.

**go-rag's v1 content**: write `0xFF|"schema_ver"` = uint64(1). If any existing on-disk layout
needs normalizing at the same time (e.g., backfilling a field), it goes in v1 too. Keep v1
trivially idempotent (a `Set` of the version key is a no-op if already 1; guards with
`if current >= 1 { return }` at the top, though the Runner already skips it).

---

## R8 — Migration safety & recovery (corrects spec FR-014)

**Decision**: go-rag achieves migration crash-safety via **idempotent migrations + per-step
`pebble.Sync` version advance** — the MuninnDB model — NOT via a Pebble Checkpoint backup copy.
A Pebble Checkpoint is added **only** for a migration that is provably non-idempotent or
destructive (escape hatch), not as the default.

**Rationale** (from `migrate.go` + `snapshot.go`):
- MuninnDB's `snapshot.go` "snapshot" is a `pebble.Snapshot` — a **point-in-time read cursor**,
  not a backup. MuninnDB performs **no full-store copy** before migrating.
- Its safety rests on: (a) each migration's `Up` is **idempotent** (stated rule in
  `key-space-schema.md`: "The migration must be idempotent"); (b) the version key is advanced
  with `pebble.Sync` only **after** `Up` succeeds. Therefore a crash mid-`Up` leaves the version
  un-advanced → on restart the same migration re-runs from a consistent state.
- Pebble's own WAL/fsync guarantees the individual writes inside `Up` are durable or absent,
  never torn at the page level.

**This corrects spec FR-014**, which assumed "snapshot (Pebble checkpoint/copy) before any
destructive migration." The MuninnDB evidence shows the lighter, proven pattern. **Action**: the
spec's `## Clarifications` note and FR-014 should be read as "migration is safe and recoverable"
— the *mechanism* is idempotent-steps + per-step fsync, with a Checkpoint reserved as an
optional escape hatch for genuinely destructive steps. (No spec text blocks this; FR-014's
intent — "a failed/interrupted migration never leaves the vault unreadable" — is fully met by
the idempotent-replay model. Planner recommends FR-014 be re-worded at implementation to name
the idempotent-replay mechanism rather than implying a copy-on-write snapshot.)

**Alternatives considered**:
- *Always Pebble Checkpoint before every migration* — heavier (Checkpoint is a file copy of the
  LSM); unnecessary when migrations are idempotent. Use only when a step cannot be made
  idempotent.
- *Copy-the-whole-vault-dir* — rejected: slow, doubles disk, and Pebble Checkpoint already does
  this correctly if ever needed.

---

## R9 — Downgrade compatibility

**Decision**: go-rag is **forward-only on the store schema**. An older binary opening a vault
migrated to a newer schema MUST detect the mismatch (stored version > binary's max known) and
**refuse to serve** with a clear error — it does NOT silently misread, and it does NOT
auto-downgrade the schema. Binary rollback (`--rollback`, R4) restores the *prior binary*,
which then sees a store at most one schema-version ahead and still refuses if incompatible.

**Rationale**: MuninnDB migrations are `Up`-only; there is no `Down`. Backward read-compat
(older binary reads newer schema) is not guaranteed. go-rag's `--allow-downgrade` (FR-009)
governs the **binary** version (refusing to "upgrade" to an older semver); it does NOT imply
schema downgrade. The two axes are independent: you may roll the binary back, but a
forward-migrated store stays migrated.

**Operational guidance** (for docs/quickstart): if a user upgrades, opens a vault (migrating
it), then rolls the binary back and the old binary can't read the new schema, the recovery is to
re-upgrade the binary — not to downgrade the data. This is the same posture as MuninnDB.

---

## R10 — go-rag integration points

**Decision**: New `internal/upgrade` package (binary self-update), new `internal/storage/migrate`
package (schema migrations, mirroring MuninnDB's layout), wired into the existing CLI and engine.

- **CLI** (`internal/cli`): add `go-rag upgrade` (with `--check`, `--yes`, `--rollback`,
  `--pre`, `--allow-downgrade`) next to the existing `go-rag version` (`newVersionCmd`). The
  existing `go-rag migrate` (re-embed; spec 028) is **unchanged and distinct** (FR-016) —
  schema migration is automatic on open, never a `migrate` subcommand.
- **Engine/storage open path** (`internal/engine`, `internal/storage`): on opening the Pebble
  store, run the migration `Runner` before serving any operation (migration-on-open, FR-013).
  This sits behind the single-writer lock the daemon already takes.
- **Daemon** (`internal/daemon`, spec 003): `upgrade` stops the daemon, swaps, restarts (R2).
  The migration-on-open runs when the restarted daemon re-opens the store.
- **Version injection**: compile-time via `-ldflags` (release pipeline sets the tag).

**Verified existing surface** (tokensave): `newVersionCmd(version string)` exists at
`internal/cli/commands.go:11`; `cmd/go-rag/main.go` passes `version` at line 14. So the version
plumbing the upgrade command needs is already present.

---

## Summary of divergences from MuninnDB (deliberate)

| Axis | MuninnDB | go-rag decision | Why |
|------|----------|-----------------|-----|
| In-process verify | runs `<bin> version` (functional) | SHA-256 vs `checksums.txt` (cryptographic) + version smoke test | Principle II content-addressed posture |
| Package-manager path | detects Homebrew → `brew upgrade` | **none** — always self-update | Principle III (no pkg managers) |
| Migration crash-safety | idempotent + per-step fsync (no backup) | same (Checkpoint only as escape hatch) | proven, lighter; corrects FR-014 wording |
| Schema-version key scope | global `0xFF` meta key + per-concern markers | global `0xFF` meta key (single linear version) | one Pebble instance, one store schema |
| Prior-binary retention | none (re-download) | `go-rag.prev`, `--rollback` | FR-004 offline rollback |

**All spec NEEDS-CLARIFICATION / Research-Note questions are resolved.** No open items block
Phase 1 design.
