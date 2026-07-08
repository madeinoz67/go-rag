# Data Model — go-rag UI Console, Documents View (Slice 1)

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md) · **Contract**: [contracts/ui-documents.md](./contracts/ui-documents.md)

## Persistence: none new

Like the Slice 0 Dashboard, the Documents view is **stateless presentation**. It owns no
Pebble key-space, adds no prefixes, ships no migration. It reads existing prefixes:
- `0x02` documents (`model.Document`) — via `engine.ListDocuments` (spec 039) and the
  document meta carried by `engine.GetChunk`.
- `0x03` chunks (`model.Chunk`) — via the new `engine.ListChunks` (R1), `engine.GetChunk`
  (035), `engine.GetChunkContext` (037).

**Constitution Storage Discipline**: no on-disk schema change → no migration, no
`ExpectedVersion` bump, no PRD §6.7 update.

## Source of truth the view reads

| Need | Engine call | Spec |
|------|-------------|------|
| Paginated document list (status/tag filter) | `Engine.ListDocuments(ListDocumentsRequest{PageSize, PageToken, After, Status, Tags})` | 039 (+ R3 `Tags`) |
| One document's full metadata (+ source) | carried by `Engine.GetChunk`'s `Document` field; or resolved from the list row | 035 |
| A document's chunks (paginated) | `Engine.ListChunks(documentID, ListChunksRequest{PageSize, PageToken})` | **047 R1 (new)** |
| One chunk + its document | `Engine.GetChunk(chunkID)` | 035 |
| A chunk's neighbours (section context window) | `Engine.GetChunkContext(chunkID, window)` | 037 |
| Content search (docs by chunk content) | `Engine.Query(ctx, QueryRequest)` → project `QueryHit[]` to distinct docs | 012/etc. (R2) |

## Entities (wire-only DTOs — mirror the existing cross-transport shapes, R8)

The UI defines mirror structs; they are byte-identical to `rest.documentMetaDTO` /
`rest.chunkDTO` / the CLI projections / proto messages, pinned by a parity test (R8).

### `documentDTO` — one list row / detail header

Projection of `model.Document` (+ spec 029 `EnrichInfo` flattened). Identical field set
to `rest.documentMetaDTO`:

| Field | Type | Source | Meaning |
|-------|------|--------|---------|
| `id` | string | `Document.ID` | identity hash (SHA-256 content+metadata) |
| `content_hash` | string | `Document.ContentHash` | distinct change-detection hash |
| `source_id` | string | `Document.SourceID` | FK → Source |
| `source_path` | string | resolved `Source.Path` | **empty in list rows** (listing skips source resolution for perf); resolved in the detail view |
| `file_path` | string | `Document.FilePath` | relative path from source root |
| `file_name` | string | `Document.FileName` |  |
| `file_type` | string | `Document.FileType` | pdf\|text\|markdown\|docx\|jpeg\|png |
| `mime_type` | string | `Document.MimeType` |  |
| `chunk_count` | int | `Document.ChunkCount` |  |
| `file_size` | int64 | `Document.FileSize` |  |
| `status` | string | `Document.Status` | `pending`\|`embedded`\|`error` (embedding state) |
| `ingested_at` | string | `Document.IngestedAt` | RFC3339 UTC (cursor key) |
| `updated_at` | string | `Document.UpdatedAt` | RFC3339 UTC |
| `tags` | []string | `EnrichInfo.Tags` | spec 029 (omit when empty) |
| `summary` | string | `EnrichInfo.Summary` | spec 029 (omit when empty) |
| `enrichment_status` | string | `EnrichInfo.Status` | `enriched`\|`failed`\|`nothing-to-enrich` (omit when absent) |
| `enrichment_model` | string | `EnrichInfo.Model` | (omit when absent) |
| `enrichment_at` | string | `EnrichInfo.GeneratedAt` | RFC3339 UTC (omit when absent) |

**Derived UI badge** (client-side): embedding health = `status=="embedded"`; enrichment
present = `enrichment_status=="enriched"`. Empty/absent enrichment fields render an empty
state, never an error (FR-012).

### `chunkDTO` — one chunk in the detail view

Identical field set to `rest.chunkDTO`:

| Field | Type | Source | Meaning |
|-------|------|--------|---------|
| `chunk_id` | string | `Chunk.ID` |  |
| `document_id` | string | `Chunk.DocumentID` |  |
| `content` | string | `Chunk.Content` | the chunk text |
| `chunk_index` | int | `Chunk.ChunkIndex` | 0-based ordinal (sort key) |
| `total_chunks` | int | `Chunk.TotalChunks` |  |
| `page_number` | int | `Chunk.PageNumber` | PDF only, 0 otherwise |
| `start_char` / `end_char` | int | `Chunk.StartCharIdx` / `EndCharIdx` |  |
| `token_count` | int | `Chunk.TokenCount` |  |
| `previous_chunk_id` / `next_chunk_id` | string | linked list | omit when empty |
| `section_context` | []string | `Chunk.SectionContext` | heading breadcrumb (specs 025/037) |
| `section_depth` | int | `Chunk.SectionLevel` | governing heading level 1-6 (spec 041) |
| `extraction_quality` / `extraction_method` | float64 / string | spec 042 | omit when default |
| `wikilinks` | []string | spec 036 | omit when empty |
| `kind` | string | spec 031 (`"caption"`) | omit when empty |
| `created_at` | string | `Chunk.CreatedAt` | RFC3339 UTC |

(Poisoning / NearDup sidecars are omitted from the Slice-1 chunk projection — they belong
to the Query/security surfaces; can be added when a later slice needs them.)

### New engine request/result (R1)

```go
// ListChunksRequest — paginated chunk listing for one document (spec 047 R1).
type ListChunksRequest struct {
    PageSize  int    // default 50, max 200 (mirrors ListDocuments)
    PageToken string // opaque resume cursor over (chunk_index ASC, chunk_id ASC)
}
type ListChunksResult struct {
    Chunks        []model.Chunk
    NextPageToken string // empty ⇒ last page
}
// Engine.ListChunks returns a page of the document's chunks ordered by chunk_index ASC.
func (e *Engine) ListChunks(documentID string, req ListChunksRequest) (*ListChunksResult, error)
```

### Additive filter on an existing request (R3)

```go
// ListDocumentsRequest gains an optional Tags filter (match-any). Empty = all docs.
type ListDocumentsRequest struct {
    PageSize  int
    PageToken string
    After     string   // RFC3339
    Status    string   // embedded|pending|error|""
    Tags      []string // NEW (spec 047 R3) — match-any; "" / nil = all
}
```

## Route table (authoritative in contracts/ui-documents.md)

| Method+Path | Auth | Handler → engine call |
|---|---|---|
| `GET /api/documents` | guard | `handleDocumentsList` → `eng.ListDocuments` |
| `GET /api/documents/{id}` | guard | `handleDocumentDetail` → resolve doc (+ source) |
| `GET /api/documents/{id}/chunks` | guard | `handleDocumentChunks` → `eng.ListChunks` |
| `GET /api/documents/{id}/chunks/{chunkID}/context` | guard | `handleChunkContext` → `eng.GetChunkContext` |
| `GET /api/documents/search` | guard | `handleDocumentsSearch` → `eng.Query` → distinct docs |

(All `/api/*` guarded by the spec 045/046 `s.guard`; the shell `/` and `/static/*` and
`POST /login` stay public, unchanged from spec 046.)

## State transitions

None. Slice 1 is strictly read-only. The only client-side state is the auth token (already
managed by the shell) and view-local UI state (current page token, selected document,
active filters). No server-side mutation.

## Migrations

**None.** No storage change. (Constitution migration-contiguity rule unaffected.)
