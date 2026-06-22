# Quickstart: Validate Metadata Filtering (H14)

> Runnable validation proving the filter scopes queries correctly and does not regress
> unfiltered retrieval. Hermetic (deterministic embedder, no real Ollama).

## Prerequisites

```bash
make build && make test   # green baseline
```

## Scenario 1 — Source filter scopes to a folder (US1, FR-001/003, SC-001)

Ingest docs from two "folders" (temp dirs); query with `--source` for one; assert only that
folder's docs appear.

**Expected**: all hits' `FilePath` match the source glob.

## Scenario 2 — Type filter scopes to a file type (US1)

Ingest `.md` and `.txt` docs; query with `--type .md`; assert only markdown docs.

**Expected**: all hits' document type is `.md`.

## Scenario 3 — Tags filter scopes by tag (US1)

Ingest docs with tags in `Metadata["tags"]`; query with `--tags security`; assert only
tagged docs.

**Expected**: all hits' document has the tag.

## Scenario 4 — Matches-nothing → empty (FR-003)

Query with a filter matching no document (e.g., `--source nonexistent/`).

**Expected**: empty result set (no error, no crash).

## Scenario 5 — Unfiltered = today's behavior (FR-004, SC-002)

Query without any filter; assert results are byte-identical to today.

**Expected**: identical hits/order/scores.

## Scenario 6 — Cross-transport parity (US3, FR-008, SC-003)

Issue the same query + filter over CLI, REST, gRPC, MCP; assert identical results.

**Expected**: identical ranked `chunk_id` order.

## Scenario 7 — No eval regression (US3, FR-004, SC-004)

```bash
make test-eval
```

**Expected**: PASS (default queries carry no filter; recall@10 unchanged).

## Scenario 8 — Filter × collapse × rerank ordering (FR-007)

Query with a filter that scopes to one doc per folder; verify collapse (top-1/doc) and
rerank operate only on the filtered set (no non-matching docs in results).

**Expected**: every result matches the filter; collapse/rerank didn't surface filtered-out docs.
