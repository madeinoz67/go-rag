# Data Model — Docker Packaging & Compose Deployment (spec 033)

**Date**: 2026-06-28

This feature is mostly infrastructure (Dockerfile/compose/workflows), so its
"data model" is narrow: (1) one new in-process layer over the existing `Config`
struct, (2) the container filesystem layout (volumes/mounts/paths), and
(3) the image manifest tags. No new persistent entities; no schema migration;
Pebble storage is untouched.

---

## 1. Configuration — hybrid file + env layer (the only code-level data change)

**Entity**: `internal/config.Config` (unchanged struct) gains a runtime-only
override layer applied at load time. The on-disk shape (`.go-rag/config.json`)
is **unchanged** — env overrides are never persisted.

### Layering (precedence, high → low)

```
CLI flag (--mcp-addr / --rest-addr / --grpc-addr, applied in serve.go/start.go)
   ▲ wins for the three listener addresses
GO_RAG_* env  (config.ApplyEnvOverrides, applied inside config.Load)
   ▲ wins for any key it sets (only when set AND non-empty)
.go-rag/config.json  (the file base, loaded by config.Load)
   ▲ the durable base
```

### Override semantics per type

| Go type | Coercion | Failure mode |
|---|---|---|
| `string` | direct assign | unset/empty → keep file value |
| `int` | `strconv.Atoi` | invalid → keep file value (never zero it) |
| `bool` | `strconv.ParseBool` (1/0/t/f/true/false; NOT on/off/yes/no) | invalid → keep file value |
| `[]string` | comma-split + `TrimSpace` + drop empties | **replaces** file list (does not append); all-empty → keep file value |

### Field coverage (container-priority subset; full list in research.md RQ4)

- Strings: `OllamaURL`, `EmbeddingModel`, `RerankModel`, `EnrichmentModel`,
  `MCPAddr`, `MCPToken`, `DBPath`.
- Ints: `ChunkSize`, `ChunkOverlap`, `PollIntervalSec`.
- Bools: `EnrichmentEnabled`, `CaptioningEnabled`, `MetricsEnabled`,
  `AuditLogEnabled`, `PoisoningEnabled`.
- Slices: `WatchDirs`.

### New function

```go
// internal/config/config.go (add)
func ApplyEnvOverrides(c *Config)   // pure os/strconv/strings; no reflection, no deps
```

Called once at the tail of `config.Load(path)`, immediately before `return c, nil`.
Because `Load` is the single chokepoint (`engine.Open` → `openDB` → every
subcommand; plus `serve.go`, `dashboard.go`, `vault.go`, `config_cli.go`), every
consumer — and therefore all three transports over the one `Engine` — sees the
same env-layered config (cross-transport parity, FR-002/003). Add `"strings"` to
the file's import block.

### Validation rule

- An env var is **ignored unless set and non-empty** — this guard is what keeps
  spec 007 (loopback-by-default) intact: an unset `GO_RAG_MCP_ADDR` must leave the
  file's `127.0.0.1:7878` untouched, never fall through to a `0.0.0.0` default.
- Invalid int/bool env values are **silently dropped** (file value kept). The
  downstream `config.Validate()` (called by `engine.Open`/daemon) is the authority
  on whether the final config is well-formed. Optionally emit a `--verbose` log
  line on a dropped override.

### State / lifecycle

- No new state machine. `os.Getenv` is read once at `Load` time → **no live
  reload**; restart the container to apply env changes (12-factor).
- `go-rag config get` reads via `Load`, so it **shows effective (env-layered)
  values** — correct ("what is the running config?") but document it.
- `go-rag config set` writes the **file** and does not round-trip env back —
  correct (env is the runtime layer; file is durable).

---

## 2. Container filesystem layout

| Path (in container) | Kind | Writable | Purpose | Single-writer? |
|---|---|---|---|---|
| `/data` | named volume (`go-rag-data`) | **yes** | Pebble vault: DB + WAL + `.go-rag/config.json` (base config) + spec-032 model dir + PID/addr/token files | **yes — exactly one writer** |
| `/ingest` | read-only bind mount (`./docs:/ingest:ro`) | **no** | host ingestion source for `add` / `scan --watch` | n/a (read-only) |
| `/go-rag` | image layer | no | the static binary | n/a |

**Invariants**:
- The vault (`/data`) and the ingestion source (`/ingest`) are **distinct** mounts
  — the daemon never writes into the user's source tree (FR-011).
- `/data` is the `--db-path` value. `dbPath`/config/PID/addr/token are all derived
  from it by the existing daemon helpers (`AddrsPath`, `ReadPID`, `ReadToken`,
  `openDB`). The exact subpath of `config.json` relative to `dbPath` is whatever
  `openDB` already computes — unchanged by this feature.
- Single-writer (constitution): `deploy.replicas: 1` (never higher); no second
  container may mount `go-rag-data` read-write. Restart/crash recovery is safe
  (the PID lock releases when the owning process dies).

---

## 3. Image manifest (registry-side)

**Entity**: the OCI image manifest list published to GHCR.

| Tag | Meaning | Mutability |
|---|---|---|
| `X.Y.Z` (e.g. `0.4.2`) | exact release (semver, `v` stripped) | immutable |
| `X.Y` (e.g. `0.4`) | minor track | moves on each minor release |
| `latest` | moving convenience tag | moves on each non-prerelease tag |

- **Architectures**: `linux/amd64`, `linux/arm64` (single manifest list).
- **Base**: `gcr.io/distroless/static-debian12:nonroot` (UID 65532; no shell).
- **Provenance/SBOM**: attestation enabled on release (set `false` only if a
  strict runtime chokes on the extra manifest entries).
- **Cache**: GHA cache (`type=gha,mode=max`) — switch to registry cache if the
  image grows past the 10 GB repo cache budget.

---

## 4. `go-rag health` probe result (ephemeral, non-persistent)

The new health subcommand produces only a process exit code + stderr line — no
persisted data:

| Outcome | Exit | stderr |
|---|---|---|
| HTTP 200 from `/mcp/health` | `0` | (stdout: `ok`) |
| connect refused / timeout | `1` | `go-rag health: <url> unreachable: <err>` |
| non-200 response | `1` | `go-rag health: <url> returned HTTP <code>` |

No entity, no state — it is a pure client of the existing `GET /mcp/health`
endpoint.
