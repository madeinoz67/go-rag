# Contract: `Embedder.Embed` (preserved under batching — H12/US3)

> H12 introduces **no new external interface**. Batching is an internal transport
> detail of the Ollama implementation. This file exists to pin the one thing that
> matters for US3: the `Embed` contract every caller depends on is **unchanged**.
> There is nothing for any caller — the pipeline, the query path, tests, or a
> future provider — to change.

## The preserved contract

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    Model() string
}
```

`Embed` MUST satisfy, for all callers, before and after H12:

| Guarantee | Statement |
|-----------|-----------|
| Count | Returns exactly `len(texts)` vectors (or `(nil, nil)` when `len(texts)==0`). |
| Order | `out[i]` is the vector for `texts[i]`. |
| Integrity | A response whose vector count ≠ its sent text count is an **error**, never a short/padded result. (Enforced per batch under H12; was per-call before.) |
| Failure | Any unrecoverable failure (after retries) returns an error and **no** partial vector set. |
| Dimensions | `Dimensions()` reflects the first successful response's vector length (set-once); stable across the call and concurrent calls. |
| Empty input | `len(texts)==0` ⇒ no network request, returns `(nil, nil)`. |
| Cancellation | Honours `ctx` — a cancelled context returns promptly. |

## What changes (internal only, invisible to callers)

- `Ollama.Embed` now issues **multiple** `/api/embed` requests (one per batch of
  ≤ 32 texts) instead of one, and concatenates the responses in order.
- Retry, integrity, and dimensionality guarantees are applied **per batch**; their
  observable effect (the contract above) is identical.
- No new type, no new method, no new config key, no new flag, no transport
  (CLI/REST/gRPC/MCP) surface change.

## Parity / regression anchor

Because the contract is unchanged, a deterministic embedding stand-in (the
`httptest` Ollama used in `internal/embed/ollama_test.go` and the parity harness)
MUST produce byte-identical vectors for the same input texts whether the input is
below or far above the batch cap. That equality is the US3 acceptance test — see
[quickstart.md](../quickstart.md) scenario 4 and `data-model.md` "Embedding
result."
