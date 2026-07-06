# Implementation Plan: Authentication & Tokens

**Branch**: `045-auth-tokens` *(single-author repo — work on `main`; slug identifies the spec)* | **Date**: 2026-07-06 | **Spec**: [`spec.md`](spec.md)

**Input**: Feature spec [`specs/045-auth-tokens/spec.md`](spec.md); technical design [`docs/design/auth-tokens.md`](../../docs/design/auth-tokens.md).

## Summary

Replace go-rag's single optional static shared secret (`mcp.token` + plain `==`, duplicated across REST/gRPC/MCP) with a two-credential auth system: labelled, scoped, hashed **API keys** for programmatic clients (CLI/MCP/REST/gRPC), and **admin-login → Bearer session** for the UI (session token in `sessionStorage`, **not** a cookie — CSRF-free). Adds a new `internal/auth` package, a `go-rag auth` CLI, three Pebble prefixes (registered via schema migration v3), and a unified validation middleware that all three transports + the future UI delegate to. Prerequisite to the web UI (spec 046).

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).
**Primary Dependencies**: cobra (CLI), cockroachdb/pebble (KV), `golang.org/x/crypto/bcrypt` (password hashing — pure Go, BSD-3-Clause). **New dep**: bcrypt only (pure-Go, permissively licensed → Principle III compliant).
**Storage**: Pebble — 3 new prefixes (`PrefixAuthAPIKey`, `PrefixAuthAdmin`, `PrefixAuthSession`) + migration v3 (`ExpectedVersion` 2 → 3).
**Testing**: `go test -race`; `golangci-lint`; `govulncheck` (the `ci.yml` gates).
**Target Platform**: cross-platform single binary (Linux/macOS/Windows), local-first; auth enforced when network-exposed.
**Project Type**: CLI + daemon (MCP/REST/gRPC) — auth is a cross-cutting internal package.
**Performance Goals**: per-request validation < 1 ms (hash-lookup; bcrypt never on the hot path); login bcrypt ≈ 200–300 ms (one-time per session, acceptable).
**Constraints**: `<10 ms` write-ACK preserved (auth never touches the ingest path); pure Go; single binary; `<25 MB` binary (bcrypt adds negligible size).
**Scale/Scope**: single-operator; 3 new prefixes; 1 new internal package + CLI subcommand + transport refactor; 6 user stories.

## Constitution Check

*GATE: evaluated before Phase 0. Re-checked after Phase 1.*

| Principle / Rule | Status | Handling |
|---|---|---|
| I. Local-First, Single-Binary | ✅ Pass | All auth state in Pebble (local); no cloud egress; pure-Go single binary. |
| II. Content-Addressed Identity | ✅ Pass (spirit) | Credentials keyed by SHA-256 hash of their random secret; not documents, but the same hash-keyed discipline. |
| III. Pure Go — No CGo | ✅ Pass | `golang.org/x/crypto/bcrypt` is pure Go, BSD-3. No CGo, no C libs. |
| IV. Async-After-ACK (`<10 ms`) | ✅ Pass | Request validation is a hash-lookup (`<1 ms`), off the ingest write-ACK path. bcrypt (≈250 ms) runs only at login, which is not a write-ACK operation. |
| V. Extension by Interface, MCP-First | ⚠ Design note | `go-rag auth` operations exposed as MCP tools (admin-gated). Chicken-and-egg resolved by CLI bootstrap creating the first admin (local FS access, not MCP). See research R6. |
| Storage discipline (new prefixes) | ⚠ **Action** | 3 new prefixes ⇒ migration `v3RegisterAuthPrefixes` in `internal/storage/migrate`, `ExpectedVersion` 2 → 3, PRD §6.7 update. Task in plan. |
| Out-of-scope: "multi-user/auth" | ⚠ **Justified** | Spec 045 is **single-operator** auth (one admin + the operator's own API keys), not multi-user/multi-tenant. Consistent with the constitution's intent (go-rag stays single-operator). The bridge/UI network exposure makes hardening necessary. PRD §2.2 to be amended to distinguish single-operator auth (in scope) from multi-user (still out) — same amendment pattern as spec 029/032. |
| Out-of-scope: "web UI" | ℹ Related | The web UI itself is spec 046 (a separate, deliberate scope expansion). Auth (045) enables it; not a violation of *this* spec. |

**Gate result**: **PASS** with two documented actions (migration v3 + PRD §2.2 amendment) and one design note (MCP exposure of auth tools). No unjustified violations → Complexity Tracking table not required.

## Project Structure

### Documentation (this feature)

```text
specs/045-auth-tokens/
├── plan.md              # this file
├── research.md          # Phase 0 — R1–R7 decisions
├── data-model.md        # Phase 1 — APIKey / AdminUser / Session / Principal
├── quickstart.md        # Phase 1 — runnable validation scenarios
├── contracts/
│   └── auth.md          # Phase 1 — CLI / HTTP / MCP / Validate contracts
└── tasks.md             # Phase 2 (/speckit-tasks — not this command)
```

### Source Code (repository root)

```text
internal/auth/                   # net-new package
├── auth.go                      # Principal, Validate(r) entry, prefix dispatch
├── apikey.go                    # APIKey: Create/Validate/List/Revoke (SHA-256)
├── admin.go                     # AdminUser: bcrypt hashing, Create/Verify
├── session.go                   # Session: mint/validate/revoke (opaque store)
├── store.go                     # Pebble-backed Store (storage.DB helpers)
└── bypass.go                    # loopback + empty-store bypass

internal/storage/storage.go      # +3 prefix constants (free bytes, T001)
internal/storage/migrate/
├── migrate.go                   # defaultMigrations += v3; ExpectedVersion 2→3
└── v3_auth_prefixes.go          # idempotent v3 Up (reserve prefixes, fsync version)

internal/cli/auth.go             # `go-rag auth create/list/revoke/bootstrap/session`
internal/cli/init.go             # first-run admin + mcp.token migration hook
internal/rest/server.go          # delete checkBearer → delegate to auth.Validate
internal/grpc/server.go          # delete bearerInterceptor/hasBearer → auth.Validate interceptor
internal/mcp/http.go             # delete checkBearer → auth.Validate
internal/audit/event.go          # +TokenMgmt/Login events (extend AuthFailEvent)
```

**Structure Decision**: Single-project (Option 1) — auth is a cross-cutting internal package that plugs into the existing CLI + daemon + transport structure. No new top-level directories; one new `internal/auth` package + a migration + CLI subcommand + surgical edits to the three transport packages.
