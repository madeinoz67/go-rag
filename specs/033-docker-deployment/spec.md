# Feature Specification: Docker Packaging & Compose Deployment

**Feature Branch**: `033-docker-deployment`

**Created**: 2026-06-28

**Status**: Draft — ready for `/speckit-clarify` or `/speckit-plan`

**Input**: User description: "we need to add Docker build file and docker compose files for deployment and add to the build and release workflow, review how muninndb does it in their source. document how files get exposed to container for ingestion"

> **What exists today.** A minimal multi-stage `Dockerfile` already builds the
> static, CGO-free binary (`golang` builder → `distroless/static-debian12:nonroot`
> runtime) and `make docker` wraps `docker build`. It has **no `EXPOSE`, no
> `VOLUME`, no default `CMD`/flags, and no healthcheck**, so it cannot be deployed
> as-is — a user must hand-compose every flag. `.github/workflows/release.yml`
> cross-compiles five OS/arch archives, bundles the spec-032 model, and ships a
> GitHub Release + Homebrew tap, but produces **no container image and pushes to
> no registry**. This feature turns that skeleton into a deployable,
> registry-distributed, compose-driven deployment story — modelled on the
> MuninnDB reference (`scrypster/muninndb`) and respecting go-rag's
> loopback-by-default bind contract (spec 007) and single-writer Pebble
> constraint.

> **Reference review — MuninnDB (`scrypster/muninndb`).** Its `Dockerfile` is
> multi-stage (`golang` → `debian:bookworm-slim`), declares `VOLUME ["/data"]`,
> `EXPOSE`s every transport port, and sets `ENTRYPOINT ["muninndb-server"]` +
> `CMD ["--daemon","--data","/data"]`. Its `docker-compose.yml` maps each port,
> mounts a named volume at `/data`, drives **all** config through `environment:`
> (incl. the critical `MUNINN_LISTEN_HOST: "0.0.0.0"` — a container must bind
> all-interfaces or port-forwarding is dead), declares a `healthcheck` (curl to
> `/mcp/health`), and ships an optional Ollama sidecar. go-rag reuses the
> *shape* but differs in three load-bearing ways: (1) go-rag is pure-Go/CGo-free
> so it stays on a minimal/distroless base, not debian-slim; (2) go-rag binds
> loopback by default and **refuses** non-loopback without `--bind-external`
> (spec 007), so the container must opt in explicitly rather than via a
> `LISTEN_HOST` env var; (3) go-rag config is **hybrid** (decided, Clarifications
> 2026-06-28 Q1) — file-based `.go-rag/config.json` in the vault as the base
> layer, with `GO_RAG_*` env-var overrides added by this feature so compose
> `environment:` can drive it MuninnDB-style; the base still persists with the
> volume.

## Clarifications

### Session 2026-06-28

- Q: How should containerized go-rag be configured? → A: **Hybrid** — keep
  file-based `.go-rag/config.json` as the base layer; add `GO_RAG_*`
  environment-variable overrides layered on top (env wins for any key it sets),
  so compose `environment:` can drive container config MuninnDB-style without a
  separate `config set` step. Adds env-override support to `internal/config` (a
  Go change). Resolves Assumption A5 and adds FR-017.
- Q: How should the container healthcheck work without a shell/curl in the image?
  → A: **Option A** — add a `go-rag health` subcommand that probes the daemon
  health endpoint and exits 0/non-zero, wired as an exec-array Docker
  `HEALTHCHECK` (no shell). Stays distroless/shell-less; small Go addition.
  Updates FR-006 and resolves C6.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — One-command local deployment (Priority: P1)

A user who already lives in Docker runs a single `docker compose up` and gets a
running go-rag daemon (MCP + REST + gRPC) on their machine, with the vault
persisted to a named volume and reachable from the host. They never hand-write
`docker run` flags.

**Why this priority**: This is the entire reason the feature exists — a
deployment story that matches go-rag's frictionless thesis for Docker users.
Today the image is effectively unrunnable without bespoke flags.

**Independent Test**: `docker compose up -d`, then query the mapped MCP/REST
port and confirm a healthy response; `docker compose down && up`, then confirm
previously-added documents are still queryable (volume persisted).

**Acceptance Scenarios**:

1. **Given** Docker is installed, **When** the user runs `docker compose up -d`
   from the repo (or against the published image), **Then** the daemon reaches a
   healthy state (compose healthcheck passes) and all three transports answer on
   their mapped ports.
2. **Given** a running compose stack, **When** the user stops and recreates the
   container, **Then** the vault contents survive via the named volume and prior
   queries return the same documents.
3. **Given** the default compose with no host files mounted for ingestion,
   **When** the stack starts, **Then** the daemon still serves queries against an
   empty vault without crashing.

---

### User Story 2 — Ingest host files into the container (Priority: P1)

A user has a directory of documents on the host and wants them indexed by the
containerized go-rag. They expose the directory to the container and trigger
ingestion (one-shot or continuous), and the documents become queryable.

**Why this priority**: Ingestion is the core loop — a deployment that cannot
ingest is decorative. The user explicitly asked to document how files get
exposed to the container.

**Independent Test**: Bind-mount a host `./docs` into the container read-only,
trigger a one-shot ingest (or enable watch), then query and confirm the host
documents' content is returned.

**Acceptance Scenarios**:

1. **Given** a host directory of files, **When** it is bind-mounted into the
   container at a documented path and ingestion is triggered, **Then** those
   files are indexed and queryable (content-addressed, deduped — Principle II).
2. **Given** the container is running, **When** the user triggers a one-shot
   ingest against a mounted read-only directory, **Then** new/changed files are
   processed and reported, with no modification to host files.
3. **Given** `scan --watch` configured against a mounted directory, **When** a
   new file appears on the host, **Then** the container detects and ingests it
   without a restart.

---

### User Story 3 — Pull the official image from the registry (Priority: P2)

A user who does not want to build locally `docker pull`s a published
multi-arch image from the project registry and runs it via the provided
compose, on x86 or ARM.

**Why this priority**: Turns the release pipeline into a real distribution
channel for Docker users; matches how MuninnDB ships.

**Independent Test**: On an amd64 and an arm64 host, `docker pull` +
`compose up` yields a working daemon; `docker run --rm <image> version` reports
the release version.

**Acceptance Scenarios**:

1. **Given** a tagged release, **When** the release workflow runs, **Then** a
   multi-arch (amd64 + arm64) image is built, tagged with the version and
   `latest`, and pushed to the project registry.
2. **Given** a user on amd64 or arm64, **When** they pull and run the published
   image, **Then** the correct architecture is selected and the binary reports
   the release version.

---

### User Story 4 — Bring-your-own Ollama (optional) (Priority: P3)

A user who wants Ollama embeddings instead of the bundled default can run Ollama
(as a sidecar container or an external host) and point go-rag at it, with the
compose showing how.

**Why this priority**: The bundled embedder (spec 032) covers the default;
Ollama is the escape hatch (Principle V). MuninnDB ships the same optional
sidecar pattern.

**Independent Test**: Uncomment the Ollama sidecar, point go-rag at it, re-embed,
and confirm embeddings come from Ollama.

**Acceptance Scenarios**:

1. **Given** the optional Ollama sidecar is enabled, **When** go-rag is
   configured to use it, **Then** embeddings are generated by Ollama and the
   bundled model is bypassed.

---

### Edge Cases

- **Loopback bind inside a container**: go-rag refuses non-loopback binds without
  `--bind-external` (spec 007). The image/compose MUST pass all-interfaces
  addresses plus the explicit opt-in, or the daemon exits at boot and the
  healthcheck never goes green. This is documented, never silently worked around.
- **Single-writer Pebble over a shared volume**: two containers mounting the same
  vault volume MUST NOT both open it; the second fails to acquire the lock.
  Compose must not scale replicas against one volume; a one-shot ingest container
  must serialize with the daemon (no concurrent writer).
- **Healthcheck without a shell**: a minimal/distroless runtime has no `curl`/`sh`,
  so a MuninnDB-style `curl` healthcheck cannot run; the image MUST provide a
  healthcheck mechanism that works in a shell-less image.
- **Vault volume vs ingestion source**: the writable vault (named volume) and the
  read-only host ingestion mount are distinct; conflating them risks the daemon
  writing into the user's source tree, or the ingest path being wiped.
- **Air-gapped pull / first-run model fetch**: the bundled embedder (spec 032)
  fetches its model on `init`; an offline/air-gapped container must either
  pre-seed the model into the volume or use the release-bundled model asset, or
  first run blocks.
- **Port collisions**: default ports 7878/7879/7880 may clash with the user's host
  daemon; the compose must let the user remap host ports without changing
  container-internal addresses.
- **No TLS on external exposure**: if the user maps ports beyond loopback, traffic
  is unencrypted (spec 007 warning); the compose default must stay host-loopback,
  with an opt-in LAN exposure that surfaces the warning.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Docker image MUST build from the existing pure-Go, CGO-free
  build (constitution Principle III) and remain a static binary in a minimal
  runtime stage — no CGo, no glibc-only runtime introduced *by this feature's
  build choices*.
- **FR-002**: The image MUST declare the daemon's default runtime so that
  `docker run <image>` (no extra flags) starts the multi-transport daemon against
  a data directory — i.e. the image is runnable out of the box, not a bare
  binary requiring a hand-written command line.
- **FR-003**: The image MUST declare the persisted data location as a Docker
  volume so the vault survives container recreation, and MUST default the database
  path to that location.
- **FR-004**: The image MUST declare the transport ports (MCP/REST/gRPC) it
  listens on, matching the daemon's loopback-default addresses overridden to
  all-interfaces for container use.
- **FR-005**: The image MUST run as a non-root user (the runtime base already
  enforces `nonroot`); this feature MUST NOT regress it.
- **FR-006**: The image MUST support a container healthcheck that Docker/compose
  can evaluate to mark the daemon healthy/unhealthy, **without requiring a shell
  or `curl` in the runtime image** (the runtime stays minimal/distroless). The
  mechanism (decided, Clarifications 2026-06-28 Q2): go-rag gains a `go-rag health`
  subcommand that probes the daemon's health endpoint and exits 0 (healthy) /
  non-zero (unhealthy), wired as an exec-array Docker `HEALTHCHECK` (no shell).
  This is a small Go addition; the runtime base does not change.
- **FR-007**: A `docker-compose.yml` MUST be provided that, with a single
  `docker compose up`, brings up a healthy daemon: it MUST bind the container
  transports to all-interfaces with the explicit `--bind-external` opt-in
  (spec 007), map the three ports to the host, mount a named volume for the
  vault, and declare the healthcheck.
- **FR-008**: The compose MUST map the default host ports to host-loopback, with
  a clearly-commented path to expose them on the LAN (which surfaces go-rag's
  no-TLS external-bind warning).
- **FR-009**: The compose MUST demonstrate an optional Ollama sidecar
  (commented) and how go-rag is pointed at it, without making Ollama a default
  dependency (Principle I; spec 032 default stays).
- **FR-010**: Ingestion of host files MUST be supported by mounting the host
  directory into the container (read-only by default) at a documented path and
  triggering `add` (one-shot) or `scan --watch` (continuous) against it — using
  the existing CLI, no new ingest API.
- **FR-011**: The deployment MUST keep ingestion source and vault distinct: the
  host ingestion mount is read-only and separate from the writable named vault
  volume, so the daemon never writes into the user's source tree.
- **FR-012**: The deployment MUST honour the single-writer constraint:
  documentation and compose MUST make explicit that only one go-rag writer may
  open the vault volume at a time; concurrent writers MUST NOT be silently
  allowed.
- **FR-013**: The release workflow MUST build and publish a multi-arch
  (linux/amd64 + linux/arm64) container image to the project registry, tagged
  with the release version and a moving `latest` tag, on every tagged release.
- **FR-014**: The image build in release MUST be driven by the same `Dockerfile`
  used for local/CI builds (single source of truth for the image), and MUST
  inject the release version so `go-rag version` reports the tag.
- **FR-015**: CI MUST build the image (at least linux/amd64) on PRs/main as a
  smoke gate, so a broken image is caught before release — consistent with the
  existing CI build/vet/test gates.
- **FR-016**: Everything added by this feature MUST be compatible with
  local-first (Principle I): the *running* container has no cloud/runtime
  dependency beyond an optional local Ollama; registry distribution is a
  build/release concern, not a runtime one. The one-time spec-032 model fetch
  remains the only accepted network egress.

- **FR-017**: go-rag MUST accept `GO_RAG_*` environment-variable overrides
  layered on top of the existing file-based config — file config
  (`.go-rag/config.json` via `go-rag config set`) is the base layer; an env var
  wins for any key it sets — so a compose `environment:` block can drive
  container configuration without a separate `config set` step. Existing
  file-based config and `go-rag config set` remain fully functional.
  *(Clarifications 2026-06-28 Q1.)*

### Key Entities *(include if feature involves data)*

- **Container image**: the static go-rag binary packaged in a minimal, non-root,
  shell-less runtime; the single artifact both local and CI/release builds
  produce.
- **Vault volume**: the named Docker volume holding the Pebble DB, WAL,
  `.go-rag/config.json`, and the spec-032 model dir; the unit of persistence and
  the single-writer boundary.
- **Ingestion mount**: a read-only bind mount of a host directory into the
  container at a documented path, supplying files for `add`/`scan --watch`;
  distinct from the vault volume.
- **Compose stack**: the `docker-compose.yml` defining the daemon service (ports,
  volume, healthcheck, `--bind-external` opt-in) plus the optional Ollama
  sidecar.
- **Registry image tags**: per-version and `latest` multi-arch manifests pushed
  by the release workflow.
- **Configuration (hybrid)**: base layer = `.go-rag/config.json` in the vault
  volume; override layer = `GO_RAG_*` env vars (compose `environment:`). Env
  overrides win for keys they set; file config remains authoritative otherwise.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user can go from `git clone` (or image pull) to a healthy
  go-rag daemon with a single `docker compose up -d`, in under 2 minutes, with no
  hand-written `docker run` flags.
- **SC-002**: After `docker compose down` + `up`, all previously-ingested
  documents remain queryable (vault persisted via the named volume).
- **SC-003**: A host directory bind-mounted into the container can be ingested
  (one-shot and watched), and its content is returned by queries — without the
  container modifying the host files.
- **SC-004**: A tagged release produces a multi-arch image in the project
  registry that runs correctly on both amd64 and arm64 and reports the release
  version.
- **SC-005**: The image runtime contains no shell/`curl` bloat yet still reports
  health to Docker (the healthcheck works in a shell-less image).
- **SC-006**: No new runtime dependency is introduced — the container runs the
  same pure-Go binary with the same local-first guarantees as the bare-binary
  deployment (constitution Principles I and III verified intact).

## Constraints

- **C1 (constitution Principle I — Local-First)**: Container packaging and
  registry distribution are *compatible* with local-first because the running
  container has no cloud dependency; this feature MUST NOT add a runtime network
  dependency. (Compatible — no amendment.)
- **C2 (constitution Principle III — Pure Go / CGo-free)**: The image build MUST
  keep `CGO_ENABLED=0`; multi-arch buildx MUST NOT introduce a C toolchain. The
  current distroless static runtime already honours this and is the baseline.
  (Compatible.)
- **C3 (spec 007 — loopback-by-default bind)**: go-rag refuses non-loopback binds
  without `--bind-external`. The image/compose MUST explicitly opt in
  (all-interfaces addresses + `--bind-external`) rather than weakening the
  contract. This is THE non-obvious deployment footgun and MUST be documented
  prominently. (Honoured, not amended.)
- **C4 (single-writer Pebble)**: exactly one writer may open the vault; the
  deployment MUST NOT enable concurrent writers against one volume. (Operational
  constraint, documented.)
- **C5 (PRD §2.2 — out of scope: cloud/hosted/multi-user/auth/TLS)**: This
  feature ships a *local* container deployment and an image distribution channel;
  it does NOT add hosted service, multi-tenant auth, or TLS termination. External
  exposure beyond host-loopback is the user's explicit, warned choice. (In scope;
  no PRD edit.)
- **C6 (shell-less healthcheck)**: RESOLVED (Clarifications 2026-06-28 Q2) — a
  minimal/`nonroot` runtime has no `curl`/`sh`; the healthcheck is a `go-rag
  health` subcommand (binary self-probe of the daemon health endpoint) wired as
  an exec-array `HEALTHCHECK`. No shell, no base-image change.

## Assumptions

- **A1**: Target registry is GitHub Container Registry (`ghcr.io/<owner>/go-rag`),
  matching the existing GitHub-based release pipeline; `GITHUB_TOKEN` is
  sufficient auth. (Docker Hub can be added later; not required for v1 of this
  feature.)
- **A2**: Image architectures are linux/amd64 + linux/arm64 (matches the existing
  release matrix and common homelab / Raspberry-Pi / Apple-Silicon targets).
  Windows containers are out of scope.
- **A3**: The default runtime is the multi-transport daemon (`start`); one-shot
  ingestion is via `docker compose exec`/`run` against the same vault volume, or
  via `scan --watch` for continuous ingest. The image is daemon-first, not a
  job-runner.
- **A4**: Container transports bind `0.0.0.0` + `--bind-external` (required for
  port-forwarding to work), while compose maps them to host **loopback** by
  default (`127.0.0.1:<port>:<port>`), with a commented LAN-exposure variant that
  surfaces the no-TLS warning.
- **A5**: Container config is **hybrid** (decided, Clarifications 2026-06-28 Q1):
  the existing file-based `.go-rag/config.json` (set via `go-rag config set`,
  persists in the vault volume) is the base layer; `GO_RAG_*` environment
  variables override it (compose `environment:` is authoritative for overridden
  keys). This adds env-var-override support to `internal/config` (a Go change).
  The bundled embedder (spec 032) still needs no config by default.
- **A6**: The data path inside the container is a fixed conventional location
  (e.g. `/data`); both the declared volume and the default `--db-path`/`CMD` use
  it.
- **A7**: The CI image build is amd64-only as a smoke (multi-arch is
  release-only) to keep CI fast.
- **A8**: No `[NEEDS CLARIFICATION]` markers — all material ambiguities resolved
  by the informed defaults above. Flag at `/speckit-clarify` if any should be
  revisited (e.g. registry choice, default exposure, healthcheck mechanism).
