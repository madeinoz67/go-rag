# Tasks: Multi-Vault Unified Store + Cross-Vault Query (v2.0 Storage Model)

**Input**: Design documents from `/specs/052-multi-vault-store/` — [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/multi-vault.md](./contracts/multi-vault.md), [quickstart.md](./quickstart.md)

**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓.

**Tests**: INCLUDED — constitution: spec/test/evals first. Every phase + the migration ships a test.

**Scope note**: the **biggest slice in the project** (~25–30 files). The foundational phase carries the storage key-widening (every vault-scoped key gains an 8-byte wsPrefix), the engine vault-param threading (every method), the v3→v4 migration, and the transport vault-selectors. The user stories are the capabilities on top. Grounded in the adversarially-verified MuninnDB extraction.

## Format: `[ID] [P?] [Story?] Description (file path)`

- **[P]**: parallelizable (different files, no deps on incomplete tasks)
- **[USx]**: user-story phase tag (Setup/Foundational/Polish carry none)
- Every task names its exact file path

---

## Phase 1: Setup (keys package + vault registry skeleton)

**Purpose**: land the key-construction package + the vault registry skeleton so the key-widening + engine threading can proceed against compiling code.

- [ ] T001 Create `internal/storage/keys/keys.go`: pure key-construction functions taking `ws [8]byte` first arg — `VaultPrefix(name string) [8]byte` (SipHash-2-4, BigEndian, fixed keys sipKey0/sipKey1), `SourceKey(ws, srcID)`, `DocumentKey(ws, docID)`, `ChunkKey(ws, chunkID)`, `FTSPostingKey(ws, ...)`, `VaultMetaKey(ws) [9]byte` (`0x1A|ws`), `VaultNameIndexKey(name) [9]byte` (`0x1B|siphash(name)`), `IncrementWSPrefix(ws) [8]byte` (BigEndian+1, overflow error). Mirror MuninnDB's `keys/keys.go`. (R1, R2, R3, data-model.md)
- [ ] T002 [P] Create `internal/storage/vault.go`: `ResolveVaultPrefix(name) [8]byte` (LRU 10k → Pebble get 0x1B → SipHash fallback), `ListVaultNames()`, `WriteVaultName(ws, name)`, `VaultNameExists(name)`, `RenameVault(ws, oldName, newName)`, `BackfillVaultNames()`. Port near-verbatim from MuninnDB (adapted to go-rag's prefix table). `vaultPrefixCache *lru.Cache`, `vaultNameWritten sync.Map`. (R3, R4, R5)
- [ ] T003 [P] `internal/storage/vault_test.go` — tests: resolve (LRU hit/miss/SipHash fallback), list, write idempotent, rename (metadata-only, zero data moves), backfill (scan 0x02, placeholder names). Rename safety: the SipHash fallback returns wrong ws for renamed vaults; the LRU seeded from 0x1B at startup is authoritative. (R4)

**Checkpoint**: `CGO_ENABLED=0 go build ./...` clean; keys package + vault registry compile.

---

## Phase 2: Foundational (storage key-widening + engine threading + migration + transport vault params)

**⚠️ CRITICAL**: the biggest phase — widens every vault-scoped key, threads vault through the engine, adds the v3→v4 migration, and wires vault selectors on all transports. All user stories depend on this.

- [ ] T004 Widen every vault-scoped key builder in `internal/storage/` — replace the existing `SetWithPrefix/GetWithPrefix/PrefixScan` calls with the `keys.*` builders that prepend `wsPrefix[8]`. The 19 vault-scoped prefixes (0x01–0x15 per data-model.md) gain wsPrefix; the 6 global prefixes (0x09, 0x17–0x19, 0x1A, 0x1B) stay flat. Add `PrefixVaultMeta=0x1A`, `PrefixVaultNameIndex=0x1B` to `storage.go`. `db.go` methods gain ws-aware variants (or the keys builders replace them). (R1, data-model.md)
- [ ] T005 Thread `vault string` through every `internal/engine/` public method — `Add(ctx, vault, path, glob)`, `Query(ctx, vault, req)`, `DeleteDoc(ctx, vault, docID)`, `Reprocess(ctx, vault, path)`, `Status(vault)`, `ListDocuments(vault, req)`, `ListChunks(vault, ...)`, `GetDocument(vault, docID)`, `AuditRead(vault, opts)`, etc. Each resolves wsPrefix once via `store.ResolveVaultPrefix(vault)` at entry, threads `[8]byte` to storage + pipeline. (R6)
- [ ] T006 Per-vault in-memory index registries in `internal/engine/engine.go` — `idxFts`/`idxVec` become `map[[8]byte]*index.FTS` / `map[[8]byte]*index.Vector` (lazily seeded on first access per vault via LoadIndex, evictable on ClearVault). The `epoch` counter becomes per-vault (`map[[8]byte]*atomic.Uint64`). The query/embed caches include wsPrefix in the cache key. The pipeline's OnChange/OnEvent callbacks carry wsPrefix so they mutate only that vault's indexes. (R6)
- [ ] T007 Engine vault-routing tests — `internal/engine/vault_test.go`: two-vault isolation (add to A, query A, query B — no cross-leak); per-vault index seeding (first query pays LoadIndex, second reuses); cache-key scoping (no cross-vault cache contamination). (FR-004)
- [ ] T008 Migration v3→v4 — `internal/storage/migrate/v4_multi_vault.go`: detect legacy per-vault DBs at `~/.go-rag/vaults/<name>/data`; for each, `ws = keys.VaultPrefix(name)`; iterate every key `kind|payload`, rewrite as `kind|ws|payload` into the unified store (mechanical prepend, opaque values); write `0x1A|ws→name` + `0x1B|siphash(name)→ws`; archive legacy dirs (rename to `.prev`). Bump `ExpectedVersion` 3→4 in `migrate.go`. (R8)
- [ ] T009 Migration test — `internal/storage/migrate/v4_test.go`: create two legacy per-vault DBs with data; run v3→v4; verify all keys appear in the unified store under correct wsPrefixes (no data loss, counts match); verify idempotency (restart-safe); verify refuse-newer (v5+ rejected). (FR-008, FR-009)
- [ ] T010 [P] CLI vault param — `internal/cli/`: `--vault <name>` flag on every command (default "default"), threaded to the engine call. Cross-vault: `--vault a,b` (comma-separated for query). (R9)
- [ ] T011 [P] REST vault param — `internal/rest/`: `?vault=` query param + optional body field (must agree); VaultAuthMiddleware resolves + validates before the handler; admin session bypasses (UI path). (R9)
- [ ] T012 [P] gRPC vault param — `proto/gorag.proto` (+regen): `string vault = N;` on every request message; `repeated string vaults = M;` on QueryRequest. `internal/grpc/`: unary interceptor validates vault. (R9)
- [ ] T013 [P] MCP vault param — `internal/mcp/server.go`: optional `vault` arg (default "default") on every tool; `vaults` array on query tool. (R9)
- [ ] T014 [P] UI vault picker — `internal/ui/`: vault picker in the shell header (session-scoped); `X-Go-Rag-Vault` header on /api/* requests; cross-vault multi-select. (R9)

**Checkpoint**: `make build && make vet` clean; engine + storage + all transports carry vault; migration v3→v4 compiles.

---

## Phase 3: User Story 1 — One daemon serves all vaults (Priority: P1) 🎯 MVP

**Goal**: one daemon, ALL vaults, no restart to switch. Vault isolation confirmed.

**Independent Test**: [quickstart.md](./quickstart.md) §1 — add to "work", query "default" (isolated), both from the same running daemon.

### Implementation

- [ ] T015 [US1] Daemon opens ONE unified store — `internal/daemon/` (or `cmd/go-rag/`): `serve`/`start` opens one Pebble at the store path (not a per-vault dir); constructs ONE engine serving all vaults. The `--db-path` flag becomes "where is the unified store" (not "which vault"). `--vault` is a per-call CLI param, not a daemon flag. (R6)
- [ ] T016 [US1] US1 tests — `internal/engine/vault_test.go`: two-vault end-to-end — add to A, add to B, query A (only A's docs), query B (only B's), no cross-leak. Self-registration: write to a new vault name → it appears in ListVaultNames. (FR-003, FR-004, SC-001)

**Checkpoint**: US1 independently testable — one daemon, all vaults, isolated (MVP).

---

## Phase 4: User Story 2 — Cross-vault query (Priority: P1)

**Goal**: query across multiple vaults — fan-out + RRF rank-merge.

**Independent Test**: [quickstart.md](./quickstart.md) §2 — cross-vault query returns hits from both vaults, ranked together.

### Implementation

- [ ] T017 [US2] Cross-vault query — `internal/engine/query.go`: `QueryRequest.Vaults []string` (empty = single-vault default); when non-empty, fan out per-vault retrieval (BM25 + vector) in parallel → N×2 ranked lists → generalize `reciprocalRankFusion` from 2-list to N-list → rerank (vault-agnostic) → threshold + dedup. Budget cap: min(N×K, 2×pool-size) pre-rerank. `internal/index/retrieval.go`: generalize reciprocalRankFusion signature. (R7)
- [ ] T018 [US2] US2 tests — `internal/engine/crossvault_test.go`: three vaults with distinct docs; cross-vault query [A,B,C] returns hits from all three (ranked together); per-vault queries return only that vault; cross-vault subsumes individual top hits; cache key includes sorted Vaults set. (FR-005, SC-002)

**Checkpoint**: US2 independently testable — cross-vault query.

---

## Phase 5: User Story 3 — Vault lifecycle (Priority: P2)

**Goal**: rename (metadata-only), clear (range-tombstone), delete from the running daemon.

**Independent Test**: [quickstart.md](./quickstart.md) §3 — rename → query (results present) → clear → query (empty).

### Implementation

- [ ] T019 [US3] Vault lifecycle engine methods — `internal/engine/engine.go`: `RenameVault(ctx, oldName, newName)` (delegates to `store.RenameVault` + re-keys in-memory registries), `ClearVault(ctx, vault)` (range-tombstone per kind + evict in-memory indexes), `DeleteVault(ctx, vault)` (clear + `DeleteVaultNameOnly`). Build the ClearVault data-prefix list from the 19 vault-scoped kinds (data-model.md). (R5)
- [ ] T020 [US3] US3 tests — `internal/engine/lifecycle_test.go`: rename (<1ms, data present after), clear (instant, empty after), delete (gone from listings/queries). ClearVault scope: all 19 vault-scoped kinds tombstoned; 6 global kinds untouched. (FR-006, SC-003)

**Checkpoint**: US3 independently testable — vault lifecycle.

---

## Phase 6: User Story 4 — All transports carry vault (Priority: P2)

**Goal**: verify vault param flows through every transport (CLI/REST/gRPC/MCP/UI) with fail-closed resolution.

**Independent Test**: from each transport, add to vault A and query vault B — all succeed and isolated.

### Implementation / Verification

- [ ] T021 [US4] Cross-transport vault-param parity test — `internal/engine/parity_test.go`: from each transport (CLI/REST/gRPC/MCP), add to vault A, query vault B — all isolated; default "default" works unchanged; unknown vault → error (fail-closed). (FR-007, SC-005)

**Checkpoint**: US4 independently testable — all transports vault-aware.

---

## Phase 7: User Story 5 — Migration validation (Priority: P2)

**Goal**: the v3→v4 migration is correct and archival-safe.

**Independent Test**: [quickstart.md](./quickstart.md) §4 — two legacy per-vault DBs → migrate → both present with correct counts → legacy dirs archived.

### Implementation / Verification

- [ ] T022 [US5] Migration end-to-end test — `internal/storage/migrate/v4_e2e_test.go`: create two real per-vault Pebble DBs with documents/chunks/embeddings; run the daemon (triggers on-open migration v3→v4); verify both vaults' data in the unified store (correct wsPrefixes, no loss, counts match); verify legacy dirs archived (.prev); verify restart opens unified directly (no re-migration). (FR-008, FR-009, SC-004)

**Checkpoint**: US5 independently testable — migration validated.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T023 [P] Gate hygiene — `make lint` (0 findings), `make vet`, `make test -race` clean across ALL packages (storage, engine, migrate, cli, rest, grpc, mcp, ui, index, pipeline).
- [ ] T024 [P] quickstart validation — run [quickstart.md](./quickstart.md) §1–§5 on a fresh unified store: two vaults, one daemon, add/query/cross-query/rename/clear + the migration from legacy DBs. Interceptor browser verify for §5 (vault picker, cross-vault query).
- [ ] T025 [P] Doc sync — update PRD §6.7 (new key-space layout); update the Constitution (ExpectedVersion 3→4); update spec 046's Slice Decomposition; update `PROJECTS.md` + MuninnDB memory; reframe spec 051 (Vaults view → switcher UI on the unified store).

---

## Dependencies & Execution Order

### Phase dependencies
- **Setup (Phase 1)**: no deps. T001 (keys) blocks T004 (key-widening); T002/T003 (vault.go) blocks T019 (lifecycle).
- **Foundational (Phase 2)**: depends on Setup. T004 (key-widening) → T005 (engine threading) → T006 (index registries) → T007 (tests). T008 (migration) depends on T004. T010–T014 (transport vault params) are [P] — parallel once T005 lands.
- **US1 (Phase 3)**: depends on Foundational (T015 daemon + T016 tests). MVP gate.
- **US2 (Phase 4)**: depends on Foundational + US1 (cross-vault query needs the multi-vault engine).
- **US3 (Phase 5)**: depends on Foundational (T002 vault.go + T019 engine methods).
- **US4 (Phase 6)**: depends on US1–US3 (verifies all transports).
- **US5 (Phase 7)**: depends on Foundational (T008 migration).
- **Polish (Phase 8)**: depends on all stories complete.

### Parallel opportunities
- Phase 1: T002/T003 (vault.go + test) parallel with T001 (keys).
- Phase 2: T010–T014 (the five transport vault-param tasks) are all [P] — fully parallel once T005 lands.
- Phase 2: T008 (migration) parallel with T006 (index registries) — different files.
- Story test tasks overlap their implementation tasks on different files.

---

## Implementation Strategy

### MVP First
1. Complete Phase 1 (Setup) + Phase 2 (Foundational) — the unified store, vault-aware engine, migration, and all transports carry vault.
2. Complete Phase 3 (US1 — one daemon, all vaults). **STOP and VALIDATE**: two vaults, one daemon, isolated (quickstart §1). This is the **MVP gate**.
3. Complete Phase 4 (US2 — cross-vault query). The **demo-complete** point: query across vaults — the "cross-repo" capability.
4. Phases 5–8 add lifecycle + transport verification + migration validation + polish.

### Incremental delivery
- Setup → Foundational → US1 (MVP) → US2 (demo) → US3 → US4 → US5 → Polish.
- Each checkpoint is independently testable per its Independent Test.

### Single-author note
This repo commits straight to `main`. Commit after each task or logical group; run `make lint && make test -race` before push. This is the biggest slice — expect multiple commits.

---

## Notes

- The **keys package** (`internal/storage/keys/`) is the one new package. Everything else is edits.
- The **migration v3→v4** is the constitution-gated storage-discipline item. ExpectedVersion bumps 3→4.
- **Cross-vault query** is fan-out + RRF — NOT prefix-scanning. go-rag's rank-fusion is scale-invariant (the property MuninnDB's activation lacks, which is why MuninnDB gates cross-vault off).
- **19 vault-scoped prefixes** widen; **6 global** stay flat (per data-model.md).
- **Rename** is metadata-only (<1ms) via the 0x1B siphash indirection.
- The per-vault in-memory index registries preserve the H01 seed-once invariant (per vault).
- Config/auth stay global (single-operator; one Ollama model, one admin).
