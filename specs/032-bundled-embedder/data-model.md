# Data Model: Bundled Pure-Go Default Embedder

## Entities

### Embedding Provider (existing interface `embed.Embedder` — UNCHANGED)
```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int   // 0 until first successful Embed
    Model() string     // model identifier (provenance + re-embed key)
}
```
New implementation: `HugotEmbedder` (`internal/embed/hugot.go`), selected by `embed.New` when `provider == "native"`.

### Bundled Model Manifest (NEW — `internal/embed/modelbundle`)
Compile-time constants — the source of truth for "the default model":
- `ModelID` — stable slug, e.g. `bge-small-en-v1.5-int8`
- `HFRepo` — `Xenova/bge-small-en-v1.5`
- `OnnxFilePath` — `onnx/model_int8.onnx`
- `EmbeddingDim` — `384`
- `ExpectedSHA256` — pinned hash (supply-chain guard)
- `DownloadURL` — release-asset URL template (D1a)
Functions: `Download(ctx, dest) (path, error)`, `Verify(path) error`, `ModelDir() string` (`~/.go-rag/models/<ModelID>/`).

### Model Store (filesystem — NOT Pebble)
`~/.go-rag/models/<ModelID>/{model.onnx, tokenizer.json}`. **Global, shared across vaults.**
State: `absent` → (`init`/`model install`: fetch + SHA-256 verify) → `present` → (upgrade: pinned hash changes) → `re-fetch` → `present(new)`.

### Stored Embedding (existing, Pebble)
The vector + a **model-identity tag** (`ModelID`). Used for re-embed detection.
Document identity (SHA-256 over content+metadata) is **separate** and unchanged (Principle II) — switching models re-embeds in place, never duplicates.

### Config (existing `.go-rag/config.json`)
`EmbeddingProvider` gains enum value `"native"` (new default; was `"ollama"`). Model path derived from the manifest, not a new persisted field. No new persistence shape.

## State Transitions

- **Model**: `absent` → fetch+verify → `present` → (hash change on upgrade) → re-fetch → `present`.
- **Embeddings**: created under model A → (provider switches to B) → flagged stale (model-ID mismatch) → (`reprocess`) → re-embedded under B (document identity unchanged).

## Validation Rules

- Model SHA-256 MUST equal `ExpectedSHA256` or the install is **rejected** (tamper/corruption; FR-010).
- Embedding dimension MUST equal `EmbeddingDim` or the vector index rejects (guard against mixed-model vectors).
- Provider `"native"` requires the model present; if absent → clear actionable error ("run `go-rag model install`"), never silent fallback to another space (FR-006).
