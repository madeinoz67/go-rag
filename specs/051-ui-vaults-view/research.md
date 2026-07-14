# Research — Vaults Management View

**Feature**: specs/051-ui-vaults-view | **Date**: 2026-07-14

Phase 0 output. The spec assumed the engine vault surface was complete; research found two gaps that
the plan closes before the UI is built.

## R1 — The unified store (spec 052) registers vaults IN THE DATABASE, not on the filesystem

**Finding**: vaults live in ONE Pebble DB (the daemon's `db-path`), partitioned by an 8-byte
`wsPrefix`. A vault is registered by two GLOBAL keys (no ws in the key — the registry records vaults
and cannot be scoped by the prefix it maps):

- `0x1A | ws[8] → name` (VaultMeta — scan to list)
- `0x1B | siphash(name) → ws[8]` (VaultNameIndex — point-get to resolve)

`DB.ListVaultNames()` scans `0x1A` and returns every vault name — the **authoritative** list of what
the daemon serves. `DB.ResolveVaultPrefix(name)` maps a name to its ws (LRU → 0x1B → SipHash
fallback for never-registered names). `DB.WriteVaultName(ws, name)` persists both registry keys.
Confirmed empirically: a single repro DB held `default` + `secondary`, isolated by wsPrefix,
switchable live via the `X-Go-Rag-Vault` header.

**Decision**: the Vaults view lists vaults from `DB.ListVaultNames` (via the engine), NOT from the
filesystem.

## R2 — `Engine.ListVaults` is STALE (pre-052) and must be fixed

**Finding**: the current `Engine.ListVaults` (config.go) calls `vaultpkg.List()` — which reads
**filesystem directories** under `~/.go-rag/vaults/` — then `Open()`s each directory. That is the
PRE-052 model (one Pebble DB per vault directory). It misses any vault registered in the unified
store but not present as a directory, so it undercounts / mislists the vaults the daemon actually
serves.

**Decision (engine edit)**: rewrite `Engine.ListVaults` to enumerate `e.db.ListVaultNames()` and
count each vault's documents with `countPrefix(e.db, ws, PrefixDocument)` against the engine's OWN
db — no per-directory opens. This is a correctness fix; it makes `ListVaults` agree with the unified
store the daemon serves (and with `go-rag vault list` once the CLI is realigned, separately).

**Rationale**: the UI can only be correct if the list source is the in-db registry. Alternatives
(keep vaultpkg + sync dirs) were rejected — they re-introduce the per-directory model spec 052
retired.

## R3 — `CreateVault` is IMPLICIT; an explicit create = write the registry entry

**Finding**: `WriteVaultName`'s own comment states "CreateVault is implicit (research R5 — the first
write for a vault name implicitly creates the vault)." There is no `Engine.CreateVault`; vaults
materialize on first ingestion to that name. `vaultpkg.Create` exists (CLI `go-rag vault create`
uses it) but it makes a filesystem DIRECTORY — the pre-052 mechanic, wrong for the unified store.

**Decision (engine add)**: add `Engine.CreateVault(ctx, name)` that validates the name
(`vaultpkg.ValidateName`), refuses if it already exists (`DB.VaultNameExists`), then registers it:
`ws := e.db.ResolveVaultPrefix(name); e.db.WriteVaultName(ws, name)`. The vault then appears in
`ListVaultNames` immediately (empty, writable, switchable) — matching the spec's "create registers
immediately" lean. No document is needed.

**Rationale**: reuses the registry's implicit-create primitive (`WriteVaultName`) rather than the
stale directory-based `vaultpkg.Create`. The vault is registered in the same store the daemon serves.

## R4 — Rename / Clear / Delete already use the in-db registry (correct post-052)

**Finding**: `Engine.RenameVault` → `DB.RenameVault` (two-key metadata op, zero data moves — the
siphash indirection's purpose). `Engine.ClearVault` range-tombstones every vault-scoped kind for the
ws (keeps the registry). `Engine.DeleteVault` = clear + `DB.DeleteVaultNameOnly`. All correct for the
unified store. **One gap**: `DeleteVault` has no "default vault" guard — the spec requires the
default vault be non-deletable.

**Decision (engine edit)**: add a `default`-vault refusal to `Engine.DeleteVault` (strongest site —
protects the UI, CLI, and any future transport). Clear on `default` remains allowed (the spec
permits it).

## R5 — Switch is LIVE and client-side (no engine call, no restart)

**Finding**: the daemon serves all vaults from one db; the active vault is the `X-Go-Rag-Vault`
header the shell already sends on every `/api/*` call (`app.js` `api()`). `switchVault(v)` already
sets `this.vault`, persists it to localStorage, and re-loads the current view. So "switch" is a
client-side state change — no new engine method, no daemon restart.

**Decision**: the Vaults view's "Switch" action calls the existing `switchVault(name)`; the picker
(made stale at `vaults: ['default']`) is populated from `GET /api/vaults`.

## R6 — Shell picker is hardcoded; must be populated from the list

**Finding**: `app.js` `vaults: ['default']` with a comment "spec 050 will populate from the server"
— never wired. The picker (`<select x-model="vault">`) offers only `default`.

**Decision**: on shell mount + on Vaults view-entry, fetch `GET /api/vaults` → populate `this.vaults`
+ keep `this.vault` valid (fall back to `default` if the active vault vanished).

## R7 — UI transport is a 4th adapter over engine methods (no new transport)

**Decision**: new routes in `internal/ui` call `Engine.ListVaults/CreateVault/RenameVault/ClearVault/
DeleteVault` in-process, exactly as the quarantine view calls the poison surface. No proto/REST/gRPC/
MCP changes (the pre-existing gap in REST/MCP vault parity is noted in the constitution check, out of
scope here).
