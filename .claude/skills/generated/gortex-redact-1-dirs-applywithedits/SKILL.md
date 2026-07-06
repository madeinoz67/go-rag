---
name: gortex-redact-1-dirs-applywithedits
description: "Work in the redact +1 dirs · ApplyWithEdits area — 171 symbols across 4 files (83% cohesion)"
---

# redact +1 dirs · ApplyWithEdits

171 symbols | 4 files | 83% cohesion

## When to Use

Use this skill when working on files in:
- `internal/pipeline/section.go`
- `internal/redact/patterns.go`
- `internal/redact/redact.go`
- `internal/redact/redact_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `internal/pipeline/section.go` | off, startIdx, out, resolveWikilinks, edits, ... |
| `internal/redact/patterns.go` | re, Error, closure@72, out, credit, ... |
| `internal/redact/redact.go` | c, out, lo, Apply, e, ... |
| `internal/redact/redact_test.go` | TestApply_Email, out, off, t, got, ... |

## Connected Communities

- **engine +19 dirs** (31 cross-edges)
- **daemon +5 dirs** (11 cross-edges)
- **reader +7 dirs** (4 cross-edges)
- **reader +8 dirs** (1 cross-edges)
- **redact +1 dirs · LoadCustom** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-79"
smart_context with task: "understand redact +1 dirs · ApplyWithEdits", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
