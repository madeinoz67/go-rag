# Implementation Plan: Vaults Management View

**Branch**: `main` | **Date**: 2026-07-14 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/051-ui-vaults-view/spec.md`

## Summary

A management Vaults view for the console (replacing the sidebar placeholder): list vaults with the
active marker, create a vault, switch the active vault live, and rename/clear/delete — all confirmed.
The view is a UI adapter over the engine's vault surface. **Phase 0 research uncovered two engine
gaps that the plan must close first**: (1) `Engine.ListVaults` is stale — it lists filesystem
directories (`vaultpkg.List`) and so misses the unified-store vaults the daemon actually serves; the
fix points it at the in-db registry (`DB.ListVaultNames`). (2) There is no `Engine.CreateVault`; per
the registry's own design ("CreateVault is implicit"), creating an empty vault = resolve its prefix +
write its registry entry (`WriteVaultName`). With those two in place, the UI is a thin adapter
mirroring the quarantine view.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`); vendored Alpine.js 3.14 (embedded).

**Primary Dependencies**:
- `internal/storage` — `DB.ListVaultNames` (the authoritative vault list, scans `0x1A` VaultMeta),
  `DB.ResolveVaultPrefix(name) → ws`, `DB.WriteVaultName(ws, name)` (registers a vault — implicit
  create), `DB.VaultNameExists`, `DB.RenameVault`, `DB.DeleteVaultNameOnly`. All EXISTING (spec 052).
- `internal/engine` — `Engine.RenameVault` / `ClearVault` / `DeleteVault` (EXISTING, vault_lifecycle.go,
  on the in-db registry). **FIX** `Engine.ListVaults` (config.go) to use `ListVaultNames`. **ADD**
  `Engine.CreateVault(name)`.
- `internal/vault` (vaultpkg) — `ValidateName` (the canonical name validator). EXISTING.
- `internal/ui` — the UI transport (spec 046); new `vaults.go` handlers + Alpine view + picker wiring.
- `internal/auth` — spec 045 Bearer guard (unchanged).

**Storage**: None new. Reads/writes the EXISTING in-db vault registry (`0x1A`/`0x1B`, spec 052). No
key-space change → **no migration, no `ExpectedVersion` bump** (Constitution storage discipline).

**Testing**: `go test -race`; curl smoke for the new routes; Interceptor browser verify.

**Constraints**: read + confirmed-write; vault-aware (per-request vault); live switch (client-side,
no restart); no Node; single binary.

**Scale/Scope**: ~5–6 files. NEW: `internal/ui/vaults.go` (+test). EDIT: `internal/engine/config.go`
(fix ListVaults), `internal/engine/vault_lifecycle.go` (add CreateVault + default-delete guard),
`internal/ui/ui.go` (routes), `internal/ui/web/{static/js/app.js, static/css/components.css,
templates/index.html}` (the Vaults view + populate the shell picker).

## Constitution Check

| # | Principle | Verdict | Reasoning |
|---|-----------|---------|-----------|
| I | Local-First, Single-Binary | **PASS** | Loopback UI in the existing binary; no cloud egress. |
| II | Content-Addressed Identity | **PASS** | No identity change. Vault names map to a frozen wsPrefix; rename is metadata-only, data untouched. |
| III | Pure Go | **PASS** | stdlib + engine + storage. No new dependency. |
| IV | Async-After-ACK Writes | **PASS** | Vault lifecycle ops are bounded metadata/registry writes (not ingest). The <10ms budget governs ingest; these are operator actions. |
| V | Extension by Interface, MCP-First | **PASS*** | The UI is a 4th adapter over EXISTING engine methods, exactly as every console view (047–053). |

**\*Note (pre-existing, not introduced here)**: the vault lifecycle ops (rename/clear/delete) are on
the engine + CLI, and only `ListVaults` is on gRPC; they are NOT on REST/MCP. This spec adds the UI
adapter; closing the full REST/MCP/gRPC parity for vault lifecycle is a separate, follow-on concern
(not required for the console to function).

**Storage discipline**: no on-disk key-space change (the registry prefixes `0x1A`/`0x1B` exist from
spec 052). **No migration, no ExpectedVersion bump.** Gate: PASS.

## Project Structure

### Documentation (this feature)

```text
specs/051-ui-vaults-view/
├── plan.md              # This file
├── research.md          # Phase 0 — the stale-ListVaults + implicit-CreateVault findings
├── data-model.md        # Routes + DTOs
├── quickstart.md        # Validation scenarios
├── contracts/
│   └── ui-vaults.md     # UI transport API contracts
└── tasks.md             # /speckit-tasks output (next)
```

### Source Code (repository root)

```text
internal/engine/
  config.go              # EDIT — fix Engine.ListVaults to use DB.ListVaultNames (+ own db doc count)
  vault_lifecycle.go     # EDIT — add Engine.CreateVault (implicit-create via WriteVaultName) +
                         #        default-vault delete guard in Engine.DeleteVault
internal/ui/
  vaults.go              # NEW — handleVaultsList (ListVaults→DTO), handleVaultCreate,
                         #        handleVaultRename, handleVaultClear, handleVaultDelete
  vaults_test.go         # NEW — list/create/rename/clear/delete + guard + parity vs CLI
  ui.go                  # EDIT — register GET /api/vaults, POST /api/vaults,
                         #        POST /api/vaults/{name}/rename, POST /api/vaults/{name}/clear,
                         #        DELETE /api/vaults/{name}
  web/static/js/app.js   # EDIT — Vaults Alpine view (list/create/switch/rename/clear/delete) +
                         #        populate the shell vault picker from /api/vaults (was hardcoded)
  web/static/css/components.css  # EDIT — active-vault row marker
  web/templates/index.html       # EDIT — real Vaults view (replaces the placeholder) + picker bind
```

**Structure decision**: no new package. A new file (`vaults.go`) inside the existing UI transport,
mirroring `quarantine.go`. Two small engine edits (fix `ListVaults`; add `CreateVault`) close the
gaps Phase 0 found.

## Complexity Tracking

None — no constitution violations to justify.
