# Phase 1 — Contracts: Evaluation Harness

> The interfaces this feature exposes: a CLI command, an MCP tool, and the
> committed golden-file format. All three honor Principle V (CLI op ⇄ MCP tool)
> and return identical metric numbers (the engine path is shared).
> Metric/dataset entity shapes live in [../data-model.md](../data-model.md);
> decision rationale in [../research.md](../research.md).

---

## 1. CLI contract — `go-rag eval`

Measures retrieval quality over a golden dataset and optionally gates against a
baseline.

**Usage**

```text
go-rag eval [flags]
```

**Flags**

| Flag | Default | Description |
|------|---------|-------------|
| `--golden` | `testdata/golden/v1.jsonl` | Path to the golden dataset (JSONL). |
| `--corpus` | `testdata/golden/corpus/` | Source corpus for self-provisioned runs; ignored when `--db-path` points at a pre-built vault. |
| `--db-path` | `--corpus` temp vault | Vault to measure. When omitted, a throwaway vault is built from `--corpus` (hermetic/reproducible). |
| `--mode` | `hybrid` | Retrieval mode forwarded to the engine (`hybrid` \| `semantic` \| `keyword`). |
| `--k` | `10` | Top-k cutoff for recall/MRR/NDCG pooling. |
| `--embedder` | `auto` | `offline` (deterministic, no network) \| `ollama` (real local model) \| `auto` (ollama if reachable & not CI, else offline). |
| `--no-rerank` | `false` | Forwarded to the engine query (skip cross-encoder rerank). |
| `--baseline` | (none) | Baseline file to compare against; with `--tolerance`, sets the exit code. |
| `--tolerance` | `2.0` | Max allowed drop (points) in recall@10 vs baseline before the gate fails. |
| `--record-baseline` | `false` | Write/overwrite `testdata/golden/baseline.json` from this run's metrics and exit. |
| `--format` | `text` | `text` (human) \| `json` (machine-readable Evaluation Run). |

**Output (text)**

```text
mode=offline  embedder=deterministic-hash  retrieval=hybrid  k=10
queries: scored=32 skipped=3

  recall@5    : 0.71   (target 0.80)
  recall@10   : 0.83   (target 0.80)   ← baseline 0.85, delta -0.02 (within tol 2.0)
  precision@5 : 0.68
  mrr         : 0.64   (target 0.60)
  ndcg@10     : 0.76   (target 0.75)

verdict: PASS (vs baseline)
```

**Output (json)** — one Evaluation Run object (see data-model.md).

**Exit codes**

| Code | Meaning |
|------|---------|
| `0` | Run succeeded; gate passed (or no `--baseline` given). |
| `1` | Run failed: golden file invalid/unreadable, corpus ingest error, or a monitored metric regressed beyond `--tolerance`. |
| `2` | Misuse (bad flags), matching cobra conventions. |

**Behavioral guarantees**: in `offline` mode, zero network dial-outs (SC-004);
the measured vault is never written (FR-006); zero-relevant-item queries are
skipped, not fatal (FR-008).

---

## 2. MCP tool contract — `go_rag_eval`

Surfaces the same measurement to AI agents (Principle V). Registered in
`internal/mcp/server.go::toolDefs()` and routed in `dispatchDB`. Returns identical
numbers to the CLI (shared `engine.Query` path).

**Tool definition**

```json
{
  "name": "go_rag_eval",
  "description": "Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset. Runs offline by default (deterministic, no network).",
  "inputSchema": {
    "type": "object",
    "properties": {
      "golden":   { "type": "string",  "description": "Path to golden JSONL (default testdata/golden/v1.jsonl)." },
      "mode":     { "type": "string",  "enum": ["hybrid", "semantic", "keyword"], "default": "hybrid" },
      "k":        { "type": "integer", "default": 10 },
      "embedder": { "type": "string",  "enum": ["offline", "ollama", "auto"], "default": "auto" },
      "no_rerank":{ "type": "boolean", "default": false }
    }
  }
}
```

**Response** — a text rendering of the Evaluation Run (same numbers as the CLI
text output). MCP tools cannot set process exit codes, so the gate verdict is
conveyed **in the text** (`verdict: PASS`/`FAIL` and any delta); the CI gate
itself runs via the CLI (`make test-eval`), not over MCP.

**Existing-test impact**: `internal/mcp/server_test.go::TestMCP_ToolsListHas12`
asserts 12 tools; adding `go_rag_eval` makes 13 — that test is updated (count +
the new tool name) in the tasks phase. The agent `guide()` text gains a
`go_rag_eval` bullet.

---

## 3. Golden-file format contract — `testdata/golden/v1.jsonl`

JSONL, one Golden Query per line (UTF-8, no trailing commas, blank lines ignored).

**Schema**

```json
{"id":"q001","query":"how are chunks split?","relevant":["<sha256-chunk-id>","<sha256-chunk-id>"],"notes":"optional"}
```

| Field | Required | Rule |
|-------|----------|------|
| `id` | yes | Unique within the file; non-empty. |
| `query` | yes | Non-empty; run verbatim through the engine. |
| `relevant` | yes | List of content-addressed chunk_ids (non-empty strings). May be empty → query is skipped in metrics (FR-008). |
| `notes` | no | Reviewer annotation. |
| `grade` | no (future) | Integer relevance grade for NDCG; absent ⇒ binary (relevant=1). |

**Stability**: `relevant` chunk_ids are content-addressed (Principle II), so they
are stable across any vault built from the same `--corpus` with the same chunker
— labels are portable (research.md D5/D6).

**Baseline companion file** — `testdata/golden/baseline.json`: a single Baseline
object (`{mode, recorded_at, metrics{…}}`) written only by
`go-rag eval --record-baseline`. Read by the gate when `--baseline` is supplied.
