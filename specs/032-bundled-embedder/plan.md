# Implementation Plan: Bundled Pure-Go Default Embedder (Hugot GoMLX)

**Branch**: `032-bundled-embedder` | **Date**: 2026-06-27 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/032-bundled-embedder/spec.md`

## Summary

Ship a pure-Go local embedding provider — Hugot's GoMLX backend + bge-small-en-v1.5 (int8) — as go-rag's DEFAULT, so `init → add → query` works with **no Ollama dependency**. The model is delivered via **hash-gated download-at-runtime** (one-time, during `init`), keeping the binary pure-Go and < 25 MB. The 2026-06-27 latency spike confirmed: `CGO_ENABLED=0` builds and runs; warm query embed **median 73 ms** (within the 500 ms hybrid budget); cold start ~91 ms; batch ~20 embeds/sec. The provider plugs into the existing `embed.Embedder` interface via the `embed.New` factory (Principle V); Ollama becomes opt-in. **Constitution-compatible (no amendment)**; requires a PRD N9 scope edit.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`), `CGO_ENABLED=0` (PRD §9.5).
**Primary Dependencies**: `github.com/knights-analytics/hugot v0.7.5` (+ `gomlx/gomlx`, pure-Go, Apache-2.0); existing cobra/pebble/chromem-go. Hugot pulls `yalue/onnxruntime_go` + `ortgenai` (its ORT backend) — **confirmed NOT compiled into the GoMLX path** (spike: `CGO_ENABLED=0` build clean). Hugot also pulls `golang.org/x/image v0.41.0` (GO-2026-5061); go-rag's `v0.43.0` wins via MVS — **verify with govulncheck** (gate, research.md R7).
**Storage**: Pebble KV (existing) for vectors + model-identity metadata; model **weights** as files under `~/.go-rag/models/<ModelID>/` (global, shared across vaults — NOT in Pebble).
**Testing**: `go test ./...`, the H02 retrieval-eval harness (`make test-eval`), a new **cosine-parity test vs Python HuggingFace**, and a **hash-verify test** (tamper/corruption).
**Target Platform**: All Go targets (pure-Go cross-compile: darwin/linux/amd64/arm64, windows). **Re-benchmark on low-end hardware** during impl (spike was M-series Mac).
**Project Type**: CLI + library + MCP + REST (single binary, `cmd/go-rag`).
**Performance Goals**: warm query embed < 100 ms (spike: 73 ms median); cold start < 1 s (spike: 91 ms); batch ≥ 20 embeds/sec; write ACK < 10 ms unchanged; hybrid query < 500 ms.
**Constraints**: pure-Go/`CGO_ENABLED=0` (constitution III); binary < 25 MB (model fetched, not embedded); SHA-256 verify on download; offline after first fetch; embeddings record model identity for re-embed (FR-005).
**Scale/Scope**: ~6 new/modified files (see Project Structure).

## Constitution Check

GATE: **PASS** (re-checked post-design). One verification caveat (govulncheck); **no violations** → no Complexity Tracking entries.

| Principle | Status | Evidence |
|---|---|---|
| I — Local-First, Single-Binary | ✅ PASS | Single static pure-Go binary. Model is local **data** (fetched once, cached), not a service. Offline after fetch. No cloud/core-op egress. |
| II — Content-Addressed Identity | ✅ PASS | Document identity unchanged (SHA-256 content+metadata). Model identity stored **separately** per embedding → model swap re-embeds, no duplicates (FR-005). |
| III — Pure Go, No CGo | ✅ PASS | Spike: `CGO_ENABLED=0 go build` succeeds + runs. GoMLX backend pure-Go; ORT backend not compiled in. Hugot + GoMLX Apache-2.0. ⚠ verify govulncheck (x/image MVS). |
| IV — Async-After-ACK | ✅ PASS | Embedding stays on background workers, off the < 10 ms ACK path. Existing architecture; native provider plugs in unchanged. |
| V — Extension by Interface, MCP-First | ✅ PASS | New provider implements `embed.Embedder`; added via `embed.New`. `model install` is a CLI op → also an MCP tool. |

## Project Structure

### Documentation (this feature)
```text
specs/032-bundled-embedder/
├── plan.md              # this file
├── research.md          # Phase 0 — R1..R8 decisions
├── data-model.md        # Phase 1 — entities + state
├── quickstart.md        # Phase 1 — validation guide
├── contracts/           # Phase 1 — Embedder interface, model-install CLI
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code
```text
internal/
├── embed/
│   ├── embedder.go         # MODIFIED: New() factory adds case "native"
│   ├── hugot.go            # NEW: Hugot GoMLX Embedder (lazy load; Embed/Dimensions/Model)
│   └── modelbundle/
│       └── bundle.go       # NEW: pinned manifest (ID+SHA-256+dim+URL) + Download+Verify
├── config/config.go        # MODIFIED: EmbeddingProvider default → "native"
├── cli/model.go            # NEW: `go-rag model install` (+ wired into init)
└── engine/                 # MODIFIED: default selection; model-identity on embeddings
.github/workflows/release.yml  # (dependent) upload model asset per release (D1a)
```

**Structure Decision**: Extends the existing 1:1 PRD-subsystem layout. The native provider is a new file in `internal/embed/` (beside ollama.go, openai.go), selected by the existing `embed.New` factory — **no new subsystem**. Model fetch/verify lives in a small `internal/embed/modelbundle/` subpackage. `model install` is a cobra command in `internal/cli/`, mirrored as an MCP tool (Principle V).

## Complexity Tracking

> None — Constitution Check passes with no violations to justify.
