# Data Model: Configurable RRF Constant (H08)

> No storage entities are added or changed — `rrf_k` is configuration + request
> state, not persisted per-document data (Constitution Principle II untouched;
> no new Pebble key-space prefix). This file describes the **in-memory entities**
> that carry `rrf_k` and their validation rules.

## Entities

### 1. `Config` (internal/config) — persisted, per-vault

The user-tunable RRF constant, read from `.go-rag/config.json`.

| Field | Go type | JSON key | Default | Validation |
|-------|---------|----------|---------|------------|
| `RRFK` | `int` | `rrf_k` (`omitempty`) | `60` (via `EffectiveRRFK()`) | `< 0` rejected by `Validate`; `0` means "unset → default 60" |

**New method**: `func (c Config) EffectiveRRFK() int` — returns `c.RRFK` when
`> 0`, else `60`. This is the single resolution site that every caller uses, so
the "absent = default" rule lives in exactly one place.

**New `Get` case**: `config.Get("rrf_k")` returns `strconv.Itoa(c.EffectiveRRFK())`
so `go-rag config get rrf_k` reports the resolved value (not the raw zero).

**State transition**: none. Config is loaded fresh per command/daemon start.

### 2. `QueryRequest` (internal/engine) — per-call, in-memory

The input to `Engine.Query`, projected 1:1 by every transport.

| Field | Go type | Meaning | Default resolution |
|-------|---------|---------|--------------------|
| `RRFK` | `int` | Per-query RRF override; `0` = unset | `0` → `cfg.EffectiveRRFK()` → `60` |

**Resolution rule** (in `Engine.Query`, one site):
`effective := req.RRFK; if effective <= 0 { effective = e.cfg.EffectiveRRFK() }`
then `r.SetRRFK(effective)` on the freshly-built `*Retrieval`.

Existing fields (`Query`, `K`, `Mode`, `NoRerank`, `Threshold`) are unchanged.

### 3. `Retrieval` (internal/index) — per-query, in-memory

The fuser. The two asymmetric constants become one.

| Before | After |
|--------|-------|
| `kVec int` (=40), `kFTS int` (=60) | `rrfK int` (=60) |

**New setter**: `func (r *Retrieval) SetRRFK(k int)` — sets `r.rrfK` only when
`k > 0`; no-op otherwise. Mirrors the existing `EnableRerankRetry()` setter
idiom (H09). Called per-query from `Engine.Query`.

**Constructor**: `NewRetrieval` initializes `rrfK: 60` (the default), so a
`Retrieval` built without a setter — e.g., in unit tests — still fuses at the
documented default.

### 4. `reciprocalRankFusion` (internal/index) — pure function

| Aspect | Value |
|--------|-------|
| Signature | `func reciprocalRankFusion(vectorHits, ftsHits []Hit, k int) []Hit` |
| Formula | `score(d) = Σ 1/(k + rank)`, rank 1-based (loop: `1/(k + i + 1)` for i 0-based) |
| Tie-break | higher score first, then lexicographic `ChunkID` (unchanged) |

The formula shape is identical to today; only the *two* per-list constants
collapse to *one* `k`.

## Cross-transport field mapping

All four transports gain the same field, projected into `engine.QueryRequest.RRFK`.

| Transport | Request type | Field |
|-----------|--------------|-------|
| CLI | `--rrf-k` flag (cobra `Int`, default 0 = unset) | `engine.QueryRequest{RRFK: …}` |
| REST | `queryRequest{ RRFK int json:"rrf_k,omitempty" }` | mapped in `handleQuery` |
| gRPC | `message QueryRequest { ... int32 rrf_k = 6; }` | `int(req.GetRrfK())` in `Adapter.Query` |
| MCP | inputSchema property `"rrf_k": {type: integer}` | `args["rrf_k"]` → `req.RRFK` in `renderQuery` |

See [contracts/query-rrf-k.md](contracts/query-rrf-k.md) for the full contract.

## Relationships

```text
.go-rag/config.json (rrf_k) ──► Config.RRFK ──► Config.EffectiveRRFK() ─┐
                                                                        │
CLI --rrf-k / REST rrf_k / gRPC rrf_k / MCP rrf_k ──► QueryRequest.RRFK ─┴─► Engine.Query
                                                                          │
                                                          effective = req>0 ? req : cfg
                                                                          │
                                                                  Retrieval.SetRRFK
                                                                          │
                                                            reciprocalRankFusion(hits, k)
```
