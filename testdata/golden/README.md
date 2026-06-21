# go-rag Golden Retrieval-Quality Dataset

This directory holds go-rag's committed, versioned evaluation dataset for
measuring retrieval quality (recall@k, precision@k, MRR, NDCG@k). It is the
fixture the `go-rag eval` command and the `make test-eval` regression gate run
against. Treat it as code: it lives in git, changes are reviewed in PRs, and
every retrieval change is validated against it.

## Files

| File | Purpose |
|------|---------|
| `v1.jsonl` | The golden dataset: one `{id, query, relevant}` record per line. |
| `corpus/` | The small source corpus the labels refer to (one markdown doc per topic). |
| `baseline.json` | The committed offline metric snapshot the regression gate compares against. Regenerate with `go-rag eval --record-baseline`. |

## Schema (`v1.jsonl`)

```json
{"id":"q01","query":"How does go-rag split documents into chunks?","relevant":["<sha256-chunk-id>"]}
```

- `id` — unique handle for the query (used in per-query output).
- `query` — the natural-language query, run verbatim through the engine.
- `relevant` — the content-addressed `chunk_id`(s) a human judged relevant.
  `chunk_id` is SHA-256 over chunk text + metadata (Principle II), so it is
  **stable** across any vault built from `corpus/` with the default chunker.
- `notes` (optional) — free-text annotation.

Blank lines and `#` comment lines are ignored. A query with an empty `relevant`
list is allowed but is skipped at scoring time (FR-008).

## Adding a labeled query

1. (If the chunk is new) find its `chunk_id`:
   ```bash
   make build
   ./bin/go-rag eval --dump-chunks
   # prints: <chunk_id>\t<file>\t<preview>
   ```
2. Append a line to `v1.jsonl` with a unique `id`, the `query`, and the
   `relevant` chunk_id(s).
3. Re-run `./bin/go-rag eval` to confirm the query scores, then refresh the
   baseline if quality legitimately changed:
   ```bash
   ./bin/go-rag eval --record-baseline
   ```
4. Commit `v1.jsonl` (and `baseline.json` if refreshed) — the change is
   reviewable in the PR diff.

## Generating candidate pairs for triage

`go-rag eval-gen` (or `go-rag eval --dump-chunks`) prints chunk_ids + previews so
a human can pick relevant labels. Candidates are **never** auto-committed —
humans remain the source of truth for relevance (research.md D8).

## After an intentional chunker change

A deliberate change to chunking (audit H10) will change chunk_ids, making the
existing labels stale. The harness will report those queries as skipped
(stale-label), which is the correct signal that labels need refreshing:
re-dump chunk_ids, re-label, and re-record the baseline in the same PR that
ships the chunker change.

## Relevance model

v1 uses **binary** relevance (a chunk is relevant or not). The NDCG formula
accepts grades, so graded labels (`"grade": n`) can be added later without a
schema break.
