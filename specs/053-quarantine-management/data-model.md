# Data Model — Quarantine Management View

**Feature**: specs/053-quarantine-management | **Date**: 2026-07-13

## Route table (new — UI transport)

| Method | Path | Auth | Body/Query | Returns | Maps to |
|--------|------|------|------------|---------|---------|
| GET | `/api/quarantine/list` | `Server.guard` | `?vault=default` | `quarantineListDTO` | `Engine.ListPoisoned` |
| POST | `/api/quarantine/{id}/release` | `Server.guard` | `?vault=default` | 204 | `Engine.ReleaseChunk` |
| POST | `/api/quarantine/{id}/reset` | `Server.guard` | `?vault=default` | 204 | `Engine.ResetChunk` |
| POST | `/api/quarantine/rescan` | `Server.guard` | `?vault=default` | 204 | `Engine.RescanPoisoning` |

All guarded. Vault from query param (spec 052). Release/reset/rescan are confirmed client-side (R4).

## DTOs

**quarantineListDTO**:
```json
{
  "chunks": [{
    "chunk_id": "...",
    "document_id": "...",
    "preview": "...(160 chars)...",
    "verdict": {
      "level": "quarantine",
      "score": 0.72,
      "signals": { "repetition": 0.5, "stuffing": 0.8, "instruction": 0.3 },
      "matched_phrases": ["ignore all previous", "system prompt"]
    }
  }],
  "count": 1
}
```

**Detail** (fetched via the existing `GET /api/documents/{docID}/chunks/{chunkID}` or a new
`GET /api/quarantine/{id}/detail`): the full chunk `content` + the `verdict` (from ListPoisoned
or the chunk record). The Alpine view overlays matched-phrase highlights on the content.

Release/reset/rescan return **204 No Content** (success) or **404** (unknown chunk).
