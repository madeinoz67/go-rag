# Implementation Plan: Chunk Wikilink Metadata (BL-004)

**Branch**: `036-chunk-wikilink-metadata` *(single-author repo — work commits to `main`; this slug identifies the spec, not a git branch)* | **Date**: 2026-06-30 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/036-chunk-wikilink-metadata/spec.md` (bridge backlog item BL-004).

**Note**: Phase 0 research corrected three load-bearing assumptions in the spec — see [research.md §Spec reconciliation](./research.md). The design below is the verified one; the deltas are flagged for a follow-up `/speckit-clarify` to sync `spec.md`.

## Summary

Expose Obsidian `[[wikilink]]` targets as a per-chunk, non-identity sidecar so the go-rag ↔ MuninnDB bridge can write Hebbian edges without re-parsing markdown. Phase 0 verified the spec's assumptions against the code and **corrected three**:

1. **Plain wikilink targets are not collected today.** `normalizeObsidian` (`internal/reader/markdown.go:84`) parses `[[wikilink]]` via `reObsidianLink` but only substitutes display text — the target is discarded. The only set it returns is `transcludes` (`![[note]]` transclusion targets), which BL-004 explicitly *excludes*. → Collection of `[[wikilink]]` targets must be **added**.
2. **There is no per-chunk metadata map.** `Chunk` (`internal/model/model.go:80`) has no `Metadata` field (that lives on `Document`). The codebase pattern for reader-derived per-chunk data is a **dedicated non-identity sidecar field** (`Poisoning`, `SectionContext`, `NearDup`, `Caption`, `Kind`). → Add `Chunk.Wikilinks []string` (omitempty), mirroring `SectionContext` (spec 025).
3. **It is new persisted state** (a new Chunk field), but **non-identity and backward-compatible** — not the "no new stored state" the spec assumed.

Verified approach (reuses the spec 025 / audit-H23 pattern end-to-end): the markdown reader emits a transient positional span table (`md["wikilink_spans"]`, target + byte offset into stripped text); the pipeline resolves per-chunk wikilinks by offset containment, drops the transient table before identity/store (so neither document nor chunk identity changes), and stores `Chunk.Wikilinks`; the field is projected to all four transports via `engine.QueryHit` + the spec 035 `GetChunk` surface, with `parity_test.go` asserting identical values.

## Technical Context

**Language/Version**: Go 1.22+ (`CGO_ENABLED=0`).

**Primary Dependencies**: existing only — cobra, pebble, chromem-go, grpc-go, protobuf. No new dependencies.

**Storage**: Pebble KV; `Chunk` records under prefix `0x03`. Change is an additive `omitempty` JSON field on the existing Chunk value — no new prefix, no key-construction change.

**Testing**: `go test -race -cover ./...`. Extends `internal/reader/markdown_test.go` (wikilink grammar), `internal/engine/parity_test.go` (cross-transport identity), and the spec 035 GetChunk tests.

**Target Platform**: cross-platform single binary (Linux / macOS / Windows).

**Project Type**: CLI + multi-transport server (MCP / REST / gRPC) over one engine.

**Performance Goals**: wikilink extraction is a single regex pass on the ingest ACK path (identical cost class to `heading_spans`, which already runs there). Zero query-time cost — the value is read from the stored chunk.

**Constraints**: `<10ms` write ACK (Constitution Principle IV). Wikilink-span extraction is validation-class text work computed during the existing reader pass; it rides the chunk record with zero extra fsync and no I/O, so the ACK budget is unaffected. Pure Go. No schema migration.

**Scale/Scope**: **size M** (corrected upward from the backlog's S once research showed the field + 5 projection points + proto regen + parity tests). Comparable to spec 025.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| **I. Local-First, Single-Binary** | ✅ PASS | Regex + `strings` extraction in-process. No cloud, no network, no new binary kind. |
| **II. Content-Addressed Identity** | ✅ PASS (with discipline) | Chunk ID = `GenerateID(text, mime, {doc,idx})` — wikilinks excluded. Document ID hashes the metadata map, so the transient `wikilink_spans` MUST be stripped from document metadata before `GenerateID` — exactly as `heading_spans` is today (spec 025 R7). Neither identity changes. |
| **III. Pure Go — No CGo** | ✅ PASS | `regexp` + `strings`; no new deps, no CGo. |
| **IV. Async-After-ACK Writes** | ✅ PASS | Wikilink-span extraction runs in the synchronous reader pass alongside `heading_spans`/poisoning scoring — validation-class text work, rides the chunk record, zero extra fsync. `<10ms` ACK preserved. |
| **V. Extension by Interface, MCP-First** | ✅ PASS | Surfaced on all four transports (`engine.QueryHit` → REST/gRPC/MCP/CLI) plus the spec 035 `GetChunk` surface. Parity asserted by `parity_test.go`. |
| **Storage discipline / Schema evolution** | ✅ PASS (nuance documented) | Additive `omitempty` JSON field on Chunk (prefix `0x03`). No new/retired prefix, no key-construction change; old blobs decode to a nil field automatically. **No migration; `migrate.ExpectedVersion` unchanged.** Consistent with the `Poisoning`/`SectionContext`/`NearDup`/`Caption` precedent. See Complexity Tracking. |

## Project Structure

### Documentation (this feature)

```text
specs/036-chunk-wikilink-metadata/
├── plan.md              # this file
├── research.md          # Phase 0 — verified findings + spec reconciliation
├── data-model.md        # Phase 1 — Chunk.Wikilinks, transient span, resolution rule
├── quickstart.md        # Phase 1 — runnable validation
├── contracts/
│   └── api.md           # Phase 1 — wire contract, 5 projection points
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root — files touched)

```text
internal/reader/markdown.go        # collect [[wikilink]] targets+offsets → md["wikilink_spans"]; reuse linkTarget()
internal/reader/markdown_test.go   # wikilink grammar: alias, anchor, embed, dup, chunk-scope
internal/pipeline/*.go             # resolve per-chunk wikilinks by offset containment; DROP wikilink_spans before GenerateID/store
internal/model/model.go            # Chunk.Wikilinks []string (non-identity sidecar, json:"wikilinks,omitempty")
internal/engine/types.go           # QueryHit.Wikilinks []string (canonical projection source)
internal/engine/query.go           # copy chunk.Wikilinks → QueryHit (beside the SectionContext copy)
internal/engine/get_chunk.go       # spec 035 GetChunk surfaces Chunk.Wikilinks
internal/rest/types.go             # queryHit.Wikilinks + GetChunk REST chunk shape
proto/gorag.proto                  # + repeated string wikilinks (Chunk=17, QueryHit=13)
proto/gen/                         # regenerated (protoc / go generate)
internal/grpc/*.go                 # map model↔proto for the two new fields
internal/cli/query.go (+ chunk cmd)# render wikilinks
internal/mcp/server.go             # include wikilinks in query hit + get_chunk tool
internal/engine/parity_test.go     # assert wikilinks identical across CLI/REST/gRPC/MCP
```

**Structure Decision**: every edit lands in the PRD-mapped directory for its subsystem (reader → `internal/reader`, etc.). No new packages, no new `main`, no new key-space prefix. The proto change is the only contract surface and is additive (`repeated` fields, tags 17 and 13).

## Complexity Tracking

> One considered position, not a violation. Logged for reviewer visibility.

| Item | Position | Rationale |
|------|----------|-----------|
| Additive `omitempty` Chunk field vs. schema migration | **No migration; `migrate.ExpectedVersion` unchanged** | The spec-034 schema-evolution rule targets the key-space *layout* (prefix, value-encoding schema, key construction). An additive `omitempty` JSON field on an existing prefix is backward-compatible — old blobs decode to nil with no transform, so there is no v(n)→v(n+1) work for a migration to do. Every prior Chunk sidecar (`Poisoning` 019, `SectionContext` 025, `NearDup` 026, `Caption`/`Kind` 031) landed this way without a migration. The PR will affirm: *no on-disk key-space layout change*. Fallback (a documented no-op migration step) is rejected as default — ceremony with no behavioral difference. |

## Phase Status

- **Phase 0 (Research)** — ✅ complete → [research.md](./research.md). All unknowns resolved; no `NEEDS CLARIFICATION` remains.
- **Phase 1 (Design & Contracts)** — ✅ complete → [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md).
- **Phase 2 (Tasks)** — ⏭ next: `/speckit-tasks`.
