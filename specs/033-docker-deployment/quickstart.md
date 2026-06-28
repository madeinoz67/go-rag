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

- **Local build** (CI smoke equivalent): `make docker` → tags `go-rag`. Or
  `docker compose build` (compose file has a commented `build: .`).
- **Published image** (release equivalent): `docker pull ghcr.io/madeinoz67/go-rag:latest`
  (compose `image:` uses this by default).

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
2. `docker compose exec go-rag go-rag add /ingest`
   → `Processed: N new, 0 skipped, 0 errors`.
3. Query: `docker compose exec go-rag go-rag query "<term>"` → returns content
   sourced from the mounted host files.
4. Confirm host files are **unchanged** (`git status ./docs` clean) — the mount is
   read-only (FR-011).

**Expected**: host documents become queryable; no writes to the source tree.
**Passes SC-003 (one-shot).**

---

## Scenario C — ingest host files (continuous watch) (US2, FR-010, SC-003)

1. In `docker-compose.yml`, uncomment `GO_RAG_WATCH_DIRS: /ingest` under
   `environment:`. `docker compose up -d`.
2. Drop a new file into host `./docs`.
3. Within `poll_interval` (default 60 s) the daemon detects + ingests it;
   `docker compose exec go-rag go-rag query "<new-term>"` returns it.

**Expected**: new host files are ingested without a container restart.

---

## Scenario D — vault persistence across recreate (US1, FR-003, SC-002)

1. After Scenario B (docs ingested), note a query result.
2. `docker compose down` then `docker compose up -d`.
3. Re-run the same query → identical results.

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

1. `docker compose exec go-rag go-rag config get` → shows file-base values.
2. Add to `environment:` e.g. `GO_RAG_ENRICHMENT_ENABLED: "true"`; `up -d`.
3. `docker compose exec go-rag go-rag config get enrichment_enabled` → `true`
   (env overrode the file default `false`), **without** editing the config file.
4. Negative: set `GO_RAG_ENRICHMENT_ENABLED: "on"` → ignored (ParseBool rejects
   `on`); file value kept. Confirms documented coercion semantics.

**Expected**: env wins only when set + valid + non-empty; file is otherwise
authoritative.

---

## Scenario G — optional Ollama sidecar (US4, FR-009, Principle V)

1. `docker compose --profile ollama up -d`.
2. `docker compose exec go-rag-ollama ollama pull nomic-embed-text`.
3. Set `GO_RAG_EMBEDDING_MODEL: nomic-embed-text` on the `go-rag` service; `up -d`.
4. Re-ingest (`go-rag add /ingest --redact` or `reprocess`); confirm embeddings
   come from Ollama (status/model report), bundled model bypassed.

**Expected**: Ollama is opt-in and off by default; when activated + selected,
embeddings come from it. **Passes US4.**

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
