# Implementation Plan: Boundary-Aware Chunking — the Cascade (H10)

**Branch**: `main` (single-author repo; Spec Kit work commits directly to `main`) | **Date**: 2026-06-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/013-boundary-chunking/spec.md` (audit backlog item **H10**, P0). **Scope locked via clarify Q1 → Option A**: implement the cascade (not the doc-only fix).

## Summary

Replace the pure word-window inside `Splitter.Split` with a boundary-aware
**paragraph → sentence → word cascade**, matching the behavior the package doc
already (falsely) claims. Chunks are assembled by greedily filling to the size
budget at **sentence** boundaries, flushing at **paragraph** boundaries, and
falling back to the existing word-window for any single sentence that exceeds the
budget. The `Splitter` / `Segment` / `NewSplitter` / `EstimateTokens` signatures
are unchanged, so the pipeline (`processFile`) and all CLI callers are untouched.
Sentence/paragraph detection is rule-based (terminator punctuation + CJK + blank
lines) — pure Go, no NLP dependency. The change is gated by the H02 eval harness
(no regression). The package doc is corrected to describe the cascade truthfully.

## Technical Context

**Language/Version**: Go 1.22+ (PRD §10.4). Pure Go, `CGO_ENABLED=0`.

**Primary Dependencies**: stdlib only — `strings`, `unicode`/`utf8` (all already in use or trivial). **No new dependencies** — no sentence-segmenter library, no CGo.

**Storage**: Pebble KV — **N/A**. Chunking is pre-storage; the `Segment` shape (Text, StartCharIdx, EndCharIdx, TokenCount) is unchanged, so the persisted `Chunk` record and all key-space prefixes are untouched (Principle II).

**Testing**: `go test -race -cover ./...` (`make test`); the H02 eval harness (`make test-eval`, spec 004) is the no-regression gate (SC-003). New boundary tests in `internal/chunk/chunk_test.go` (sentence-end, paragraph-honor, word-fallback, overlap, tail-merge, CJK).

**Target Platform**: Local single binary; chunking runs in the ingest path.

**Project Type**: CLI + multi-transport server. This feature is internal to `internal/chunk` — **no transport adapter, CLI flag, config, or proto change** (the cascade is default-on, no knob).

**Performance Goals**: Chunking runs on the **sync ingest path** (`processFile`, before the Pebble `Sync` ACK). The cascade MUST stay O(text) — a single linear scan for sentence/paragraph boundaries, no catastrophic backtracking — so the < 10ms write-ACK budget is unaffected (Principle IV). For typical docs this is microseconds-to-low-ms, same order as the current word-window.

**Constraints**: Pure Go; `Splitter.Split` signature unchanged (callers untouched); `Segment` shape unchanged; sentence detection is rule-based (no NLP); results must not regress retrieval quality (eval gate); preserve overlap + minimum-tail-merge guarantees.

**Scale/Scope**: Per-document, O(text); no corpus-wide work at chunk time.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I  | Local-First, Single-Binary | ✅ Pass | Pure in-process splitter; no network, no new binary. |
| II | Content-Addressed Identity | ✅ Pass | No change to identity/change hashes or the `Chunk` record. **Note:** re-chunking changes chunk *text* → chunk IDs change, so an existing vault keeps its old chunking until re-ingested via `Reprocess`/`migrate` (idempotent — no duplicates, Principle II intact). This is documented in the spec; not a migration the feature performs. |
| III | Pure Go — No CGo | ✅ Pass | stdlib only; rule-based sentence detection (no NLP library, no CGo). |
| IV | Async-After-ACK Writes | ✅ Pass | Chunking is pre-Sync sync work; the cascade stays O(text) so the < 10ms ACK budget is unaffected. Embedding/indexing remain async-after-ACK. |
| V  | Extension by Interface, MCP-First | ✅ Pass | The `Splitter.Split` contract is unchanged — the pipeline and all CLI callers are untouched. No new interface, no transport surface. |

**No violations.** Complexity Tracking table below is empty.

## Project Structure

### Documentation (this feature)

```text
specs/013-boundary-chunking/
├── plan.md              # This file
├── research.md          # Phase 0 — cascade algorithm, sentence/paragraph detection, overlap, fallback, O(text) constraint
├── data-model.md        # Phase 1 — Segment (unchanged shape) + the boundary units; preserved Split contract
├── quickstart.md        # Phase 1 — boundary-aware chunk validation + eval gate
└── tasks.md             # (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/chunk/
├── chunk.go             # Split → cascade (paragraph→sentence→word greedy-fill); corrected package doc (FR-005)
├── sentences.go         # splitParagraphs (blank-line) + splitSentences (terminator + CJK-aware, char offsets)
└── chunk_test.go        # boundary tests: sentence-end, paragraph-honor, word-fallback, overlap, tail-merge, CJK
```

**Structure Decision**: The change is contained to `internal/chunk/`. The `Splitter` struct and `NewSplitter`/`EstimateTokens`/`Segment` are unchanged; only the body of `Split` is rewritten to the cascade, with sentence/paragraph helpers in a new `sentences.go`. The pipeline call site (`internal/pipeline/pipeline.go:206`, `p.splitter.Split(content)`) and the four CLI `NewSplitter(...)` sites are **deliberately untouched** — `Split`'s contract (text → `[]Segment`) is preserved. No contracts file: the chunker is an internal package with no external/transport surface, and the Split signature is unchanged (documented in `data-model.md`).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

*(Empty — Constitution Check passes cleanly on all five principles. No violations to justify.)*
