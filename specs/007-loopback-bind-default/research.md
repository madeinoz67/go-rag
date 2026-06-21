# Research — Loopback Bind by Default (H13)

**Phase 0 output.** Resolves every design unknown by grounding in the current
code (`internal/cli/serve.go`, `internal/cli/start.go`, `internal/daemon/lifecycle.go`,
`internal/daemon/pid.go`, `internal/config/config.go`). No `[NEEDS CLARIFICATION]`
remained entering Phase 0; the items below are best-practice / pattern decisions.

---

## D1 — Enforcement point: `serve` boot, not `start` or `config.Validate`

**Decision.** Put the loopback gate at the top of `serve`'s `RunE`
(`internal/cli/serve.go`), after reading the three addr flags and **before**
`openDB`/listener setup.

**Rationale.** `serve` is the only command that opens listeners — MCP and REST via
`http.Server.ListenAndServe` (`serve.go:112,115`), gRPC via `net.Listen`+`Serve`
(`serve.go:69,119`). Every binding path converges here:

- direct `go-rag serve …` (the path the audit names),
- `go-rag start …` → `daemon.Start` spawns `serve` with the addr flags
  (`lifecycle.go:30-37`),
- any future config-sourced bind.

Validating in `daemon.Start` would miss direct `serve`; validating in
`config.Validate` would reject a legitimate `go-rag config set mcp_addr
0.0.0.0:7878` at *persistence* time, before the user can express `--bind-external`
at *run* time. `serve` is necessary and sufficient.

**Alternatives considered.**
- *Gate in `daemon.Start` only:* rejected — misses the direct-`serve` path, which
  is exactly the audit's vector.
- *Gate in `config.Validate`:* rejected — wrong lifecycle (persist vs. run) and
  can't see the runtime opt-in flag.
- *Gate in each transport constructor:* rejected — triplicates the check and
  diverges from "one chokepoint" hygiene.

---

## D2 — Loopback classification algorithm (stdlib `net` only)

**Decision.** `IsLoopbackBind(addr string) bool` in `internal/daemon/bind.go`:

1. `host, _, err := net.SplitHostPort(addr)` — on error, treat as **external**
   (fail-safe; an unparseable bind should never be silently treated as safe).
2. `host == ""` (bare `:7878`) → **external** (all interfaces).
3. `host == "0.0.0.0"` or `host == "::"` → **external** (wildcard).
4. `host == "localhost"` → **loopback** (special-case; avoids a DNS round-trip for
   the canonical name).
5. `ip := net.ParseIP(host); ip != nil` → `ip.IsLoopback()` (covers all of
   `127.0.0.0/8` and `::1`).
6. otherwise (a hostname) → resolve: `net.LookupIP(host)`; **loopback** iff any
   returned IP `IsLoopback()`; on resolve error → **external** (fail-safe).

**Rationale.** Uses only stdlib already in the dependency graph (`net`, imported by
`config.go:8`). `IP.IsLoopback()` is the canonical check and handles the whole
loopback family, not just `127.0.0.1`. Fail-safe on every uncertain branch
(wildcard, unparseable, unresolvable) keeps the contract "external unless
positively loopback."

**Alternatives considered.**
- *Check only `127.0.0.1` literally:* rejected — misses `127.x.x.x`, `::1`, and
  `localhost`.
- *Resolve every host including `localhost`:* rejected — adds a DNS dependency and
  latency for the common case; `localhost` special-cased instead.

---

## D3 — Opt-in: one global `--bind-external`, on `start` and `serve`

**Decision.** Add `--bind-external` (bool, default false) to both `start`
(user-facing) and `serve` (internal, `Hidden`). `start` reads it, sets
`addrs.BindExternal`, and `daemon.Start` appends `--bind-external` to the spawned
`serve` arg list when set. `serve` reads its own flag and passes
`allowExternal` into `ValidateBind`.

**Rationale.** The spec (FR-004) and its Assumptions commit to a single global
opt-in rather than per-transport toggles — simpler UX, matches the threat model
("am I exposing this process at all?"). Forwarding through `daemon.Addrs` keeps
`start`→`serve` plumbing explicit and testable.

**Alternatives considered.**
- *Per-transport `--mcp-external`/`--rest-external`/`--grpc-external`:* rejected —
  UX overkill for a single-user local tool; the spec's assumption explicitly rules
  it out.
- *`GO_RAG_BIND_EXTERNAL` env var only:* rejected — env-only is invisible in
  `--help`; a flag is discoverable. (An env var can be added later if desired.)

---

## D4 — `config.Default().MCPAddr` `":7878"` → `"127.0.0.1:7878"`

**Decision.** Change the persisted default in `config.Default()` (`config.go:42`)
from `":7878"` to `"127.0.0.1:7878"`.

**Rationale.** `serve` currently binds from **flags** (whose defaults are already
loopback, `serve.go:138-140`), not from `config.MCPAddr` — so this is latent
safety, not a live fix. But it satisfies FR-001 ("loopback … regardless of
source"), aligns the persisted config with the loopback contract, and closes the
saved-config footgun the audit names if any future path binds from
`config.MCPAddr`. Cost is one string literal; no migration needed (existing
configs with `":7878"` are caught by the `serve` gate at next boot, which is the
desired fail-closed behavior — see D5/quickstart).

**Alternatives considered.**
- *Leave `":7878"` and rely solely on the serve gate:* rejected — leaves a
  contradicting default in the codebase and a stale audit finding; the gate would
  reject it at boot anyway, so fixing the source is strictly cleaner.

---

## D5 — TLS scope: explicitly out of scope; external = plaintext at user's risk

**Decision.** v1 permits external binding as **plaintext** when `--bind-external`
is set, with a prominent warning. No TLS/mTLS/cert work in this spec.

**Rationale.** The constitution and backlog mark TLS out of scope for v1
(backlog line 80 / audit line 289: "TLS … N/A on loopback; flips to **P0** if
anyone binds non-loopback — see H13"). H13 is rated **S**; pulling TLS in would
balloon it to L and conflate two concerns. This spec deliberately *opens* the
external-binding door that a future TLS spec will *lock down* — at which point
non-loopback binding should require TLS. The backlog already tracks that
escalation.

**Alternatives considered.**
- *Hard-error on any non-loopback bind until TLS ships:* rejected — blocks the
  legitimate trusted-home-network use case (project context: home server serving
  other LAN devices) with no v1 workaround; defies FR-004.
- *Ship self-signed TLS in this spec:* rejected — scope blow-up; cert lifecycle
  is its own spec.

---

## D6 — Exposure warning: placement and text

**Decision.** When `allowExternal` is true **and** at least one configured
transport is non-loopback, `serve` prints a one-time, prominent warning to
`stderr` at boot, immediately after the existing `"go-rag daemon serving …"`
line (`serve.go:107`). Shape:

```
go-rag daemon serving MCP 0.0.0.0:7878, REST 0.0.0.0:7879
⚠ go-rag is bound to a non-loopback address. The document vault and all
  transports are reachable from other machines. Traffic is UNENCRYPTED (no TLS).
  Access control is your responsibility. (allowed by --bind-external)
```

**Rationale.** `stderr` matches the existing boot log line and survives
redirection to the daemon log file (`lifecycle.go` wires `cmd.Stderr` → log file).
"One-time at boot" satisfies FR-005 without per-request noise. The text names all
three risks the spec requires (exposed vault, no TLS, user-owned access control)
and ties the opt-in to `--bind-external` so the cause is unmistakable.

**Alternatives considered.**
- *Warn on every query:* rejected — noise, and FR-005 says startup.
- *Interactive y/N prompt:* rejected — the daemon runs detached (no TTY) under
  `start`; a prompt would hang. The explicit flag **is** the consent.
