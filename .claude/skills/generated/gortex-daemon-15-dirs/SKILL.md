---
name: gortex-daemon-15-dirs
description: "Work in the daemon +15 dirs area — 515 symbols across 34 files (73% cohesion)"
---

# daemon +15 dirs

515 symbols | 34 files | 73% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `external-call::dep:github.com/knights-analytics/hugot`
- `external-call::dep:github.com/knights-analytics/hugot/pipelines`
- `internal/audit/rotate.go`
- `internal/caption/captioner.go`
- `internal/caption/ollama.go`
- `internal/caption/openai.go`
- `internal/cli/config_cli.go`
- `internal/cli/health.go`
- `internal/cli/health_test.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/daemon/lifecycle.go`
- `internal/daemon/lifecycle_test.go`
- `internal/daemon/pid.go`
- `internal/daemon/pid_test.go`
- `internal/daemon/process_unix.go`
- `internal/daemon/process_windows.go`
- `internal/embed/hugot.go`
- `internal/embed/modelbundle/bundle.go`
- `internal/embed/ollama.go`
- `internal/engine/threat.go`
- `internal/enrich/enricher.go`
- `internal/enrich/ollama.go`
- `internal/enrich/openai.go`
- `internal/eval/beir/beir.go`
- `internal/eval/dataset.go`
- `internal/eval/run.go`
- `internal/rerank/openai.go`
- `internal/rerank/rerank.go`
- `internal/rerank/rerank_test.go`
- `internal/upgrade/download.go`
- `internal/upgrade/selfupdate.go`
- `internal/upgrade/verify.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | os, ReadAll, exec, tar, Parse, ... |
| `external-call::dep:github.com/knights-analytics/hugot` | github.com/knights-analytics/hugot |
| `external-call::dep:github.com/knights-analytics/hugot/pipelines` | github.com/knights-analytics/hugot/pipelines |
| `internal/audit/rotate.go` | path, dir, err, n, i, ... |
| `internal/caption/captioner.go` | err, WrapPermanent |
| `internal/caption/ollama.go` | b64, c, imageBytes, ctx, err, ... |
| `internal/caption/openai.go` | err, Choices, chat, ctx, MaxTokens, ... |
| `internal/cli/config_cli.go` | newConfigSetCmd, closure@55 |
| `internal/cli/health.go` | client, runHealth, err, addr, resp, ... |
| `internal/cli/health_test.go` | err, TestRunHealth_Non200, srv, closure@28, t |
| `internal/config/config.go` | err, validatePrefixString, r, err, err, ... |
| `internal/config/config_test.go` | err, err, v, err, good, ... |
| `internal/daemon/lifecycle.go` | err, err, args, deadline, args, ... |
| `internal/daemon/lifecycle_test.go` | got, err, dbPath, err, pid, ... |
| `internal/daemon/pid.go` | pid, pid, LogPath, dbPath, PIDPath, ... |
| `internal/daemon/pid_test.go` | pid, t, dir, err, err, ... |
| `internal/daemon/process_unix.go` | err, proc, isPebbleLockHeld, lockPath, f, ... |
| `internal/daemon/process_windows.go` | daemonExtraSetup, daemonSysProcAttr |
| `internal/embed/hugot.go` | ensure, closure@31 |
| `internal/embed/modelbundle/bundle.go` | HashFile, resp, err, tr, err, ... |
| `internal/embed/ollama.go` | lastErr, decErr, resp, rb, url, ... |
| `internal/engine/threat.go` | err, data, err, resp, err, ... |
| `internal/enrich/enricher.go` | err, WrapPermanent |
| `internal/enrich/ollama.go` | ctx, Response, rb, generate, err, ... |
| `internal/enrich/openai.go` | Choices, resp, Stream, docText, rb, ... |
| `internal/eval/beir/beir.go` | tmp, dir, err, resp, download, ... |
| `internal/eval/dataset.go` | LoadGolden, GoldenQuery, err, err, seen, ... |
| `internal/eval/run.go` | err, data, LoadBaseline, path, err |
| `internal/rerank/openai.go` | req, rb, i, Stream, Temperature, ... |
| `internal/rerank/rerank.go` | p, err, i, candidates, c, ... |
| `internal/rerank/rerank_test.go` | TestReranker_EmptyInput, err, rr, scores, t |
| `internal/upgrade/download.go` | err, err, f, hdr, binaryName, ... |
| `internal/upgrade/selfupdate.go` | binaryName, SelfUpdate, tmpPath, rerr, err, ... |
| `internal/upgrade/verify.go` | path, expectedVersion, err, VerifyRunsVersion, out |

## Entry Points

- `internal/enrich/openai.go::OpenAI.Enrich`
- `internal/rerank/rerank.go::OllamaReranker.Score`
- `internal/config/config_test.go::TestRRFK_Config`

## Connected Communities

- **engine +12 dirs** (31 cross-edges)
- **engine +19 dirs** (28 cross-edges)
- **cli +13 dirs** (21 cross-edges)
- **reader +7 dirs** (16 cross-edges)
- **reader +19 dirs** (11 cross-edges)
- **reader +8 dirs** (5 cross-edges)
- **eval +6 dirs** (5 cross-edges)
- **daemon +5 dirs** (5 cross-edges)
- **cli +7 dirs** (4 cross-edges)
- **daemon +2 dirs** (3 cross-edges)
- **engine +7 dirs** (3 cross-edges)
- **engine +13 dirs** (3 cross-edges)
- **engine +2 dirs · waitForEpoch** (3 cross-edges)
- **enrich · TestParseEnrichJSON** (2 cross-edges)
- **rest +4 dirs** (2 cross-edges)
- **engine +10 dirs** (2 cross-edges)
- **. +1 dirs · stripInlineEmphasis** (2 cross-edges)
- **embed/modelbundle +3 dirs** (2 cross-edges)
- **. +3 dirs** (1 cross-edges)
- **engine +6 dirs** (1 cross-edges)
- **pipeline +2 dirs** (1 cross-edges)
- **engine +2 dirs · Get** (1 cross-edges)
- **model +2 dirs** (1 cross-edges)
- **upgrade +1 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-219"
smart_context with task: "understand daemon +15 dirs", format: "gcx"
find_usages with id: "internal/enrich/openai.go::OpenAI.Enrich", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
