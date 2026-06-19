# Data Model: Local RAG Database (go-rag v1)

**Phase**: 1 (Design) | **Date**: 2026-06-19 | **Source**: PRD §6, §7

All state lives in one Pebble instance, key-space partitioned by single-byte
prefixes. Entity chain: **Source 1:N Document 1:N Chunk 1:1 Embedding**.

## Entities

### Source
A watched directory or file collection.

| Field | Type | Notes |
|-------|------|-------|
| `ID` | string | SHA-256 of the canonical absolute path |
| `Path` | string | Absolute directory path |
| `Kind` | enum | `directory` \| `file` |
| `AddedAt` | timestamp | |
| `UpdatedAt` | timestamp | |

**Relationships**: 1:N → Document. **Pebble key**: `0x01 | source_id`.

### Document
A single ingested file, content-addressed.

| Field | Type | Notes |
|-------|------|-------|
| `ID` | string | SHA-256(content + canonical metadata) — canonical identity |
| `SourceID` | string | FK → Source |
| `FilePath` | string | Relative path from source root |
| `FileName` | string | Base filename |
| `FileType` | enum | `pdf` \| `text` \| `markdown` \| `docx` \| `jpeg` \| `png` |
| `MimeType` | string | e.g. `application/pdf` |
| `ContentHash` | string | SHA-256 of raw file bytes (change detection — distinct from ID) |
| `Metadata` | map | File-specific (title, author, page_count, headings…) |
| `ChunkCount` | int | Derived |
| `FileSize` | int64 | Bytes |
| `IngestedAt` | timestamp | |
| `UpdatedAt` | timestamp | |
| `Status` | enum | `pending` \| `embedded` \| `error` (see state machine) |

**Relationships**: N:1 Source; 1:N Chunk. **Validation**: `FileType` ∈ supported set;
`ContentHash` non-empty. **Pebble key**: `0x02 | document_id`; secondary
`0x0A | source_id | document_id` (list docs by source); `0x0C | file_path` (path→doc
lookup); `0x0D | content_hash` (dedup index).

### Chunk
A text segment split from a Document; the unit of retrieval.

| Field | Type | Notes |
|-------|------|-------|
| `ID` | string | SHA-256(chunk text + metadata) |
| `DocumentID` | string | FK → Document |
| `Content` | string | The chunk text |
| `ChunkIndex` | int | 0-based position |
| `TotalChunks` | int | Parent's chunk count |
| `StartCharIdx` | int | Char offset in original text |
| `EndCharIdx` | int | Char offset |
| `PageNumber` | int | PDF only; 0 otherwise |
| `PreviousChunkID` | string | Linked list (empty if first) |
| `NextChunkID` | string | Linked list (empty if last) |
| `TokenCount` | int | Heuristic estimate |
| `CreatedAt` | timestamp | |

**Relationships**: N:1 Document; 1:1 Embedding. **Validation**: `0 ≤ ChunkIndex <
TotalChunks`; linked-list integrity (prev/next consistent). **Pebble key**:
`0x03 | chunk_id`; ordered secondary `0x0B | document_id | chunk_index` → chunk_id.

### Embedding
A vector for a Chunk.

| Field | Type | Notes |
|-------|------|-------|
| `ChunkID` | string | FK → Chunk (1:1) |
| `Model` | string | Embedding model name |
| `Dimensions` | int | Vector width |
| `Vector` | []float32 | Held in chromem-go, not Pebble |
| `CreatedAt` | timestamp | |

**Validation**: `len(Vector) == Dimensions`. **Pebble key**: `0x04 | chunk_id`
(metadata only; the vector itself lives in chromem-go).

## Key-Space Schema (PRD §6.7)

| Prefix | Data |
|--------|------|
| `0x01` | Source records |
| `0x02` | Document records |
| `0x03` | Chunk records |
| `0x04` | Embedding metadata |
| `0x05`–`0x08` | BM25 inverted index (term → [(chunk_id, positions, field_weight)]) |
| `0x09` | Config key/value |
| `0x0A` | Source → Document index |
| `0x0B` | Document → Chunks ordered index |
| `0x0C` | File path → Document ID |
| `0x0D` | Content hash → Document ID (dedup) |
| `0x0E` | Change-detection state |
| `0x0F` | Idempotency receipts |

## State Machines

### Document.Status
```
pending ──(chunks stored)──► embedded
   │                            │
   │ └─(embed error)─► error ◄──┘ (retry returns to pending)
```

### Change Detection (PRD §7.3)
```
            [file not in DB]
                UNKNOWN
                   │  fsnotify CREATE / polling discovers file
                   ▼
      NEW ──(ingest + embed)──► TRACKED
                                  │
        ┌─────────────────────────┼─────────────────────────┐
        │ hash changed            │ hash same               │ file deleted
        ▼                         ▼                         ▼
    MODIFIED                   SKIP                     DELETED
   (re-ingest:               (no-op)              (remove chunks,
    del old chunks,                                 embeddings, FTS)
    new chunks,                                          │
    re-embed)                                           ▼
        │                                           [gone]
        ▼
    TRACKED
```

## Data Invariants

- **Uniqueness**: `(FilePath)` → exactly one Document (prefix `0x0C`); `ContentHash`
  → at most one active Document (prefix `0x0D`, enables idempotent ingest).
- **Referential integrity**: every Chunk.DocumentID and Embedding.ChunkID resolves to
  an existing row; deletions cascade.
- **Consistency on crash**: Pebble `Sync` + WAL — a SIGKILL never loses an
  acknowledged write; un-acked async indexing is replayable from stored chunks.
- **Single writer**: exactly one process holds the Pebble lock; concurrent readers use
  snapshots (eventual consistency per research Q6).
