# Contract: `go-rag model install` CLI command (+ MCP tool)

## CLI
```
go-rag model install [--force]
```
- **Behavior**: ensure the default model is present at `~/.go-rag/models/<ModelID>/`. If absent or `--force`, download (default source: go-rag GitHub Releases asset; interim: HuggingFace), **verify SHA-256 against `modelbundle.ExpectedSHA256`**, reject on mismatch. Idempotent — no-op if present and hash matches.
- **Triggered automatically by `go-rag init`** (first-run fetch, per spec Q2). `add`/`query` never fetch.
- **Exit codes**: 0 installed/present; non-zero on download-failure or hash-mismatch (with an actionable message).
- **Air-gapped escape hatch**: a user may place the model file out-of-band; `model install` then only verifies its hash.

## MCP tool (Principle V — MCP-first)
Mirror the CLI op as an MCP tool `gorag.install_model` (force?: bool) → `{model_id, status: "present"|"fetched"|"error"}`, so AI agents can provision the model without shelling out. Same hash-verify semantics.

## Errors (FR-006)
- Download failure / offline → "could not fetch model <ID>; check network or place the file at <path>".
- Hash mismatch → "model <ID> failed integrity check; refusing to install".
- Never silently fall back to a different embedding space.
