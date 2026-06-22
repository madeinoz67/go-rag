# Contract: Context Window (cross-transport, H15)

> The context window is part of the query request + response, exposed on all four transports.

## Request

| Transport | Field | Type | Default |
|-----------|-------|------|---------|
| CLI | `--context-window N` | int | 0 (off) |
| REST | `"context_window": N` | int | 0 |
| gRPC | `int32 context_window = 10` | int32 | 0 |
| MCP | `"context_window": {"type":"integer","default":0}` | int | 0 |

## Response — context on each hit

Each `QueryHit` gains an optional `context` field:

```json
{
  "chunk_id": "...",
  "content": "the hit text...",
  "context": [
    {"chunk_id": "prev1", "content": "previous chunk text", "direction": "previous"},
    {"chunk_id": "next1", "content": "next chunk text", "direction": "next"}
  ]
}
```

- `context` is `null`/omitted when `context_window` is 0 (default — byte-identical to today).
- Context chunks are NOT ranked hits — they are siblings for reading context.
- Up to N previous + N next siblings per hit; fewer at document boundaries.

## Semantics

- **Opt-in**: `context_window=0` = no context (today's behavior).
- **After ranking**: context is expanded post-ranking/rerank; does not affect top-k or ranking.
- **Graceful boundaries**: first/last chunks → only available siblings; empty linked-list IDs → no context for that direction.
