# Quickstart Validation: go-rag v1

**Phase**: 1 (Design) | **Date**: 2026-06-19

End-to-end validation guide for User Story 1 (the MVP: init → ingest → query).
This is the acceptance script that proves the feature works once implemented.
References the [CLI contracts](./contracts/cli-commands.md) and
[data model](./data-model.md) rather than duplicating them.

> **Current status**: scaffold only. The six commands exist and respond (stubs
> return "not yet implemented"); the steps below validate the TARGET behavior once
> US1 is implemented by `/speckit-tasks` → `/speckit-implement`.

## Prerequisites

- Go 1.22+ (installed: 1.26.4)
- Ollama running locally with an embedding model pulled, e.g.:
  `ollama pull nomic-embed-text`
- A small set of test documents (a PDF, a Markdown file, a .txt) in `./sample-docs/`

## Steps

### 1. Build
```bash
make build
```
**Expected**: `./bin/go-rag` exists; `./bin/go-rag version` prints a version string;
binary is < 25 MB.

### 2. Initialize
```bash
./bin/go-rag init --model nomic-embed-text
```
**Expected**: `.go-rag/` created with `config.json` and `data/`; Ollama detected at
`http://localhost:11434`; success message with next-step hints. Exit 0.

### 3. Ingest
```bash
./bin/go-rag add ./sample-docs/
```
**Expected**: per-file lines (`[n/total] file ... NEW (N chunks)` / `SKIPPED` /
`ERROR`), then a summary (`X new, Y skipped, Z errors`) and an async embedding notice.
Re-running immediately should report all files `SKIPPED` (idempotent — Principle II).

### 4. Query
```bash
./bin/go-rag query "your question here" --k 5 --mode hybrid
```
**Expected**: up to 5 ranked results, each with chunk text, source file path, page
number (for PDFs), and relevance score; returns in < 500 ms (SC-002). Try `--mode
keyword` (< 50 ms, SC-003) and `--mode semantic`.

### 5. Status
```bash
./bin/go-rag status
```
**Expected**: correct source/document/chunk counts, embedded %, storage size, model
`nomic-embed-text`, health `OK`.

## Success = Acceptance

This quickstart passes iff spec acceptance scenarios US1.1–US1.4 hold (see
[spec.md](./spec.md)): ingest processes supported types and skips unsupported, queries
return cited results, re-ingest is a no-op, and the whole loop runs offline once the
embedding model is local.
