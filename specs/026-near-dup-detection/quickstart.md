# Quickstart — Near-Duplicate Chunk Detection (spec 026, audit H20)

> Phase 1 output for `/speckit-plan`. A runbook that proves the feature works
> end-to-end against an **isolated** database. Each scenario maps to a Success
> Criterion (SC-001…005) and a spec acceptance scenario. This is a validation
> guide, not an implementation — code lives in `tasks.md`.

## Prerequisites

- A built binary: `make build` → `./bin/go-rag`.
- An Ollama embedding model reachable at the configured `ollama_url` (needed only
  for scenarios that actually embed; keyword-only checks skip it).
- **An isolated DB.** Per the repo `CLAUDE.md` smoke rule, the default `dbPath` is
  the user's global vault — every command below passes `--db-path <tmp>` and
  **non-default** daemon addresses so it cannot collide with a live instance:

```bash
export GR_TMP="$(mktemp -d)"
export GR_REST=127.0.0.1:17891   # default would be :7879
export GR_GRPC=127.0.0.1:17892   # default would be :7880
```

- Fixture documents (under `$GR_TMP/docs/`). Content is in each scenario.

## Start the daemon (once, isolated)

```bash
./bin/go-rag start --db-path "$GR_TMP/db" --rest-addr "$GR_REST" --grpc-addr "$GR_GRPC"
./bin/go-rag status --db-path "$GR_TMP/db"   # confirm storage_open
```

---

## Scenario A — near-duplicates collapse; results stay diverse (SC-001, US1)

**Fixtures** — a passage and a near-identical revision:

- `$GR_TMP/docs/v1.md`: a section whose body is a distinctive paragraph.
- `$GR_TMP/docs/v2.md`: the **same** paragraph with one small edit (a typo fixed /
  one sentence reworded) — near-duplicate, not byte-identical.

**Steps:**

1. `go-rag add "$GR_TMP/docs"`.
2. Query for a phrase present in the shared paragraph, **without** `--dedup`:
   ```bash
   go-rag query "<phrase>" --db-path "$GR_TMP/db"
   # expect BOTH near-duplicate passages in the results (flag-only default)
   ```
3. Query the same phrase **with** `--dedup`:
   ```bash
   go-rag query "<phrase>" --dedup --db-path "$GR_TMP/db"
   # expect ONE representative of the near-duplicate pair (highest-scored)
   ```
4. **Cross-transport parity (SC-002):** issue the `--dedup` query over REST and
   gRPC and diff:
   ```bash
   curl -s "$GR_REST/v1/query" -d '{"query":"<phrase>","mode":"keyword","dedup":true}' | jq '[.hits[].chunk_id]'
   grpcurl -plaintext -d '{"query":"<phrase>","mode":"keyword","dedup":true}' "$GR_GRPC" gorag.Gorag/Query | jq '[.hits[].chunkId]'
   ```
   CLI / REST / gRPC return the **same** collapsed set.

**Expected outcome:** with `dedup`, the near-duplicate pair occupies one slot —
the top-k is more diverse than without. → SC-001, SC-002.

---

## Scenario B — near-duplicates are detected (SC-001/SC-005, US2)

**Fixture** — copy-pasted section across two otherwise-different documents:
- `$GR_TMP/docs/policy_a.md`: original content + a "## Refunds" boilerplate block.
- `$GR_TMP/docs/policy_b.md`: different content + the **same** "## Refunds" block.

**Steps:**

1. `go-rag add "$GR_TMP/docs"`.
2. **Enumerate chunks** (via an engine/pipeline test — see `tasks.md`: ingest the
   pair, list chunks, inspect `NearDup`) and assert:
   - the two "Refunds" chunks list **each other** as siblings (cross-document
     near-dup at chunk granularity — US2-scenario-2);
   - the chunks that are *different* between the two docs have **no** siblings
     (distinct content not flagged — US2-scenario-3 / FR-009).

**Expected outcome:** chunk-level pairwise near-dup detected across documents;
distinct chunks untouched. → US2, FR-001, FR-009.

---

## Scenario C — distinct content is never collapsed (FR-009, SC-005)

**Fixture** — two documents on the **same topic** but **different wording** (two
distinct summaries). These are topically similar but textually distinct.

**Steps:**

1. `go-rag add`, then query with `--dedup`.
2. Inspect the chunks' `near_dup`.

**Expected outcome:** neither chunk lists the other as a sibling; `--dedup` does
**not** collapse them (no false-positive merge). Validates the conservative
threshold + precision guard (R9/R10). → FR-009, SC-005.

---

## Scenario D — status reports near-dup counts (US3-1)

**Steps:**

1. After Scenario A/B, `go-rag status --db-path "$GR_TMP/db"`.
2. Confirm a `near_dup_chunks` count > 0 reflecting the clustered near-duplicates.

**Expected outcome:** status surfaces near-duplicate counts identically across
transports. → US3-1.

---

## Scenario E — idempotent re-add is a no-op (SC-003, FR-003)

**Steps:**

1. Record counts: `go-rag status` → `documents`, `chunks`.
2. Re-add an unchanged file: `go-rag add "$GR_TMP/docs/v1.md"`.
3. Re-check status.

**Expected outcome:** counts unchanged; re-add is a no-op (near-dup is a
non-identity sidecar; content-hash dedup intact). → SC-003.

---

## Scenario F — pre-feature chunks load absent (US3-3, FR-008)

**Setup:** a chunk record written before the feature (no `near_dup` key), or a
hand-crafted JSON record, present in the DB.

**Steps:**

1. Query it; inspect the hit.

**Expected outcome:** the chunk loads without error and the hit **omits**
`near_dup` (absent). Collapse treats it as a singleton. Back-fill on the live
corpus is via `go-rag reprocess <path>` (the SimHash is derived at ingest; there
is no cheap rescan — research R3). → US3-3.

---

## Scenario G — retrieval does not regress (SC-004)

**Steps:**

1. Run the project's retrieval-eval harness (spec 004), pre-feature vs
   post-feature, under the same embedding model — with `dedup` **off** (default).
2. Compare overall metrics.

**Expected outcome:** with `dedup` off, retrieval is byte-identical to the
pre-feature baseline (near-dup capture + the sidecar are non-invasive; FR-007).
With `dedup` on, redundancy in the top-k drops without losing relevant coverage
(recall of distinct relevant passages maintained). → SC-004.

---

## Mapping to Success Criteria

| Scenario | Proves | SC |
|----------|--------|----|
| A (collapse + parity) | near-dups collapse; diverse top-k; identical across transports | SC-001, SC-002 |
| B (detection) | chunk-level pairwise near-dup, cross-document | SC-001, SC-005 |
| C (distinct not flagged) | precision guard — distinct content never merged | SC-005, FR-009 |
| D (status counts) | near-dup visibility in status | US3-1 |
| E (idempotent re-add) | re-add is a no-op | SC-003 |
| F (pre-feature graceful) | absent near-dup on old records | SC-005, US3-3 |
| G (eval non-regression) | no retrieval regression; redundancy drops with dedup | SC-004 |

## Cleanup

```bash
go-rag stop --db-path "$GR_TMP/db" 2>/dev/null
rm -rf "$GR_TMP"
```
