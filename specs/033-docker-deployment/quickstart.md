# Quickstart / Validation — Docker Packaging & Compose Deployment (spec 033)

**Date**: 2026-06-28

Runnable scenarios that prove the feature works end-to-end. Each maps to spec
acceptance scenarios (FR-### / US#). Implementation code lives in `tasks.md` /
the implementation phase; this is the validation guide. Interface details are in
`contracts/interface-contracts.md`; data/path details in `data-model.md`.

## Prerequisites

- Docker Engine + Docker Compose v2 (`docker compose …`).
- A host directory of test documents (e.g. `./docs` with a few `.md`/`.pdf`/`.txt`).
- (Optional, US4) nothing else — the bundled pure-Go embedder (spec 032) needs no
  external service. Ollama is opt-in via profile.

## Build / obtain the image

- **Local build** (default): `docker compose build` — the compose file builds from
  the repo `Dockerfile` (`build: .`). No published image exists until a `v*` tag is
  pushed (release.yml `docker-image` job); then swap to `image: ghcr.io/madeinoz67/go-rag:latest`.
- **Published image** (once a release exists): `docker pull ghcr.io/madeinoz67/go-rag:latest`.

---

## Scenario A — one-command healthy deployment (US1, FR-002/003/007, SC-001)

1. `docker compose up -d`
2. `docker compose ps` → `go-rag` is `Up (healthy)`.
3. `curl -s http://127.0.0.1:7878/mcp/health` → `ok`.
4. (REST parity) `curl -s http://127.0.0.1:7879/health` → 200.

**Expected**: container reaches `healthy` within `start_period` (~15 s); all three
transports answer on host loopback. **Passes SC-001.**

**Failure-mode check**: temporarily drop `--bind-external` from `command:` →
container exits at boot with the spec-007 `refusing to bind non-loopback address`
error; healthcheck never goes green. Confirms the loopback-by-default contract is
honoured, not weakened.

---

## Scenario B — ingest host files (one-shot) (US2, FR-010/011, SC-003)

1. Put docs in `./docs` (the compose `./docs:/ingest:ro` mount).
2. Ingest via the daemon's REST API (single-writer-safe — the daemon owns the
   Pebble lock; do NOT `exec` a second `go-rag add`):
   `curl -s -X POST http://127.0.0.1:7879/v1/add -H 'Content-Type: application/json' -d '{"path":"/ingest"}'`
   → `{"new":N,"skipped":0,...,"errors":0}`.
3. Query: `curl -s -X POST http://127.0.0.1:7879/v1/query -H 'Content-Type: application/json' -d '{"query":"<term>","k":5}'`
   → returns content sourced from the mounted host files.
4. Confirm host files are **unchanged** (`git status ./docs` clean) — the mount is
   read-only (FR-011).

**Expected**: host documents become queryable; no writes to the source tree.
**Passes SC-003 (one-shot).**

---

## Scenario C — auto-watch a directory (US2, FR-010)

> The daemon auto-watches `GO_RAG_WATCH_DIRS` (set to `/ingest` in the compose).
> Drop a file in host `./docs` and it is ingested automatically — no manual call.

1. Drop a new file into host `./docs` (e.g. `echo '# unique token zephyrxyz' > docs/new.md`).
2. Within the poll interval (`GO_RAG_POLL_INTERVAL_SECS`, 15 s in the compose —
   fsnotify is unreliable across Docker Desktop bind mounts, so the poll is what
   catches host-side changes on macOS) the daemon ingests it.
3. Query the new content with NO manual `/v1/add`:
   `curl -s -X POST http://127.0.0.1:7879/v1/query -H 'Content-Type: application/json' -d '{"query":"zephyrxyz","k":5}'`
   → returns the new file.

**Expected**: new host files are indexed by the watcher — no restart, no manual
add. (The initial scan at boot also ingests files already present under `/ingest`.)

---

## Scenario D — vault persistence across recreate (US1, FR-003, SC-002)

1. After Scenario B (docs ingested), note a `POST /v1/query` result.
2. `docker compose down` then `docker compose up -d` (the `go-rag-init` sidecar
   is idempotent; the vault + model persist in `go-rag-data`).
3. Re-run the same `POST /v1/query` → identical results.

**Expected**: the named `go-rag-data` volume survives recreate; no re-ingest
needed. **Passes SC-002.**

---

## Scenario E — healthcheck under cold start (FR-006, SC-005)

1. `docker compose down -v` (wipe) then `docker compose up -d`.
2. `docker inspect --format '{{.State.Health.Status}}' go-rag` polled over ~20 s:
   `starting` → `healthy`.
3. Confirm the image has **no shell/curl**:
   `docker run --rm --entrypoint sh ghcr.io/madeinoz67/go-rag:latest -c true` →
   fails (`executable file not found`) — proving the exec-array `go-rag health`
   probe is what makes the healthcheck work on distroless.

**Expected**: healthcheck passes on a shell-less image. **Passes SC-005.**

---

## Scenario F — env-var config override (FR-017)

1. `curl -s http://127.0.0.1:7879/v1/config` (or `-H 'Authorization: Bearer $TOK'`
   if a token is set) → shows the effective (env-layered) config.
2. Add to `environment:` e.g. `GO_RAG_ENRICHMENT_ENABLED: "true"`; `docker compose up -d`.
3. `GET /v1/config` again → `enrichment_enabled: true` (env overrode the file
   default), **without** editing the config file.
4. Negative: set `GO_RAG_ENRICHMENT_ENABLED: "on"` → ignored (`strconv.ParseBool`
   rejects `on`); file value kept. Confirms documented coercion semantics.

**Expected**: env wins only when set + valid + non-empty; file is otherwise
authoritative.

---

## Scenario G — optional external Ollama (US4, FR-009, Principle V)

> Ollama is opt-in (the bundled pure-Go embedder is the default). Point go-rag at
> an Ollama you already run — e.g. on the host (Docker/Podman Desktop), reachable
> from the container at `host.docker.internal`.

1. In `docker-compose.yml`, uncomment the Ollama block:
   `GO_RAG_OLLAMA_URL: http://host.docker.internal:11434`,
   `GO_RAG_EMBEDDING_PROVIDER: ollama`, `GO_RAG_EMBEDDING_MODEL: nomic-embed-text`
   (the URL alone does NOT switch the embedder — `GO_RAG_EMBEDDING_PROVIDER=ollama`
   is what turns off the bundled model).
2. `docker compose down -v && docker compose up -d` (fresh vault re-embeds under
   Ollama via the watcher's initial scan).
3. `curl -s http://127.0.0.1:7879/v1/status` → `embedding_model: nomic-embed-text`
   + `ollama_url: http://host.docker.internal:11434` (bundled model bypassed).
4. `curl -s -X POST http://127.0.0.1:7879/v1/query -H 'Content-Type: application/json' -d '{"query":"...","k":5}'`
   → hits via Ollama embeddings.

(No host Ollama? Run the bundled sidecar instead: `docker compose --profile ollama
up -d` and set `GO_RAG_OLLAMA_URL: http://ollama:11434`; pull a model with
`docker compose exec go-rag-ollama ollama pull nomic-embed-text`.)

**Expected**: when activated + selected, embeddings come from Ollama. **Passes US4.**

---

## Scenario H — release image artefact (US3, FR-013/014, SC-004)

- Push a `v*` tag → the `docker-image` job in `release.yml` publishes
  `linux/amd64,linux/arm64` to GHCR tagged `X.Y.Z` + `latest`.
- On an arm64 host and an amd64 host: `docker pull ghcr.io/madeinoz67/go-rag:X.Y.Z`
  then `docker run --rm ghcr.io/madeinoz67/go-rag:X.Y.Z version` → reports the tag
  on both architectures.

**Expected**: multi-arch image runs on both; binary reports the release version.
**Passes SC-004.** (CI smoke: the `docker-smoke` job in `ci.yml` builds amd64 +
runs `docker run --rm go-rag:smoke version` on every PR/main push.)

---

## Out of scope for these scenarios

- TLS termination / reverse-proxy in front of the container (C5 — user's choice).
- Multi-container replica scaling (single-writer forbids it; C4).
- Windows containers (A2 — linux/amd64 + linux/arm64 only).
- Cloud/hosted/managed deployment (Principle I — local only).
