# Implementation Plan: Loopback Bind by Default (H13)

**Branch**: `007-loopback-bind-default` *(uncommitted — no `before_plan` git hook is registered in this repo, so no branch was created; create it manually if desired)* | **Date**: 2026-06-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/007-loopback-bind-default/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Close the accidental-exposure gap from audit H13 (§1.7/§1.8). Today **nothing**
prevents a user from binding any transport to all-interfaces or an external IP,
and the persisted default (`config.Default().MCPAddr = ":7878"`, `config.go:42`)
is itself all-interfaces. The `start`/`serve` flag *defaults* are already
loopback (`127.0.0.1:78xx`), so the default happy-path is safe — but any explicit
override (`start --mcp-addr 0.0.0.0:7878`, `serve --mcp-addr :7878`) is accepted
with **zero validation or warning**.

This plan makes loopback the enforced contract, not just the default:

- **Boot-time gate in `serve`** (the single listener-opener, `serve.go`): before
  any `net.Listen`/`ListenAndServe`, classify each enabled transport's address as
  loopback or external. **Reject** non-loopback binds unless the user passed
  `--bind-external`; emit a single, actionable error naming every offending
  address. This is the one chokepoint — direct `serve`, `start`→`serve`, and any
  future config-sourced bind all flow through it (FR-001/003).
- **`--bind-external` opt-in** on `start` (user-facing) **and** `serve` (internal,
  `Hidden`), forwarded start→serve via a new `daemon.Addrs.BindExternal` field
  (FR-004). One global flag authorizes external binding for whichever transports
  the user configured externally.
- **Prominent exposure warning** at boot when external binding is authorized
  (FR-005) — vault + plaintext transports exposed, no TLS, access control is the
  user's job.
- **Defense-in-depth default fix**: `config.Default().MCPAddr` `":7878"` →
  `"127.0.0.1:7878"` (FR-001 "regardless of source"). `serve` binds from flags
  today, so this is latent safety + consistency — but it closes the saved-config
  footgun the audit names if any path ever binds from config.
- **Docs**: README + command help document loopback-by-default, `--bind-external`,
  and the explicit no-TLS warning (FR-007).

**Technical approach:** new `internal/daemon/bind.go` with
`IsLoopbackBind(addr string) bool` (stdlib `net` only) and
`ValidateBind(addrs Addrs, allowExternal bool) error`. `serve`'s `RunE` calls
`ValidateBind` right after reading the addr flags and before `openDB`/listener
setup; on external+authorized it prints the warning line alongside the existing
`"go-rag daemon serving …"` line (`serve.go:107`). `start` registers
`--bind-external`, sets `addrs.BindExternal`, and `daemon.Start` appends
`--bind-external` to the spawned `serve` args when set. **No new dependencies;
boot-path only; no storage, proto, or transport-interface changes.**

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4); `CGO_ENABLED=0`, pure Go.

**Primary Dependencies**: cobra (CLI flags), stdlib `net` (loopback classification),
stdlib `net/http` + grpc-go (transports). **No new dependencies** — classification
uses `net.ParseIP(...).IsLoopback()` and `net.SplitHostPort`, both already
imported by `internal/config/config.go:8`.

**Storage**: Pebble KV. **No storage changes** — this is boot-time bind
enforcement; no new key prefixes, no writes. `daemon.Addrs` gains one
non-persisted-by-default field (`BindExternal`, `omitempty`).

**Testing**: `go test -race -cover ./...`. New `internal/daemon/bind_test.go`
covers the classifier table; `internal/cli` boot-gate test covers
reject/opt-in/loopback-default; existing `internal/daemon/lifecycle_test.go`
extended to assert `--bind-external` forwarding.

**Target Platform**: Single static binary, all Go targets. Loopback classification
must work identically on Linux/macOS/Windows (pure stdlib `net` — it does).

**Project Type**: single-binary local service + CLI; multi-transport
(MCP `:7878` / REST `:7879` / gRPC `:7880`) over one `engine.Engine`.

**Performance Goals**: **Zero cost on the default (loopback) path** — one
`net.SplitHostPort` + `ParseIP` per enabled transport at boot, then never again.
No per-query impact.

**Constraints**: Pure Go / no CGo (Constitution III); single Pebble writer (N/A —
boot path, no writes); cross-transport parity (Constitution V) preserved — the
gate treats all three transports uniformly; **loopback-only by default**
(Constitution I, this spec's whole point).

**Scale/Scope**: Local, single-user, <10K docs. Boot-time behavior; no scale
concerns. Effort **S** per the backlog.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|---|---|---|
| I. Local-First, Single-Binary | ✅ Pass (**upheld**) | H13 *enforces* the "no external surface" posture — loopback by default, external requires explicit opt-in. No new deps, no network egress, single binary unchanged. |
| II. Content-Addressed Identity | ✅ Pass (N/A) | Boot/bind path only; no identity, hash, or document changes. |
| III. Pure Go — No CGo | ✅ Pass | Uses stdlib `net` (`ParseIP`, `SplitHostPort`, `LookupIP`) already in the dependency graph via `config.go`. No new imports beyond stdlib + existing project packages. |
| IV. Async-After-ACK Writes | ✅ Pass (N/A) | Enforcement runs at boot, before any listener opens and before any write. `<10ms` write-ACK budget untouched. |
| V. Extension by Interface, MCP-First | ✅ Pass | No transport interface changes; `--bind-external` is an additive flag applied uniformly to all transports including MCP. Cross-transport parity preserved. |

**Gate result: PASS — no violations.** Complexity Tracking table not required.
Re-checked after Phase 1 design (research.md D1–D6): the design adds one boot-time
validation call, one boolean field, and one additive CLI flag — still no violation.

## Project Structure

### Documentation (this feature)

```text
specs/007-loopback-bind-default/
├── plan.md              # this file
├── spec.md              # /speckit-specify output
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── bind-policy.md
└── tasks.md             # /speckit-tasks output (NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── config/
│   └── config.go            # Default().MCPAddr ":7878" → "127.0.0.1:7878" (FR-001, defense-in-depth)
├── daemon/
│   ├── pid.go               # Addrs += BindExternal bool `json:"bind_external,omitempty"`
│   ├── lifecycle.go         # Start: append "--bind-external" to serve args when addrs.BindExternal
│   ├── bind.go              # NEW — IsLoopBackBind(addr) bool + ValidateBind(addrs, allowExternal) error
│   └── bind_test.go         # NEW — classifier table: 127.x, ::1, localhost, "", 0.0.0.0, ::, LAN IP, hostname
├── cli/
│   ├── start.go             # += --bind-external flag (user-facing); set addrs.BindExternal
│   ├── serve.go             # += --bind-external flag (internal); ValidateBind before openDB/listeners; exposure warning (FR-005)
│   └── serve_bind_test.go   # NEW — boot gate: external w/o opt-in → error+no listener; w/ opt-in → starts+warns; loopback default → starts silent
└── (transports rest/grpc/mcp — UNCHANGED; they receive already-validated addrs)

README.md                    # document loopback-by-default + --bind-external + no-TLS warning (FR-007)
```

**Structure Decision**: No new packages beyond one file (`internal/daemon/bind.go`)
co-located with the `Addrs` struct and `Start` logic it augments — consistent with
the constitution's 1:1 directory-to-subsystem map and "no new packages unless the
PRD calls for it." The enforcement lives in `serve` (the only listener-opener),
not in `daemon.Start` (which only covers the `start` path) or `config.Validate`
(which would reject intentional external configs at `config set` time, before the
user can express opt-in). This single-chokepoint choice is justified in
research.md D1.

## Complexity Tracking

> **Not applicable.** Constitution Check passes with no violations to justify.
