---
description: "Task list for spec 033 — Docker Packaging & Compose Deployment"
---

# Tasks: Docker Packaging & Compose Deployment

**Input**: Design documents from `/specs/033-docker-deployment/`
(plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md)

**Tests**: Unit-test tasks are included for the two Go code changes because the
go-rag constitution mandates a green `go test` gate on every change (not because
the spec requested TDD). Docker / compose / workflow tasks validate via the
runnable scenarios in `quickstart.md`.

**Organization**: Grouped by user story. US1+US2 are P1, US3 is P2, US4 is P3.
The two shared Go surfaces (`GO_RAG_*` env overrides, `go-rag health`) and the
hardened image live in the Foundational phase — they block every story.

## Format: `[ID] [P?] [Story] Description — <file path>`

- **[P]**: parallelizable (different file, no dependency on an incomplete task).
- **[Story]**: US1/US2/US3/US4 — present only on user-story-phase tasks.
- Every task names its target file path.

---

## Phase 1: Setup

**Purpose**: Confirm the build baseline before changing anything.

- [x] T001 Verify baseline: `make build`, `make docker`, and `./bin/go-rag version` all pass on the current `Dockerfile`; snapshot the current runtime stage (3 lines) so the Phase 2 diff is clean — `Dockerfile`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared Go surfaces + hardened image that EVERY user story depends on.
**⚠️ CRITICAL**: No user-story work can begin until this phase is green.

Two independent tracks (config / health) can be developed in parallel; the
Dockerfile harden depends on the health subcommand existing (its `HEALTHCHECK`
calls it).

### Config env-override track

- [x] T002 [P] Implement `config.ApplyEnvOverrides(c *Config)` (string direct; int via `strconv.Atoi`; bool via `strconv.ParseBool`; `WatchDirs` comma-split/trim/drop-empties and **replaces**; env wins ONLY when set AND non-empty — guards spec 007) covering the `contracts/interface-contracts.md` table; call it at the tail of `config.Load` just before `return c, nil`; add `"strings"` to the import block — `internal/config/config.go`
- [x] T003 [P] Unit tests: `ApplyEnvOverrides` — override-wins-when-set; unset/empty-keeps-file; invalid int/bool keeps file value (not zeroed); `WatchDirs` replaces not appends; `GO_RAG_MCP_ADDR` unset leaves loopback `127.0.0.1:7878` intact — `internal/config/config_test.go`

### Health-subcommand track (parallel with the config track)

- [x] T004 [P] Implement `go-rag health` cobra subcommand: `http.Client{Timeout:3s}` GET `daemon.HealthURL(addr)`, exit 0 on HTTP 200 (print `ok`), exit 1 + stderr reason on refused/non-200/timeout; `--addr` default = `GO_RAG_MCP_ADDR` env else `127.0.0.1:7878`; flag style matches `internal/cli/start.go`; no auth token (`/mcp/health` is unauthenticated) — `internal/cli/health.go`
- [x] T005 Register `newHealthCmd()` in `rootCmd.AddCommand(...)` alongside `newStartCmd`/`newServeCmd` — `internal/cli/root.go`
- [x] T006 [P] Unit tests: `health` subcommand — 200→exit 0; 500→exit 1; connection-refused→exit 1; `--addr` override; `GO_RAG_MCP_ADDR` default resolution — `internal/cli/health_test.go`

### Image harden (depends on the health subcommand)

- [x] T007 Harden the Dockerfile runtime stage (build stage untouched): `EXPOSE 7878 7879 7880`; `VOLUME /data`; keep `ENTRYPOINT ["/go-rag"]`; add `CMD ["serve","--db-path","/data","--mcp-addr","0.0.0.0:7878","--rest-addr","0.0.0.0:7879","--grpc-addr","0.0.0.0:7880","--bind-external"]`; add exec-array `HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 CMD ["/go-rag","health"]` (absolute path — HEALTHCHECK does NOT inherit ENTRYPOINT); keep inherited `nonroot` USER — `Dockerfile`

**Checkpoint**: foundation ready — `make test` green; `docker build` yields an image whose default `CMD` runs `serve` foreground and whose `HEALTHCHECK` works on the shell-less distroless runtime. User-story work can begin.

---

## Phase 3: User Story 1 — One-command local deployment (Priority: P1) 🎯 MVP

**Goal**: `docker compose up -d` → a healthy go-rag daemon (MCP+REST+gRPC) on host loopback, vault persisted to a named volume, zero hand-written `docker run` flags.

**Independent Test**: `quickstart.md` Scenario A — `docker compose up -d`, then `docker compose ps` shows `Up (healthy)` and `curl 127.0.0.1:7878/mcp/health` → `ok`.

### Implementation for User Story 1

- [x] T008 [US1] Create `docker-compose.yml`: `go-rag` service (`image: ghcr.io/madeinoz67/go-rag:latest` with commented `build: .`); `command: [serve, --db-path, /data, --mcp-addr, 0.0.0.0:7878, --rest-addr, 0.0.0.0:7879, --grpc-addr, 0.0.0.0:7880, --bind-external]`; host-loopback `ports` (`127.0.0.1:7878:7878` etc.) with a commented LAN variant; `volumes: go-rag-data:/data`; `healthcheck: test:["CMD","/go-rag","health"] interval:10s timeout:3s retries:3 start_period:15s`; `restart: unless-stopped`; `deploy.replicas: 1`; single-writer + spec-007 warning comments; top-level `volumes: go-rag-data:` — `docker-compose.yml`
- [ ] T009 [US1] Validate: `docker compose up -d` → `Up (healthy)` within `start_period`; `curl -s 127.0.0.1:7878/mcp/health` → `ok`; `curl -s 127.0.0.1:7879/health` → 200; `docker compose down && docker compose up -d` → prior vault still queryable (named volume persists) — `specs/033-docker-deployment/quickstart.md` (Scenario A + D)

**Checkpoint**: MVP delivered — a one-command healthy local deployment. STOP and validate before proceeding.

---

## Phase 4: User Story 2 — Ingest host files (Priority: P1)

**Goal**: A host directory is exposed read-only to the container and ingested (one-shot or continuous), becoming queryable without the container modifying the source tree.

**Independent Test**: `quickstart.md` Scenario B/C — bind-mount `./docs`, `docker compose exec go-rag go-rag add /ingest`, query returns host content, `git status ./docs` clean.

### Implementation for User Story 2

- [x] T010 [US2] Add the read-only ingestion mount (`./docs:/ingest:ro`) to the `go-rag` service and document both modes in compose comments: one-shot (`docker compose exec go-rag go-rag add /ingest`) and continuous (uncomment `GO_RAG_WATCH_DIRS: /ingest`, layered via the Phase 2 env override) — `docker-compose.yml`
- [ ] T011 [US2] Validate one-shot: add test docs to `./docs`; `docker compose exec go-rag go-rag add /ingest` → `Processed: N new…`; `docker compose exec go-rag go-rag query "<term>"` → returns host content; `git status ./docs` clean (read-only honoured). Validate continuous: set `GO_RAG_WATCH_DIRS: /ingest`, drop a new file, confirm ingested within `poll_interval` — `specs/033-docker-deployment/quickstart.md` (Scenario B + C)

**Checkpoint**: host files flow into the containerized vault; vault and source tree stay distinct.

---

## Phase 5: User Story 3 — Pull the official image from the registry (Priority: P2)

**Goal**: A tagged release publishes a multi-arch (amd64+arm64) image to GHCR (`X.Y.Z` + `latest`); users `docker pull` + `compose up` on either arch.

**Independent Test**: `quickstart.md` Scenario H — on a `v*` tag the `docker-image` job publishes; `docker run --rm ghcr.io/madeinoz67/go-rag:<ver> version` reports the tag on amd64 and arm64.

### Implementation for User Story 3

- [x] T012 [US3] Add a `docker-image` job to `release.yml` (`needs: build`, sibling of `release`/`tap`): `setup-qemu-action@v4`, `setup-buildx-action@v4`, `login-action@v4` to `ghcr.io` via `${{ secrets.GITHUB_TOKEN }}`, `metadata-action@v6` (`images: ghcr.io/madeinoz67/go-rag`, `type=semver {{version}}` + `{{major}}.{{minor}}` + `type=raw,value=latest,enable=${{ !contains(github.ref_name,'-') }}`), `build-push-action@v7` (`platforms: linux/amd64,linux/arm64`, `push: true`, `cache-from/to: type=gha`, `provenance: true`, `sbom: true`); job `permissions: { contents: read, packages: write, id-token: write }` — `.github/workflows/release.yml`
- [x] T013 [US3] Document registry validation (post-tag, manual): push a `v*` tag → confirm the image appears at `ghcr.io/madeinoz67/go-rag:X.Y.Z` + `:latest`; on an amd64 and an arm64 host `docker pull` + `docker run --rm … version` reports the tag — `specs/033-docker-deployment/quickstart.md` (Scenario H)

**Checkpoint**: the release pipeline distributes a real multi-arch container image alongside the existing archives.

---

## Phase 6: User Story 4 — Bring-your-own Ollama (Priority: P3)

**Goal**: An optional Ollama sidecar (off by default) lets users swap the bundled embedder for an Ollama model, with the compose showing how.

**Independent Test**: `quickstart.md` Scenario G — `docker compose --profile ollama up -d`, pull a model, set `GO_RAG_EMBEDDING_MODEL`, re-embed → embeddings from Ollama.

### Implementation for User Story 4

- [x] T014 [US4] Add an optional `ollama` service to `docker-compose.yml` (`image: ollama/ollama:0.9`, `profiles: ["ollama"]`, `127.0.0.1:11434:11434`, `ollama-models` volume, commented nvidia GPU passthrough) and set `GO_RAG_OLLAMA_URL: http://ollama:11434` on `go-rag` with a comment that switching the embedder also requires `GO_RAG_EMBEDDING_MODEL` — `docker-compose.yml`
- [ ] T015 [US4] Validate: `docker compose --profile ollama up -d`; `docker compose exec go-rag-ollama ollama pull nomic-embed-text`; set `GO_RAG_EMBEDDING_MODEL: nomic-embed-text` on `go-rag`; `docker compose exec go-rag go-rag reprocess` (or `add …`); confirm embeddings come from Ollama and the bundled model is bypassed — `specs/033-docker-deployment/quickstart.md` (Scenario G)

**Checkpoint**: Ollama is opt-in and off by default (Principle I preserved); when activated + selected, embeddings come from it.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: CI gate + user docs that span every story.

- [x] T016 [P] Add a `docker-smoke` job to `ci.yml` (`needs: test`, sibling of `build`): `setup-buildx-action@v4`; `build-push-action@v7` with `platforms: linux/amd64`, `push: false`, `load: true`, **`provenance: false`**, `sbom: false`, `tags: go-rag:smoke`, `cache-from/to: type=gha`; then `docker run --rm go-rag:smoke version` — `.github/workflows/ci.yml`
- [x] T017 [P] Add a "Deployment (Docker)" section to the README: `docker compose up -d`, the read-only ingestion mount + one-shot/watch modes, the `GO_RAG_*` env-var contract (link `contracts/interface-contracts.md`), the single-writer rule, the host-loopback default + LAN-exposure no-TLS warning, and the optional `ollama` profile — `README.md`
- [x] T018 Final green gate: `make build && make vet && make test && make lint` all pass; `docker compose up -d` → healthy end-to-end; `docker compose down` (vault preserved) — repo root

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no deps — start immediately.
- **Phase 2 (Foundational)**: depends on Phase 1; **BLOCKS all user stories**.
- **Phases 3–6 (US1–US4)**: each depends on Phase 2. US1 first (MVP); US2 and US4
  extend the compose file US1 created; US3 (release job) depends on the hardened
  image (T007) but is independent of compose.
- **Phase 7 (Polish)**: T016 depends on T007; T017 after the stories land.

### Within Phase 2 (parallel tracks)

- Config track: T002 → T003.
- Health track: T004 → T005, and T004 → T006 (T005/T006 independent of each other).
- **Config track ∥ Health track** (different packages, no shared state).
- T007 (Dockerfile) depends on T004 (the `HEALTHCHECK` invokes `go-rag health`).

### Cross-story file edit order (avoids conflicts)

- `docker-compose.yml` is touched in US1 (T008, create), US2 (T010, +ingest
  mount), US4 (T014, +ollama profile) — sequential, in that order.
- `Dockerfile` (T007) and the two workflow files (T012, T016) are independent.

### Parallel Opportunities

- Phase 2: (T002+T003) ∥ (T004+T006); T005 and T007 follow their deps.
- Phase 7: T016 ∥ T017 (different files).
- US3 (release.yml) can proceed in parallel with US2/US4 (compose) once Phase 2 is done.

---

## Parallel Example: Phase 2 Foundational

```text
# Two independent Go tracks can be developed concurrently:
Track A (config):  T002 internal/config/config.go  →  T003 internal/config/config_test.go
Track B (health):  T004 internal/cli/health.go     →  T005 internal/cli/root.go
                                                   →  T006 internal/cli/health_test.go
# Then the image harden, which needs the health subcommand:
                   T007 Dockerfile
```

---

## Implementation Strategy

### MVP First (Setup + Foundational + US1)

1. Phase 1 (T001) — confirm baseline.
2. Phase 2 (T002–T007) — env overrides, health probe, hardened image.
3. Phase 3 (T008–T009) — compose up → healthy daemon.
4. **STOP and VALIDATE**: `docker compose up -d` → `Up (healthy)`, vault persists.
   This alone delivers the feature's core thesis (one-command local deployment).

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. + US1 → one-command healthy deploy (**MVP**).
3. + US2 → host-file ingestion.
4. + US3 → registry-distributed multi-arch image.
5. + US4 → optional Ollama sidecar.
6. Polish (T016–T018) → CI smoke gate + docs + final green build.

Each story adds value without breaking prior stories; each is independently
validatable via its `quickstart.md` scenario.

---

## Notes

- Tests are included for the Go changes (T003, T006) because the constitution
  mandates a green `go test` gate — not because the spec requested TDD.
- Every Docker/compose/workflow task validates via a named `quickstart.md`
  scenario rather than a unit test.
- Single-author repo: commit each task (or logical group) straight to `main`
  with Conventional Commits (project CLAUDE.md).
- Honour spec 007 throughout: the container always passes `0.0.0.0` addrs +
  `--bind-external`; host exposure stays loopback by default.
