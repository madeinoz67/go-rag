# Data Model — Document Auto-Tag & Summary Enrichment (spec 029)

**Phase 1 output.** This feature adds **one new non-identity sidecar** on
`Document` (`Enrichment *EnrichInfo`) and **one new interface** (`Enricher`). It
persists **no new prefix** — the sidecar rides on the existing document record
(prefix `0x02`). Identity, chunks, vectors, and the write-ACK path are unchanged.

---

## 1. `EnrichInfo` — the document enrichment sidecar (NEW)

Added to `Document` as `Enrichment *EnrichInfo` (nil = unenriched / pre-feature /
off). Mirrors the per-`Chunk` sidecars (`Poisoning`, `SectionContext`, `NearDup`):
a non-identity attribute, absent-until-populated, omitted from `GenerateID`.

| Field | Type | Purpose |
|-------|------|---------|
| `Tags` | `[]string` | small set of auto-generated topic tags; feeds the existing tag filter via the bridge (R1) |
| `Summary` | `string` | one-line document summary; surfaced on status/hits |
| `Model` | `string` | the generation model that produced it (provenance) |
| `GeneratedAt` | `time.Time` | when enrichment ran |
| `Status` | `string` | `enriched` \| `failed` \| `nothing-to-enrich` — drives retry/visibility (R5) |

**Non-identity (the load-bearing rule).** `GenerateID(content, mime, metadata)`
folds the **metadata map** into the document ID. `Enrichment` is a **separate
struct field**, NOT a `Metadata` entry, so it never enters the identity hash. The
ID is fixed once at ingest from the original metadata; populating `Enrichment`
after store leaves `Document.ID`, `ContentHash`, chunk IDs, and vectors untouched
→ re-add stays a no-op (FR-002/SC-005). This is the same discipline
`SectionContext` uses (span data stripped before `GenerateID`).

**Serialization.** `json:"enrichment,omitempty"` — absent and "none" serialize
identically (nil sidecar = unenriched), so pre-feature documents load unchanged.

---

## 2. `Enricher` — the generation interface (NEW)

The document-level sibling of `embed.Embedder`, but for **generation** (R3):

```
Enrich(ctx, doc) (*EnrichInfo, error)
```

- Input: the document (its text — e.g. a concatenation/representative slice of its
  chunks, bounded).
- Output: an `EnrichInfo` (tags + summary) OR a typed error:
  - permanent failure (bad/unparseable output) → status `failed`;
  - nothing-to-enrich (empty/trivial doc) → status `nothing-to-enrich`;
  - transient (model unreachable, circuit open, ctx cancelled) → NOT marked
    permanent (retried later).
- v1 provider: a local-Ollama generation client (`/api/generate` or `/api/chat`
  over the existing loopback base URL).

The interface keeps `internal/index` and the pipeline free of generation details
(Principle V) — the pipeline orchestrates, the provider owns the model call.

---

## 3. The tag-filter bridge (the US1 payoff, one line)

The existing tag filter reads `Document.Metadata["tags"]` (manual/frontmatter).
The bridge extends the **tag-resolution helper** to also read
`Document.Enrichment.Tags`:

```
documentTags(doc) = Metadata["tags"]  ∪  (Enrichment?.Tags ?? ∅)
```

So `--tags` filtering consumes auto-tags with **no query-surface change** and
**no new transport field**. Manual tags and auto-tags merge; both are optional
and independently absent.

---

## 4. Lifecycle / state transitions

```text
Ingest (processFile):  read → dedup → chunk → store(Sync,<10ms) → ACK
                                                                    │
processJob (async):    embed + BM25 index + near-dup cluster       │
                       └─ if Enricher bound (config on):           ▼
                            Enrich(ctx, doc)
                              ├─ ok            → Enrichment{Status:"enriched", Tags, Summary, Model, at}
                              ├─ nothing       → Enrichment{Status:"nothing-to-enrich"}
                              ├─ permanent err → Enrichment{Status:"failed"}      (no infinite retry)
                              └─ transient err → Enrichment left nil this pass    (retried on back-fill/next)
Back-fill:             re-enrich pass over docs with Enrichment==nil||failed (mirrors Reprocess)
```

No new state machine on `Document.Status` (that stays `pending|embedded|error`);
enrichment state lives on `Enrichment.Status`. The worker never blocks the ACK
(it's async) and never loops (failed/nothing docs are terminal).

---

## 5. Validation rules (from requirements)

- **FR-002 → identity:** `Enrichment` is a struct field, not a `Metadata` key;
  `GenerateID` is unchanged; a test asserts doc/chunk IDs + content hash are
  byte-identical with enrichment on vs off.
- **FR-003 → bridge:** a doc with `Enrichment.Tags={"security"}` is returned by
  `--tags security` even with empty `Metadata["tags"]`.
- **FR-004 → async:** the <10 ms ACK baseline is unchanged with enrichment on.
- **FR-007 → graceful:** a doc whose enrichment failed/nil still ingests + queries;
  `Enrichment.Status` is surfaced, not an error.
- **FR-009 → resilience:** the circuit breaker opens after consecutive model
  failures (5/30 s defaults); permanently-failed docs carry `Status:"failed"` and
  are not retried indefinitely.
- **FR-010 → surface:** `Summary` + `Enrichment.Status` appear on status/hits
  across all four transports, omitted when absent.
