# Implementation Plan: Multi-Transport Server APIs (REST + gRPC + MCP)

**Branch**: `003-rest-grpc-api` | **Date**: 2026-06-20 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/003-rest-grpc-api/spec.md`

## Summary

Add **REST** and **gRPC** transports to go-rag's existing server, alongside the
already-shipped **MCP** transport, mirroring MuninnDB's "interface layer of
protocol adapters over one unified engine" architecture. The codebase already
has the MuninnDB-style skeleton — a detached daemon (`go-rag start` → `serve`),
PID-file single-instance enforcement, Pebble-LOCK single-writer guard, a local
bearer token, graceful shutdown, health probing, and a working MCP adapter
(`internal/mcp`, 11 tools). The work is three things:

1. **Introduce a shared engine facade** (`internal/engine`) that encapsulates the
   operations currently duplicated between the CLI and MCP handlers (Query, Add,
   Status, Scan, Files, Dirs, Reprocess, Migrate, Config, Init) and returns
   **structured results** instead of pre-formatted strings.
2. **Add a REST adapter** (`internal/rest`, stdlib `net/http`, OpenAPI-described)
   and a **gRPC adapter** (`internal/grpc`, via grpc-go — same stack as
   MuninnDB, pure-Go) that each call the facade. Refactor the existing MCP
   adapter to call it too.
3. **Extend the `serve` daemon** to run all three listeners in one process (one
   Pebble writer), each on its own loopback port with per-service address flags,
   reusing the existing token/PID/lock/health/shutdown machinery.

Cross-transport equivalence (FR-002/003) falls out for free: all three adapters
invoke the same facade, so REST, gRPC, and MCP return identical results.

## Technical Context

**Language/Version**: Go 1.26.4 (verified: `go version go1.26.4 darwin/arm64`).

**Primary Dependencies**:
- Existing (verified in `go.mod`): `cockroachdb/pebble` v1.1.5, `spf13/cobra`
  v1.10.2, `pdfcpu`, `fsnotify`. `google.golang.org/protobuf` v1.33.0 is already
  present as an indirect dep.
- **New (this feature)**:
  - `google.golang.org/grpc` (grpc-go) — canonical gRPC over HTTP/2, Apache-2.0,
    pure-Go. **Chosen to match MuninnDB's stack** (`proto/gen`, gRPC server +
    engine adapter) per user decision (see [research.md](research.md) R1).
    `google.golang.org/protobuf` v1.33.0 already in `go.mod` (indirect → direct);
    grpc-go is the one new runtime dependency.
  - REST uses the **Go stdlib `net/http`** (Go 1.22+ pattern mux) — **zero new
    deps**, consistent with Principle III's minimal-dependency ethos.
  - Protobuf codegen: `buf` CLI (dev-only, not a runtime dep) with
    `protoc-gen-go` + `protoc-gen-go-grpc`, generating `proto/gen/*.pb.go` +
    `*_grpc.pb.go` from `proto/gorag.proto`.
- OpenAPI: hand-authored `openapi.yaml` (REST contract), kept in sync manually
  (no generator dependency in v1).

**Storage**: Pebble KV (unchanged). The server process holds the single Pebble
`LOCK`; all API clients multiplex through it. Verified single-writer guard:
`internal/daemon/process_unix.go:isPebbleLockHeld`.

**Testing**: `go test -race -cover ./...` (Makefile `test`). Adapter tests via
`httptest` (REST), in-process grpc-go client (gRPC), and the existing MCP
dispatch tests. Cross-transport parity tested by a shared table-driven test that
runs the same operation through all three adapters and asserts identical
structured results.

**Target Platform**: Cross-platform single binary, `CGO_ENABLED=0`, Linux/macOS/
Windows (daemon detach already has `process_unix.go` + `process_windows.go`).

**Project Type**: CLI + long-running local daemon (existing); this feature adds
network API adapters to the daemon.

**Performance Goals**: Inherited from the constitution — write ACK <10ms (the
facade reuses `pipeline.Ingest`'s async-after-ACK), query latency unchanged
(facade reuses `index.NewRetrieval`). API overhead target: <2ms per request over
loopback above the underlying operation.

**Constraints**: Loopback-only binding (Principle I); pure-Go, no CGo
(Principle III); async-after-ACK preserved at the API boundary (Principle IV);
MCP stays first-class (Principle V); single Pebble writer (constitution).

**Scale/Scope**: 11 operations across 3 transports (REST + gRPC + MCP), one
shared facade, extended daemon. No cluster, no auth beyond the existing local
token, no web UI.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluated against `.specify/memory/constitution.md` v1.0.0 (all five principles).
**All PASS — no violations to justify** (Complexity Tracking table empty).

| Principle | Verdict | Evidence |
|---|---|---|
| **I. Local-First, Single-Binary** | ✅ PASS | REST/gRPC bind loopback by default (`127.0.0.1:7879` / `:7880`, overridable via `--rest-addr`/`--grpc-addr`). No cloud egress. Still one `CGO_ENABLED=0` binary; grpc-go and net/http are statically linked. |
| **II. Content-Addressed Identity** | ✅ PASS | Facade delegates ingest to the existing `pipeline.Ingest`, which uses SHA-256 identity. Idempotency holds unchanged across the API boundary. |
| **III. Pure-Go, No CGo** | ✅ PASS | grpc-go + protobuf are pure-Go, permissively licensed (Apache-2.0/BSD). REST is stdlib. `CGO_ENABLED=0 go build ./...` must stay green in CI. (License/CGo re-verification: [research.md](research.md).) |
| **IV. Async-After-ACK Writes** | ✅ PASS | Facade's Add reuses `pipeline.Ingest` (ACKs fast, embeds/indexes async). API ingest ACKs independently of embedding latency. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | This *is* extension by interface: a new `Engine` interface implemented once, consumed by three adapters. MCP remains first-class (already shipped) and is refactored onto the same facade — not replaced. |

## Project Structure

### Documentation (this feature)

```text
specs/003-rest-grpc-api/
├── plan.md              # This file
├── research.md          # Phase 0 — toolchain/routing/auth/streaming decisions
├── data-model.md        # Phase 1 — engine facade result types + entities
├── quickstart.md        # Phase 1 — end-to-end validation guide
├── contracts/           # Phase 1 — interface contracts
│   ├── gorag.proto      # gRPC/Connect schema (operation surface)
│   ├── rest-openapi.yaml# REST endpoints + schemas
│   └── mcp-tools.md     # MCP tool inventory (parity reference)
└── tasks.md             # /speckit-tasks output (not created here)
```

### Source Code (repository root)

**Structure Decision**: go-rag uses a **flat `internal/<package>` layout that maps
1:1 to PRD subsystems** (e.g. `internal/mcp`, `internal/pipeline`, `internal/index`).
The new code follows that convention — **flat adapter packages parallel to
`internal/mcp`**, plus one new `internal/engine` facade — rather than MuninnDB's
nested `internal/transport/{rest,grpc}/`. This keeps consistency with the existing
`internal/mcp` placement and the project's directory→PRD rule.

```text
internal/
├── engine/              # NEW — shared operation facade (the "unified engine")
│   ├── engine.go        #   Engine struct (cfg + db), constructors, op methods
│   ├── query.go         #   Query() -> []QueryHit   (wraps index.NewRetrieval)
│   ├── ingest.go        #   Add()/Scan()/Reprocess()/Migrate() (wrap pipeline)
│   ├── status.go        #   Status()/Files()/Dirs()  (replace duplicated scans)
│   ├── config.go        #   GetConfig()/SetConfig()
│   └── types.go         #   Structured results: QueryHit, StatusInfo, IngestSummary...
├── rest/                # NEW — REST adapter (stdlib net/http)
│   ├── server.go        #   handler build, bearer-token guard, routes
│   ├── engine_adapter.go#   request -> engine op, result -> JSON
│   ├── openapi.go       #   serves /openapi.yaml; error -> HTTP status mapping
│   └── types.go         #   REST request/response DTOs
├── grpc/                # NEW — gRPC adapter (grpc-go, MuninnDB-parity)
│   ├── server.go        #   grpc.NewServer, RegisterGoragServer, bearer interceptor
│   └── engine_adapter.go#   pb request -> engine op, result -> pb response
proto/                   # NEW — protobuf schema + generated code
├── gorag.proto          #   service Gorag { Query, Add, Status, ... }
└── gen/                 #   generated *.pb.go + *_grpc.pb.go (buf, committed)
internal/mcp/            # EXISTING — refactored to call internal/engine (dedup)
internal/cli/            # EXISTING — serve.go extended for 3 listeners;
│                        #           start.go passes rest/grpc addrs;
│                        #           query.go/add.go/status.go optionally onto engine
internal/daemon/         # EXISTING — Addrs{MCP,REST,GRPC}; Start/Status extended
```

**Out of scope structurally**: no `internal/transport/` nesting, no MBP package,
no web UI package, no auth/TLS package (token reuse only).

## Complexity Tracking

> No Constitution Check violations — table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |

---

## Phase 0 → [research.md](research.md) (toolchain, routing, auth, streaming resolved)
## Phase 1 → [data-model.md](data-model.md) · [contracts/](contracts/) · [quickstart.md](quickstart.md)
