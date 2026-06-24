# Quickstart — Per-Chunk Section Context (spec 025, audit H23)

> Phase 1 output for `/speckit-plan`. A runbook that proves the feature works
> end-to-end against an **isolated** database. Each scenario maps to a Success
> Criterion (SC-001..005) and an acceptance scenario in `spec.md`. This is a
> validation guide, not an implementation — code lives in `tasks.md`.

## Prerequisites

- A built binary: `make build` → `./bin/go-rag`.
- An Ollama instance with an embedding model available (e.g. `nomic-embed-text`),
  reachable at the configured `ollama_url` (needed only for scenarios that
  actually query embeddings; keyword-only checks skip it).
- **An isolated DB.** Per the repo `CLAUDE.md` smoke rule, the default `dbPath` is
  the user's global vault. Every command below passes `--db-path <tmp>` and
  **non-default** daemon addresses so it cannot collide with a live instance:

```bash
export GR_TMP="$(mktemp -d)"
export GR_MCP=127.0.0.1:17878     # default would be :7878
export GR_REST=127.0.0.1:17879    # default would be :7879
export GR_GRPC=127.0.0.1:17880    # default would be :7880
```

- Fixture documents (create under `$GR_TMP/docs/`). Exact content is in each
  scenario; the nested-heading fixture is the keystone for SC-001/002.

## Start the daemon (once, isolated)

```bash
./bin/go-rag start --db-path "$GR_TMP/db" \
  --mcp-addr "$GR_MCP" --rest-addr "$GR_REST" --grpc-addr "$GR_GRPC"
./bin/go-rag status --db-path "$GR_TMP/db"   # confirm storage_open, embedder reachable
```

(All scenarios reuse this daemon. For single-shot CLI checks, add a file with
`go-rag add` and query with `go-rag query`; the daemon is used only where
cross-transport parity is asserted.)

---

## Scenario A — nested-heading breadcrumb, positional + cross-transport (SC-001, SC-002; US1, US2)

**Fixture** `$GR_TMP/docs/ops.md`:

```markdown
# Operations
## Backups
### Retention
We keep 30 days of incremental backups.
### Schedule
Backups run nightly at 02:00.
## Restores
Restore from the latest snapshot.
```

**Steps:**

1. `go-rag add "$GR_TMP/docs"` → ingest.
2. **Positional correctness (US2, SC-001)** — enumerate the stored chunks and
   assert each carries the heading governing its start position. Via the engine
   in a test (see `tasks.md`: a `pipeline_test.go` case that ingests `ops.md`,
   lists chunks, and asserts):
   - the "We keep 30 days…" chunk → `["Operations","Backups","Retention"]`
   - the "Backups run nightly…" chunk → `["Operations","Backups","Schedule"]`
   - the "Restore from…" chunk → `["Operations","Restores"]`
3. **Visible on the hit (US1, SC-002)** — query for `incremental backups` and
   confirm the hit shows the breadcrumb:
   ```bash
   go-rag query "incremental backups" --db-path "$GR_TMP/db"
   # expect a Section: Operations / Backups / Retention line on the hit
   ```
4. **Cross-transport parity (FR-004, SC-002)** — issue the same query over REST,
   gRPC, and MCP and diff the `section_context` value:
   ```bash
   curl -s "$GR_REST/v1/query" -d '{"query":"incremental backups","mode":"keyword"}' | jq '.hits[0].section_context'
   grpcurl -plaintext -d '{"query":"incremental backups","mode":"keyword"}' "$GR_GRPC" gorag.Gorag/Query | jq '.hits[0].section_context'
   ```
   All three (CLI, REST, gRPC) MUST return `["Operations","Backups","Retention"]`.

**Expected outcome:** 100% of `ops.md`'s chunks carry the correct governing path;
the breadcrumb is present with zero extra user actions and identical across
transports. → SC-001, SC-002.

---

## Scenario B — heading-less documents degrade gracefully (SC-005; US3-1)

**Fixtures:**
- `$GR_TMP/docs/notes.txt` — a few lines of plain prose, no `#`.
- `$GR_TMP/docs/snippet.md` — Markdown with only a fenced code block (e.g. a
  script) and no in-body headings.

**Steps:**

1. `go-rag add "$GR_TMP/docs"`.
2. Query each for a distinctive term. Confirm:
   - ingestion succeeded (exit 0, no error),
   - the returned hits **omit** `section_context` (absent — not `null`, not `[]`),
   - identical absence across CLI / REST / gRPC.

```bash
curl -s "$GR_REST/v1/query" -d '{"query":"<distinctive term>","mode":"keyword"}' | jq '.hits[0] | has("section_context")'
# expect false
```

**Expected outcome:** no errors; hits carry no section context, identically
across transports. → SC-005, US3-1.

---

## Scenario C — idempotent re-add is a no-op (SC-003; FR-003, US3-3)

**Steps:**

1. Note counts after Scenario A/B: `go-rag status --db-path "$GR_TMP/db"` →
   record `documents` and `chunks`.
2. Re-add the unchanged heading-bearing doc: `go-rag add "$GR_TMP/docs/ops.md"`.
3. Re-check status.

**Expected outcome:** `documents` and `chunks` are **unchanged**; the re-add is a
no-op (content-hash dedup; section context is a non-identity sidecar). → SC-003.

---

## Scenario D — `#` inside a code fence is not a heading (FR-009)

**Fixture** `$GR_TMP/docs/code.md`:

````markdown
# Install

```sh
#!/bin/sh
# this is a comment, not a heading
echo hi
```
````

**Steps:**

1. `go-rag add "$GR_TMP/docs/code.md"`.
2. Query for `echo hi`; inspect the hit's `section_context`.

**Expected outcome:** the breadcrumb is `["Install"]` only — the `#!/bin/sh` and
`# this is a comment` lines inside the fence are **not** treated as headings.
Validates FR-009 (and the unified code-fence-aware scan from research R4).

---

## Scenario E — pre-feature chunk loads absent (US3-2)

**Setup:** a chunk record written by a pre-feature build (or a hand-crafted
JSON record without `section_context`) is present in the DB.

**Steps:**

1. Query it.

**Expected outcome:** the chunk loads without a parse error and the hit omits
`section_context` (absent). Validates US3-2 (graceful read of old records). For
back-fill on the live corpus, run `go-rag reprocess <path>` (re-reads the source
to re-derive spans; there is no cheap rescan — see `data-model.md` §5 / research
R7).

---

## Scenario F — retrieval does not regress (SC-004)

**Steps:**

1. Run the project's existing retrieval-eval harness (spec 004) on a fixture
   corpus, once on a pre-feature build and once on the feature build, using the
   **same** embedding model.
2. Compare overall metrics.

**Expected outcome:** retrieval metrics do **not** regress — section capture does
not perturb chunking or embeddings (FR-008; the chunker and embedded text are
unchanged, research R1). → SC-004.

---

## Mapping to Success Criteria

| Scenario | Proves | SC |
|----------|--------|----|
| A (positional) | correct governing heading per chunk | SC-001 |
| A (parity) | identical breadcrumb across transports, zero extra actions | SC-002 |
| C | re-add is a no-op | SC-003 |
| F | retrieval metrics do not regress | SC-004 |
| B (and E) | heading-less / pre-feature degrade gracefully | SC-005 |

## Cleanup

```bash
go-rag stop --db-path "$GR_TMP/db" 2>/dev/null
rm -rf "$GR_TMP"
```
