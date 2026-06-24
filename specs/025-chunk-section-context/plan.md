# Implementation Plan: Per-Chunk Section Context

**Branch**: `025-chunk-section-context` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/025-chunk-section-context/spec.md` (audit finding **H23**, Phase 6 §1.1). Consumes spec 013's chunker output; adds the structural context 013 deliberately left out.

## Summary

Thread the **heading active at each chunk's start position** into per-chunk
metadata during chunking, and surface it as an ordered breadcrumb
(`Operations / Backups / Retention`) on every query hit across all transports.
Today the Markdown reader (`internal/reader/markdown.go`) collects headings into a
flat `[]string` and returns `stripMarkdown(body)`, destroying structure before the
chunker (`internal/chunk`) ever sees it. The fix is surgical and additive:

1. Replace the reader's two divergent heading/strip passes with **one
   code-fence-aware scan** that emits the stripped text **plus** a positional
   `HeadingSpan` table (level, text, offset) — closing FR-009 in the same stroke.
2. Resolve each `Segment`'s breadcrumb in the pipeline (`internal/pipeline`) from
   that table and write it onto `model.Chunk.SectionContext` — a **non-identity
   sidecar** modelled exactly on `Chunk.Poisoning` (spec 019).
3. Surface `SectionContext` on `engine.QueryHit` and project it to REST, gRPC
   (new proto field), MCP, and CLI — one canonical field, identical everywhere
   (FR-004).

The one subtlety — heading offsets live in stripped-text space while chunk
`StartCharIdx` lives in redacted-text space (redaction is variable-length and
runs before chunking) — is solved by making the redactor additively return an
offset-edit map and translating spans through it (identity when redaction is
off, the default). The chunker, write-ACK ordering, chunk/document identity, and
embedded text are all unchanged.

Full design rationale: [research.md](./research.md) (R1–R8). Entity/field detail:
[data-model.md](./data-model.md). Wire contract: [contracts/api.md](./contracts/api.md).
Validation runbook: [quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: cobra (CLI), cockroachdb/pebble (KV), chromem-go
(vectors), grpc-go (gRPC transport), protobuf (schema). All pure-Go, permissively
licensed (Constitution III). **No new dependencies** are introduced by this
feature.

**Storage**: single Pebble instance, key-space partitioned by single-byte prefix
(PRD §6.7). `Chunk` records live under prefix `0x03` (`storage.PrefixChunk`).
**No new prefix, no new index** — section context rides the existing chunk
record's JSON.

**Testing**: `go test -race -cover ./...` (`make test`). Key suites this feature
extends: `internal/reader/markdown_test.go` (code-fence heading exclusion),
`internal/pipeline/pipeline_test.go` (positional attachment, idempotent re-add),
`internal/engine/query_test.go` + `internal/engine/parity_test.go`
(cross-transport parity of `section_context`), and the spec-004 retrieval-eval
harness (no-regression, SC-004).

**Target Platform**: single statically-linked binary, local-first, cross-compiled
to every Go target (Constitution I/III). No platform change.

**Project Type**: single-binary local RAG database + multi-transport daemon
(CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution, unchanged by this feature): write ACK <10ms;
query <500ms hybrid / <50ms keyword; cold start <1s. Section-context resolution
is O(headings+chunks) of pure string work on the ACK path, riding the existing
chunk record — zero added fsync, zero added ACK latency (research R8).

**Constraints**: Local-First (no network/LLM — FR-002), Pure-Go/No-CGo, single
Pebble writer, idempotent ingestion (FR-003), chunking geometry frozen by spec
013 (FR-008), redaction-before-storage is a privacy invariant (must not reorder).

**Scale/Scope**: ~1 new struct field (`Chunk.SectionContext`) + its 4 transport
projections; one unified reader scan replacing two; one additive redactor return
value + an offset-translation helper; one breadcrumb resolver. Touches
`internal/{reader,pipeline,model,engine,rest,grpc,mcp,cli}`, `proto/`. No
storage/index changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All
five PASS — no violations, so the Complexity Tracking table is empty.

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | Section context is derived locally from the reader's already-extracted headings — no LLM, no network, no cloud (FR-002). Single `CGO_ENABLED=0` binary, no new runtime dep. |
| II | Content-Addressed Identity | ✅ PASS | `SectionContext` is a **non-identity sidecar** (like `Poisoning`); it does not enter chunk `cid` (`pipeline.go:252`) or document `GenerateID` (the span key is removed before identity — research R2). Content-hash dedup (`pipeline.go:214`) is untouched → re-add stays a no-op (FR-003). |
| III | Pure Go — No CGo | ✅ PASS | Pure-Go string scanning + offset arithmetic. No C deps, no new modules. |
| IV | Async-After-ACK Writes | ✅ PASS | Resolution is validation-class O(headings+chunks) work on the ACK path at the same site as poisoning scoring (`pipeline.go:283`), riding the existing chunk record's single `Sync` — zero extra fsync, <10ms ACK preserved (research R8). Embedding/indexing stay async. |
| V | Extension by Interface, MCP-First | ✅ PASS | The `FileReader.Read` signature is **unchanged** — headings flow through the existing `map[string]any` metadata, so non-Markdown readers are untouched (section context simply absent, FR-006). Surfaced on MCP like every other hit field (research R6). |

**Post-design re-check (after `data-model.md` / `contracts/api.md`):** the design
introduces no new entity, no new Pebble prefix, no new storage index, no
interface-signature change, no identity change, and no ACK-path reordering. The
five verdicts above are unchanged. **Gate: PASS.**

## Project Structure

### Documentation (this feature)

```text
specs/025-chunk-section-context/
├── spec.md              # Feature spec (/speckit-specify)
├── plan.md              # This file (/speckit-plan)
├── research.md          # Phase 0 — design decisions R1–R8
├── data-model.md        # Phase 1 — Chunk.SectionContext, HeadingSpan, flow
├── quickstart.md        # Phase 1 — validation runbook (SC-001..005)
├── contracts/
│   └── api.md           # Phase 1 — section_context wire contract, 4 transports
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

Concrete layout for this feature — additive edits to existing packages, no new
package:

```text
internal/
├── reader/
│   └── markdown.go          # UNIFIED code-fence-aware scan: stripped text + []HeadingSpan (R1/R4); closes FR-009
│   (reader.go unchanged — FileReader signature frozen, Constitution V)
├── redact/
│   └── redact.go            # Apply gains an additive offset-edit return value (R3); existing callers unchanged
├── pipeline/
│   └── pipeline.go          # extract span key pre-identity (R2); translate offsets (R3); resolveBreadcrumb onto Chunk (R5); site = processFile chunk loop
├── model/
│   └── model.go             # Chunk.SectionContext []string (json:"section_context,omitempty") — non-identity sidecar (R2)
├── engine/
│   ├── types.go             # QueryHit.SectionContext []string (canonical projection, R6)
│   └── query.go             # copy c.SectionContext onto each hit (next to Poisoning at :252)
├── rest/
│   └── types.go             # queryHit.SectionContext []string `json:"section_context,omitempty"`
├── grpc/                    # maps engine.QueryHit → proto (auto via generated types)
├── mcp/
│   └── server.go            # render breadcrumb in text + structured hit (R6)
├── cli/
│   └── query.go             # renderResults: Section line (text) + field (machine formats)
proto/
├── gorag.proto              # QueryHit: repeated string section_context = 9; (R6)
└── gen/                     # regenerated (go generate / make protoc)
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem (per
`CLAUDE.md`). The feature is a cross-cutting additive change along the existing
reader → pipeline → model → engine → transport path — exactly the path
`Poisoning` (spec 019) and `ChunkIndex` (spec 021/023) followed. No new package,
no new `main`, single binary entrypoint (`cmd/go-rag`) untouched.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles. This
table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
