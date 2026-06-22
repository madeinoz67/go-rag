# Contract: `rrf_k` on the Query Operation (H08)

> The RRF smoothing constant is exposed identically on all four transports
> (Constitution Principle V — MCP-first; spec 003 cross-transport parity). Each
> transport projects its request field into `engine.QueryRequest.RRFK`. The
> engine resolves the effective value once: `req.RRFK` if `> 0`, else
> `cfg.EffectiveRRFK()`, else `60`.

## Semantics (transport-independent)

- **Name**: `rrf_k` (snake_case on the wire / flag / schema; `RRFK` in Go).
- **Type**: integer.
- **Default**: `60` (the retrieval book's canonical RRF constant).
- **Effective value**: per-query override (`> 0`) wins; otherwise the configured
  value; otherwise `60`.
- **Scope**: affects **hybrid mode only**. In `keyword` / `semantic` mode the
  constant is a silent no-op (one list, no fusion) — never an error.
- **Validation**: an **explicit** value `≤ 0` supplied via flag is rejected at
  parse time; an **explicit negative** in config is rejected by `Validate`. An
  **absent** key/flag (`0`) means "use default" (backward compatible).
- **Formula** (documented once, in `internal/index` package doc + README):
  `score(d) = Σ 1/(k + rank)`, rank 1-based.

## CLI

```text
go-rag query "<query>" [--rrf-k N]
```

- Flag: `--rrf-k` (cobra `Int`, default `0` = unset).
- `0`/absent → config/default. Explicit `≤ 0` when `Flags().Changed("rrf-k")` →
  error: `--rrf-k must be a positive integer`.
- Mapped into `engine.QueryRequest{RRFK: v}` in `internal/cli/query.go`.

## REST

`POST /v1/query` — request body gains one optional field:

```json
{
  "query": "string",
  "k": 5,
  "mode": "hybrid",
  "no_rerank": false,
  "threshold": 0.0,
  "rrf_k": 60
}
```

- DTO: `queryRequest{ ..., RRFK int json:"rrf_k,omitempty" }` (`internal/rest/types.go`).
- Mapped in `handleQuery` (`internal/rest/engine_adapter.go`) into
  `engine.QueryRequest.RRFK`. `omitempty` keeps the field absent from clients
  that don't set it.

## gRPC

`proto/gorag.proto` `QueryRequest` gains field 6:

```proto
message QueryRequest {
  string query = 1;
  int32 k = 2;
  string mode = 3;
  bool no_rerank = 4;
  double threshold = 5;
  int32 rrf_k = 6;            // RRF smoothing constant; 0 = use configured/default (60)
}
```

- Regenerate `proto/gen/gorag.pb.go` (regen command determined in tasks; see
  research.md §4).
- Mapped in `Adapter.Query` (`internal/grpc/engine_adapter.go`):
  `RRFK: int(req.GetRrfK())`.
- Field 6 is additive and optional → wire-compatible with existing clients.

## MCP

The `go_rag_query` tool input schema gains one property:

```json
{
  "rrf_k": { "type": "integer", "default": 60 }
}
```

- Schema map literal in `internal/mcp/server.go` (the `go_rag_query` tool entry,
  alongside `query` / `k` / `mode` / `no_rerank` / `threshold`).
- `renderQuery` reads `args["rrf_k"]` (float64 from JSON → int) and sets
  `req.RRFK` when present and `> 0`.

## Parity guarantee

A query with the same `rrf_k` over CLI, REST, gRPC, and MCP MUST return
identical rankings (same effective `k` → same fusion). This is asserted by the
existing cross-transport parity test extended to set `rrf_k` (see quickstart.md
scenario 5).
