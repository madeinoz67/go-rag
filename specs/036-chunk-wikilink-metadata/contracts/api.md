# Contract — `wikilinks` on Chunk / QueryHit (spec 036, BL-004)

> Phase 1 output for `/speckit-plan`. The external interface this feature exposes is **one new `repeated string` field, surfaced identically on every transport**, on both the query hit and the fetched chunk. This file is the wire contract; the canonical types live on `engine.QueryHit` (`internal/engine/types.go`) and `model.Chunk` (`internal/model/model.go`). See [data-model.md](./data-model.md) for entity detail and [research.md](./research.md) (R7) for the projection rationale. Format mirrors spec 025's `contracts/api.md`.

## Field contract

| Property | Value |
|----------|-------|
| Name | `wikilinks` |
| Type | list of strings (canonicalised Obsidian wikilink targets) |
| Canonicalisation | `linkTarget` — alias (`\|`) and anchor (`#` / `#^block`) stripped → note name |
| Ordering | document order, first occurrence; de-duplicated |
| Example | `["authentication", "JWT tokens", "RBAC"]` |
| Included | `[[target]]`, `[[target\|display]]`, `[[target#heading]]` → `target` |
| Excluded | `![[image.png]]`, `![[Note]]` (embeds / transclusions) |
| Absent when | source has no wikilinks **or** chunk was ingested before this feature |
| Absent semantics | omitted in JSON (`omitempty`) / empty list on the gRPC wire — retrieval never errors (FR-008) |
| Parity | identical value across CLI, REST, gRPC, MCP for the same chunk (FR-009) |
| Scope | chunk-scoped: only links whose text falls in this chunk's range (FR-005) |

The field is **additive** to every existing response. Pre-feature clients see a new key/field they ignore; no existing field's position, type, or value changes. This mirrors how `section_context` (spec 025), `near_dup` (spec 026), and `poisoning` (spec 019) were added.

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

Two messages gain one field each. Both are the next free tag in their message.

**`message Chunk`** (`proto/gorag.proto:170`, defined by spec 035 for `GetChunkResponse`) — `created_at = 16` is the last used tag → **17**:

```proto
message Chunk {
  // ...fields 1–16 unchanged...
  string          created_at        = 16;
  repeated string wikilinks         = 17; // spec 036 / BL-004: [[wikilink]] targets in this chunk (absent = none/pre-feature)
}
```

**`message QueryHit`** (`proto/gorag.proto`) — `enrichment_status = 12` is the last used tag → **13**:

```proto
message QueryHit {
  // ...fields 1–12 unchanged...
  string          enrichment_status = 12;
  repeated string wikilinks         = 13; // spec 036 / BL-004: [[wikilink]] targets in this chunk (absent = none/pre-feature)
}
```

`proto3` repeated-field semantics already provide the absent/empty behaviour (an unset repeated field serialises to nothing on the wire; clients receive an empty list, rendered as "absent"). Regenerate `proto/gen/gorag.pb.go` (`go generate` / the repo's protoc step). Wire the model↔proto mapping in `internal/grpc`.

### REST — `internal/rest/types.go`

`type queryHit` (`rest/types.go:23`) gains one field, matching the `section_context` `omitempty` convention:

```go
type queryHit struct {
    // ...existing fields...
    SectionContext []string `json:"section_context,omitempty"` // spec 025
    Wikilinks      []string `json:"wikilinks,omitempty"`       // spec 036 / BL-004
}
```

The spec 035 `GET /v1/chunks/{id}` chunk response shape gains the same field so a fetched chunk carries its wikilinks identically to a query hit.

### Engine (canonical) — `internal/engine/types.go`

```go
type QueryHit struct {
    // ...existing fields...
    SectionContext []string
    Wikilinks      []string // spec 036 / BL-004
}
```

Populated in `internal/engine/query.go` beside the `SectionContext` copy: `hit.Wikilinks = chunk.Wikilinks`. The spec 035 `GetChunk` result projects `model.Chunk.Wikilinks` directly (it already carries every Chunk field).

### CLI — `internal/cli`

`renderResults` (`cli/query.go`) renders the wikilinks line per hit when non-empty; the spec 035 `go-rag chunk get` command renders them on the fetched chunk. Format suggestion: `wikilinks: authentication, JWT tokens, RBAC` (or omit the line when absent).

### MCP — `internal/mcp/server.go`

Include `wikilinks` in the structured query-hit payload and in the `go_rag_get_chunk` tool result (spec 035). MCP already projects every `QueryHit` field; omitting it here would make MCP the one transport that silently drops the field (spec 025 R6 rationale).

---

## Parity & determinism

- **Parity (FR-009):** `internal/engine/parity_test.go` is extended to assert `wikilinks` is byte-identical across CLI, REST, gRPC, and MCP for the same chunk (mirrors the `section_context` parity assertion, spec 025 SC-002).
- **Determinism (FR-006):** re-ingesting an unchanged chunk yields an identical `wikilinks` value — it is a pure function of chunk text + `linkTarget` + de-dup. Covered by a reader test.
- **Scope (FR-005):** each link appears on exactly one chunk (offset containment) — covered by a chunk-scope test on a multi-paragraph document.

## Backward compatibility

- Old chunk records (pre-feature) decode with `Wikilinks == nil` — Go JSON unmarshalling of a missing field yields a nil slice, not a parse error. Retrieval treats nil as absent.
- Old clients reading new responses see an additional field they ignore. No existing field's tag, type, or value changes.
- No on-disk key-space layout change; no migration; `migrate.ExpectedVersion` unchanged (research R5).
