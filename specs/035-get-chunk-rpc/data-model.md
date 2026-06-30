# Data Model — GetChunk RPC (spec 035)

> Phase 1 output of `/speckit-plan`. Defines the response entities `GetChunk`
> exposes. **`GetChunk` introduces no new stored entities** — it is a read that
> projects go-rag's existing `model.Chunk` + `model.Document` (+ optional
> `model.Source`) into transport messages. Field-level provenance is grounded in
> `internal/model/model.go` (verified by the research workflow).

## Entity overview

| Entity | Kind | Provenance | New on the wire? |
|---|---|---|---|
| `Chunk` | response message | projection of `model.Chunk` (`model.go:81-128`) | **yes** (wrapper; reuses existing `Poisoning` / `NearDup` / `PoisoningSignals` proto messages) |
| `DocumentMeta` | response message | projection of `model.Document` (`model.go:24-49`) + `*EnrichInfo` (spec 029) | **yes** (no existing full-Document projection) |
| `chunk_id` | foreign key (input) | `model.Chunk.ID` — SHA-256-derived content-addressed identity (Constitution II) | no — the existing identity `GetChunk` makes resolvable |
| `Poisoning` | nested message | 1:1 mirror of `model.PoisonVerdict` (`verdict.go`) | **no — reuse** `proto/gorag.proto:101` |
| `NearDup` | nested message | 1:1 mirror of `model.NearDupInfo` | **no — reuse** `proto/gorag.proto:96` |
| `PoisoningSignals` | nested message | 1:1 mirror of `model.PoisonSignals` | **no — reuse** `proto/gorag.proto:112` |

Projection precedent: `QueryHit` (`internal/engine/types.go:55-90`,
`proto/gorag.proto:80-94`) is the canonical "chunk + document-context" template;
`Chunk`/`DocumentMeta` are a strict superset that splits the join explicitly
rather than flattening it into one ranked-hit row.

---

## Entity: `Chunk`

The unit of retrieved text, identified by its content-addressed `chunk_id`.

| Field | Proto type | Source (`model.Chunk`) | Notes |
|---|---|---|---|
| `chunk_id` | `string` | `.ID` | SHA-256-derived content-addressed identity (Constitution II). The resolvable foreign key. |
| `document_id` | `string` | `.DocumentID` | FK → `DocumentMeta.id`. Carried inline on every stored chunk. |
| `content` | `string` | `.Content` | Full chunk text, verbatim. No truncation (FR-004). |
| `chunk_index` | `int32` | `.ChunkIndex` | 0-based ordinal within the source document. |
| `total_chunks` | `int32` | `.TotalChunks` | Document chunk count (for position context). |
| `page_number` | `int32` | `.PageNumber` | PDF only; **0 = not paginated** (mirrors `QueryHit.page`). |
| `start_char` | `int32` | `.StartCharIdx` | Char offset into the source text. |
| `end_char` | `int32` | `.EndCharIdx` | Char offset into the source text. |
| `token_count` | `int32` | `.TokenCount` | |
| `previous_chunk_id` | `string` | `.PreviousChunkID` | Linked-list sibling (enables `GetChunkContext`, BL-002). |
| `next_chunk_id` | `string` | `.NextChunkID` | Linked-list sibling. |
| `poisoning` | `Poisoning` | `.Poisoning *PoisonVerdict` | **Reuse** existing proto message. The poisoning verdict (query-time signal that travels with the chunk as metadata). |
| `section_context` | `repeated string` | `.SectionContext []string` | Heading breadcrumb (mirrors `QueryHit.section_context`). |
| `near_dup` | `NearDup` | `.NearDup *NearDupInfo` | **Reuse** existing proto message. |
| `kind` | `string` | `.Kind` | e.g. `"caption"`. New vs `QueryHit`; non-identity sidecar. |
| `created_at` | `string` | `.CreatedAt` | RFC3339. |

**Validation / invariants.**
- `chunk_id` is the SHA-256-derived content-addressed ID (Constitution II). An
  unchanged chunk keeps its ID; a changed chunk gets a new one — so a stale
  `chunk_id` after re-ingest simply resolves to not-found (FR-002), never to a
  different chunk.
- All sidecars (`poisoning`, `section_context`, `near_dup`, `kind`) are
  documented **non-identity** in `model.go` (never enter `GenerateID`), so
  exposing them changes nothing stored → Constitution storage-discipline holds.

**Deliberately omitted from v1 `Chunk`** (see `research.md` R2):
- `model.Chunk.Caption *CaptionInfo` — no existing proto message; add later if needed.
- `model.Embedding.Vector` — not in Pebble (`json:"-"`, chromem-go); never returned (PRD §6.5).

---

## Entity: `DocumentMeta`

The parent document's metadata, returned alongside the chunk so a caller has
full context in one round-trip (FR-005 / Story 2).

| Field | Proto type | Source | Notes |
|---|---|---|---|
| `id` | `string` | `model.Document.ID` | `GenerateID` — content + mimetype + metadata identity hash. |
| `content_hash` | `string` | `model.Document.ContentHash` | SHA-256 of raw bytes — **change-detection hash, distinct from `id`**. Powers PRD §7.2 idempotent re-add; must stay a separate field. |
| `source_id` | `string` | `model.Document.SourceID` | FK → `Source`. |
| `source_path` | `string` | `model.Source.Path` | Absolute source directory. *(optional 3rd point read, prefix `0x01` — see Open decision §below; constant-time, no scan.)* |
| `file_path` | `string` | `model.Document.FilePath` | Relative path from source root. Satisfies FR-005 "source file path" with no extra read. |
| `file_name` | `string` | `model.Document.FileName` | |
| `file_type` | `string` | `model.Document.FileType` | `pdf` \| `text` \| `markdown` \| `docx` \| `jpeg` \| `png`. |
| `mime_type` | `string` | `model.Document.MimeType` | |
| `chunk_count` | `int32` | `model.Document.ChunkCount` | |
| `file_size` | `int64` | `model.Document.FileSize` | |
| `status` | `string` | `model.Document.Status` | `pending` \| `embedded` \| `error`. |
| `ingested_at` | `string` | `model.Document.IngestedAt` | RFC3339. |
| `updated_at` | `string` | `model.Document.UpdatedAt` | RFC3339. |
| `tags` | `repeated string` | `.Enrichment.Tags` | Flattened from `*EnrichInfo` (spec 029); empty if absent. |
| `summary` | `string` | `.Enrichment.Summary` | Document summary (mirrors `QueryHit.summary`). |
| `enrichment_status` | `string` | `.Enrichment.Status` | `enriched` \| `failed` \| `nothing-to-enrich` (mirrors `QueryHit.enrichment_status`). |
| `enrichment_model` | `string` | `.Enrichment.Model` | |
| `enrichment_at` | `string` | `.Enrichment.GeneratedAt` | RFC3339. |

**Validation / invariants.**
- Enrichment is read off the **document**, not the chunk (the chunk carries no
  enrichment). When `Enrichment` is `nil`, the four enrichment fields are empty
  and `enrichment_status` is `""` — the caller treats absent cleanly.
- **Two distinct hashes** (`id` vs `content_hash`) are kept as separate wire
  fields. Collapsing them would lose the change-detection hash.

**Orphan-chunk edge.** If the chunk exists but its parent document was
deleted/stale (`GET #1` hits, `GET #2` misses), `GetChunk` **succeeds** with a
populated `Chunk` and an empty/zero `DocumentMeta` — matching `ListChunks`'
tolerant `FilePath=""` behaviour (`internal/eval/run.go:205-217`). The contract
is explicit in `contracts/get-chunk.md`.

**Deliberately omitted from v1 `DocumentMeta`** (see `research.md` R2):
- `model.Document.Metadata map[string]any` — lossy in proto3 (only
  `map<string,string>`); not identity, not on `QueryHit`. Caller can fetch separately.

---

## Reused nested messages (do **not** redefine)

These already exist in `proto/gorag.proto` and mirror their `model.*` counterparts
1:1. `GetChunk` reuses them verbatim (cross-transport parity, spec 003).

```protobuf
message Poisoning {                       // <- model.PoisonVerdict (verdict.go)
  string           level         = 1;     //   clean|suspicious|quarantine|released
  double           score         = 2;     //   [0,1]
  PoisoningSignals signals       = 3;
  repeated string  matched_phrases = 4;
}
message PoisoningSignals {               // <- model.PoisonSignals
  double repetition  = 1;
  double stuffing    = 2;
  double instruction = 3;
}
message NearDup {                         // <- model.NearDupInfo
  repeated string siblings  = 1;
  double          similarity = 2;
}
```

---

## Open implementer decision: `source_path` (3rd point read)

`file_path` (on `Document`, no extra read) already satisfies FR-005. `source_path`
(absolute, from `Source` at prefix `0x01`) is a bonus the bridge's
`ActivateWithRAG` benefits from and costs one constant-time Get.

- **Recommendation:** include `source_path` (3 reads total — still no scan,
  Constitution gate holds).
- **Minimal alternative:** surface `source_id` only and let the caller resolve.

Either choice keeps `GetChunk` constant-time; the difference is one extra
point Get and one extra response field.

---

## State transitions

**None.** `GetChunk` is a pure read. It creates, updates, and deletes nothing.
The on-disk schema version (`migrate.ExpectedVersion = 1`) is unchanged, and no
migration is added (see `research.md` R4).
