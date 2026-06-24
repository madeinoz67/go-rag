# Contract — `section_context` on the QueryHit (spec 025, audit H23)

> Phase 1 output for `/speckit-plan`. The external interface this feature
> exposes is **one new field on the query hit, surfaced identically on every
> transport**. This file is the wire contract; the canonical type lives on
> `engine.QueryHit` (`internal/engine/types.go`) and `model.Chunk`
> (`internal/model/model.go`). See `data-model.md` for entity detail and
> `research.md` (R6) for the four-projection rationale.

## Field contract

| Property | Value |
|----------|-------|
| Name | `section_context` |
| Type | ordered list of strings (top-level heading → governing heading) |
| Ordering | document order, outermost heading first |
| Example | `["Operations", "Backups", "Retention"]` |
| Display (text renders) | elements joined by `" / "` → `Operations / Backups / Retention` |
| Absent when | source has no headings **or** chunk was ingested before this feature |
| Absent semantics | omitted (NOT `null`/empty array) — retrieval never errors (FR-006) |
| Parity | identical value across CLI, REST, gRPC, MCP for the same chunk (FR-004) |
| Determinism | a straddling chunk carries the heading at its **start** position (FR-007) |

The field is **additive** to every existing response. Pre-feature clients parsing
a response simply see a new key/field they ignore; no existing field's position,
type, or value changes. This mirrors how `poisoning` (spec 019) and `chunk_index`
(spec 021/023) were added.

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

`message QueryHit` (`proto/gorag.proto:77-86`) gains one field. `9` is the next
free tag after `chunk_index = 8`:

```proto
message QueryHit {
  string   chunk_id    = 1;
  string   document_id = 2;
  double   score       = 3;
  string   content     = 4;
  string   file_path   = 5;
  int32    page        = 6;
  Poisoning poisoning  = 7;
  int32    chunk_index = 8;
  repeated string section_context = 9; // H23/spec 025: heading breadcrumb at the chunk's start (absent = nil/empty)
}
```

`proto3` repeated-field semantics already give the absent/empty behaviour the
contract requires (an unset repeated field serialises to nothing on the wire;
clients receive an empty list, which transports render as "absent"). Regenerate
`proto/gen/gorag.pb.go` (`make` protoc step / `go generate`).

### REST — `internal/rest/types.go`

`type queryHit` (`rest/types.go:23-32`) gains one field, matching the
`poisoning` precedent's `omitempty` convention:

```go
type queryHit struct {
    ...existing fields...
    ChunkIndex    int            `json:"chunk_index"`          // :30
    Poisoning     *poisonVerdict `json:"poisoning,omitempty"`  // :31
    SectionContext []string      `json:"section_context,omitempty"` // H23/spec 025
}
```

`omitempty` on a slice omits the key when nil → absent, not `null` (FR-006). The
REST adapter maps `engine.QueryHit.SectionContext` → this field verbatim
(adapters carry no logic, `rest/types.go:1-5`).

### MCP — `internal/mcp/server.go`

The MCP hit render already projects every `engine.QueryHit` field. Add the
breadcrumb to both renderings the server produces:
- **structured/tool result:** include `section_context` as an array (or a
  pre-joined `" / "` string, consistent with how the server renders
  `chunk_index`/`poisoning` today).
- **text render:** one line per hit, e.g. `📄 [score] section: Operations / Backups / Retention`.

### CLI — `internal/cli/query.go`

`renderResults` (`cli/query.go:123`) renders each hit. Add the breadcrumb to the
human text format (a `Section:` line, omitted when absent) and to the
machine-readable format (JSON/text output gets a `section_context` field/column),
mirroring how `chunk_index` (spec 023) and `poisoning` are rendered.

---

## Parity requirement (FR-004 / SC-002)

A single chunk retrieved over REST, gRPC, and MCP (and rendered by the CLI) MUST
report a byte-identical `section_context` value. This is enforced by routing all
transports through one `engine.QueryHit` and is **asserted by the existing
cross-transport parity suite** (`internal/engine/parity_test.go`). Tasks for this
spec extend `parity_test.go` to:
- include a Markdown fixture with nested headings in the parity corpus, and
- assert `section_context` is equal across REST/gRPC/MCP (and absent-equal for a
  heading-less fixture).

This closes SC-002 ("zero additional user actions … identical across all three
transports").

---

## Non-goals (explicit, to bound the contract)

- `section_context` is **not queryable/filterable** in v1 — it is a display/cite
  attribute on the hit, not a retrieval filter (no `Filter` dimension, unlike
  source/type/tags at `engine/types.go:23`). A metadata-filter integration is
  future work.
- The contract exposes only the **resolved breadcrumb**, never the raw heading
  list or byte offsets (those are internal — `data-model.md` §2).
- `section_context` does **not** appear on `ContextChunk` (sibling context,
  `engine/types.go:46-50`) in v1; only on the ranked hit. (Siblings could carry
  their own context later; out of scope here.)
