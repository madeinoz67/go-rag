# Phase 0 Research — Docker Packaging & Compose Deployment (spec 033)

**Date**: 2026-06-28
**Method**: 5 parallel investigations (workflow `w2i6uail3`), each grounded in
go-rag source reads + 2026-06 web verification of current tool/action versions.
This file is the consolidated, reconciled record. Where the parallel
investigations disagreed, the reconciliation is noted inline (the disagreements
are the highest-value part of this file).

---

## RQ1 — Multi-arch GHCR image publish job (`release.yml`)

**Decision**: Add a `docker-image` job to `.github/workflows/release.yml`, sibling
to `build`/`release`/`tap`, keyed off `needs: build` (NOT `needs: release`, so the
image is not gated on model-bundling/Homebrew). Uses the current (2026-06) action
stack — `docker/setup-qemu-action@v4`, `setup-buildx-action@v4`, `login-action@v4`,
`metadata-action@v6`, `build-push-action@v7` — to build `linux/amd64,linux/arm64`
from the repo `Dockerfile` and push to `ghcr.io/madeinoz67/go-rag` with semver
(`X.Y.Z`, `X.Y`) + moving `latest` tags. Job-scoped `permissions: { contents: read,
packages: write, id-token: write }`. Implicit `GITHUB_TOKEN` (no PAT). GHA cache.

**Rationale**: Pure-Go/CGO-free + distroless static means buildx cross-builds
arm64 via QEMU user-mode emulation with **no C toolchain pulled** (Principle III
intact). `ghcr.io/madeinoz67/go-rag` is already lowercase (GHCR rejects
mixed-case). Registry distribution is build-time only → the *running* container
stays local-first (Principle I). Version pins verified against the GitHub releases
API this session.

**Alternatives rejected**: docker/github-builder reusable workflow (3.7× faster
arm64, but overkill for a tiny static binary); `latest`-only tagging (spec wants
both for reproducible rollbacks); SHA-pinning (later hardening pass); PAT
(GITHUB_TOKEN suffices for first-push package creation).

**Gotchas**: job-level `permissions:` **replaces** (not merges) workflow-level —
must re-declare `contents: read`; `type=semver,pattern={{version}}` strips the
leading `v` (yields `0.4.2`); for a pure tag trigger the prerelease guard must be
`enable=${{ !contains(github.ref_name,'-') }}` (not the release-event form); first
push inherits repo visibility (public repo → public package).

**Sources**: docs.docker.com/build/ci/github-actions/multi-platform ·
github.com/docker/{build-push,metadata,login,setup-buildx,setup-qemu}-action ·
docs.docker.com/build/ci/github-actions/manage-tags-labels.

---

## RQ2 — CI image smoke build (`ci.yml`)

**Decision**: Add a `docker-smoke` job to `.github/workflows/ci.yml`, sibling of
`build` (both `needs: test`), using `setup-buildx-action@v4` +
`build-push-action@v7` with `push: false`, `load: true`, **`provenance: false`**,
`platforms: linux/amd64`, local tag `go-rag:smoke`. No registry push, no
`packages: write`. Optional post-build `docker run --rm go-rag:smoke version`
(~2s) to prove ENTRYPOINT + distroless can exec the binary.

**Rationale**: `load: true` is the 2026 idiom for "build but don't push."
Buildx (not plain `docker build`) so the smoke path matches the release builder.
**`provenance: false` is mandatory** — build-push-action defaults provenance to
`true` since v3.3, attestations require a registry push, and `push:false` therefore
FAILS the build unless provenance is explicitly disabled. This is the #1 CI
smoke-build footgun.

**Alternatives rejected**: plain `docker build` (masks buildx-specific failures);
`load:true` without `provenance:false` (the broken config); tarball +
`docker load` (indirect); local registry service container (unneeded); full
serve+health probe in CI (belongs in integration tests, not the smoke gate).

**Gotchas**: `load:true` only works single-platform (keep amd64-only here —
multi-arch stays in release); `tags:` required even when not pushing; match the
release.yml action versions exactly; do NOT add `packages: write` (no push).
GHA cache may need `actions: write` if cache-to fails — cache is nice-to-have,
not a gate.

**Sources**: github.com/docker/{setup-buildx,build-push}-action (releases, README,
action.yml) · docs.docker.com/build/ci/github-actions/test-before-push.

---

## RQ3 — Shell-less Docker HEALTHCHECK via `go-rag health`

**Decision**: Add a `go-rag health` cobra subcommand (`internal/cli/health.go`)
that HTTP-GETs the always-on, **unauthenticated** MCP health endpoint (reusing
`daemon.HealthURL`), exits 0 on HTTP 200 / non-zero otherwise. Wire the Dockerfile
`HEALTHCHECK` in **exec-array** form. No new server code — `GET /mcp/health`
(→ 200 "ok") already exists in `internal/mcp/http.go` and is always-on (MCP can
never be disabled, unlike REST/gRPC).

**Address resolution**: `--addr` flag > `GO_RAG_MCP_ADDR` env > `127.0.0.1:7878`.
Probes loopback **inside** the container, so it works regardless of
`--bind-external` or the compose port mapping.

**Reconciliation (cadence)**: RQ3 proposed interval=10s/timeout=3s/start-period=
15s; RQ5 proposed 30s/5s/20s. **Adopt RQ3's tighter values** — better for
`depends_on: condition: service_healthy` and a sub-second local endpoint. A cold
vault with a boot-time drift verdict can take 5–10s, so start-period=15s gives 3×
margin; do not lower below 10s or cold start flaps unhealthy.

**Reconciliation (HEALTHCHECK form — bug caught)**: ⚠️ The exec-array form is
mandatory on distroless (no `/bin/sh`; shell-form `CMD go-rag health` is rewritten
to `sh -c` and fails with `executable file not found`). But Docker's `HEALTHCHECK
CMD [...]` runs the command **directly — it does NOT inherit ENTRYPOINT**. So:
- ✅ Dockerfile: `HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 CMD ["/go-rag", "health"]` (**absolute path** — `/go-rag` is at the image root, not on a PATH dir).
- ✅ compose: `test: ["CMD", "/go-rag", "health"]`.
- ❌ RQ3's `CMD ["go-rag","health"]` (PATH-dependent, fragile on distroless) and
  RQ5's Dockerfile `CMD ["health"]` (would exec bare `health`, not found) are both
  incorrect and were corrected here.

**Go sketch** (`internal/cli/health.go`): ~30 lines — `http.Client{Timeout: 3s}`
GET `daemon.HealthURL(addr)`, `os.Exit(1)` on connect-refused / non-200 with a
stderr reason, print `ok` on 200. Flag style matches `internal/cli/start.go`.
Register via `newHealthCmd()` in `root.go`'s `AddCommand(...)`.

**Alternatives rejected**: shell-form HEALTHCHECK (no shell on distroless);
curl/wget healthcheck (no curl/wget on distroless — would force a base-image
switch + CVE surface); exporting `daemon.probeHealth` (returns only bool, swallows
error — healthcheck needs distinct exit codes + stderr); REST `/health` (can be
disabled); gRPC health RPC (heavier than a loopback HTTP GET).

**Gotchas**: `--addr` default reads `GO_RAG_MCP_ADDR` at flag-registration
(cobra startup) time, not probe time — correct for a healthcheck but use explicit
`--addr` for ad-hoc `docker exec` probes; healthcheck interval×retries sets the
unhealthy window (~30s post start-period).

**Sources**: internal/cli/{start,stop,root}.go · internal/daemon/lifecycle.go ·
internal/mcp/http.go · Dockerfile · docs.docker.com/reference/dockerfile/#healthcheck ·
GoogleContainerTools/distroless.

---

## RQ4 — `GO_RAG_*` env-var config overrides layered over JSON (no viper)

**Decision**: Add `config.ApplyEnvOverrides(&c)` **inside `config.Load()` itself**
(the single chokepoint every consumer funnels through: `engine.Open` → `openDB` →
every CLI subcommand, plus `serve.go`/`dashboard.go`/`vault.go`/`config_cli.go`).
Layering rule: file JSON is the base; an env var wins **only when set AND
non-empty** (matches the existing `GO_RAG_VAULT_ROOT` precedent in
`internal/vault/registry.go`). Hand-rolled explicit switch (no reflection, no new
dep — consistent with the file's existing explicit `Get`/`Set` style). Pure
`os`/`strconv`/`strings`.

**Coercions**: string (direct), int (`strconv.Atoi`; invalid → keep file value,
never zero it), bool (`strconv.ParseBool` — accepts 1/0/t/f/true/false, **not**
yes/no/on/off), []string (comma-split, trim, drop empties; **REPLACES** the file
list when set).

**Container-priority env var → field map** (`GO_RAG_` + UPPER_SNAKE of the json tag):

| Env var | Field | Type | json tag |
|---|---|---|---|
| `GO_RAG_MCP_ADDR` | `MCPAddr` | string | `mcp_addr` |
| `GO_RAG_MCP_TOKEN` | `MCPToken` | string | `mcp_token,omitempty` |
| `GO_RAG_OLLAMA_URL` | `OllamaURL` | string | `ollama_url` |
| `GO_RAG_EMBEDDING_MODEL` | `EmbeddingModel` | string | `embedding_model,omitempty` |
| `GO_RAG_RERANK_MODEL` | `RerankModel` | string | `rerank_model,omitempty` |
| `GO_RAG_ENRICHMENT_MODEL` | `EnrichmentModel` | string | `enrichment_model,omitempty` |
| `GO_RAG_WATCH_DIRS` | `WatchDirs` | []string | `watch_dirs` |
| `GO_RAG_CHUNK_SIZE` | `ChunkSize` | int | `chunk_size` |
| `GO_RAG_CHUNK_OVERLAP` | `ChunkOverlap` | int | `chunk_overlap` |
| `GO_RAG_POLL_INTERVAL_SECS` | `PollIntervalSec` | int | `poll_interval_secs` |
| `GO_RAG_ENRICHMENT_ENABLED` | `EnrichmentEnabled` | bool | `enrichment_enabled,omitempty` |
| `GO_RAG_CAPTIONING_ENABLED` | `CaptioningEnabled` | bool | `captioning_enabled,omitempty` |
| `GO_RAG_METRICS_ENABLED` | `MetricsEnabled` | bool | `metrics_enabled,omitempty` |
| `GO_RAG_AUDIT_LOG_ENABLED` | `AuditLogEnabled` | bool | `audit_log_enabled,omitempty` |
| `GO_RAG_POISONING_ENABLED` | `PoisoningEnabled` | bool | `poisoning_enabled,omitempty` |
| `GO_RAG_DB_PATH` | `DBPath` | string | `db_path` (cosmetic post-PreRunE — see gotchas) |

(Full `ApplyEnvOverrides` Go body + the one-line hook at the tail of `Load()` are
in `data-model.md` / carried into tasks.md. Add `"strings"` to config.go imports.)

**Precedence chain (document it)**: file JSON (base) → `GO_RAG_*` env (`Load`) →
CLI `--mcp-addr`/`--rest-addr`/`--grpc-addr` flags (`serve.go`/`start.go`, applied
after `openDB`). So **flag > env > file** for the listener address. The container
typically passes `serve --bind-external` (flag) + `GO_RAG_MCP_ADDR=0.0.0.0:7878`
(env).

**Alternatives rejected**: reflection/tag-driven loader (would be the only
reflection in the file; breaks grep-able style); `envconfig`/`caarlos0/env` (new
dep in a local-first repo that deliberately avoids even viper); hook in
`engine.Open` (misses dashboard/vault/config-get paths → two-tier config);
per-command `PersistentPreRunE` (no single PreRunE covers stdio + daemon);
append-semantics for `WATCH_DIRS` (replace is the 12-factor-correct container
default).

**Gotchas**: env-wins-only-when-non-empty is **critical for spec 007** — an
unguarded `c.MCPAddr = v` would silently bind all-interfaces; `WatchDirs`
REPLACES not appends (document); bools reject `on`/`off` (document ParseBool set);
int/bool parse failures silently keep the file value (downstream `Validate()` is
the backstop — consider a `--verbose` log line); `config get` will show
env-overridden values (correct, but note it); no live reload (restart to apply);
`GO_RAG_DB_PATH` is cosmetic by Load-time (root's PreRunE already resolved the
active vault path).

**Sources**: internal/config/config.go · internal/engine/helpers.go ·
internal/cli/{wire,root,serve,config_cli}.go · internal/vault/registry.go
(`GO_RAG_VAULT_ROOT` precedent) · go.mod (confirmed: no viper/envconfig/godotenv).

---

## RQ5 — `docker-compose.yml` + hardened `Dockerfile`

**Decision**: One `go-rag` service running the **foreground** daemon `serve`
(never `start`), `0.0.0.0` addrs + `--bind-external`, host-loopback port maps by
default, named `go-rag-data` volume at `/data`, exec-form healthcheck
(`go-rag health`), separate read-only `./docs:/ingest:ro` bind mount for one-shot
(`docker compose exec go-rag go-rag add /ingest`) or continuous
(`GO_RAG_WATCH_DIRS=/ingest`) ingest, `restart: unless-stopped`, and an **optional**
Ollama sidecar gated behind `profiles: [ollama]` (off by default). Dockerfile
harden additively: `EXPOSE 7878 7879 7880`, `VOLUME /data`, default `CMD` =
`serve … --bind-external`, exec-array `HEALTHCHECK`. USER `nonroot` inherited from
distroless (do not change).

**Reconciliation (db-path)**: RQ3 wrote `--db-path /data/vaults/default`; RQ5
wrote `--db-path /data`. **Adopt `/data`** — the named volume root IS the vault;
the extra `vaults/default` subdir is unnecessary inside a single-vault container.
Config + Pebble DB + model dir all live under `/data` (the config.json subpath
is whatever `openDB(dbPath)` already derives — implement phase confirms the exact
relative path).

**Three load-bearing go-rag constraints driving every line**:
1. Pure-Go/CGO-free/distroless → no shell, no curl → healthcheck must be the
   binary itself via exec-array.
2. Single-writer Pebble → `serve` (foreground PID 1 owning SIGTERM) is correct,
   `start` (detaches, forks child, parent exits → container reaped) is wrong;
   `deploy.replicas` must never exceed 1; no second RW mounter of the vault.
3. Loopback-by-default (spec 007) → container MUST pass `0.0.0.0` addrs AND
   `--bind-external`; host exposure governed by compose `ports:` (loopback default,
   LAN opt-in with the no-TLS warning).

**Ingestion model** (the "how files get exposed" answer): **read-only** bind mount
of a host dir to a container path (e.g. `./docs:/ingest:ro`), kept **separate**
from the writable vault volume. One-shot: `docker compose exec go-rag go-rag add
/ingest`. Continuous: `GO_RAG_WATCH_DIRS=/ingest` so the daemon's watcher tails
it. Read-only guarantees the daemon never writes into the user's source tree.
Single-writer: never run a second writer container against `/data`.

**Alternatives rejected**: curl/wget/shell-form healthcheck (no shell on
distroless); `start` as container CMD (detaches → PID 1 exits → reaped); a
`go-rag-init` sidecar for the model fetch (unnecessary — `serve` does
`engine.EnsureEmbedder` on boot; `start_period` covers it; a sidecar would
complicate single-writer); ingestion mount inside `/data` (conflates vault with
source); omitting `EXPOSE`/`VOLUME` (both are declarative, zero bytes, aid
bare-`docker run` safety).

**Gotchas**: `start` vs `serve` is the #1 footgun; forgetting `--bind-external`
exits the container at boot with a `ValidateBind` error; `EXPOSE` is informational
only (reachability = compose `ports:`); `VOLUME /data` yields an anonymous volume
on bare `docker run` (compose overrides with the named one); the healthcheck pings
container-loopback (always reachable regardless of `--bind-external`/port mapping);
Ollama `profiles:[ollama]` is off by default — setting `GO_RAG_OLLAMA_URL` alone
doesn't switch the embedder (also set `GO_RAG_EMBEDDING_MODEL`); distroless
`nonroot` is UID 65532 — Pebble must create `/data` on open (don't assume a
pre-chowned volume); keep the builder's `golang:1.26-alpine` in lock-step with
go.mod.

**Sources**: docs.docker.com/compose/profiles · docs.docker.com/reference/dockerfile
(#healthcheck, #expose, #volume) · internal/cli/serve.go ·
internal/daemon/lifecycle.go.

---

## Cross-cutting note — what this feature costs in Go code

Two small, additive Go changes (both flagged in clarify Q1/Q2):
1. `internal/config/config.go`: `ApplyEnvOverrides(*Config)` + one-line hook in
   `Load()` + `"strings"` import. (~50 lines.)
2. `internal/cli/health.go` (new) + one-line registration in `root.go`. (~30 lines.)

Everything else is infrastructure: `Dockerfile` (harden runtime stage),
`docker-compose.yml` (new), `release.yml` (+`docker-image` job), `ci.yml`
(+`docker-smoke` job). No new packages; no constitution-principle impact.
