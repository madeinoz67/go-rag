# Implementation Plan: Docker Packaging & Compose Deployment

**Branch**: `033-docker-deployment` | **Date**: 2026-06-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/033-docker-deployment/spec.md`

## Summary

Turn the existing minimal `Dockerfile` + `make docker` skeleton into a deployable,
compose-driven, GHCR-distributed container for go-rag — following the MuninnDB
reference shape, adapted to go-rag's pure-Go/distroless, loopback-by-default
(spec 007), and single-writer-Pebble constraints. Deliverables: hardened
`Dockerfile` (EXPOSE/VOLUME/default `serve` CMD/exec-array HEALTHCHECK); new
`docker-compose.yml` (daemon + optional Ollama profile + read-only ingest mount);
a multi-arch GHCR `docker-image` job in `release.yml`; an amd64 `docker-smoke` job
in `ci.yml`; and two small additive Go changes — `config.ApplyEnvOverrides`
(`GO_RAG_*` env layered over the JSON config, inside `config.Load`) and a
`go-rag health` subcommand (shell-less HEALTHCHECK probe of the existing
`/mcp/health` endpoint). Approach + decisions: `research.md` (RQ1–RQ5, reconciled);
surfaces: `contracts/`; config/path model: `data-model.md`; validation:
`quickstart.md`.

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: Go 1.22+ (`go.mod`); pure Go, `CGO_ENABLED=0` — no CGo ever.

**Primary Dependencies**: spf13/cobra (CLI), cockroachdb/pebble (KV), chromem-go (vector), pdfcpu (PDF), fsnotify (watch). Distribution: Docker Buildx + GHCR. **No new runtime deps** — infra (Dockerfile/compose/workflows) + two small additive Go changes (`internal/config` env-override layer; `internal/cli/health` subcommand).

**Storage**: single Pebble KV instance (prefix-partitioned), persisted in the container at `/data` via a named Docker volume. Single-writer.

**Testing**: `go test -race -cover` (existing CI: vet/lint/vuln/eval). New: `docker-smoke` CI job (amd64 image build); unit tests for `ApplyEnvOverrides` + `health`; manual compose validation per `quickstart.md`.

**Target Platform**: Linux container image — `linux/amd64` + `linux/arm64` (multi-arch manifest); runtime `gcr.io/distroless/static-debian12:nonroot`. Host = any Docker Engine + Compose v2.

**Project Type**: CLI + multi-transport daemon (MCP/REST/gRPC) packaged as a container image + compose deployment.

**Performance Goals**: unchanged from the constitution (<10 ms write ACK, <500 ms hybrid query, <1 s cold start, <25 MB binary, <50 MB idle / <500 MB under load). Container adds: healthy-within-`start_period` (~15 s, covering cold-vault boot + model-bundle init).

**Constraints**: pure-Go/CGo-free (Principle III); local-first — no runtime cloud dep (Principle I); loopback-by-default + `--bind-external` (spec 007); single-writer Pebble; shell-less distroless healthcheck (exec-array only); multi-arch via QEMU user-mode emulation (no C toolchain).

**Scale/Scope**: single-user local deployment; one daemon per vault volume; optional Ollama sidecar. NOT multi-tenant, NOT cloud/hosted, NOT TLS-terminated (PRD §2.2).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Pre-Phase-0: PASS. Post-Phase-1 re-check: PASS — the two small Go additions (`ApplyEnvOverrides`, `go-rag health`) are additive CLI/config surfaces with no principle impact. No Complexity Tracking entries needed.

| Principle | Status | Note |
|---|---|---|
| I — Local-First, Single-Binary | ✅ PASS | Container runs the same local binary; registry distribution is build-time only; running container has no cloud dep beyond optional local Ollama. FR-016. |
| II — Content-Addressed Identity | ✅ PASS | Unchanged; ingestion reuses existing `add`/`scan`; identity still SHA-256. |
| III — Pure Go, No CGo | ✅ PASS | `Dockerfile` keeps `CGO_ENABLED=0`; multi-arch buildx pulls NO C toolchain (QEMU user-mode only); distroless static base. FR-001/C2. |
| IV — Async-After-ACK Writes | ✅ PASS | Container runs `serve` unchanged; <10 ms ACK budget intact. |
| V — Extension by Interface, MCP-First | ✅ PASS | All transports still exposed; `health` subcommand + env overrides are additive surfaces. |
| spec 007 — loopback-by-default | ✅ PASS (honoured) | Container explicitly opts in via `0.0.0.0` addrs + `--bind-external`; compose maps host-loopback by default. C3. |
| Single-writer Pebble | ✅ PASS (documented) | `deploy.replicas: 1`; no second RW vault mounter. C4. |
| Performance standards | ✅ PASS | No regression; image runtime stays minimal; binary <25 MB unchanged. |

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```text
# Files ADDED / MODIFIED by spec 033 (repo root). Minimal + additive — no new packages.
Dockerfile                       # MODIFIED — runtime stage: + EXPOSE, VOLUME /data, default `serve` CMD, exec-array HEALTHCHECK
docker-compose.yml               # NEW      — daemon service + optional `ollama` profile + read-only ingest mount
.github/workflows/release.yml    # MODIFIED — + `docker-image` job (multi-arch GHCR push, needs: build)
.github/workflows/ci.yml         # MODIFIED — + `docker-smoke` job (amd64 image-build gate, needs: test)
internal/config/config.go        # MODIFIED — + ApplyEnvOverrides(); one-line hook in Load(); + "strings" import
internal/cli/health.go           # NEW      — `go-rag health` subcommand (shell-less HEALTHCHECK probe)
internal/cli/health_test.go      # NEW      — unit tests (200 / non-200 / refused / --addr / GO_RAG_MCP_ADDR)
internal/cli/root.go             # MODIFIED — register newHealthCmd() in AddCommand(...)
internal/config/config_test.go   # MODIFIED — + tests for ApplyEnvOverrides (layering, coercions, invalid-keep)
```

**Structure Decision**: Minimal and additive — 2 new files in `internal/cli` + `internal/config` test additions, 1 new top-level `docker-compose.yml`, and edits to `Dockerfile`, the 2 workflows, `internal/config/config.go`, and `internal/cli/root.go`. Honours the 1:1 PRD-subsystem directory map (no new packages; config stays in `internal/config`, CLI in `internal/cli`). Docker/compose/workflow artefacts sit at the repo root alongside the existing `Dockerfile` and `.github/workflows/`.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
