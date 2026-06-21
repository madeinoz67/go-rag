# Contract: Bind Policy (CLI surface + boot behavior)

**Phase 1 output.** The user-facing contract for H13. This is a CLI/behavior
contract, not a network API contract — go-rag's transport APIs (MCP/REST/gRPC)
are unchanged by this feature.

---

## Default behavior

`go-rag start` (no flags) binds **every enabled transport to loopback**
(`127.0.0.1`). No transport is reachable from another machine. No warning is
printed.

## Flag: `--bind-external` (bool, default false)

Registered on `start` (user-facing) and `serve` (internal/hidden).

- **Absent / false:** any non-loopback bind address on any enabled transport
  causes the daemon to **refuse to start**.
- **Present / true:** non-loopback bind addresses are **permitted** for whichever
  transports the user configured externally, and a prominent exposure warning is
  printed once at boot.

Semantics are global (one flag authorizes all external transports), per spec
FR-004 and Assumptions.

## Address flags (unchanged, restated for the contract)

| Flag | Default | Empty means |
|---|---|---|
| `--mcp-addr`  | `127.0.0.1:7878` | (MCP is always-on; empty not expected) |
| `--rest-addr` | `127.0.0.1:7879` | REST disabled |
| `--grpc-addr` | `127.0.0.1:7880` | gRPC disabled |

A disabled transport (empty addr) is **not** subject to the bind check (FR-008).

## Loopback classification (contract-level)

Loopback = IPv4 `127.0.0.0/8`, IPv6 `::1`, and the hostname `localhost`.
Everything else — wildcard (`0.0.0.0`, `::`, bare `:port`), a LAN IP, a public IP,
or a non-loopback hostname — is **external**. (Full algorithm: `data-model.md`.)

## Rejection error (FR-003)

When an external bind is requested without `--bind-external`, `start`/`serve`
exits non-zero **before opening any listener** with a single error of the form:

```
refusing to bind non-loopback address(es) without --bind-external:
  MCP  0.0.0.0:7878
  gRPC 192.168.1.10:7880
go-rag stays local-only by default. To expose it on purpose, re-run with --bind-external.
```

- Names **every** offending transport + address.
- Zero network listeners created.
- Exit is fast (boot-time, well under the SC-002 1-second budget).

## Exposure warning (FR-005)

When `--bind-external` is set **and** at least one transport is non-loopback, the
daemon prints to stderr at boot (exact text in research.md D6), stating: vault +
transports reachable from other machines; traffic unencrypted (no TLS); access
control is the user's responsibility; authorized via `--bind-external`.

When `--bind-external` is set but **all** configured addresses are loopback, no
warning is printed (nothing is actually exposed).

## Documentation contract (FR-007)

README and `start --help` / `serve --help` MUST state: loopback-by-default; how
to opt in (`--bind-external`); and that external binding is plaintext with no TLS
(at the user's risk). The warning text is the single source of truth for the
wording shown to users at runtime.
