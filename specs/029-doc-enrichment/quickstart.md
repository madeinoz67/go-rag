# Quickstart — Document Auto-Tag & Summary Enrichment (spec 029)

**Phase 1 output.** A validation runbook proving enrichment delivers the
retrieval-quality win (tags→filter), the summary, and the safety properties
(non-blocking, local, graceful, identity-preserving). Every scenario maps to a
Success Criterion in [spec.md](./spec.md).

> **Smoke rule (repo `CLAUDE.md`):** use an **isolated DB** (`--db-path <tmp>`)
> and non-default daemon addrs; never run against the live vault. Enrichment
> needs a **local generation model** pulled (the `enrichment_model`); the
> model-down scenarios (SC-004) deliberately run without it.

---

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0`.
- A local Ollama with **a generation model** for tags+summary (e.g. a 7–8 B
  instruct model) — configured via `enrichment_model`.
- Baseline: `make build vet test` green and `make test-eval` recorded, with
  enrichment **off** (the default).

## Build gate

```bash
make build vet test
```

**Expected:** green with enrichment off (the default) — zero behaviour change.

---

## SC-001 — Auto-tags flow into the existing filter

Enable enrichment, ingest a topical document, wait for the background pass, then
filter:

```bash
DB=$(mktemp -d)
go run ./cmd/go-rag init --db-path "$DB"
# set enrichment_enabled=true + enrichment_model=<a local gen model> in the config
go run ./cmd/go-rag add <a security-themed doc> --db-path "$DB"
sleep <enrichment tick>            # background enrichment is async-after-ACK
go run ./cmd/go-rag query "<phrase>" --tags security --db-path "$DB"
# EXPECT: the doc is returned by the --tags filter (auto-tag), with no query-field change.
```

**Pass:** a doc carrying an auto-tag is returned by `--tags <that tag>`; an
off-topic tag does not match. The unit test asserts the bridge: a doc with
`Enrichment.Tags` but empty `Metadata["tags"]` is filtered correctly.

## SC-002 — Summary is present and surfaced

```bash
go run ./cmd/go-rag status --db-path "$DB"
go run ./cmd/go-rag query "<phrase>" --db-path "$DB"
# EXPECT: each enriched doc shows a concise summary + enrichment_status; a too-short doc shows absent (not error).
```

**Pass:** enriched docs carry a non-empty, topic-accurate summary across
CLI/REST/gRPC/MCP; trivially-short docs carry an absent summary cleanly.

## SC-003 — Write ACK is unchanged (non-blocking)

```bash
go test ./internal/pipeline/   # asserts ingest ACK latency with enrichment on == baseline
```

**Pass:** the <10 ms write ACK is identical to the pre-feature baseline —
enrichment is strictly post-ACK/background.

## SC-004 — Graceful when the model is unreachable

With Ollama down / `enrichment_model` not pulled:

```bash
go run ./cmd/go-rag add <doc> --db-path "$DB"   # enrichment_enabled=true, but model unreachable
go run ./cmd/go-rag query "<phrase>" --db-path "$DB"
```

**Pass:** the doc still ingests and queries (untagged); `enrichment_status`
reflects the failure; enrichment does not loop indefinitely (failed docs are
terminal). Confirmed across all transports.

## SC-005 — Identity preserved + back-fill works

```bash
# identity: doc/chunk IDs + content hash identical with enrichment on vs off
go test ./internal/engine/ ./internal/pipeline/   # asserts GenerateID unaffected by Enrichment
# back-fill: a pre-feature doc (Enrichment==nil) gains tags/summary after a re-enrich pass
go run ./cmd/go-rag <re-enrich cmd> --db-path "$DB"
```

**Pass:** with enrichment on vs off, document/chunk IDs, content hashes, and
vectors are byte-identical (non-identity sidecar); re-add is a no-op; a
pre-feature doc loads without error and gains `Enrichment` after back-fill.

---

## Summary of expected outcomes

| SC | Check | Pass condition |
|----|-------|----------------|
| SC-001 | tags→filter | auto-tagged doc returned by `--tags` (no query-field change) |
| SC-002 | summary | concise topic-accurate summary on status/hits, all transports |
| SC-003 | non-blocking | <10 ms ACK unchanged with enrichment on |
| SC-004 | graceful | model-down → doc still ingests/queries, untagged, no infinite retry |
| SC-005 | identity + back-fill | IDs/hash/vectors identical on vs off; pre-feature doc back-fills |

If all five pass, enrichment delivers the retrieval-quality win safely: tags make
the existing filter useful, summaries aid triage, and the database's core
guarantees (identity, ACK budget, local-only, graceful failure) are intact.
