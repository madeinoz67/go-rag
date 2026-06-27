# Phase 0 Research: Bundled Pure-Go Default Embedder

Resolves the plan's open technical questions. Each item: **Decision / Rationale / Alternatives**. Grounded in the 2026-06-27 spike + go-rag source (`internal/embed/embedder.go`, `internal/config/config.go`).

## R1 — How does Hugot GoMLX map onto go-rag's `Embedder` interface?

- **Decision**: New `internal/embed/hugot.go` `type HugotEmbedder` wraps `*pipelines.FeatureExtractionPipeline` (built with `WithNormalization()` = mean-pool + L2-norm, which BGE requires). `Embed(ctx, texts)` → `pipe.RunPipeline(ctx, texts)` → returns `[][]float32` (Hugot's native output type — 1:1 match). `Dimensions()` caches from the first successful `Embed` (0 before — matches the interface contract). `Model()` returns the pinned model ID. Lazy-load the pipeline on first `Embed` to keep startup fast.
- **Rationale**: zero-shape conversion (Hugot already yields `[][]float32`); lazy load preserves the < 1 s cold-start budget.
- **Alternatives**: eager-load at construction (rejected — slows startup); adapt in `embedproc` instead (rejected — keeps the provider self-contained).

## R2 — How is the pinned model hash baked into the binary?

- **Decision**: `internal/embed/modelbundle/bundle.go` holds compile-time `const`: `ModelID`, `HFRepo` (`Xenova/bge-small-en-v1.5`), `OnnxFilePath` (`onnx/model_int8.onnx`), `EmbeddingDim` (384), `ExpectedSHA256`, and a download URL template. `Download(ctx, dest) (path, error)` fetches; `Verify(path) error` checks SHA-256 vs `ExpectedSHA256`. Bumping the model = editing these consts (and existing docs re-embed because `Model()` changes).
- **Rationale**: consts are ~bytes (binary stays tiny); SHA verify is the supply-chain guard; a model bump is an explicit, reviewable code change.
- **Alternatives**: `go:embed` a JSON manifest (more indirection); ldflags injection (fragile).

## R3 — Where is the model stored?

- **Decision**: `~/.go-rag/models/<ModelID>/{model.onnx, tokenizer.json}` — **global, shared across vaults**, NOT in Pebble. Reuses go-rag's existing `~/.go-rag/` data-dir resolution.
- **Rationale**: the model is a shared global resource (same embeddings regardless of vault); large binary blobs don't belong in Pebble.
- **Alternatives**: per-vault copy (wasteful); Pebble value (wrong store for large blobs).

## R4 — GoMLX concurrency for go-rag's async embedding workers?

- **Decision**: Treat the pipeline as **single-writer** — go-rag's background embed worker calls `Embed` serially; batch ingest passes N texts in one `RunPipeline` (Hugot batches internally; spike: 20/sec at batch-32). Verify GoMLX's goroutine-safety at impl before allowing shared concurrent use.
- **Rationale**: avoids GoMLX concurrency pitfalls; matches go-rag's existing single-writer model.
- **Alternatives**: parallel workers sharing one pipeline (risk — verify safety first); one pipeline per worker (memory cost).

## R5 — Config + provider default change

- **Decision**: `Config.EmbeddingProvider` (already present from spec 031) gains value `"native"` as the new **default**. `embed.New` gets `case "native","gomlx","hugot": return NewHugot(modelPath)`. Default (empty/omitted) → `"native"` (was `"ollama"`). Ollama remains selectable via `embedding_provider: "ollama"`. The native provider ignores `endpoint`/`apiKey` (local).
- **Rationale**: makes zero-setup the default (US1) while keeping Ollama opt-in (US4); no new config field, just a new enum value + default flip.
- **Alternatives**: keep Ollama default, native opt-in (rejected — defeats US1).

## R6 — Re-embed on model change (FR-005)

- **Decision**: Each stored embedding records the model identity (`Model()` string). On load, if stored model ID ≠ current provider's `Model()`, the doc is flagged for re-embed via the existing reprocess path. Switching provider/model → reprocess re-embeds **in place** (content-addressed identity unchanged → no duplicates).
- **Rationale**: honors Principle II; model identity is distinct from document identity.
- **Note**: confirm at impl where go-rag currently records embedding model identity (Pebble key/sidecar). `internal/eval/run.go` already records an `Embedder` string — likely a pattern to reuse.

## R7 — govulncheck / dep hygiene (gate)

- **Decision**: After `go get github.com/knights-analytics/hugot`, run `govulncheck ./...`. go-rag's `x/image v0.43.0` must win over Hugot's `v0.41.0` via MVS. If any vuln remains, bump the transitive dep. CI's govulncheck step is the gate.
- **Rationale**: constitution requires a clean supply chain; CI already runs govulncheck (the step we just fixed in the lint/CYML work).
- **Alternatives**: none — must pass.

## R8 — Download source (D1a, deferred)

- **Decision (default)**: model uploaded as a release asset on go-rag's GitHub Releases (depends on the separate `release.yml` work); URL pinned in the manifest. **Interim**: fetch from HuggingFace via Hugot's `DownloadModel`, verify vs the pinned `ExpectedSHA256`.
- **Rationale**: same-origin trust once the release pipeline exists; HF as the interim/upgrade source.
- **Alternatives**: dedicated `go-rag-models` repo to decouple model/binary versioning (defer).

## Open items for `/speckit-tasks`

- Confirm GoMLX goroutine-safety (R4) before writing concurrent-worker tests.
- Locate/extend the embedding model-identity storage (R6).
- Stage the PRD N9 edit (spec C4).
