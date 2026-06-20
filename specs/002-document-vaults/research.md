# Research: Document Vaults

**Phase**: 0 | **Date**: 2026-06-20 | **Source**: muninndb vault source code analysis

## Decision 1: Physical vs Logical Isolation

- **Decision**: Physical isolation (separate directories + Pebble instances per vault).
- **Rationale**: go-rag is a CLI/local tool. Physical isolation means each vault is a directory — backup = copy, delete = rm, config = config.json. Each vault can use a different embedding model/dimension without complexity. No cross-vault compaction overhead.
- **Alternatives**: muninndb's logical isolation (SipHash prefix on one Pebble). Good for a server with many concurrent vaults (O(1) routing, range-tombstone deletion). But go-rag doesn't need concurrent multi-vault access — each command/daemon targets one vault. Physical is simpler and sufficient.

## Decision 2: Vault Directory Layout

- **Decision**: `~/.go-rag/vaults/<name>/` as the vault root, configurable via `GO_RAG_VAULT_ROOT`.
- **Rationale**: Centralised vault management (all vaults in one place, `vault list` = scan directory). Alternative: per-cwd vaults (scattered `.go-rag` dirs) — harder to list/manage. Centralised is better for a vault-centric model.
- **Backward compat**: `--db-path` remains the low-level override. `--vault` resolves to the vault root + name. Without `--vault`, the current per-cwd `.go-rag` behaviour is preserved.

## Decision 3: Vault Name Validation

- **Decision**: Lowercase alphanumeric + hyphens, 1–64 chars (matching muninndb's `isValidVaultName`).
- **Rationale**: Filesystem-safe, URL-safe (for future RBAC), consistent with container/k8s naming conventions.

## Decision 4: Agent/Vault Binding

- **Decision**: `--vault <name>` flag on all commands + the daemon/MCP proxy. One daemon per vault. MCP clients connect to a specific vault's daemon.
- **Rationale**: Simplest model for v1. muninndb's 1:1 API-key→vault model is the future path (when auth is added). For now, vault selection is by flag/env-var, not by auth token.

## Decision 5: Future RBAC Path

- **Decision**: v1 has no auth. The architecture accommodates future RBAC via per-vault `mcp.token` files + a vault→account/group membership layer.
- **Rationale**: muninndb's auth model (vault→API key→mode) is flat and proven. go-rag can adopt the same when multi-tenant access is needed. Physical isolation makes this simpler: access to a vault = access to its directory + token.

## Conclusion

All design decisions resolved. Physical isolation, centralised vault root, filesystem-safe names, flag-based selection, future RBAC via tokens. Ready for Phase 1.
