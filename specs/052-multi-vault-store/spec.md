# Feature Specification: Multi-Vault Unified Store + Cross-Vault Query (v2.0 Storage Model)

**Feature Branch**: `052-multi-vault-store`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: *"Collapse go-rag's N separate per-vault Pebble DBs into ONE unified store (MuninnDB's proven pattern). One daemon serves ALL vaults — vault is a per-request parameter, not a process-level config. Query across vaults ('cross-repo') via fan-out + RRF rank-merge. Not in production — migrate aggressively. Shared embedder + config across vaults."*

## Context & Background

go-rag today is **single-vault-per-process**: each vault is a separate Pebble DB under
`~/.go-rag/vaults/<name>`, the daemon opens ONE engine on ONE vault, and switching means a
restart. The Vaults sidebar view (spec 051) exposed this as a gap: the operator can *see* their
vaults but can't *use* more than one without restarting.

**This spec is the v2.0 storage-model epic.** It collapses N per-vault DBs into ONE unified,
prefix-keyed store — exactly MuninnDB's proven, adversarially-verified pattern. Every key gains
an 8-byte vault prefix (`wsPrefix` = SipHash-2-4 of the vault name). The daemon opens ONE engine
serving ALL vaults; vault becomes a per-operation parameter (resolved to `wsPrefix` at the engine
boundary). The operator can use multiple vaults from one running daemon, switch instantly
(parameter change, not DB reopen), manage vault lifecycle (create/rename/clear/delete — rename is
metadata-only), and — critically — **query across vaults** ("cross-repo") via fan-out + RRF
rank-merge.

go-rag is **not in production**, so the migration from N per-vault DBs to the unified store is
aggressive: a one-way key-widening rewrite (prepend `wsPrefix` to every key) via spec 034's
existing migration runner, or a clean fresh-start. No backward-compat burden.

The design is grounded in MuninnDB's source (extracted + adversarially verified by a 5-agent
research workflow, 886k tokens): `storage/vault.go` (prefix-keying + registry + lifecycle),
`engine/engine_vault.go` (single-engine-multi-vault routing), `transport/mbp/vault_scope.go`
(per-request vault selection), and the cross-vault recall analysis.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One daemon serves all vaults (Priority: P1) 🎯 MVP

An operator starts ONE daemon and it serves ALL their vaults simultaneously. Every operation
(query, add, remove, reingest, status) takes a vault parameter — the operator specifies which
vault (default "default") on each call. No restart to switch. The daemon holds ONE engine, ONE
store, ONE embedder — vaults are a routing dimension inside the unified store, not separate
processes or databases.

**Why this priority**: the foundation. Without one-daemon-all-vaults, cross-vault query and
vault lifecycle have nothing to operate on. This single story is a viable MVP.

**Independent Test**: Start a daemon with two vaults ("default" and "work"); from the CLI/console,
add a document to "work" and query "default" — both succeed against the same running daemon
without restart; the documents are isolated (querying "default" doesn't return "work"'s docs).

**Acceptance Scenarios**:

1. **Given** two vaults, **When** the operator adds to vault A and queries vault B, **Then** both
   succeed against the same daemon (no restart) and the results are isolated by vault.
2. **Given** the daemon is running, **When** the operator switches from vault A to vault B, **Then**
   the switch is instant (a parameter change, not a DB reopen) — no latency spike, no reconnect.
3. **Given** a vault that doesn't exist yet, **When** the operator writes to it, **Then** it is
   self-registered (the vault is created on first write, no explicit create needed).
4. **Given** the unified store, **When** the operator lists vaults, **Then** every vault appears
   (from the registry index), including ones created implicitly by first write.

---

### User Story 2 - Query across vaults (Priority: P1)

An operator can search ACROSS multiple vaults in a single query — the "cross-repo" capability.
The query fans out to each target vault's BM25 + vector retrieval in parallel, then fuses the
per-vault results by rank via reciprocal rank fusion (RRF — go-rag's existing rank-based,
scale-invariant fusion; BM25/vector scores aren't comparable across vaults, but ranks are). The
fused pool is then reranked (the reranker is vault-agnostic). The operator specifies which vaults
to search (empty/default = the current single-vault behaviour; a list = cross-vault mode).

**Why this priority**: the primary reason the unified store exists for the operator. Without
cross-vault query, multi-vault is just "switch faster" — with it, the operator can find
information across their entire corpus in one search.

**Independent Test**: Ingest distinct documents into vaults A, B, and C; run a cross-vault query
targeting all three; the results include hits from all three, ranked by RRF-fused rank; the same
query against each vault individually returns only that vault's hits; the cross-vault results
subsume the individual ones.

**Acceptance Scenarios**:

1. **Given** documents in vaults A, B, and C, **When** the operator queries across [A, B, C],
   **Then** results from all three vaults appear, ranked together.
2. **Given** a cross-vault query, **When** compared to per-vault queries, **Then** the cross-vault
   results subsume the individual vaults' top hits (no vault is silenced).
3. **Given** a cross-vault query, **When** reranking is enabled, **Then** the reranker scores
   candidates from all vaults uniformly (it never sees the vault — only query + text).
4. **Given** a cross-vault query, **When** vaults have very different sizes, **Then** each vault's
   top-ranked hits get fair RRF representation (rank-based fusion is size-invariant).
5. **Given** a query with no vaults specified, **When** executed, **Then** it queries the default
   vault only (backward-compatible single-vault behaviour).

---

### User Story 3 - Vault lifecycle from one daemon (Priority: P2)

An operator can create, rename, clear, and delete vaults from the running daemon — no restart.
**Rename** is metadata-only (the vault's 8-byte prefix is stable; only the name↔prefix registry
indexes change — two Pebble keys, zero data moves). **Clear** is a range-tombstone per key-family
per vault (fast, bounded). **Create** is implicit (first write self-registers) or explicit. The
existing `go-rag vault` CLI (create/list/delete/clear/clone/export/import) operates against the
unified store, not per-vault directories.

**Why this priority**: essential for managing a multi-vault corpus, but the daemon-all-vaults
(US1) + cross-vault query (US2) already carry the primary value.

**Independent Test**: Create vault "test", add documents, rename to "test-renamed" (instant — no
data copy), query "test-renamed" (results present), clear "test-renamed" (instant — range
tombstone), query "test-renamed" (empty). All from the running daemon, no restart.

**Acceptance Scenarios**:

1. **Given** vault "old", **When** renamed to "new", **Then** the rename is instant (metadata-only
   — no data moves), and queries against "new" return the same results "old" did.
2. **Given** vault "temp", **When** cleared, **Then** all its data is gone (range-tombstone per
   key-family) but the vault still exists (empty); re-adding works.
3. **Given** vault "temp" (cleared), **When** deleted, **Then** the vault is gone from listings
   and queries — its registry keys are removed.
4. **Given** a new vault name, **When** the operator writes to it, **Then** it self-registers (no
   explicit create needed).

---

### User Story 4 - All transports carry the vault parameter (Priority: P2)

Every transport — CLI, MCP, REST, gRPC, and the UI console — carries a vault selector on every
operation. The default is "default". The resolution is fail-closed: an unauthenticated or
unrecognised vault is rejected. The CLI's `--vault <name>` flag becomes a per-call parameter (not
a DB-path selector); REST accepts `?vault=`; gRPC carries a `vault` field on every request; MCP
tools take an optional `vault` arg; the UI console has a vault picker in the shell. The engine
resolves vault name → wsPrefix once at the boundary; the storage layer never sees a vault string.

**Why this priority**: the plumbing that makes US1–US3 reachable from every surface. Functional
but invisible; must hold before the epic ships.

**Independent Test**: From each transport (CLI, REST, gRPC, MCP, UI), add a document to vault "A"
and query vault "B" — all succeed and are isolated. The default vault ("default") works
unchanged.

**Acceptance Scenarios**:

1. **Given** any transport, **When** the operator specifies a vault, **Then** the operation
   targets that vault only (isolation).
2. **Given** any transport, **When** no vault is specified, **Then** the operation targets
   "default" (backward-compatible).
3. **Given** a cross-vault query from any transport, **When** multiple vaults are specified,
   **Then** the query fans out and fuses (US2).

---

### User Story 5 - Aggressive migration from N per-vault DBs (Priority: P2)

go-rag is not in production. The migration from N separate per-vault Pebble DBs to the unified
prefix-keyed store is aggressive and one-way: for each legacy vault, every key is rewritten as
`kind(1)|wsPrefix(8)|payload` into the unified store, the registry indexes are written, and the
legacy per-vault directories are archived (not deleted — a rollback window). The migration runs
via spec 034's existing on-open migration runner (numbered, idempotent, per-step fsync,
refuse-newer). A fresh-start option (no legacy data) is also supported — the unified store starts
empty, self-registering vaults on first write.

**Why this priority**: the bridge from today's model to the unified store. Not in production =
no backward-compat burden.

**Independent Test**: With two existing per-vault DBs, run the migration; both vaults' data
appears in the unified store (queryable, correct counts); the legacy directories are archived; a
restart opens the unified store directly (no re-migration).

**Acceptance Scenarios**:

1. **Given** N per-vault DBs with data, **When** the migration runs, **Then** all data appears in
   the unified store under the correct wsPrefixes — no data loss, counts match.
2. **Given** the migration is complete, **When** the daemon restarts, **Then** it opens the unified
   store directly (no re-migration; idempotent).
3. **Given** a fresh install (no legacy vaults), **When** the daemon starts, **Then** the unified
   store is empty and vaults self-register on first write.

---

### Edge Cases

- **Vault name collision** (two names hash to the same wsPrefix) — astronomically unlikely (2^64
  SipHash space); detectable at startup (BackfillVaultNames checks); document explicitly.
- **Renamed vault resolution** — the wsPrefix is frozen at creation; a renamed vault's
  `siphash(newName) ≠ wsPrefix`. Resolution must use the registry (LRU/Pebble-get), NOT the raw
  SipHash fallback (which silently mis-resolves renamed vaults). Load the LRU from the persisted
  index at startup so the fallback is cold-only.
- **Cross-vault query with heterogeneous embedding models** — if vaults were built under different
  models, vector retrieval is per-vault (each vault's HNSW uses its own model). Cross-vault RRF
  still works (rank-based, not score-based), but vector-similarity is only meaningful within a
  vault. The reranker (text-based) is unaffected.
- **Cross-vault query cost** — N vaults × pool-size each = N×K candidates to rerank. Cap the
  post-RRF pre-rerank pool to keep the rerank budget flat.
- **Per-vault in-memory index memory** — N vaults = N FTS indexes + N HNSW graphs in memory.
  Evict cold vaults (LRU by wsPrefix) if memory is a concern.
- **Empty vault** — exists in the registry (0x0E) but has no data keys; queries return empty;
  healthy state, not an error.
- **Deleted-then-recreated vault** — re-creation (first write) gets a NEW wsPrefix (SipHash of
  the name — deterministic, so same wsPrefix as before if the name is reused); the old data is
  gone (cleared on delete). Self-consistent.
- **Concurrent writes to different vaults** — safe (the unified store is single-writer; Pebble
  serializes all writes; different vaults' keys don't conflict).
- **Watcher/scan targeting a vault** — the watcher (spec 007) and enrichment (spec 029) currently
  target a vault directory; under the unified store they target a vault NAME (resolved to wsPrefix).

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST store ALL vaults in ONE unified Pebble database, with every key
  shaped `kindByte(1) | wsPrefix(8) | payload`, where `wsPrefix` is a deterministic 8-byte hash
  of the vault name.
- **FR-002**: The system MUST maintain a bidirectional vault registry: `wsPrefix → name` (for
  listing) and `siphash(name) → wsPrefix` (for resolution), enabling metadata-only rename.
- **FR-003**: The daemon MUST open ONE engine serving ALL vaults — vault is a per-operation
  parameter (default "default"), resolved to wsPrefix at the engine boundary. No restart to
  switch.
- **FR-004**: Every operation (query, add, remove, reingest, status, list, etc.) MUST take a
  vault parameter and scope its reads/writes to that vault's wsPrefix. Results MUST be isolated
  by vault (a query on vault A never returns vault B's data).
- **FR-005**: The system MUST support cross-vault query: when multiple vaults are specified, the
  query fans out to each vault's retrieval in parallel and fuses results by rank (RRF). The
  reranker MUST score candidates uniformly (vault-agnostic).
- **FR-006**: The system MUST support vault lifecycle from the running daemon: create (implicit
  on first write or explicit), rename (metadata-only — zero data moves), clear (range-tombstone
  per key-family), delete (clear + registry-key removal).
- **FR-007**: All five transports (CLI, MCP, REST, gRPC, UI) MUST carry a vault selector on every
  operation, with a fail-closed resolution rule (unrecognised/unauthenticated vault rejected).
  Default "default" when unspecified.
- **FR-008**: The system MUST provide a one-way migration from N per-vault Pebble DBs to the
  unified store (rewrite every key with its wsPrefix), via spec 034's migration runner (numbered,
  idempotent, per-step fsync, refuse-newer). Legacy directories MUST be archived (not deleted).
- **FR-009**: The migration MUST be idempotent (restart-safe) and refuse a newer-schema store
  (never silently misread).
- **FR-010**: The embedder, embedding model, and core config MUST be shared across all vaults
  (one Ollama model, one engine — not per-vault config).
- **FR-011**: The per-vault in-memory indexes (FTS, vector) MUST be lazily seeded on first access
  per vault and evictable (to bound memory for many vaults). The query/embed caches MUST include
  wsPrefix in the cache key (no cross-vault contamination).
- **FR-012**: The system MUST ship as a single binary, pure Go, CGO_ENABLED=0, no Node/build
  chain (unchanged from the constitution).

### Key Entities *(include if feature involves data)*

- **Vault**: a named corpus inside the unified store. Identity = an 8-byte `wsPrefix` (SipHash of
  the name). All vault data lives under its wsPrefix. The name↔wsPrefix mapping is in the
  registry.
- **wsPrefix**: the 8-byte deterministic hash of a vault name (SipHash-2-4). The vault's identity
  inside the key space. Stable across renames (frozen at creation).
- **Vault Registry**: two global key indexes — `wsPrefix → name` (list) and `siphash(name) →
  wsPrefix` (resolve). The siphash indirection enables metadata-only rename.
- **Cross-Vault Query**: a query that fans out to multiple vaults and fuses results by RRF rank.
  Specified by a `Vaults []string` field on the query request (empty = single-vault default).
- **Migration Step**: a numbered, idempotent transform in spec 034's migration runner that
  rewrites every key from the legacy per-vault layout to the unified `kind|wsPrefix|payload`
  layout.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can start one daemon and use N vaults simultaneously without restart —
  verifiable by adding/querying across vaults from the same running process.
- **SC-002**: A cross-vault query across 3 vaults returns results from all three, ranked together,
  within 2× the latency of a single-vault query (the fan-out is parallel).
- **SC-003**: Vault rename completes in under 1ms (metadata-only — two Pebble key writes, zero
  data moves), regardless of vault size.
- **SC-004**: The migration of N per-vault DBs preserves 100% of documents/chunks/embeddings —
  zero data loss, counts match before and after.
- **SC-005**: Every transport (CLI/MCP/REST/gRPC/UI) can target any vault by name — verifiable
  by adding to vault A and querying vault B from each transport.
- **SC-006**: The unified store ships as one binary, one Pebble instance, pure Go — unchanged
  from the constitution's build constraint.

---

## Assumptions

- The unified-store model is grounded in MuninnDB's proven, adversarially-verified pattern
  (storage/vault.go + engine/engine_vault.go + transport vault-scope). Key shape, registry,
  resolve, lifecycle, and engine routing are all extracted from working source.
- go-rag is **not in production** — the migration is aggressive and one-way (no backward-compat
  burden; legacy per-vault directories archived, not deleted).
- The embedder, embedding model, and core config are **shared** across vaults (one Ollama model,
  one engine — Stephen's explicit steer: "each vault to have same embedder etc."). Per-vault
  embedding-model overrides are a future concern.
- The migration reuses spec 034's existing on-open migration runner (numbered, idempotent,
  per-step fsync, refuse-newer) — it adds a new step, not a new system.
- The cross-vault query's RRF fusion is clean because go-rag's `reciprocalRankFusion` is
  **rank-based and scale-invariant** — confirmed from source (internal/index/retrieval.go).
- Auth (spec 045) stays single-operator (no per-vault RBAC/ACL — PRD §2.2). The fail-closed vault
  resolution prevents cross-vault data leakage without RBAC.
- Spec 051 (Vaults view) becomes the vault-switcher UI on top of this epic. The read-only 051
  spec is reframed when the unified store lands.
- The Constitution's five Core Principles are UPHELD: the unified store strengthens Principle IV
  (single-writer — one global writer instead of per-vault) and Principle V (every operation
  vault-aware across all transports). The storage-discipline rule (any key-space layout change
  needs a numbered migration) is satisfied by the migration step.

---

## Open Questions (to resolve in plan / tasks)

- **Key-family layout** — lead with `kindByte` then `wsPrefix` (preserves the existing family-byte
  convention, but scatters each vault's keys across N family prefixes → worse SSTable compaction
  locality) OR lead with `wsPrefix` then `kindByte` (better per-vault locality, breaks the family-
  byte convention). MuninnDB leads with kind. Lean: lead with kind (matches MuninnDB, preserves
  convention); revisit if compaction benchmarks show a problem.
- **Per-vault vs global CorpusMeta** (0x10, the H11 drift baseline) — embedding drift is
  conceptually per-vault (each vault has its own corpus baseline). Lean: per-vault (give it the
  wsPrefix treatment; include in ClearVault).
- **Auth/config scope** — auth (0x17/0x18/0x19) and config (0x09) stay GLOBAL (flat, no wsPrefix)
  — consistent with single-operator (auth is instance-wide today). Confirm.
- **Idempotency scope** — receipts (PrefixIdempotency) global or per-vault? Lean: global (matches
  MuninnDB).
- **ClearVault orphan keys** — the MuninnDB verifier flagged six vault-scoped prefixes that
  survive ClearVault (0x18/0x1A–0x1E). go-rag's clear list must be built per-key (decide each),
  not by analogy.
- **ResolveVaultPrefix rename safety** — the SipHash fallback returns a WRONG ws for renamed
  vaults. The LRU MUST be seeded from the persisted registry at startup; the fallback is cold-only.
  Never call `VaultPrefix(name)` directly on the hot path.
- **Cross-vault rerank budget** — N vaults × pool-size = N×K candidates. Cap the post-RRF
  pre-rerank pool to keep the rerank budget flat. Lean: pool-size / N per vault, or a fixed
  post-RRF ceiling.
