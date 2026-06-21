# Quickstart — Embedding Instruction-Prefix (spec 008)

> Runnable validation scenarios that prove the feature works end-to-end. This is
> a validation/run guide — implementation detail belongs in `tasks.md`. References
> the [data model](data-model.md) and the [role/prefix contract](contracts/embed-role-prefix.md).
> Reuses the spec-007 isolated-daemon discipline: always pass `--db-path <tmp>`
> and non-default transport addrs.

## Prerequisites

- `make build` succeeds → `./bin/go-rag`.
- A local Ollama with `nomic-embed-text` pulled (`ollama pull nomic-embed-text`)
  for the real-model scenarios (SC-001). The deterministic/CI scenarios need no
  Ollama.
- An isolated DB path, e.g. `export GR=$(mktemp -d)/vault`.

## Gate (run before any scenario)

```bash
make build && make vet && make test
```

All green before declaring a scenario passed.

---

## Scenario 1 — Default nomic: query and document get distinct, correct prefixes (mechanism, CI)

**Proves:** FR-001, FR-002, FR-008 (idempotent), contract §1–§2.

1. Configure `embedding_model: nomic-embed-text`, `embedding_prefix: auto`
   (default).
2. Run the prefix unit tests (pure `Prefixer`): assert
   `nomic-embed-text` resolves to query `search_query:` / document
   `search_document:`, that the prefix is prepended once (not doubled for a text
   already starting with it), and that an empty/whitespace text is handled
   without error.
3. Assert the eval `DeterministicEmbedder` is role-aware: the vector derived for
   a query differs from that for the same text as a document (research D5).

**Pass:** the resolved prefixes match the default map; prepend is idempotent;
role produces distinct vectors in the eval embedder. No Ollama required.

---

## Scenario 2 — Non-prefix model is never corrupted (safety, CI)

**Proves:** FR-003, FR-004, contract §2.

1. Configure a model not in the default map (e.g. `embedding_model: all-MiniLM-L6-v2`),
   `embedding_prefix: auto`.
2. Assert the `Prefixer` resolves to **no** prefix for both roles.
3. Set `embedding_prefix: off` explicitly on a prefix model and assert no prefix
   is applied; set explicit `embedding_query_prefix`/`embedding_doc_prefix` and
   assert those exact strings are applied verbatim per role.

**Pass:** unknown model → no prefix; `off` → no prefix; explicit overrides →
applied exactly. No Ollama required.

---

## Scenario 3 — A legacy corpus is not silently half-prefixed (consistency)

**Proves:** FR-005, FR-006, FR-007, US3, contract §3.

1. Build a corpus under the **legacy** convention (convention `""`), e.g. by
   ingesting against a stored fixture or an older build.
2. Enable prefixes (`embedding_prefix: auto`, `nomic-embed-text`) **without**
   re-embedding, then issue a query.
3. Assert the query is **refused** with a convention-mismatch error naming both
   conventions and directing a re-embed (same family as the model/dim mismatch
   error).
4. Re-embed the corpus, then assert: queries succeed; the corpus
   `MajorityConvention` is now the nomic convention; `Consistent` is true; and
   the document count is **unchanged** (no duplicates — identity is over content
   + metadata, not the prefix).

**Pass:** mismatch is loud before scoring; re-embed is consistent and
duplicate-free.

---

## Scenario 4 — Prefixing measurably improves retrieval (quality, manual, SC-001)

**Proves:** SC-001. Requires a real `nomic-embed-text` (not CI).

1. Ingest the spec-004 golden corpus against a real `nomic-embed-text`.
2. Baseline: with `embedding_prefix: off`, run `go-rag eval` (or `go_rag_eval`)
   and record recall@5/10 and NDCG@10.
3. Re-embed with `embedding_prefix: auto` (prefixes on) and re-run eval.
4. Compare.

**Pass:** recall@5/10 and NDCG@10 with prefixes on are **no lower — and
higher** — than the unprefixed baseline, reproducibly. (Mechanism parity — that
the same prefix is applied across CLI/REST/gRPC/MCP — is covered by the engine's
single shared path, contract §4, and checked in CI.)

---

## Scenario 5 — Cross-transport parity (CI)

**Proves:** FR-009, contract §4, SC-005.

1. Start the isolated daemon (`--db-path $GR`, non-default addrs).
2. Ingest a document; then issue the **same** query over each transport (CLI,
   REST `:7879`, gRPC `:7880`, MCP `:7878`).
3. Assert the ranked results are identical across all four (the query prefix is
   applied once, in the shared engine path, regardless of origin).

**Pass:** identical rankings from every transport.

---

## Cleanup

```bash
go-rag stop --db-path "$GR"   # or kill the isolated daemon
rm -rf "$(dirname "$GR")"
```

## Done-when

Scenarios 1, 2, 3, 5 green in CI (`make test`, incl. the prefix unit tests and
the eval mechanism test); Scenario 4 demonstrated manually against a real
`nomic-embed-text` and the numbers recorded. `make build && make vet && make
test` green; no new Pebble prefix; `Embedder` interface unchanged.
