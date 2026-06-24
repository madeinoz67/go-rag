# Contract — `near_dup` on the QueryHit + `dedup` query flag (spec 026, audit H20)

> Phase 1 output for `/speckit-plan`. The external surfaces this feature exposes:
> (1) a new **`near_dup` field on the query hit**, surfaced identically on every
> transport; (2) an opt-in **`dedup` query flag** that collapses near-duplicate
> hits; (3) **near-duplicate counts on status**. The canonical types live on
> `engine.QueryHit` (`internal/engine/types.go`) and `model.Chunk`
> (`internal/model/model.go`). See `data-model.md` for entity detail and
> `research.md` (R7/R8) for collapse semantics.

## Field contract — `near_dup` on the hit

| Property | Value |
|----------|-------|
| Name | `near_dup` |
| Type | object: `{ siblings: [string], similarity: number }` |
| `siblings` | chunkIDs of this chunk's near-duplicates (pairwise, within the configured Hamming threshold) |
| `similarity` | closest sibling's normalised similarity, `[0,1]` |
| Absent when | the chunk has no near-duplicates **or** was ingested before the feature |
| Absent semantics | omitted (NOT `null`/empty) — never an error (FR-008) |
| Parity | identical value across CLI, REST, gRPC, MCP for the same chunk (FR-004) |
| Determinism | siblings are pairwise (no transitivity); a straddling/borderline pair follows the configured threshold (FR-006/009) |

The field is **additive** to every existing response (pre-feature clients ignore
it). This mirrors how `poisoning` (spec 019), `chunk_index` (spec 021), and
`section_context` (spec 025) were added.

## Field contract — `dedup` query flag

| Property | Value |
|----------|-------|
| Name | `dedup` |
| Type | boolean |
| Default | `false` (flag-only; results unchanged unless opted in — US1-scenario-3) |
| Effect | when `true`, near-duplicate hits are collapsed: one representative (highest-scored) per near-dup group survives into the top-k |
| Scope | post-ranking, purely subtractive — does not change retrieval, scores, or ranking (FR-007) |
| Parity | honoured identically on CLI, REST, gRPC, MCP (FR-005) |

## Field contract — status counts

| Property | Value |
|----------|-------|
| Name | `near_dup_chunks` |
| Type | integer |
| Meaning | count of chunks that have ≥1 near-duplicate sibling (clustered so far) |
| Semantics | eventually consistent (clusters land async-after-ACK); reflects the current corpus state |

---

## Per-transport projection

### gRPC / protobuf — `proto/gorag.proto`

`message QueryHit` (`proto/gorag.proto:77`, currently through `section_context = 9`)
gains one field. `10` is the next free tag:

```proto
message QueryHit {
  ...existing fields through...
  repeated string section_context = 9; // H23/spec 025
  NearDup near_dup = 10;               // H20/spec 026: near-duplicate context (nil = none/pre-feature)
}
message NearDup {
  repeated string siblings = 1; // chunkIDs within the Hamming threshold
  double similarity = 2;        // closest sibling, [0,1]
}
```

`message QueryRequest` gains the dedup flag (next free tag after `pool_size = 13`
→ `14`):

```proto
bool dedup = 14; // H20/spec 026: collapse near-duplicate hits (default false)
```

`message StatusResponse` gains the count (next free tag). Regenerate
`proto/gen/gorag.pb.go` (`protoc --go_out=. --go_opt=module=…
--go-grpc_out=. … proto/gorag.proto`, as in spec 025).

### REST — `internal/rest/types.go`

`type queryHit` (`rest/types.go:23`) gains one field, matching the `omitempty`
convention:

```go
NearDup *nearDupInfo `json:"near_dup,omitempty"` // H20/spec 026
```

`type queryRequest` gains `Dedup bool \`json:"dedup,omitempty"\``; `statusResponse`
gains `NearDupChunks int \`json:"near_dup_chunks"\``. The REST adapter maps
`engine.QueryHit.NearDup` and `QueryRequest.Dedup` verbatim (adapters carry no
logic, `rest/types.go:1-5`).

### MCP — `internal/mcp/server.go`

The MCP hit render already projects every `engine.QueryHit` field (as for
`section_context`). Add `near_dup` to both renders (joined siblings list /
similarity in the text render; structured in the tool result), omitted when nil.
Pass `dedup` through `renderQuery`'s arg parsing.

### CLI — `internal/cli/query.go`

`renderResults` (`cli/query.go:124`) gains a `near_dup:` line per hit (omitted
when absent) in the text format, and a `near_dup` field in the JSON/text
machine formats (mirroring `section_context`). Add a `--dedup` flag
(`cmd.Flags().Bool("dedup", false, …)`) wired into `engine.QueryRequest.Dedup`.

---

## Parity requirement (FR-004 / SC-002)

A single chunk retrieved over REST, gRPC, and MCP (and rendered by the CLI) MUST
report a byte-identical `near_dup` value, and a `dedup=true` query MUST collapse
identically across all four. This is enforced by routing every transport through
one `engine.QueryHit` + one `engine.Query` collapse pass, and is asserted by the
existing cross-transport parity suite (`internal/engine/parity_test.go`), which
tasks extend with a near-dup fixture (SC-002) — following the exact pattern used
for `section_context` (spec 025) and `poisoning` (spec 019).

---

## Non-goals (explicit, to bound the contract)

- `near_dup` is **not a filter** in v1 — it is a display/collapse attribute on the
  hit, not a retrieval predicate (no `Filter` dimension). Filtering by "only
  near-dups" / "exclude near-dups" is future work.
- The contract exposes only the **resolved pairwise siblings + similarity**, never
  the raw `uint64` SimHash fingerprint (internal — `data-model.md` §2).
- Collapse does **not** expose which representative was kept beyond the returned
  set (the dropped siblings are simply absent from the top-k). A future "show
  collapsed siblings" view is out of scope.
- `near_dup` is **not** part of `ContextChunk` (sibling context, spec 015) in v1.
