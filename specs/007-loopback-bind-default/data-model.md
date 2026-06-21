# Data Model — Loopback Bind by Default (H13)

**Phase 1 output.**

> **No storage / persistence changes.** H13 is boot-time bind enforcement. It
> adds no Pebble key prefixes, no documents, no embeddings, no migrations. The
> "entities" below are in-memory / CLI-surface constructs. This satisfies
> Constitution Principles II and IV by leaving the durable store untouched.

---

## Entity: `daemon.Addrs` (existing — extended)

The set of transport bind addresses passed from `start` to the spawned `serve`
process, and persisted to the addrs file for `status` reporting.

| Field | Type | Change | Notes |
|---|---|---|---|
| `MCPAddr`  | `string` | unchanged | MCP listen addr; empty ⇒ not started |
| `RESTAddr` | `string` | unchanged | REST listen addr; empty ⇒ disabled |
| `GRPCAddr` | `string` | unchanged | gRPC listen addr; empty ⇒ disabled |
| `BindExternal` | `bool` | **added** | `json:"bind_external,omitempty"` — authorizes non-loopback binding for this run; forwarded as `serve --bind-external` |

**Validation rule (new):** before any listener opens, every non-empty address is
classified by `IsLoopbackBind`. A non-loopback address is rejected unless
`BindExternal` (start path) / `--bind-external` (serve path) is set.

**State transitions:** none — this is a boot-time check, not a lifecycle state.

---

## Entity: bind classification (new, `internal/daemon/bind.go`)

A pure function over an address string, not a persisted entity. Documented here
because it is the core "data" of the feature.

| Input class | `IsLoopbackBind` result |
|---|---|
| unparseable (`net.SplitHostPort` err) | `false` (external, fail-safe) |
| bare port `":7878"` (empty host) | `false` |
| `0.0.0.0` / `::` (wildcard) | `false` |
| `localhost` | `true` |
| any `127.x.x.x` IP | `true` |
| `::1` | `true` |
| other IP (LAN/public) | `false` |
| resolvable hostname → any loopback IP | `true` |
| resolvable hostname → no loopback IP / unresolvable | `false` (fail-safe) |

---

## Entity: `config.Config.MCPAddr` (default value change only)

The persisted default produced by `config.Default()`.

| Field | Before | After |
|---|---|---|
| `MCPAddr` | `":7878"` (all interfaces) | `"127.0.0.1:7878"` (loopback) |

No struct field added/removed; no JSON shape change; no migration. Existing
on-disk configs with `":7878"` are caught by the `serve` boot gate on next start
(fail-closed), which is the intended behavior.

---

## Relationships to other subsystems

- **`internal/cli/serve`** — consumes `ValidateBind`; the sole enforcement site.
- **`internal/cli/start`** + **`internal/daemon`** — produce `Addrs` (now with
  `BindExternal`) and forward it.
- **`internal/config`** — default-value change only; `Validate()` keeps its
  existing host:port parse check (loopback enforcement is a *run-time* concern,
  not a *persist-time* one — see research.md D1).
- **transports (`rest`/`grpc`/`mcp`)** — **unchanged**; they receive addresses
  that have already passed the gate.
