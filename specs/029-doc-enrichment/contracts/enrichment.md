# Contract — Document Enrichment (spec 029)

**Phase 1 output.** Two surfaces change externally: (1) **tags** now flow through
the *existing* tag filter (no new query field — via the sidecar bridge); (2) a new
**summary + enrichment-status** field appears on document/status/hits across all
four transports. The `Enricher` interface and the sidecar itself are internal.
This document is the external contract.

---

## 1. Tags — unchanged query surface, new population source

The tag filter (`--tags` / `tags` / REST `tags` / gRPC `tags`) is **unchanged**.
It now matches a document if the requested tag appears in **either**:

- `Document.Metadata["tags"]` — manual / frontmatter tags (today's source), OR
- `Document.Enrichment.Tags` — auto-generated tags (new).

Both are optional; a document may have either, both, or neither. No new request
field, no new transport message for tags — the bridge is internal.

---

## 2. Summary + enrichment status — new field on document/status surfaces

A new optional field on the document/status view, surfaced identically on
CLI/REST/gRPC/MCP, **omitted when absent** (unenriched / pre-feature / off):

| Field | Type | Meaning |
|-------|------|---------|
| `summary` | string (omitted if empty) | one-line model-written document summary |
| `enrichment_status` | string (omitted if nil) | `enriched` \| `failed` \| `nothing-to-enrich` |
| `tags` (on the document/status view) | `[]string` | the *effective* tag set (manual ∪ auto), for visibility |

Status additionally reports an **aggregate**: count of enriched documents vs
total, and whether enrichment is enabled.

---

## 3. The non-identity + local-only contract clauses (load-bearing)

- **Non-identity (FR-002).** Enrichment is a sidecar struct, never a
  `Document.Metadata` key. `GenerateID` is unchanged. Re-adding an unchanged file
  is a no-op; chunk IDs, content hashes, and vectors are identical with
  enrichment on vs off. *This must hold under any future code that re-derives a
  document ID.*
- **Local-only (FR-005).** Enrichment uses the bundled local model only — no
  network/cloud egress (Constitution I). The `Enricher` interface permits future
  providers, but v1 ships the local one.
- **Post-ACK / non-blocking (FR-004).** Enrichment runs on the background ingest
  worker; the <10 ms write ACK is unaffected.
- **Graceful absence (FR-007/FR-008).** A document with `Enrichment == nil`
  (off, pre-feature, or not-yet-enriched) loads and queries normally; the filter
  simply has no auto-tags for it. Pre-feature documents are back-filled on demand.
- **Bounded / no-infinite-retry (FR-009).** A circuit breaker fast-fails a
  failing model; permanently-failed documents are marked and not retried forever.

---

## 4. Configuration

| Key | Default | Meaning |
|-----|---------|---------|
| `enrichment_enabled` | `false` | opt-in master switch (FR-006) |
| `enrichment_model` | `""` | the local generation model to use for tags+summary |

When `enrichment_enabled` is false (the default), the system makes **zero**
enrichment model calls and is byte-identical to today.

---

## What is explicitly NOT in the contract

- **No new query request field** for tags (rides the existing `tags` filter).
- **No chunk-level enrichment** (doc-level only in v1).
- **No entity/relationship graph** (GraphRAG out of scope).
- **No cloud LLM** (local-only).
- **No answer generation / query-time LLM** (retrieval-only remains the product;
  the PRD N4 revision is narrow — background local enrichment only).
