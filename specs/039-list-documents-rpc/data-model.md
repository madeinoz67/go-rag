# Data Model — ListDocuments (BL-007)

> Phase 1 output for `/speckit-plan`. No new persisted entities — the feature is a read-only projection over existing document records. See [research.md](./research.md) (R1–R4) for the pagination-encoding + `ingested_at`-reliability decisions, and [contracts/api.md](./contracts/api.md) for the wire shape.

## Entities (engine projection, new — not persisted)

`internal/engine/` — the engine-level request/result of `ListDocuments`.

```go
// ListDocumentsRequest is the engine-level filter + page request (spec 039).
// All fields optional except the implied "list this vault's documents".
type ListDocumentsRequest struct {
	PageSize  int    // default 50, max 200; 0 → default; <1 or >200 → ErrInvalid
	PageToken string // opaque; empty → first page
	After     string // RFC3339; only docs with ingested_at > After; "" → unbounded below
	Status    string // "embedded"|"pending"|"error"|"" (all); AND with After
}

// ListDocumentsResult is a page of documents + the cursor for the next page
// (empty NextPageToken ⇒ last page). Documents are in (ingested_at ASC, id ASC).
type ListDocumentsResult struct {
	Documents    []model.Document
	NextPageToken string // opaque; empty when this is the last page
}
```

**Validation + pagination invariants**

| Property | Rule |
|----------|------|
| Ordering | `(ingested_at ASC, document_id ASC)` — a total order (tie-broken by id) |
| `after` | RFC3339; keep only docs with `ingested_at > after`; empty/omitted → all |
| `status` | exact match on `Document.Status` (`pending`\|`embedded`\|`error`); empty → all; AND with `after` |
| `page_size` | default 50 (when 0 or omitted); `< 1` or `> 200` → `ErrInvalid` |
| `page_token` | opaque; empty → first page; malformed → `ErrInvalid`; carries only the resume point (NOT the filter — the client re-sends `after`/`status`/`page_size` each page) |
| `next_page_token` | emitted iff more matching docs remain after this page; empty ⇒ last page |
| Empty result | empty `documents` + empty `next_page_token`; never an error |
| `ingested_at` reliability | every doc has a non-empty `ingested_at` (set at ingest; content-addressing mints fresh record on re-ingest) — affirmed, no migration (R2) |

## Resolution algorithm (engine, read-only)

```
validate page_size: if provided and (<1 or >200) → ErrInvalid
validate after:     if non-empty and not RFC3339 → ErrInvalid
validate status:    if non-empty and not in {pending, embedded, error} → ErrInvalid
decode page_token:  if non-empty, base64-decode → (resumeIngestedAt, resumeID); malformed → ErrInvalid

// 1. scan all documents (one PrefixScan over prefix 0x02)
var docs []model.Document
db.PrefixScan(storage.PrefixDocument, func(key, val []byte) bool {
    var d model.Document
    if json.Unmarshal(val, &d) == nil { docs = append(docs, d) }
    return true
})

// 2. filter (after + status, AND)
filter := docs[:0]  // in-place
for _, d := range docs {
    if after != "" && !d.IngestedAt.After(parseRFC3339(after)) { continue }
    if status != "" && d.Status != status { continue }
    filter = append(filter, d)
}

// 3. order by (ingested_at ASC, id ASC)
sort.SliceStable(filter, func(i, j int) bool {
    if filter[i].IngestedAt.Equal(filter[j].IngestedAt) { return filter[i].ID < filter[j].ID }
    return filter[i].IngestedAt.Before(filter[j].IngestedAt)
})

// 4. skip-to-resume-point (page_token), then take page_size
start := 0
if page_token != "" { start = indexAfter(filter, resumeIngestedAt, resumeID) } // first doc strictly after (resumeIngestedAt, resumeID)
end := min(start + pageSize, len(filter))
page := filter[start:end]

// 5. emit next_page_token iff more remain
var next string
if end < len(filter) {
    last := page[len(page)-1]
    next = encodePageToken(last.IngestedAt, last.ID) // base64("<rfc3339nano>\x1f<id>")
}
return &ListDocumentsResult{Documents: page, NextPageToken: next}, nil
```

**Cost**: one `PrefixScan` over prefix `0x02` (O(documents)) + in-memory filter + stable sort (O(n log n)) + slice. No scan per page beyond the single prefix scan (the scan re-runs per call, but each call is one logical read; the `after` cursor bounds the meaningful working set for incremental polls). No write, no index.

**Page-token codec**: `encodePageToken(t time.Time, id string) string` → base64-url-no-pad of `t.UTC().Format(time.RFC3339Nano) + "\x1f" + id`. `decodePageToken(s) (time.Time, string, error)` reverses it; any decode/format error → `ErrInvalid`. The codec is a pure function in the engine package; the token is **never persisted**.

## Reused entities (unchanged)

- **Document** (`internal/model`) — the ingested-file record (id, source/file metadata, content hash, `IngestedAt`, `UpdatedAt`, `Status`, `Enrichment`). Read as-is by the scan; unchanged by this feature.
- **DocumentMeta** (proto) + the `toDocumentMeta*` projections (gRPC `toDocumentMetaPB`, REST `toDocumentMetaDTO`, CLI `toDocumentOut`) — the spec-035 transport projection. `ListDocuments` reuses them verbatim per result; no new projection code.

## Identity & storage invariants (constitution Principle II)

- **No new stored state.** `ListDocuments` is a pure read over prefix `0x02`. It introduces no new key, no new prefix, no new persisted struct. The `page_token` is an in-memory artefact (a base64 string the client round-trips).
- **On-disk layout**: unchanged. No migration; `migrate.ExpectedVersion` unchanged (R2 verified `ingested_at` is reliable by construction).
- **Identity**: read-only — no `document_id` is created or changed.

## Validation rules (map to FRs)

- **FR-001/FR-002**: `ListDocuments(page_size, page_token, after, status)` returns a page + `next_page_token`; ordered by `(ingested_at, id)`.
- **FR-003**: `after` (RFC3339) → only `ingested_at > after`; empty → all.
- **FR-004**: `status` exact-match filter; AND with `after`.
- **FR-005**: `page_size` default 50; `<1` or `>200` → `ErrInvalid`.
- **FR-006/FR-007**: opaque `page_token`; empty `next_page_token` at end; pagination composes with `after`+`status`.
- **FR-008**: every returned doc carries full `DocumentMeta` (reuses spec-035 projection).
- **FR-009/FR-010**: `ingested_at` reliability affirmed (R2).
- **FR-011/FR-012/FR-016**: all four transports identical; REST `GET /v1/documents`; no `vault` field.
- **FR-013**: single logical read (one PrefixScan + in-memory filter/sort/paginate).
- **FR-014/FR-015**: pure read, no migration; pure Go, no new deps.
