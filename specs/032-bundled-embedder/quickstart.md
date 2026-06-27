# Quickstart / Validation: Bundled Pure-Go Default Embedder

Runnable checks that prove the feature works end-to-end. No implementation bodies — see `tasks.md` for those.

## Prerequisites
- Go 1.26+, `CGO_ENABLED=0` toolchain.
- A clean vault (use `--db-path /tmp/gorag-quickstart` to avoid the global vault).
- **No Ollama running** (the whole point — the default must not need it).

## 1. Zero-setup first run (US1)
```sh
CGO_ENABLED=0 go build -o /tmp/go-rag ./cmd/go-rag
/tmp/go-rag init --db-path /tmp/gorag-quickstart      # fetches bge-small int8, SHA-256 verified
/tmp/go-rag add  --db-path /tmp/gorag-quickstart ./README.md
/tmp/go-rag query --db-path /tmp/gorag-quickstart "what is go-rag"
```
**Expected**: `init` reports the model fetched + verified; `query` returns semantic results with **no** "Ollama not found" error.

## 2. Offline after first fetch (US2)
After step 1, disconnect the network. `add` + `query` again → succeed with no network call.

## 3. Pure-Go build gate (FR-009 / Constitution III)
```sh
CGO_ENABLED=0 go build ./...   # MUST succeed
```

## 4. Hash-gated fetch (FR-010)
Corrupt the model: `printf x >> ~/.go-rag/models/<ModelID>/model.onnx`. Run `query` → clear error, no silent re-space. Run `go-rag model install` → re-fetches, verifies, restores.

## 5. Model swap re-embeds in place, no duplicates (US3 / FR-005)
Configure Ollama (`embedding_provider: "ollama"`), `go-rag reprocess`. **Expected**: document count unchanged, queries use the new vectors.

## 6. Retrieval-quality parity (FR-003)
```sh
make test-eval    # recall@10 within tolerance of the Ollama baseline
```

## 7. Cosine parity vs reference (new test)
A Go test embedding fixed probes with the native provider and asserting cosine similarity ≥ 0.9999 vs precomputed Python HuggingFace vectors for the same model (catches tokenizer/pooling regressions).

## Out of scope here
Implementation, migrations, full test suites → `tasks.md`.
