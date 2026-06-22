# Quickstart: Validate Context Window (H15)

## Prerequisites

```bash
make build && make test
```

## Scenario 1 — Context expansion (US1, FR-002, SC-001)

Ingest a multi-chunk document; query with `ContextWindow=1`; assert each hit includes up to 1 previous + 1 next sibling's text.

**Expected**: `QueryHit.Context` has up to 2 entries (previous + next), each with content + direction.

## Scenario 2 — Boundary chunks (FR-006)

Ingest a document where the hit is the first (or last) chunk; query with `ContextWindow=1`.

**Expected**: only the available sibling (next for first; previous for last) — no error.

## Scenario 3 — Opt-in default (FR-005, SC-002)

Query without ContextWindow (=0); assert results are byte-identical to today (no context field).

**Expected**: `QueryHit.Context` is nil/empty; results unchanged.

## Scenario 4 — Linked-list populated (FR-003)

After ingest, verify chunks have `PreviousChunkID`/`NextChunkID` set (non-empty for non-boundary chunks).

**Expected**: `chunks[0].NextChunkID != ""`, `chunks[1].PreviousChunkID == chunks[0].ID`, etc.

## Scenario 5 — Context distinguishable (US2, FR-004)

Assert context chunks are in a separate field, not counted as ranked hits.

**Expected**: top-k count unchanged; context in `QueryHit.Context`, not in the ranked list.

## Scenario 6 — No eval regression (SC-003)

```bash
make test-eval
```

**Expected**: PASS (default ContextWindow=0; recall unchanged).

## Scenario 7 — Cross-transport parity (US3, SC-004)

Same query + context_window over CLI/REST/gRPC/MCP; assert identical context.

**Expected**: identical context per hit across all four transports.
