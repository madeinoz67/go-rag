<!--
=== Sync Impact Report ===
Version change: (unratified template) -> 1.0.0
Modified principles: none prior. All five principles are newly defined in this
  initial ratification, derived from PRD_RAG_Database.md.
  I.   Local-First, Single-Binary        (new - PRD G1, G2, S9.5)
  II.  Content-Addressed Identity         (new - PRD G3, S7.2)
  III. Pure Go - No CGo, No Runtime       (new - PRD S9.4, S9.5)
  IV.  Async-After-ACK Writes             (new - PRD S4.2.1, S10.1)
  V.   Extension by Interface, MCP-First  (new - PRD S4.2.5, S8, G7)
Added sections: Core Principles; Performance & Reliability Standards;
  Development & Quality Workflow; Governance.
Removed sections: none.
Templates requiring updates:
  - .specify/templates/plan-template.md   OK no change (Constitution Check gate reads this file dynamically)
  - .specify/templates/spec-template.md   OK no change (no hardcoded principle references)
  - .specify/templates/tasks-template.md  OK no change (language-agnostic samples)
Follow-up TODOs: none. All placeholders resolved.
Source: PRD_RAG_Database.md (read in full, 2026-06-19).
-->

# go-rag Constitution

## Core Principles

### I. Local-First, Single-Binary

All data and processing MUST remain on the user's machine. go-rag MUST NOT depend
on cloud services, managed databases, accounts, or network egress for any core
operation (ingest, index, query). It MUST ship as exactly ONE statically-linked
binary, built with `CGO_ENABLED=0`, with no runtime dependencies beyond an optional
local Ollama process for embeddings.

**Rationale:** The product thesis (PRD §1) is that a local RAG database MUST be as
frictionless as `git init; git add; git commit`. Only a single, dependency-free
binary delivers that; anything external re-introduces the friction the project
exists to remove.

### II. Content-Addressed Identity

Every document's canonical identity MUST be its SHA-256 hash over content plus a
canonicalized metadata map (PRD §7.2). Ingesting the same file twice MUST be a
no-op. The identity hash and the change-detection hash (`ContentHash`, SHA-256 of
raw bytes) MUST remain distinct, so content can be re-embedded under a different
model without creating duplicate documents.

**Rationale:** Idempotent-by-construction ingestion (PRD G3) eliminates duplicate
work, dedup race conditions, and silent double-counting — the failure modes that
plague path-based or timestamp-based identity.

### III. Pure Go — No CGo, No External Runtime

Every dependency MUST be pure Go. The project MUST NOT use CGo, C libraries, or
system packages. Only permissively-licensed, actively-maintained libraries are
permitted (cobra, pebble, chromem-go, pdfcpu, fsnotify — PRD §9.2). GPL, AGPL,
SSPL, archived, or dormant libraries are prohibited (PRD §9.4).

**Rationale:** Pure Go guarantees `CGO_ENABLED=0` static builds, single-file
cross-compilation to every Go target, and a clean supply chain with no transitive
C dependencies (PRD §9.5).

### IV. Async-After-ACK Writes

Writes MUST validate, commit to Pebble with fsync, and acknowledge in under 10ms
(PRD §10.1). All embedding generation, BM25 indexing, and vector indexing MUST
occur asynchronously on background workers AFTER the acknowledgement. The <10ms
write-ACK budget is non-negotiable and MUST be independent of corpus size or
embedding latency.

**Rationale:** The async-after-ACK model (PRD §4.2.1, proven in MuninnDB) decouples
write latency from indexing work, keeping the CLI responsive regardless of how
large the database grows.

### V. Extension by Interface, MCP-First

New file formats MUST be added by implementing the `FileReader` interface and
self-registering — no core changes required (PRD §8.6). New embedding providers
MUST implement the `Embedder` interface. Every CLI operation MUST ALSO be exposed
as a Model Context Protocol tool, so human and AI-agent consumers are first-class
from day one (PRD G7).

**Rationale:** Interface-based extension keeps the core closed while the format and
provider set stays open. MCP-first exposure lets AI coding agents query go-rag
directly instead of shelling out.

## Performance & Reliability Standards

Non-functional budgets and durability guarantees (PRD §10):

- **Write ACK latency**: < 10ms (Pebble `Sync` commit).
- **Query latency**: < 500ms hybrid (top-5); < 50ms keyword-only (top-5); vector
  search < 100ms (top-60).
- **Cold start**: < 1s to open the database (Pebble open + index load).
- **Binary size**: < 25 MB; **memory**: < 50 MB idle, < 500 MB under load.
- **Durability**: fsync on every write batch; Pebble WAL recovery MUST tolerate
  SIGKILL with no data loss.
- **Concurrency**: single-writer — exactly one go-rag process may open the Pebble
  database at a time. Concurrent reads during writes are eventual-consistent.
- **Pure-Go build gate**: `CGO_ENABLED=0 go build ./...` MUST succeed in CI.

## Development & Quality Workflow

- **Build gate**: `go build ./...`, `go vet ./...`, and `go test ./...` MUST pass
  on every change. The repository is never left red.
- **Lint & vuln**: `golangci-lint` and `govulncheck` run in CI
  (`.github/workflows/ci.yml`).
- **Single binary, single entrypoint**: only `cmd/go-rag/main.go` is a `main`
  package. Internal packages live under `internal/`, mapping 1:1 to PRD subsystems.
- **Storage discipline**: all state lives in ONE Pebble instance, key-space
  partitioned by single-byte prefixes (PRD §6.7). No second database, no sidecar
  files for core data.
- **Commits**: Conventional Commits (`feat:`, `fix:`, `chore:` …) — `cliff.toml`
  generates the changelog.
- **Code navigation**: tokensave-indexed; prefer `tokensave_context` over ad-hoc
  grep for structural questions.
- **Out of scope for v1** (PRD §2.2): cloud, multi-user/auth, LLM inference,
  audio/video, web UI, plugin system, non-Ollama embedding providers.

## Governance

This constitution is the highest-authority governance document for go-rag's
architecture and engineering principles. The product specification is
`PRD_RAG_Database.md` (what to build); `ISA.md` is the project's done-condition;
this constitution is the non-negotiable rules every build MUST respect. On
conflict: the constitution wins on principles and constraints; the PRD wins on
product behavior.

- **Compliance**: every `/speckit-plan` run MUST pass the Constitution Check gate
  before Phase 0 research; every PR MUST state compliance with the five principles,
  or justify a logged violation in the plan's Complexity Tracking table.
- **Amendments**: require a PR, an updated Sync Impact Report, and a semver bump —
  MAJOR for principle removal or incompatible redefinition, MINOR for a new
  principle or material expansion, PATCH for clarifications and wording.
- **Runtime guidance**: see `CLAUDE.md` for day-to-day development conventions.

**Version**: 1.0.0 | **Ratified**: 2026-06-19 | **Last Amended**: 2026-06-19
