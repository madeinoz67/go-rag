# Implementation Plan: Secret / PII Scanning at Ingest

**Branch**: `022-secret-pii-scan` *(single-author repo — commits directly to `main`)* | **Date**: 2026-06-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature spec from `/specs/022-secret-pii-scan/spec.md` — backlog item **H19** (P2, S).

## Summary

An opt-in regex scanner (`internal/redact`) that detects + redacts secrets/PII (API keys,
tokens, private keys, credit cards, SSNs, emails) in ingested text **between identity
computation and chunking** — so indexed text never contains live credentials. Document
identity stays over the **original** content (Constitution II preserved); redaction is a
pipeline transform, re-applied on `reprocess`. Findings reported per-type in the ingest
summary + audit log. Pure stdlib `regexp`, no new dependency; local, no egress.

## Technical Context

**Language/Version**: Go 1.22+ (pure Go, `CGO_ENABLED=0`).

**Primary Dependencies**: existing only — stdlib `regexp`. **No new dependency** (Constitution III).

**Storage**: no new persistence — redaction is a text transform applied in-memory during
ingest; the stored chunks carry the (redacted) text. Findings are reported via the ingest
summary + the H18 audit log (a `redaction` audit event).

**Testing**: `go test -race -cover ./...`; a redact-package test (each pattern type →
correct redaction + placeholder), a pipeline integration test (identity stable across
redact/no-redact; chunks redacted; summary reports counts), and a default-off no-regression
test (verbatim ingest when disabled).

**Target Platform**: single static binary, local-first.

**Project Type**: CLI + multi-transport daemon over one Engine.

**Performance Goals**: opt-in (default off ⇒ no cost); when on, bounded regex cost
(validation-class, like the H04 poisoning detector) within the <10ms ACK budget.

**Constraints**: air-gap (local regex — Constitution I); identity over original (II);
opt-in (default off); recall on redacted terms drops by design.

**Scale/Scope**: local single-user; bounded pattern set (no per-document cardinality).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Verdict | Evidence |
|---|-----------|---------|----------|
| I | Local-First, Single-Binary | ✅ PASS | The scanner is a pure-local regex pass over in-memory text; no egress, no cloud. |
| II | Content-Addressed Identity | ✅ PASS (key design) | Document identity (`docID = GenerateID(originalContent, ...)`) + content-hash (over raw bytes) are computed BEFORE redaction. Redaction is applied to the `content` local var AFTER identity, BEFORE chunking. So the same file ingests with/without `--redact` to the **same docID**; ingestion is idempotent regardless of the setting (redact is a pipeline transform, like the H07 embedding prefix). |
| III | Pure Go — No CGo, No Runtime | ✅ PASS | Stdlib `regexp` only. **No new dependency.** |
| IV | Async-After-ACK Writes | ✅ PASS | **Opt-in (default off)** → no ACK-path cost when disabled. When on, redaction is validation-class (bounded regex over in-memory text, µs–low-ms), applied synchronously pre-chunk like the H04 poisoning detector. The <10ms ACK budget is untouched when off (the default). |
| V | Extension by Interface, MCP-First | ✅ PASS | New self-contained `internal/redact` package; the pipeline calls `redact.Apply(content)` when enabled. Findings surfaced via the ingest summary + audit (cross-transport via the engine). |

**No violations → Complexity Tracking table intentionally empty.** Principle II is the key
design nuance — identity-over-original preserved by inserting redaction post-identity,
pre-chunk.

## Project Structure

### Documentation (this feature)

```text
specs/022-secret-pii-scan/
├── plan.md              # This file
├── research.md          # Phase 0 — pattern set, placement, identity rule, report shape
├── data-model.md        # Phase 1 — Redaction/Finding + state
├── quickstart.md        # Phase 1 — runnable validation (redact, identity, rescan, default-off)
├── contracts/
│   └── patterns.md      # Phase 1 — the curated regex set + placeholder format
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/
├── redact/              # NEW — regex scanner + redactor (stdlib only)
│   ├── redact.go        #   Scanner (patterns) + Apply(text) → (redactedText, Findings)
│   ├── patterns.go      #   curated built-in set (AWS/GitHub/generic/key/cc/ssn/email)
│   └── redact_test.go
├── pipeline/            # MODIFY — call redact.Apply between identity (line ~222) and chunk (~238) when enabled
├── engine/              # MODIFY — surface Findings in IngestSummary; report to audit
├── config/              # MODIFY — pii_redact_enabled (default false), pii_patterns (path)
└── cli/                 # MODIFY — `--redact` flag on add/scan/reprocess (forwards to pipeline)
```

**Structure Decision**: one new self-contained `internal/redact` package owns the patterns +
the Apply transform (stdlib `regexp`). The pipeline calls it **post-identity, pre-chunk** —
NOT in the reader (the reader returns original content; identity needs original; redaction
is a pipeline transform post-identity). This preserves Constitution II (identity-over-
original). Mirrors the `internal/poison` / `internal/audit` isolation pattern.

## Complexity Tracking

> Empty — no Constitution violations to justify. (Principle II's identity-over-original is
> preserved by the insertion point, not violated.)
