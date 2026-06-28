# Interface Contracts — Docker Packaging & Compose Deployment (spec 033)

**Date**: 2026-06-28

The public surfaces this feature adds/exposes. go-rag is a CLI + multi-transport
daemon, so "contracts" = the new CLI subcommand, the new env-var config surface,
the container image/runtime contract, and the compose service contract. All four
are **additive** — no existing contract changes.

---

## Contract 1 — `go-rag health` subcommand (NEW)

**Purpose**: shell-less Docker `HEALTHCHECK` probe + ad-hoc operator check.

| Element | Value |
|---|---|
| Invocation | `go-rag health` · `go-rag health --addr <host:port>` |
| Default `--addr` | `GO_RAG_MCP_ADDR` env, else `127.0.0.1:7878` |
| Probes | `GET http://<addr>/mcp/health` (always-on, **unauthenticated**) |
| Timeout | 3 s |
| Exit 0 | HTTP 200 |
| Exit 1 | connect-refused / timeout / non-200 (reason on stderr) |
| Auth required | **no** (`/mcp/health` is unauthenticated in `internal/mcp/http.go`) |

**Invariant**: default addr is loopback (`127.0.0.1:7878`), never `0.0.0.0` —
the probe runs **inside** the container's network namespace, which is reachable
regardless of `--bind-external` or the compose port mapping. Reuses
`daemon.HealthURL(addr)` for URL construction (single source of truth for the
`:port` / empty-host normalization).

**Non-goal**: this is a client of the existing health endpoint — it does NOT add a
new server endpoint or require the daemon's auth token.

---

## Contract 2 — `GO_RAG_*` environment variables (NEW config layer)

**Layering**: file JSON (base) → `GO_RAG_*` env (wins when set + non-empty) → CLI
flags (win for the three listener addrs). See `data-model.md` §1.

**Naming rule**: `GO_RAG_` + `UPPER_SNAKE` of the JSON tag (extends the existing
`GO_RAG_VAULT_ROOT` precedent in `internal/vault/registry.go`).

| Env var | Field | Type | Accepted values |
|---|---|---|---|
| `GO_RAG_MCP_ADDR` | `MCPAddr` | string | `host:port` |
| `GO_RAG_MCP_TOKEN` | `MCPToken` | string | any string (secret) |
| `GO_RAG_OLLAMA_URL` | `OllamaURL` | string | URL |
| `GO_RAG_EMBEDDING_MODEL` | `EmbeddingModel` | string | model name |
| `GO_RAG_RERANK_MODEL` | `RerankModel` | string | model name |
| `GO_RAG_ENRICHMENT_MODEL` | `EnrichmentModel` | string | model name |
| `GO_RAG_WATCH_DIRS` | `WatchDirs` | []string | comma-separated; **replaces** file list |
| `GO_RAG_CHUNK_SIZE` | `ChunkSize` | int | integer |
| `GO_RAG_CHUNK_OVERLAP` | `ChunkOverlap` | int | integer |
| `GO_RAG_POLL_INTERVAL_SECS` | `PollIntervalSec` | int | integer (seconds) |
| `GO_RAG_ENRICHMENT_ENABLED` | `EnrichmentEnabled` | bool | `1/0/t/f/true/false` |
| `GO_RAG_CAPTIONING_ENABLED` | `CaptioningEnabled` | bool | `1/0/t/f/true/false` |
| `GO_RAG_METRICS_ENABLED` | `MetricsEnabled` | bool | `1/0/t/f/true/false` |
| `GO_RAG_AUDIT_LOG_ENABLED` | `AuditLogEnabled` | bool | `1/0/t/f/true/false` |
| `GO_RAG_POISONING_ENABLED` | `PoisoningEnabled` | bool | `1/0/t/f/true/false` |

**Invariants**:
- Unset or empty env → file value is kept (never a `0.0.0.0` default — guards
  spec 007).
- Invalid int/bool → file value kept (no error at `Load`; `Validate()` backstops).
- Bools do **not** accept `yes/no/on/off` (Go `strconv.ParseBool` semantics).
- No live reload — restart to apply.
- `go-rag config get` reflects env-overridden (effective) values; `config set`
  writes only the file.

---

## Contract 3 — Container image / runtime (hardened `Dockerfile`)

| Element | Value |
|---|---|
| Registry | `ghcr.io/madeinoz67/go-rag` |
| Tags | `X.Y.Z`, `X.Y`, `latest` (see data-model §3) |
| Architectures | `linux/amd64`, `linux/arm64` |
| Runtime base | `gcr.io/distroless/static-debian12:nonroot` (UID 65532, **no shell**) |
| `ENTRYPOINT` | `["/go-rag"]` |
| `CMD` (default) | `["serve","--db-path","/data","--mcp-addr","0.0.0.0:7878","--rest-addr","0.0.0.0:7879","--grpc-addr","0.0.0.0:7880","--bind-external"]` |
| `EXPOSE` | `7878 7879 7880` (informational) |
| `VOLUME` | `/data` (the vault; single-writer) |
| `USER` | inherited `nonroot` (65532) — **no change** |
| `HEALTHCHECK` | `--interval=10s --timeout=3s --start-period=15s --retries=3 CMD ["/go-rag", "health"]` (**absolute path**; exec-array — HEALTHCHECK does not inherit ENTRYPOINT) |

**Invariants**:
- The default `CMD` runs the **foreground** daemon `serve` (NOT `start`, which
  detaches and would exit PID 1). Operator can override `CMD`
  (e.g. `docker run … version`); ENTRYPOINT keeps `/go-rag` as the binary.
- `--bind-external` + `0.0.0.0` addrs are **mandatory** in the default CMD —
  without them the daemon refuses to bind (spec 007) and the container exits.
- HEALTHCHECK exec-array is the only form that works on distroless (no `/bin/sh`).

---

## Contract 4 — `docker-compose.yml` service

| Element | Value |
|---|---|
| Service | `go-rag` (default profile, always on) |
| Image | `ghcr.io/madeinoz67/go-rag:latest` (`build: .` for local) |
| `command` | `serve --db-path /data --mcp-addr 0.0.0.0:7878 --rest-addr 0.0.0.0:7879 --grpc-addr 0.0.0.0:7880 --bind-external` |
| `ports` (default) | `127.0.0.1:7878:7878`, `127.0.0.1:7879:7879`, `127.0.0.1:7880:7880` (host **loopback**; LAN variant commented) |
| `volumes` | `go-rag-data:/data` (vault, RW, single-writer) · `./docs:/ingest:ro` (ingestion source, RO) |
| `healthcheck` | `test: ["CMD","/go-rag","health"]`, `interval:10s`, `timeout:3s`, `retries:3`, `start_period:15s` |
| `restart` | `unless-stopped` |
| `deploy.replicas` | `1` (**never higher**; single-writer) |
| Optional sidecar | `ollama` service under `profiles: ["ollama"]` (off by default); `GO_RAG_OLLAMA_URL: http://ollama:11434` |
| Top-level `volumes` | `go-rag-data`, `ollama-models` |

**Invariants**:
- Host exposure defaults to **loopback**; LAN exposure is opt-in and surfaces the
  no-TLS warning (spec 007 / C3).
- The ingestion mount is **read-only** and **separate** from the vault volume
  (FR-011).
- Ollama sidecar off by default (Principle I — bundled embedder is the
  zero-config default). Activating the profile + setting `GO_RAG_OLLAMA_URL` does
  not alone switch the embedder — also set `GO_RAG_EMBEDDING_MODEL`.
- Single-writer: no second service may mount `go-rag-data` read-write.
