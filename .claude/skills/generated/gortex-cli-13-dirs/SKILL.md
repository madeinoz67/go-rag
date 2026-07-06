---
name: gortex-cli-13-dirs
description: "Work in the cli +13 dirs area — 418 symbols across 26 files (68% cohesion)"
---

# cli +13 dirs

418 symbols | 26 files | 68% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `internal/audit/audit_test.go`
- `internal/cli/commands_test.go`
- `internal/cli/config_cli.go`
- `internal/cli/config_test.go`
- `internal/cli/init.go`
- `internal/cli/status_test.go`
- `internal/cli/vault.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/daemon/lifecycle_test.go`
- `internal/embed/modelbundle/bundle_test.go`
- `internal/engine/threat_test.go`
- `internal/eval/beir/beir.go`
- `internal/eval/benchmark.go`
- `internal/eval/run.go`
- `internal/mcp/server_test.go`
- `internal/pipeline/concurrent_test.go`
- `internal/reader/readers_test.go`
- `internal/upgrade/rollback_test.go`
- `internal/upgrade/selfupdate_e2e_test.go`
- `internal/upgrade/selfupdate_test.go`
- `internal/upgrade/verify.go`
- `internal/upgrade/verify_test.go`
- `internal/vault/registry.go`
- `internal/vault/registry_test.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | MarshalIndent, Pipe, WriteFile, filepath, NotFound, ... |
| `internal/audit/audit_test.go` | i, path, t, fi, TestAppender_Rotation, ... |
| `internal/cli/commands_test.go` | t, queryOut, err, docPath, t, ... |
| `internal/cli/config_cli.go` | closure@16, cmd, newConfigGetCmd, closure@35, newConfigCmd |
| `internal/cli/config_test.go` | saved, closure@39, dbPath, TestConfig_GetUnknownKey, initCmd, ... |
| `internal/cli/init.go` | closure@19, cmd, newInitCmd |
| `internal/cli/status_test.go` | info, TestStatus_DegradedWhenOllamaDown, out, closure@17, dbPath, ... |
| `internal/cli/vault.go` | cmd, closure@208, newVaultImportCmd, closure@253, newVaultExportCmd, ... |
| `internal/config/config.go` | Save, err, path, err, data, ... |
| `internal/config/config_test.go` | c, err, loaded, path, TestLoadSave_RoundTrip, ... |
| `internal/daemon/lifecycle_test.go` | t, TestPebbleLockHeld_AbsentNotHeld |
| `internal/embed/modelbundle/bundle_test.go` | p, err, body, TestVerifyHash_AcceptsMatchingHash, err, ... |
| `internal/engine/threat_test.go` | res, t, err, err, dir, ... |
| `internal/eval/beir/beir.go` | name, err, cacheDir, err, zipPath, ... |
| `internal/eval/benchmark.go` | content, err, dir, content, writeBenchmarkCorpus, ... |
| `internal/eval/run.go` | data, Save, err, path |
| `internal/mcp/server_test.go` | doc, dp, err, err, db, ... |
| `internal/pipeline/concurrent_test.go` | p, t, dbDir, err, err, ... |
| `internal/reader/readers_test.go` | name, t, err, TestTestDataFixturesExist |
| `internal/upgrade/rollback_test.go` | exe, err, dir, err, dir, ... |
| `internal/upgrade/selfupdate_e2e_test.go` | t, oldExe, targz, releaseBaseURL, TestSelfUpdateEndToEnd, ... |
| `internal/upgrade/selfupdate_test.go` | exe, TestApplySwap, err, err, dir, ... |
| `internal/upgrade/verify.go` | err, fi, path, VerifyExecutable |
| `internal/upgrade/verify_test.go` | execPath, noExec, err, t, err, ... |
| `internal/vault/registry.go` | name, home, err, err, List, ... |
| `internal/vault/registry_test.go` | TestCreateExistsList, err, err, names, err, ... |

## Entry Points

- `internal/cli/commands_test.go::TestCLI_InitAddQuery`
- `internal/mcp/server_test.go::TestMCP_Init`
- `internal/cli/status_test.go::TestStatus_AfterIngest`

## Connected Communities

- **cli +7 dirs** (39 cross-edges)
- **daemon +15 dirs** (29 cross-edges)
- **engine +12 dirs** (28 cross-edges)
- **engine +19 dirs** (14 cross-edges)
- **reader +19 dirs** (13 cross-edges)
- **engine +7 dirs** (9 cross-edges)
- **engine +13 dirs** (7 cross-edges)
- **daemon +5 dirs** (4 cross-edges)
- **eval +6 dirs** (4 cross-edges)
- **pipeline +2 dirs** (4 cross-edges)
- **rest +4 dirs** (3 cross-edges)
- **reader +8 dirs** (2 cross-edges)
- **embed/modelbundle +3 dirs** (2 cross-edges)
- **embed/modelbundle** (2 cross-edges)
- **. +1 dirs · zipReader** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **engine · ImportThreatSource** (1 cross-edges)
- **audit** (1 cross-edges)
- **config +2 dirs · ApplyEnvOverrides** (1 cross-edges)
- **index +1 dirs · Search** (1 cross-edges)
- **watcher +3 dirs** (1 cross-edges)
- **vault** (1 cross-edges)
- **audit +4 dirs** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-227"
smart_context with task: "understand cli +13 dirs", format: "gcx"
find_usages with id: "internal/cli/commands_test.go::TestCLI_InitAddQuery", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
