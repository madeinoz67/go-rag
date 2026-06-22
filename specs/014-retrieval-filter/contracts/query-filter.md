# Contract: Query Filter (cross-transport, H14)

> The filter is part of the query operation's request, exposed identically on CLI, REST,
> gRPC, and MCP. This file pins the contract so a caller on any transport can scope a query.

## The filter dimensions

| Dimension | Field name | Type | Match | Empty = |
|-----------|-----------|------|-------|---------|
| Source | `source` / `--source` | string (glob) | `path.Match` against `Document.FilePath` | no constraint |
| Type | `type` / `--type` | string | exact, case-insensitive, against `Document.FileType` | no constraint |
| Tags | `tags` / `--tags` | []string (comma-separated on CLI) | conjunction (ALL) against `Document.Metadata["tags"]` | no constraint |

## Semantics

- **Conjunction**: a document must match EVERY specified dimension (source AND type AND tags).
  Unspecified dimensions are ignored (no constraint).
- **Opt-in**: if NO dimension is specified (all empty), the filter is a no-op — the query
  behaves identically to today (every document eligible). This is `Filter.Empty()`.
- **Matches-nothing**: if the filter matches no document, the result set is empty (not an error).
- **Pre-fusion**: the filter is applied to the FTS and Vector candidate lists BEFORE RRF
  fusion, collapse-by-doc, and rerank — so no non-matching chunk reaches scoring/ranking.
- **All modes**: the filter applies in keyword, semantic, and hybrid modes.

## Transport surface

### CLI

```bash
go-rag query "<q>" --source "docs/**" --type ".md" --tags "security,ops"
```

- `--source` (existing, currently dead — H14 wires it): glob pattern.
- `--type` (new): exact file type (e.g., `.md`, `pdf`).
- `--tags` (new): comma-separated tag list (conjunction).

### REST

`POST /v1/query` gains optional fields:

```json
{"query": "...", "source": "docs/**", "type": ".md", "tags": ["security","ops"]}
```

### gRPC

`QueryRequest` proto gains fields:

```proto
string source = 7;          // path glob (empty = no constraint)
string type = 8;            // file type (empty = no constraint)
repeated string tags = 9;   // conjunction (empty = no constraint)
```

### MCP

`go_rag_query` inputSchema gains:

```json
{"source": {"type": "string"}, "type": {"type": "string"}, "tags": {"type": "array", "items": {"type": "string"}}}
```

## Parity guarantee

A query with the same filter values over CLI, REST, gRPC, and MCP returns identical ranked
results (the filter runs in the shared `Engine.Query` path). Verified by a cross-transport
parity test.
