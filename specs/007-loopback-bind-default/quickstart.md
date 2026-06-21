# Quickstart — Loopback Bind by Default (H13)

**Phase 1 output.** Runnable validation that the feature works end-to-end. This
is a validation/run guide, not an implementation — code lives in `tasks.md` and
the implementation phase.

> **Smoke-test safety (CLAUDE.md).** The default `dbPath` is the global vault
> (`~/.go-rag/vaults/default`). Always point smoke runs at a **tmp DB** and
> **non-default ports** so you never collide with or stop a live daemon.

## Prerequisites

- Built binary: `make build` → `./bin/go-rag`
- A tmp work dir, e.g. `export T=/tmp/go-rag-h13 && mkdir -p $T`

## Scenario 1 — default start is loopback-only (SC-001, FR-001)

```
./bin/go-rag start --db-path $T/db --mcp-addr 127.0.0.1:17878
# expected: "go-rag started (pid N) — 127.0.0.1:17878"
```

Verify only loopback is bound (macOS/Linux respectively):

```
lsof -nP -iTCP -sTCP:LISTEN | grep 17878     # → 127.0.0.1:17878, never *:17878
# or: ss -ltnp | grep 17878
./bin/go-rag stop --db-path $T/db
```

**Pass:** the listener is on `127.0.0.1`, not `0.0.0.0`/`*`.

## Scenario 2 — external bind without opt-in is rejected (SC-002, FR-003)

```
./bin/go-rag start --db-path $T/db --mcp-addr 0.0.0.0:17878
# expected: non-zero exit, error names "0.0.0.0:17878" and "--bind-external",
#           NO listener created
lsof -nP -iTCP -sTCP:LISTEN | grep 17878 || echo "no listener (correct)"
```

**Pass:** exits < 1s with the actionable error; nothing is listening on 17878.

## Scenario 3 — external bind with opt-in starts + warns (SC-003, FR-004/005)

```
./bin/go-rag start --db-path $T/db --mcp-addr 0.0.0.0:17878 --bind-external
# expected: starts; daemon log shows the exposure warning (vault exposed, no TLS,
#           user-owned access control, "allowed by --bind-external")
cat $T/db/daemon.log | grep -i "non-loopback"   # or the stderr line
./bin/go-rag stop --db-path $T/db
```

Repeat **without** `--bind-external` → must be rejected (Scenario 2). The opt-in
is the sole determinant.

**Pass:** starts on `0.0.0.0:17878`; warning present exactly once; without opt-in
it's rejected.

## Scenario 4 — persisted default is loopback; gate is source-agnostic (FR-001)

> **Architectural note.** `serve` binds from its **flags** (loopback default),
> not from `config.MCPAddr`, so a persisted non-loopback `mcp_addr` is never
> silently bound. US3's guarantees are therefore: (a) the persisted default is
> itself loopback and can't regress to all-interfaces, and (b) the boot gate is
> source-agnostic — any external addr is rejected regardless of how it arrived.

```
# (a) persisted default is loopback — locked by a regression test:
go test ./internal/config/ -run TestDefault_HasExpectedValues -v
#   → asserts Default().MCPAddr == "127.0.0.1:7878" (never ":7878")

# (b) the gate rejects an external addr no matter its source — already proven by
#     Scenario 2 (external flag override → refused). A non-loopback addr can only
#     bind when --bind-external is set, which is independent of config.
```

**Pass:** `config.Default().MCPAddr` is loopback; no path can reintroduce an
all-interfaces default (regression test fails the build otherwise).

## Scenario 5 — opt-in with all-loopback addrs stays silent (edge case)

```
./bin/go-rag start --db-path $T/db --bind-external
# expected: starts normally, NO exposure warning (nothing is actually exposed)
```

**Pass:** `--bind-external` is authorization, not a request to bind externally.

## Classifier unit check (FR-006)

```
go test ./internal/daemon/ -run IsLoopbackBind -v
```

**Pass:** table covers `127.0.0.1`, `127.5.6.7`, `::1`, `localhost` → loopback;
`""`, `0.0.0.0`, `::`, `:7878`, `192.168.1.10`, unresolvable hostname → external.

## Gate / regression

```
make build && make vet && make test      # build/vet/test green
go test ./internal/daemon/ ./internal/cli/ -cover -race
```

No eval-harness run needed — H13 is a bind/boot change with zero retrieval impact
(`make test-eval` is the gate for *retrieval-quality* changes, which this is not).
