# Contract — `QueryClassifier` Extension Point

> The pluggable query-classification seam (FR-004/FR-005/FR-008). Mirrors the `QueryTransformer` pattern (`internal/index/transform.go`) and the `Reranker` pattern (`internal/index/retrieval.go`): interface + pure-Go default in `internal/index`; future model-based implementations live in an **adapter** package, keeping `internal/index` embedder-free (Constitution V).

## The interface

```go
// internal/index/classify.go (NEW)

// QueryClassification is a classifier's recommendation for one query.
// K is the ONLY recommendation it carries — a classifier never recommends
// or changes retrieval mode (clarification Q1 / FR-004).
type QueryClassification struct {
    K         int    // recommended retrieval depth; 0 == "no recommendation" (default applies)
    Rationale string // human-readable, surfaced in status/log only; never affects ranking
}

// QueryClassifier recommends a retrieval depth for a query. It runs entirely
// in-process and MUST NOT call the embedding server or any network service
// (FR-008), so the index package stays dependency-free. Applied by the engine
// ONLY when the caller has not set k explicitly (FR-006: explicit > recommended > default).
type QueryClassifier interface {
    Classify(ctx context.Context, query string) QueryClassification
}
```

**Contract guarantees** (every implementation MUST hold):
1. **`K`-only** — never recommends mode. There is no mode field.
2. **In-process, network-free** — no Ollama, no HTTP, no files. Pure function of the query string.
3. **Graceful on edge input**:
   - empty-after-normalization query (H05) ⇒ return `K: 0` ("no recommendation"), never panic (spec edge case).
   - any unrecognised shape ⇒ return `K: 0` ⇒ default depth ⇒ **never worse than today's fixed default** (spec edge case: misclassification degrades gracefully).
4. **Deterministic** — same query ⇒ same `QueryClassification` (so the result-cache key, which folds effective `k`, is stable).

## The default implementation (v1, ships in this spec)

```go
// RuleBasedClassifier — heuristic only, no model, no network (FR-005/FR-008).
// Maps obvious query shapes to a shallow/standard/deep k recommendation.
type RuleBasedClassifier struct{}

func (RuleBasedClassifier) Classify(_ context.Context, query string) QueryClassification {
    tokens := strings.Fields(query)            // query is already normalized by the transformer upstream
    n := len(tokens)
    switch {
    case n == 0:
        return QueryClassification{K: 0, Rationale: "empty query — no recommendation"}
    case n <= 3 && !hasComparative(query):      // short factoid lookup, e.g. "max batch size"
        return QueryClassification{K: 3, Rationale: "short factoid"}
    case hasComparative(query):                 // broad/comparative, e.g. "compare caching and drift approaches"
        return QueryClassification{K: 0, Rationale: "comparative — defer to default"} // default depth is deepest-safe
    default:
        return QueryClassification{K: 0, Rationale: "standard — no recommendation"}
    }
}
```
`hasComparative` is a pure keyword/token signal (e.g. presence of "compare", "vs", "versus", "difference", "across", "all", "list") — the exact token set is tuned via the eval harness against SC-002 and recorded in `tasks.md`. The classifier **recommends shallow `k` only for obvious factoids**; everything else returns `K: 0` (default depth), so a misclassification can only ever *fail to speed up* a query, never *hurt* its recall (SC-003).

**Recommendation buckets**: shallow factoid → small `K` (3); everything else → `K: 0` (no recommendation ⇒ today's default). The classifier does not invent "deep" — deeper-than-default is the caller's job (set `k`), preserving the no-regression posture.

## Engine wiring

```go
// internal/engine/engine.go
type Engine struct {
    // ...
    classifier index.QueryClassifier // nil when adaptive_depth_enabled=false (default posture)
}

// NewWithDB / NewWithEmbedder — set the default classifier when enabled:
if cfg.EffectiveAdaptiveDepthEnabled() {
    e.classifier = index.RuleBasedClassifier{}
}
```

```go
// internal/engine/query.go — applied once, after transform, before cache/retrieval:
effK := req.K
if effK <= 0 && e.classifier != nil {
    if rec := e.classifier.Classify(ctx, req.Query); rec.K > 0 {
        effK = rec.K // recommended (explicit > recommended > default; req.K was unset)
    }
}
if effK <= 0 { effK = 5 } // today's default
// effective pool then resolved from effK (or per-query override) — data-model.md Entity 2
```

A future LLM-based classifier (deferred, out of scope) implements `QueryClassifier` in a new adapter package (e.g. `internal/classify`) and is wired the same way — no `Engine` or `index` change beyond construction. That is the point of the interface.

## Disable semantics

- `adaptive_depth_enabled = false` (default) ⇒ `e.classifier == nil` ⇒ **zero classification calls**, `req.K` resolves to today's default, byte-identical results (FR-007/SC-005).
- The classifier can also be neutralized per-corpus by an implementation returning `K: 0` for every query (the rule-based default does this for everything but short factoids).
