# Implementation Plan: Document Vaults

**Branch**: `002-document-vaults` | **Date**: 2026-06-20 | **Spec**: [spec.md](./spec.md)

## Summary

Add multi-vault support to go-rag: each vault is a physically-isolated directory (`~/.go-rag/vaults/<name>/`) containing its own Pebble database, config, and indexes. A `--vault <name>` global flag selects the active vault. This enables per-collection RAG (cybersecurity vault, personal vault) without cross-contamination. Based on muninndb's vault research — physical isolation chosen over muninn's logical prefix-partitioning because go-rag is a CLI tool where per-vault directories are simpler, support different embedding models naturally, and make backup/deletion trivial.

## Technical Context

**Language/Version**: Go 1.26 (existing).

**Primary Dependencies**: Existing go-rag stack (cobra, pebble, chromem-go, pdfcpu, fsnotify). No new dependencies.

**Storage**: Each vault = a separate Pebble instance at `~/.go-rag/vaults/<name>/data/`. Physical isolation — no shared DB, no key-prefix partitioning. This is the key architectural decision (see research below).

**Testing**: `go test` — table-driven; integration tests verify cross-vault isolation (ingest to vault A, query vault B, verify zero results).

**Architecture decision — physical vs logical isolation**:
- muninndb uses **logical** (SipHash prefix on one Pebble) — good for a server with many concurrent vaults.
- go-rag uses **physical** (separate directories + Pebble instances) — better for a CLI tool where: (a) each vault may use a different embedding model/dimension; (b) backup = copy directory; (c) deletion = rm directory; (d) no cross-vault compaction overhead.

## Constitution Check

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First, Single-Binary | ✅ PASS | Vaults are local directories; one binary serves all vaults |
| II | Content-Addressed Identity | ✅ PASS | Per-vault SHA-256 identity (unchanged within each vault) |
| III | Pure Go — No CGo | ✅ PASS | No new deps; vault management is pure Go filesystem ops |
| IV | Async-After-ACK Writes | ✅ PASS | Per-vault pipeline (unchanged) |
| V | Extension by Interface, MCP-First | ✅ PASS | MCP daemon gains `--vault` flag; per-vault daemons |

No violations.

## Project Structure

### New files
```
internal/cli/vault.go           # vault create/list/delete/clear commands
internal/vault/registry.go      # vault directory management (resolve, list, create, delete)
```

### Modified files
```
internal/cli/root.go            # add --vault global flag + vault resolution
internal/cli/start.go           # --vault awareness (daemon serves vault's db-path)
internal/cli/stop.go            # --vault awareness
internal/cli/dashboard.go       # show active vault in panel
```

### Vault directory layout
```
~/.go-rag/vaults/               # GO_RAG_VAULT_ROOT (configurable)
  default/                       # backward-compat default vault
    config.json
    data/                        # Pebble KV
    daemon.pid
    daemon.log
  cyber-notes/
    config.json
    data/
  personal/
    config.json
    data/
```

## Implementation phases

### Phase 1: Vault registry + `--vault` flag (MVP)
- `internal/vault/registry.go`: resolve vault name → directory path, list vaults, create/delete/clear.
- `internal/cli/root.go`: add `--vault <name>` persistent flag. When set, `dbPath` resolves to `~/.go-rag/vaults/<name>/`. When unset, current `--db-path` behaviour (backward compat).
- `internal/cli/vault.go`: `vault create/list/delete/clear` commands.

### Phase 2: Per-vault daemon + MCP
- `start`/`stop`/`status` gain vault awareness (daemon serves the vault's db-path).
- MCP proxy (`go-rag mcp --vault X`) connects to the vault's daemon.
- Each vault can have its own `mcp_addr` in its config.

### Phase 3: Dashboard + polish
- Dashboard shows active vault name.
- `vault list` shows doc counts, embedding model, daemon status per vault.
- Backward compat verified: existing scripts without `--vault` work unchanged.

### Phase 4: Clone/export (future)
- `vault clone <src> <dst>` — copy directory + optional re-embed with different model.
- `vault export <name>` — tar archive.

## Complexity Tracking

No constitution violations to justify. (Table intentionally empty.)
