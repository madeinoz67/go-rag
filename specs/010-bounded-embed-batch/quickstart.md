# Quickstart: Validate Bounded Embedding Batches (H12)

> Runnable validation scenarios proving the feature works end-to-end. These are
> *validation* steps — implementation/test bodies live in `tasks.md`. All
> scenarios use an in-process embedding stand-in (`httptest` Ollama) so they are
> hermetic and need no real Ollama.

## Prerequisites

```bash
make build                 # ./bin/go-rag
make test                  # go test -race -cover ./... must be green
```

The validation is driven through `internal/embed/ollama_test.go` (and optionally
an end-to-end pipeline ingest). No external service required.

---

## Scenario 1 — Large document ingests without timeout/OOM (US1, SC-001/SC-002)

Call `Ollama.Embed` with **N ≫ batch cap** texts (e.g. 500) against an `httptest`
Ollama that records the `input` length of every request it receives.

**Expected**:
- The call succeeds and returns exactly 500 vectors in order.
- Every recorded request carried **≤ 32** texts (the cap) — no single oversized
  request. (This is the direct proof of SC-002: per-request size is bounded by
  the cap, not by N.)

## Scenario 2 — Transient per-batch failure is retried (US2 acceptance 1)

An `httptest` Ollama that returns HTTP 500 for the **first attempt of one
specific batch** (then 200 for every other request and the retry of that batch).

**Expected**: `Embed` succeeds; the document's full set of vectors is returned.
The transient batch was retried and recovered; no batch was dropped.

## Scenario 3 — Permanent batch failure fails the whole call, no partial result (US2 acceptance 2, FR-006)

An `httptest` Ollama that returns HTTP 500 **persistently** for one batch (all 3
attempts), 200 for the others.

**Expected**:
- `Embed` returns a **non-nil error** and **no** vectors.
- The error surfaces which batch failed. (Through the pipeline this means the
  document is marked errored and no vectors are stored — the existing
  `processJob` behavior, unchanged.)

## Scenario 4 — Batching is invisible: identical vectors, in order (US3, SC-003)

A deterministic `httptest` Ollama (vector derived solely from the input text, e.g.
a hash). Embed the same N texts twice — once with N below the cap, once with N
far above it (forcing many batches).

**Expected**: the two returned `[][]float32` slices are **byte-identical** and in
input order. (Proves order preservation + that batching does not change results —
the core US3 invariant.)

## Scenario 5 — Per-batch integrity guard (FR-005)

An `httptest` Ollama that, for one batch, returns a vector count ≠ the batch's
text count (e.g. truncates by one).

**Expected**: `Embed` returns an error; it never returns a short/padded result
that would silently misalign later vectors.

## Scenario 6 — Edge cases (FR-007)

- **Empty input**: `Embed(ctx, nil)` → `(nil, nil)`, and the stand-in records
  **zero** requests.
- **Sub-cap input** (e.g. 5 texts): exactly **one** request carrying all 5 —
  behavior identical to today.
- **Non-multiple-of-cap** (e.g. 70 texts, cap 32): three requests (32 + 32 + 6);
  all 70 vectors returned in order.

## Scenario 7 — Context cancellation between batches (FR-008)

Start `Embed` on a large input; cancel `ctx` after the first batch completes.

**Expected**: `Embed` returns the context error promptly, without issuing the
remaining batches or hanging.

## Scenario 8 — End-to-end pipeline ingest (optional, real-ish)

With a local Ollama (or a fast `httptest` stand-in wired as `cfg.OllamaURL`),
ingest a large document and confirm it reaches `embedded` status and is queryable:

```bash
TMPDB="$(mktemp -d)/v"
./bin/go-rag init --db-path "$TMPDB" --model nomic-embed-text
./bin/go-rag add --db-path "$TMPDB" <large-file>
./bin/go-rag status --db-path "$TMPDB"      # documents embedded, no error status
./bin/go-rag query --db-path "$TMPDB" "<phrase from the large file>"
```

**Expected**: the large document ingests fully (no timeout/error status) and
returns relevant hits — the failure regime H12 fixes.
