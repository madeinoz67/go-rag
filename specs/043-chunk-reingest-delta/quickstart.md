# Quickstart: Chunk Change Deltas on Re-Ingest (BL-010)

> Phase 1 output for `/speckit-plan` — runnable validation that the feature works end-to-end. See [spec.md](./spec.md) (acceptance), [contracts/api.md](./contracts/api.md) (the proto), [data-model.md](./data-model.md) (entities). Implementation details belong in `tasks.md`.

## Prerequisites

- A built `go-rag` binary (`make build`) running its daemon on an **isolated** DB (per project CLAUDE.md: pass `--db-path <tmp>` + non-default `--mcp-addr`/`--rest-addr`/`--grpc-addr` when scripting the daemon, to avoid colliding with the live `~/.go-rag/vaults/default` daemon).
- A `WatchDocuments` gRPC consumer (e.g. the bufconn test client in `internal/grpc/watch_documents_test.go`, or `grpcurl` against the daemon's gRPC port).

## Scenario 1 — a localized edit yields mostly-UNCHANGED (US1, MVP)

1. Ingest a multi-paragraph markdown doc: `go-rag add doc.md`.
2. Open a `WatchDocuments` stream.
3. Edit **one paragraph** of `doc.md`; re-ingest: `go-rag add doc.md` (or via reprocess).
4. **Expect**: exactly one `RE_INGESTED` event (NOT an `INGESTED`+`DELETED` pair) whose `chunk_deltas` classify the edited chunk as `ADDED`, the unchanged-text chunks as `UNCHANGED`, and any removed chunk as `REMOVED`. ≥80% `UNCHANGED` for a ≤10%-of-text edit (SC-001 — the target; see [research.md](./research.md) R6).

## Scenario 2 — the old→new chunk-ID map preserves references (US1)

1. From Scenario 1's `RE_INGESTED` event, take an `UNCHANGED` delta entry.
2. **Expect**: both `chunk_id` (new) and `prev_chunk_id` (old) are populated; `GetChunk(prev_chunk_id)` returns not-found (the old version is gone) while `GetChunk(chunk_id)` returns the unchanged chunk. A consumer can remap a stored `prev_chunk_id` reference → `chunk_id` (no orphaned refs).

## Scenario 3 — embed-skip when the baseline is unchanged (US2)

1. Ingest a doc; wait for `EMBEDDED`.
2. Edit one paragraph; re-ingest.
3. **Expect**: the `UNCHANGED` chunks are NOT re-embedded (no embedding worker activity for them — their `PrefixEmbedding` was copied old→new cid); only the `ADDED` chunk triggers embedding. (Verify via the worker's metrics/log, or by asserting the embed count == ADDED count.)

## Scenario 4 — config drift forces re-embed (US2)

1. Change the embedding model (or simulate a `CorpusBaseline` drift).
2. Re-ingest a doc with mostly-unchanged text.
3. **Expect**: ALL chunks (including `UNCHANGED`) are re-embedded — the stale vectors are not reused (FR-007). The `RE_INGESTED` delta still reports `UNCHANGED` for the unchanged-text chunks (the delta is content-identity, independent of embedding currency).

## Scenario 5 — repeated + moved text (US3)

1. Ingest a doc with a paragraph repeated 3× and another paragraph at position 2.
2. Edit so the repeated paragraph is now 2×, and move the position-2 paragraph to position 5 (no text change).
3. Re-ingest.
4. **Expect**: the delta reports 2 `UNCHANGED` + 1 `REMOVED` for the repeated paragraph (multiset, not set); the moved paragraph is `UNCHANGED` (content identity, not position).

## Conventions

- All scenarios run under `go test -race` (the streaming + reorder + embed-skip are concurrency-sensitive — mirror spec 040's `TestGRPC_WatchDocuments_*` over bufconn).
- The delta + the embed-skip gate are pure functions — unit-testable in isolation (`internal/pipeline/delta.go` + the baseline-drift verdict) before the end-to-end streaming tests.
