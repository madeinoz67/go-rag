# Feature Specification: Document Vaults

**Feature Branch**: `002-document-vaults`

**Created**: 2026-06-20

**Status**: Draft

**Input**: Derived from muninndb vault research + go-rag architecture analysis.

> Scope note: vaults are isolated document corpora — each vault is a separate
> Pebble database with its own config, embedding model, and indexes. This enables
> per-collection RAG (e.g., a cybersecurity vault, a personal vault) without
> cross-contamination. v1 has no auth/RBAC (local trusted); the architecture
> accommodates future tenanted access.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Create and Use a Vault (Priority: P1) 🎯 MVP

A user creates a named vault for a specific document collection, ingests documents into it, and queries it independently of other vaults. Each vault can use a different embedding model.

**Why this priority**: Vaults have no value if you can't create one and use it end-to-end. This is the minimal useful unit.

**Independent Test**: Create a vault, add one file, query it, get results. Verify the results don't include documents from other vaults.

**Acceptance Scenarios**:
1. **Given** no vaults exist, **When** the user runs `go-rag vault create cyber-notes`, **Then** a vault directory is created with a default config.
2. **Given** vault "cyber-notes" exists, **When** the user runs `go-rag --vault cyber-notes add ./docs/`, **Then** documents are ingested into that vault only.
3. **Given** vault "cyber-notes" has documents, **When** the user runs `go-rag --vault cyber-notes query "threat model"`, **Then** results come only from that vault.
4. **Given** two vaults with different documents, **When** the user queries each, **Then** results are isolated — no cross-vault leakage.

---

### User Story 2 - List and Manage Vaults (Priority: P2)

A user lists all vaults, sees their document counts + embedding models, and can delete/clear vaults they no longer need.

**Independent Test**: Create 2 vaults, add docs to each, run `vault list`, verify both appear with counts. Delete one, verify it's gone.

**Acceptance Scenarios**:
1. **Given** vaults "default" and "cyber-notes" exist, **When** the user runs `go-rag vault list`, **Then** both vaults are listed with doc counts, embedding model, and storage size.
2. **Given** vault "old-project" exists, **When** the user runs `go-rag vault delete old-project`, **Then** the vault directory is removed and it no longer appears in `vault list`.
3. **Given** vault "test-data" exists with documents, **When** the user runs `go-rag vault clear test-data`, **Then** the data is cleared but the config is preserved (reusable vault).

---

### User Story 3 - Per-Vault Daemon and Agent Assignment (Priority: P3)

A user starts a daemon for a specific vault and configures an MCP client (Claude Desktop) to connect to that vault only. Multiple daemons can run for different vaults simultaneously.

**Independent Test**: Start a daemon for vault A, configure Claude Desktop to connect to it, query via MCP, verify results come from vault A.

**Acceptance Scenarios**:
1. **Given** vault "cyber-notes" exists, **When** the user runs `go-rag --vault cyber-notes start`, **Then** a daemon starts serving MCP on the vault's configured port.
2. **Given** the daemon is running, **When** an MCP client connects via the stdio proxy, **Then** `go_rag_query` returns results from that vault only.
3. **Given** two daemons running for two vaults on different ports, **When** two MCP clients connect to each, **Then** each client sees only its vault's data.

---

### User Story 4 - Vault Clone and Export (Priority: P4)

A user clones a vault (for experimentation with a different embedding model) and exports a vault for backup or transfer.

**Acceptance Scenarios**:
1. **Given** vault "cyber-notes" exists with 1000 docs, **When** the user runs `go-rag vault clone cyber-notes cyber-experiment`, **Then** a new vault is created with all documents copied.
2. **Given** vault "cyber-notes" exists, **When** the user runs `go-rag vault export cyber-notes`, **Then** a portable archive is written.

---

### Edge Cases

- What happens when `--vault` names a non-existent vault? → ERROR "vault not found, run 'go-rag vault create <name>'"
- What happens when the default vault path (`.go-rag` in cwd) conflicts with the vaults directory (`~/.go-rag/vaults/`)? → The `--db-path` flag takes precedence; `--vault` is a higher-level abstraction that resolves to `~/.go-rag/vaults/<name>/`.
- Can two daemons run on the same port for different vaults? → No; each vault must have a unique `mcp_addr` or use the default port with only one daemon running.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST support multiple isolated vaults, each with its own Pebble database, config, and indexes.
- **FR-002**: The system MUST provide a `--vault <name>` global flag that selects the active vault for all commands.
- **FR-003**: `go-rag vault create <name>` MUST create a new vault directory with a default config, initialized Pebble, and a success message.
- **FR-004**: `go-rag vault list` MUST list all vaults with their document count, embedding model, storage size, and daemon status.
- **FR-005**: `go-rag vault delete <name>` MUST remove the vault directory (with confirmation).
- **FR-006**: `go-rag vault clear <name>` MUST remove all data but preserve the vault config.
- **FR-007**: Each vault MUST have its own `config.json` supporting independent `embedding_model`, `rerank_model`, `chunk_size`, `mcp_addr`, etc.
- **FR-008**: The `--vault` flag MUST resolve to a vault directory under `~/.go-rag/vaults/<name>/` (or a configurable root).
- **FR-009**: `go-rag --vault <name> start` MUST start a daemon serving only that vault.
- **FR-010**: `go-rag --vault <name> mcp` MUST proxy to that vault's daemon.
- **FR-011**: The default vault (no `--vault` flag) MUST be backward-compatible with the current `--db-path` behaviour.
- **FR-012**: The vault root directory MUST be configurable via `GO_RAG_VAULT_ROOT` environment variable (default: `~/.go-rag/vaults/`).

### Key Entities

- **Vault**: A named directory containing a `config.json` + `data/` (Pebble) + optional `daemon.pid` / `daemon.log` / `mcp.token`. Identified by name (lowercase alphanumeric + hyphens, 1–64 chars).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user can create a vault, add documents, and query it in under 30 seconds.
- **SC-002**: Two vaults on the same machine show zero cross-contamination in query results.
- **SC-003**: `vault list` completes in under 1 second regardless of vault count.
- **SC-004**: Existing go-rag commands (without `--vault`) work unchanged (backward compat).
- **SC-005**: Each vault can use a different embedding model without conflict.

## Assumptions

- v1 has no authentication or RBAC — vaults are local-trusted (anyone with filesystem access can access any vault).
- The vault root is a single directory on the local filesystem (no network/cluster vaults in v1).
- Vault names are filesystem-safe (validated: lowercase alphanumeric + hyphens).
- The `--db-path` flag remains the low-level override; `--vault` is a convenience layer on top.
- Each vault's daemon uses a unique port (user responsibility to avoid conflicts; default `:7878` per vault unless configured otherwise).
