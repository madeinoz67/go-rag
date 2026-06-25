# Implementation Plan: Migration Dry-Run

**Branch**: `028-migrate-dry-run` (commits to `main` directly — single-author repo; see `CLAUDE.md`) | **Date**: 2026-06-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/028-migrate-dry-run/spec.md` (audit finding **H24**, Phase 6 §1.8). Adds a no-side-effect migration preview (`migrate --dry-run` + a preview operation on every transport) so an operator can see what a model migration would do and cost before committing to a full-corpus re-embed.

## Summary

Today `migrate` shows a per-model breakdown but then **immediately re-embeds every
stale embedding** — a whole-corpus, one-shot, expensive operation — with no way to
stop, no cost estimate, and CLI-only. The fix extracts the read-only "plan"
computation that is already the **first half of `Engine.Migrate`**
(`internal/engine/ingest.go:124`: it already reads `EmbeddingModelStats`, sums
stale, and branches on `stale == 0`) into a shared, read-only
`Engine.MigratePlan`, then surfaces it on every transport:

1. **`Engine.MigratePlan(ctx) (*MigrationPlan, error)`** — computes the plan from
   the two existing metadata-only readers (`EmbeddingModelStats` +
   `CorpusProfile`); never embeds, flushes caches, reprocesses, or refreshes the
   baseline (R1/R5).
2. **Refactor `Engine.Migrate`** to call `MigratePlan` first and proceed only when
   `StaleTotal > 0` — so the preview and execution share one code path
   (FR-008 preview-matches-execution, for free).
3. **`MigrationPlan`** payload: target model, per-source counts + stale flags,
   stale total, stored dimensionality distribution, consistency flag, and a
   labelled-approximate estimate (R2/R4 — no target-dim prediction, no time
   prediction; both would need the backend, which FR-004 forbids).
4. **Surface on every transport**: CLI `--dry-run` flag; new `MigratePlan`
   gRPC rpc + REST endpoint + MCP tool — a *separate* operation, not an overload
   of `Migrate` (R3, clean read-only semantics + return types).

The real `Migrate` is unchanged in behaviour; only its plan computation is shared.

Full design rationale: [research.md](./research.md) (R1–R5). Entity/field detail:
[data-model.md](./data-model.md). Wire contract: [contracts/api.md](./contracts/api.md).
Validation runbook: [quickstart.md](./quickstart.md).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/madeinoz67/go-rag`; `CGO_ENABLED=0`, PRD §10.4).

**Primary Dependencies**: none added. The plan is pure Go over existing Pebble
readers + the existing proto/grpc-go/cobra stack. No new library.

**Storage**: unchanged — single Pebble instance. `MigratePlan` reads only: the
embeddings prefix (`0x04`) via `pipeline.EmbeddingModelStats`, and the corpus
profile via `engine.CorpusProfile`. **No new prefix, no writes.**

**Testing**: `go test -race -cover ./...` (`make test`). New:
`internal/engine/migrate_plan_test.go` (read-only + no-backend + preview==execution),
and a parity-test extension (identical `MigrationPlan` across CLI/REST/gRPC/MCP,
zero mutation). The real-`Migrate` path keeps its existing tests
(`TestQuery_AfterMigrate_IndexIntact`, cache-flush tests).

**Target Platform**: single statically-linked binary, local-first (Constitution I/III). No platform change.

**Project Type**: single-binary local RAG database + multi-transport daemon (CLI/MCP/REST/gRPC).

**Performance Goals** (Constitution, unchanged): `MigratePlan` is two `PrefixScan`
reads over Pebble — sub-100 ms on a local corpus, no embedding round-trip. It does
not touch the <10 ms write-ACK path (it doesn't write).

**Constraints**: strictly read-only (FR-003 — structural, not flag-gated),
no-backend (FR-004 — calls no `Embedder`), parity across 4 transports (FR-006),
preview == execution (FR-008 — shared code path). Real `Migrate` behaviour unchanged.

**Scale/Scope**: one new engine method + result type, a `Migrate` refactor that
reuses it, a proto rpc + message + regen, and one preview path each in
REST/gRPC/MCP/CLI. Touches `internal/engine` (primary), `internal/cli`, and the
three transport adapters + `proto/`. No storage/config/on-disk change. Targets
local corpora.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution: `.specify/memory/constitution.md` v1.0.0 (five principles). All
five PASS — no violations, so the Complexity Tracking table is empty.

| # | Principle | Verdict | Justification (grounded) |
|---|-----------|---------|--------------------------|
| I | Local-First, Single-Binary | ✅ PASS | `MigratePlan` is two local Pebble reads; no LLM, no network, no cloud (FR-004). Single `CGO_ENABLED=0` binary, no new runtime dep. |
| II | Content-Addressed Identity | ✅ PASS | The plan is a read over existing embeddings/chunks; it touches neither chunk `cid` nor document `GenerateID`. Identity/content-hash dedup untouched. |
| III | Pure Go — No CGo | ✅ PASS | Pure-Go plan computation over existing readers; no new dependency. |
| IV | Async-After-ACK Writes | ✅ PASS | `MigratePlan` is strictly read-only — it performs no write and never reaches the ACK path. The real `Migrate` keeps its existing (synchronous-reembed-then-async-index) timing; only its plan computation is shared. <10 ms ACK preserved. |
| V | Extension by Interface, MCP-First | ✅ PASS | The preview is exposed as a first-class MCP tool (`migrate_plan`) like every other operation, and on REST/gRPC/CLI — MCP-first, parity with the existing surface. No new `FileReader`/`Embedder` interface change. |

**Post-design re-check** (after `data-model.md` / `contracts/api.md`): no new
persisted entity, no new Pebble prefix, no on-disk shape change, no identity
change, no ACK-path change, no new dependency. One new read-only engine method
+ result type, surfaced as a new operation on the existing transports. The five
verdicts are unchanged. **Gate: PASS.**

## Project Structure

### Documentation (this feature)

```text
specs/028-migrate-dry-run/
├── spec.md              # Feature spec (/speckit-specify)
├── plan.md              # This file (/speckit-plan)
├── research.md          # Phase 0 — design decisions R1–R5
├── data-model.md        # Phase 1 — MigrationPlan + the two read sources + lifecycle
├── quickstart.md        # Phase 1 — validation runbook (SC-001..005)
├── contracts/
│   └── api.md           # Phase 1 — MigratePlan operation across 4 transports + payload
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

One new engine method + result type; a `Migrate` refactor that reuses it; and one
preview path per transport. No new package, no new `main`:

```text
internal/
├── engine/
│   ├── ingest.go             # refactor Migrate to call MigratePlan first (R1, FR-008)
│   ├── migrate_plan.go       # NEW: Engine.MigratePlan + MigrationPlan/ModelCount/DimCount/Estimate types + planFrom() (R1/R2)
│   └── migrate_plan_test.go  # NEW: read-only, no-backend, preview==execution (SC-001/002/005)
├── rest/
│   └── (routes + types)      # NEW: POST /v1/migrate/plan → MigrationPlan JSON
├── grpc/
│   └── engine_adapter.go     # NEW: MigratePlan handler → engine.MigratePlan
├── mcp/
│   └── server.go             # NEW: migrate_plan tool
├── cli/
│   └── migrate.go            # --dry-run flag → render MigratePlan + exit; plain migrate renders the same plan then proceeds
proto/
├── gorag.proto               # rpc MigratePlan + MigrationPlan/ModelCount/DimCount/Estimate messages
└── gen/                      # regenerated
```

**Structure Decision.** Every directory maps 1:1 to a PRD subsystem (per
`CLAUDE.md`). The plan computation lives in `internal/engine` (alongside
`Migrate`), the new `MigrationPlan` types alongside it, and each transport gets
one thin preview adapter — the same one-engine-method-per-RPC/MCP/REST pattern
every other operation follows. The CLI keeps the `--dry-run` *flag* humans expect
but routes it (and plain `migrate`'s pre-amble) through the shared engine method,
retiring the hand-rolled inline preview in `newMigrateCmd`. No new package, no new
`main`, single binary entrypoint (`cmd/go-rag`) untouched.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

*No violations.* The Constitution Check gate PASSES on all five principles. This
table is intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
