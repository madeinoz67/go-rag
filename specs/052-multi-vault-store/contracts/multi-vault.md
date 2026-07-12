# Contract: Multi-Vault Transport Vault Selection

**Feature**: specs/052-multi-vault-store | **Date**: 2026-07-13

How the vault parameter flows through each transport. Default "default". Fail-closed (unrecognised
vault rejected). The engine resolves vault name → wsPrefix; transports never see wsPrefix.

---

## CLI
`--vault <name>` flag on every command. Default "default". Per-call, not a DB-path selector.
```sh
go-rag add --vault work ~/docs/report.md
go-rag query --vault work "charge deadline" --mode keyword
go-rag query --vault default,work "charge deadline"   # cross-vault (comma-separated)
```

## REST
`?vault=<name>` query param + optional `"vault"` field in JSON body (must agree). VaultAuthMiddleware
resolves before the handler. Admin session bypasses vault locking (UI path).
```
POST /v1/add?vault=work        {"path": "/docs/report.md"}
POST /v1/query?vault=work      {"query": "charge deadline", "mode": "keyword"}
POST /v1/query?vault=default,work  {"query": "..."}   # cross-vault
DELETE /v1/documents/{id}?vault=work
```

## gRPC
`string vault = N;` field on every request message. Unary interceptor validates. Default "default".
Cross-vault: `repeated string vaults = M;` on QueryRequest (empty = single-vault).
```proto
message AddRequest     { string path = 1; string glob = 2; string vault = 3; }
message QueryRequest   { string query = 1; ...; string vault = 15; repeated string vaults = 16; }
```

## MCP
Optional `vault` tool arg on every tool (default "default"). Cross-vault: `vaults` array on query.
```json
{"tool": "go_rag_add", "params": {"path": "/docs/report.md", "vault": "work"}}
{"tool": "go_rag_query", "params": {"query": "...", "vaults": ["default", "work"]}}
```

## UI (4th transport)
Vault picker in the shell header (session-scoped). `X-Go-Rag-Vault` header on /api/* requests.
Cross-vault: multi-select in the vault picker → `X-Go-Rag-Vaults: default,work` header.

## Resolution
All transports produce a validated vault NAME string. The engine's `ResolveVaultPrefix(name)`
maps it to wsPrefix. Storage never sees a vault string.
