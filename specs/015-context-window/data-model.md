# Data Model: Context Window (H15)

> No storage schema change. `Chunk.PreviousChunkID`/`NextChunkID` already exist (model.go:80-81);
> H15 populates them (pipeline) and uses them (engine). The result shape gains a `Context` field.

## Entities

### ContextWindow (request-state)

| Field | Type | Default | Semantics |
|-------|------|---------|-----------|
| `ContextWindow` | `int` | 0 (off) | N sibling chunks each side of a hit (previous + next) |

### ContextChunk (response — new)

```go
type ContextChunk struct {
    ChunkID   string
    Content   string
    Direction string // "previous" | "next"
}
```

### QueryHit (extended)

| Field | Type | When |
|-------|------|------|
| `Context` | `[]ContextChunk` | populated when ContextWindow > 0; nil when 0 |

### Chunk linked list (populated by H15)

`processFile` sets `chunks[i].PreviousChunkID`/`NextChunkID` after construction:
```text
chunks[0].NextChunkID = chunks[1].ID
chunks[i].PreviousChunkID = chunks[i-1].ID  (i > 0)
chunks[i].NextChunkID = chunks[i+1].ID      (i < len-1)
chunks[last].PreviousChunkID = chunks[last-1].ID
```

## Relationships

```text
QueryRequest.ContextWindow ──► Engine.Query
                                  │  after building []QueryHit (post-ranking)
                                  │  if ContextWindow > 0:
                                  │    for each hit:
                                  │      follow PreviousChunkID chain N steps → fetch siblings
                                  │      follow NextChunkID chain N steps → fetch siblings
                                  │      attach as QueryHit.Context []ContextChunk
                                  ▼
                              QueryHit { Content (hit), Context [{Content, Direction}, ...] }
```

## Validation rules

- **FR-001**: ContextWindow option on query (default 0).
- **FR-002**: each hit includes up to N prev + N next siblings.
- **FR-003**: pipeline populates Previous/Next.
- **FR-004**: context distinguishable from hit (separate field).
- **FR-005**: ContextWindow=0 → byte-identical to today.
- **FR-006**: missing siblings → graceful (only available included).
- **FR-007**: context after ranking (doesn't affect top-k/ranking).
- **FR-008**: all four transports.
