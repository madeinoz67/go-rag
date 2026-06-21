# MCP Tools — Parity Reference

**Feature**: 003-rest-grpc-api | **Date**: 2026-06-20

The MCP transport is **already implemented** (`internal/mcp/server.go`) and is the
existing operation surface. REST and gRPC are added as adapters over the same
operations. This document is the parity map: every MCP tool ↔ gRPC RPC ↔ REST
endpoint, all backed by one `internal/engine` method.

Source of truth for the current tool set: `internal/mcp/server.go:toolDefs()`
(11 tools, verified this session).

## Operation parity matrix

| MCP tool | gRPC RPC | REST endpoint | Engine method |
|---|---|---|---|
| `go_rag_query` | `Query` | `POST /v1/query` | `Query` |
| `go_rag_status` | `Status` | `GET /v1/status` | `Status` |
| `go_rag_add` | `Add` | `POST /v1/add` | `Add` |
| `go_rag_scan` | `Scan` | `POST /v1/scan` | `Scan` |
| `go_rag_reprocess` | `Reprocess` | `POST /v1/reprocess` | `Reprocess` |
| `go_rag_migrate` | `Migrate` | `POST /v1/migrate` | `Migrate` |
| `go_rag_files` | `Files` | `GET /v1/files` | `Files` |
| `go_rag_dirs` | `Dirs` | `GET /v1/dirs` | `Dirs` |
| `go_rag_config` | `GetConfig`/`SetConfig` | `GET`/`PUT /v1/config` | `GetConfig`/`SetConfig` |
| `go_rag_vault_list` | `ListVaults` | `GET /v1/vaults` | `ListVaults` |
| `go_rag_init` | — (deferred¹) | — (deferred¹) | (config init) |
| `go_rag_guide` | — (MCP-only²) | — (MCP-only²) | — |

¹ `init` creates the DB *before* one exists, so it cannot run against an open
server. It stays a CLI/stdio-MCP operation in v1; the network server requires an
already-initialized DB (matching `serve.go:openDB` which fails if no DB).
² `guide` is an agent-onboarding artifact specific to MCP's tool model; it has no
RPC/REST equivalent and is intentionally MCP-only.

## Refactor note

Today `internal/mcp/server.go` implements each operation inline (query wiring at
`:152`, add at `:329`, status at `:204`, etc.) and duplicates helpers
(`openDB`, `docOf`, `lookupChunk`, `countPrefix`) that also exist in
`cli/wire.go`. As part of this feature, MCP is refactored to call the
`internal/engine` facade, and those duplicated helpers collapse into the facade.
The 11 tool *names*, *input schemas*, and *text renderings* stay unchanged — only
the implementation behind them moves to the shared engine.

## Parity verification

A table-driven test runs each shared operation (query, status, add, files, dirs,
vault_list, …) through all three adapters and asserts identical **structured**
results (see [../research.md](../research.md) R6, [../data-model.md](../data-model.md)).
