# Implementation Plan: Embedding Instruction-Prefix (Asymmetric Query/Document Encoding)

**Branch**: `main` (single-author repo — no feature branch, per project CLAUDE.md) | **Date**: 2026-06-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-embedding-instruction-prefix/spec.md` (audit H07).

## Summary

go-rag's default model `nomic-embed-text` is instruction-tuned and expects
**asymmetric** prefixes (`search_query:` for queries, `search_document:` for
passages), but the system embeds query and documents identically and unprefixed
today — silently degrading retrieval on exactly the model most users run. This
plan adds a pure, config-gated **Prefixer** applied at the two embed boundaries
(documents in the pipeline, query in `engine.Query`) so each text reaches the
model in its trained role; extends the spec-005 `EmbeddingProfile` + mismatch
guard with a **convention** axis so a legacy corpus can never be silently
half-prefixed; and leaves the `Embedder` interface untouched (Principle V). See
[research.md](research.md) for the six design decisions.

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`), `CGO_ENABLED=0`.

**Primary Dependencies**: cobra (CLI), pebble (KV), chromem-go (vectors), grpc-go
(transports). No new dependency added by this feature.

**Storage**: single Pebble instance; the prefix convention rides the **existing
0x04 embedding record** (`storedEmbedding{Model, Vector}` → `+ convention`) — no
new key-space prefix, no schema migration beyond an optional JSON field
(backward-compat: missing field = legacy `""`).

**Testing**: `go test -race -cover ./...`; pure-Prefixer unit tests; eval
mechanism test (role-aware `DeterministicEmbedder`); cross-transport parity
test. Real-model quality gain is a manual quickstart step (SC-001), not CI.

**Target Platform**: local single binary, all Go targets (pure Go, no CGo).

**Project Type**: single-binary CLI + multi-transport daemon (MCP/REST/gRPC).

**Performance Goals**: write-ACK `<10ms` preserved (Principle IV — prefixing is
off the write path); query latency unchanged (a string prepend vs. the network
embed call).

**Constraints**: no `Embedder` interface change (Principle V); no new Pebble
prefix; idempotent prefixing; no duplicate documents on re-embed (Principle II).

**Scale/Scope**: local `<10K` docs; S-effort (audit). Touch-points enumerated in
[research.md §Summary](research.md).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Status | How this plan honors it |
|-----------|--------|-------------------------|
| **I. Local-First, Single-Binary** | ✅ Pass | Prefixing is a local string transform; no network/cloud change; same single binary. |
| **II. Content-Addressed Identity** | ✅ Pass | Prefix affects vectors only, never `Chunk.Content` or the identity hash; re-embed is no-duplicate (FR-007). |
| **III. Pure Go — No CGo** | ✅ Pass | Pure-Go string prepend; no new dependency. |
| **IV. Async-After-ACK Writes** | ✅ Pass | Prefix applied in async pipeline workers + query-embed time, never on the write path; `<10ms` ACK preserved (FR-008). |
| **V. Extension by Interface, MCP-First** | ✅ Pass | `Embedder.Embed` signature unchanged; prefix logic is one pure function at the boundary; cross-transport parity free (shared engine path). |

**Verdict:** No violations. **Complexity Tracking table not required.**

*Re-check after Phase 1 design:* the design (research D1–D6, data-model,
contract) introduces no new Pebble prefix, no interface change, no new
dependency, and routes everything through the existing single shared engine
paths — all five principles still satisfied.

## Project Structure

### Documentation (this feature)

```text
specs/008-embedding-instruction-prefix/
├── plan.md              # this file
├── research.md          # Phase 0 — decisions D1–D6
├── data-model.md        # Phase 1 — entities + 0x04 record extension
├── contracts/
│   └── embed-role-prefix.md   # Phase 1 — role/prefix/convention contract
├── quickstart.md        # Phase 1 — validation scenarios 1–5
└── tasks.md             # Phase 2 (/speckit-tasks — not yet created)
```

### Source Code (repository root)

```text
internal/
├── embed/
│   ├── ollama.go            # UNCHANGED — Embedder interface & Ollama.Embed left as is (Principle V)
│   └── prefix.go            # NEW — pure Prefixer, Role, default convention map (+ prefix_test.go)
├── pipeline/
│   └── workers.go           # EDIT — prepend document prefix before embed; store convention in 0x04 record
├── engine/
│   ├── query.go             # EDIT — wrap em.Embed as query-role EmbedFunc; extend checkEmbeddingMismatch with convention
│   ├── embedding_profile.go # EDIT — add MajorityConvention + ConventionCounts; widen Consistent
│   ├── status.go            # EDIT — surface active mode/resolved prefixes + convention drift
│   └── config.go            # EDIT — knownConfigKeys += embedding_prefix / _query_prefix / _doc_prefix
├── config/
│   └── config.go            # EDIT — Default/Get/Set + 3 new keys (spec-006 rerank-field pattern)
├── eval/
│   └── embedder.go          # EDIT — role-aware DeterministicEmbedder (research D5)
└── index/
    └── retrieval.go         # UNCHANGED — takes EmbedFunc as a value; prefix applied by the caller wrapper

# Docs
README.md                    # EDIT — document the new embedding_prefix config keys
```

**Structure Decision:** Pure **Go internal-package extension** — no new package
beyond a single `internal/embed/prefix.go` for the pure Prefixer (and its test).
Every other change is an edit to an existing file in its PRD-mapped directory
(`internal/embed`, `pipeline`, `engine`, `config`, `eval`). Two files
(`internal/embed/ollama.go`, `internal/index/retrieval.go`) are deliberately
**unchanged** to honor Principle V. This matches the repo's 1:1
directory→subsystem discipline (CLAUDE.md architecture map).
