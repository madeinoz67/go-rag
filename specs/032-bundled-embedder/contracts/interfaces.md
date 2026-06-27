# Contract: Embedder Interface + Provider Factory

## `embed.Embedder` (existing, UNCHANGED — `internal/embed/embedder.go`)
```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int   // 0 until the first successful Embed
    Model() string     // model identifier (provenance + re-embed key)
}
```

## `embed.New` factory (MODIFIED — add the `"native"` case)
```go
func New(provider, endpoint, model, apiKey string) Embedder {
    switch strings.ToLower(strings.TrimSpace(provider)) {
    case "openai", "openai-compatible":
        return NewOpenAI(endpoint, model, apiKey)
    case "native", "gomlx", "hugot":          // NEW
        return NewHugot(modelPathFromManifest())   // NEW — local, pure-Go
    default:
        return NewHugot(modelPathFromManifest())   // DEFAULT flipped: was NewOllama
    }
}
```
- `"native"` ignores `endpoint`/`apiKey` (local in-process engine).
- Default (empty/omitted) changes from Ollama → native (US1 zero-setup).

## New constructor — `embed.NewHugot(modelPath string) Embedder`
- Lazily builds a Hugot `FeatureExtractionPipeline` (GoMLX backend, `WithNormalization()`) on first `Embed`.
- `Embed` → `pipe.RunPipeline(ctx, texts)` → `[][]float32`.
- `Dimensions()` returns 384 after first Embed (0 before).
- `Model()` returns `modelbundle.ModelID`.

## Config (existing, `internal/config/config.go`)
- `EmbeddingProvider string` — gains value `"native"` (new default). `"ollama"` / `"openai"` remain.
- No new persisted field; the model path is derived from `modelbundle.ModelDir()`.
