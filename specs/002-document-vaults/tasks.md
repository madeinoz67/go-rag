# Tasks: Document Vaults

**Input**: Design documents from `specs/002-document-vaults/` (plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md)

**Tests**: Not requested — test tasks omitted.

**Organization**: Tasks grouped by user story (US1 = MVP, US2 manage, US3 daemon, US4 clone/export).

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency)
- **[Story]**: which user story (US1–US4)

---

## Phase 1: Setup (Shared Infrastructure)

- [x] T001 [P] Create `internal/vault/registry.go`: vault root resolution (`VaultRoot()` → `GO_RAG_VAULT_ROOT` env or `~/.go-rag/vaults/`), `VaultPath(name)` → absolute path, `ValidateName(name)` → lowercase alphanumeric + hyphens, 1–64 chars
- [x] T002 [P] Create `internal/vault/registry.go`: `Exists(name)`, `List()` → `[]string` (scan vault root for directories with `config.json`), `Create(name, cfg)` → mkdir + write config.json + mkdir data/
- [x] T003 [P] Create `internal/vault/registry_test.go`: ValidateName (valid/invalid cases), VaultPath resolution, Create + Exists + List round-trip

---

## Phase 2: Foundational (--vault flag)

**⚠️ CRITICAL**: No user story work begins until the `--vault` flag resolves correctly.

- [x] T004 Add `--vault` persistent flag to `internal/cli/root.go`: when set, resolves `dbPath` to `VaultPath(vaultName)` AFTER persistent flags are parsed but BEFORE subcommand RunE executes. Use cobra's `PersistentPreRunE` on rootCmd to intercept and set the package-level `dbPath`. `--db-path` takes precedence over `--vault` if both are set.
- [x] T005 Verify backward compat: `go-rag init` / `go-rag status` / `go-rag add` (without `--vault`) work unchanged — `dbPath` stays `./.go-rag`. Run `go build && ./bin/go-rag --help` to confirm `--vault` flag appears alongside `--db-path`.

**Checkpoint**: `--vault cyber-notes` resolves to `~/.go-rag/vaults/cyber-notes/`; no flags = current behaviour.

---

## Phase 3: User Story 1 - Create and Use a Vault (Priority: P1) 🎯 MVP

**Goal**: Create a vault, add documents, query it — all scoped to that vault.
**Independent Test**: Create vault A, add a doc, query → results from A only.

- [x] T006 [US1] Implement `go-rag vault create <name>` in `internal/cli/vault.go`: accepts `--embedding_model`, `--ollama-url`, `--mcp-addr` flags; calls `vault.Create(name, cfg)`; prints success with vault path + model. Errors if vault exists.
- [x] T007 [US1] Register `vault` command + `create` subcommand in `internal/cli/root.go` AddCommand list.
- [x] T008 [US1] Implement `go-rag --vault <name> add <path>` end-to-end: verify the existing `add` command works when `dbPath` points to a vault directory (it should — add uses `openDB(dbPath)` which reads config.json + opens Pebble). Test: create vault → add a file → verify doc stored.
- [x] T009 [US1] Implement `go-rag --vault <name> query "<q>"` end-to-end: same — query uses `openDB(dbPath)`. Test: create vault → add → query → get results.
- [x] T010 [US1] Verify cross-vault isolation: create vault A + vault B, add different docs to each, query A → results from A only, query B → results from B only. No cross-contamination (each vault = separate Pebble instance, guaranteed by construction).

**Checkpoint**: Vaults are created and used end-to-end with full isolation.

---

## Phase 4: User Story 2 - List and Manage Vaults (Priority: P2)

**Goal**: List vaults with stats, delete/clear vaults.
**Independent Test**: Create 2 vaults, list → both appear. Delete one → gone.

- [x] - [ ] T011 [US2] Implement `go-rag vault list` in `internal/cli/vault.go`: scan vault root for directories with config.json; for each, open config.json (embedding model) + scan Pebble 0x02 prefix (doc count) + stat dir (storage size) + check daemon pidfile. Print table: VAULT / DOCS / MODEL / DAEMON / STORAGE. `--json` output.
- [x] - [ ] T012 [US2] Implement `go-rag vault delete <name>` in `internal/cli/vault.go`: confirm prompt (skip with `--force`); `os.RemoveAll(vaultPath)`. Errors if vault doesn't exist or daemon is running for it.
- [x] - [ ] T013 [US2] Implement `go-rag vault clear <name>` in `internal/cli/vault.go`: remove `data/` directory only (preserve `config.json`). Recreate empty `data/`.

**Checkpoint**: Vaults are fully manageable from the CLI.

---

## Phase 5: User Story 3 - Per-Vault Daemon + Agent (Priority: P3)

**Goal**: Start/stop/status per vault; MCP proxy connects to the right vault.
**Independent Test**: Start daemon for vault A, query via MCP, results from A.

- [x] - [ ] T014 [US3] Update `internal/cli/start.go`: `--vault <name> start` passes the vault's `VaultPath(name)` as the dbPath to `daemon.Start()`. The daemon reads the vault's config.json for `mcp_addr`.
- [x] - [ ] T015 [US3] Update `internal/cli/stop.go` and `internal/cli/status.go`: same vault-aware resolution (daemon.Status reads pidfile from the vault's directory).
- [x] - [ ] T016 [US3] Update `internal/cli/mcp.go` (stdio proxy): `--vault <name> mcp` reads the vault's `daemon.addrs` for the proxy target.
- [x] - [ ] T017 [US3] Update `internal/cli/dashboard.go`: when `--vault` is set, show the vault name in the panel header (e.g., "go-rag — vault: cyber-notes").

**Checkpoint**: Each vault can run its own daemon; MCP clients connect to a specific vault.

---

## Phase 6: User Story 4 - Clone and Export (Priority: P4)

- [x] - [ ] T018 [P] [US4] Implement `go-rag vault clone <src> <dst>` in `internal/cli/vault.go`: copy the source vault directory to dst (including config.json + data/). Optionally `--embedding_model` to re-embed with a different model (calls reprocess after copy). Async for large vaults (show progress bar).
- [x] - [ ] T019 [P] [US4] Implement `go-rag vault export <name>` in `internal/cli/vault.go`: tar the vault directory to stdout or `--output <file>`. Include config.json + data/.

---

## Phase 7: Polish & Cross-Cutting

- [x] - [ ] T020 [P] Update `README.md` with vault documentation: quickstart (create, add, query across vaults), `--vault` flag, vault management commands.
- [x] - [ ] T021 [P] Update `internal/config/config.go`: add `vault_root` to config (for the vault root path, stored in a top-level `~/.go-rag/config.json` if needed — or env-only via `GO_RAG_VAULT_ROOT`).
- [x] - [ ] T022 Final green build: `make build && go test ./... && make vet`; validate quickstart.md end-to-end (create 2 vaults, add docs, verify isolation).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no deps — start immediately.
- **Foundational (Phase 2)**: depends on Phase 1 (registry). BLOCKS all stories.
- **US1 (Phase 3)**: depends on Phase 2 (--vault flag). The MVP.
- **US2 (Phase 4)**: depends on Phase 1 (registry) + Phase 2 (--vault). Can overlap US1.
- **US3 (Phase 5)**: depends on Phase 2 + the daemon existing (it does). Can overlap US2.
- **US4 (Phase 6)**: depends on Phase 1 + Phase 3 (vaults work before cloning them).
- **Polish (Phase 7)**: depends on US1.

### MVP scope

Phase 1 + 2 + 3 (US1) = **10 tasks**. Create a vault, add docs, query with isolation.

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 (vault registry) → Phase 2 (--vault flag) → Phase 3 (create + use).
2. **STOP & VALIDATE**: create two vaults, add docs, query each — verify zero cross-contamination.
3. Ship.

### Incremental Delivery

- After US1: add US2 (vault list/delete/clear) — quick management commands.
- Add US3 (per-vault daemon) — MCP agents connect to specific vaults.
- Add US4 (clone/export) — backup and experimentation.
