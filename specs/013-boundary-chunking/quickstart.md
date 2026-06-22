# Quickstart: Validate the Boundary-Aware Cascade (H10)

> Runnable validation proving the cascade produces boundary-aware chunks and does
> not regress retrieval. These are *validation* steps — implementation/test bodies
> live in `tasks.md`. Scenarios 1–6 are hermetic unit tests (no Ollama); scenario 7
> is the eval gate.

## Prerequisites

```bash
make build                 # ./bin/go-rag
make test                  # go test -race -cover ./... must be green
```

New validation lives in `internal/chunk/chunk_test.go` (unit) plus the H02 eval
harness — no external service required for scenarios 1–6.

---

## Scenario 1 — Chunks end at sentence boundaries (US1, FR-002, SC-001)

Feed multi-sentence prose sized to force ≥2 chunks. Assert no chunk ends
mid-sentence — each chunk's text ends with a sentence terminator (`.`, `!`, `?`,
or CJK `。！？`) or is the final chunk, unless that single sentence exceeded the
budget (scenario 3).

**Expected**: every chunk boundary aligns with a sentence boundary.

## Scenario 2 — Paragraph breaks honored (US1, FR-003, SC-002)

Feed two paragraphs (blank-line separated), each fitting the size budget. Assert
no chunk spans the paragraph boundary — the split happens at the `\n\n`.

**Expected**: each paragraph's sentences stay within chunks that don't cross the
paragraph break.

## Scenario 3 — Over-long sentence → word fallback (US1, FR-004)

Feed a single sentence longer than the size budget. Assert it is split at word
boundaries (the cascade's word level) into budget-sized chunks — never one
oversized chunk, never a failure.

**Expected**: ≥2 chunks, each ≤ size budget, split on word boundaries.

## Scenario 4 — No sentence terminators → graceful degradation (edge case)

Feed text with no sentence punctuation (a list, a log line). Assert it degrades to
word-window behavior (no one-giant-chunk, no crash).

**Expected**: multiple budget-sized chunks via the word fallback.

## Scenario 5 — CJK sentence boundaries (edge case)

Feed CJK prose with `。` / `！` / `？` terminators. Assert chunks end at those
boundaries (CJK not silently mis-boundaried) and characters are preserved.

**Expected**: chunk boundaries align with CJK terminators; content intact.

## Scenario 6 — Overlap + tail-merge preserved (FR-007)

Feed prose that produces several chunks. Assert neighbor chunks share overlap
content (now at sentence granularity) and any sub-`MinTokens` final tail merges
into its predecessor (existing behavior).

**Expected**: overlap present between neighbors; no tiny trailing chunk.

## Scenario 7 — No retrieval regression (US3, FR-006, SC-003)

```bash
make test-eval
```

**Expected**: PASS — recall@10/MRR no worse than the word-window baseline. (Chunk
boundaries shift, so this is the gate that proves the change helps or is neutral.)

## Scenario 8 — Package doc accurate (US2, FR-005)

Read the `internal/chunk` package doc; confirm every behavioral claim (cascade
levels, ~Size budget, sentence-granularity overlap, MinTokens tail-merge, 1.3×
token heuristic) is implemented and covered by a test.

**Expected**: no claim in the doc lacks a matching implementation + test.

## Scenario 9 — End-to-end ingest (optional)

With a local Ollama (or fast stand-in), ingest a markdown file with clear
paragraph/sentence structure and confirm chunks (via `go-rag status` / a query)
reflect boundary-aware splitting.

```bash
TMPDB="$(mktemp -d)/v"
./bin/go-rag init --db-path "$TMPDB" --model nomic-embed-text
./bin/go-rag add --db-path "$TMPDB" <structured-md-file>
./bin/go-rag status --db-path "$TMPDB"
```

**Expected**: coherent, boundary-respecting chunks ingested without error.
