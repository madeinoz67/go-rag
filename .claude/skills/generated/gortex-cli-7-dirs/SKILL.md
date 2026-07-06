---
name: gortex-cli-7-dirs
description: "Work in the cli +7 dirs area — 335 symbols across 35 files (71% cohesion)"
---

# cli +7 dirs

335 symbols | 35 files | 71% cohesion

## When to Use

Use this skill when working on files in:
- ``
- `cmd/go-rag/main.go`
- `external-call::dep:github.com/spf13/cobra`
- `internal/cli/add.go`
- `internal/cli/audit.go`
- `internal/cli/chunk.go`
- `internal/cli/commands.go`
- `internal/cli/dashboard.go`
- `internal/cli/dirs.go`
- `internal/cli/enrich.go`
- `internal/cli/eval.go`
- `internal/cli/eval_gen.go`
- `internal/cli/files.go`
- `internal/cli/health.go`
- `internal/cli/migrate.go`
- `internal/cli/poison.go`
- `internal/cli/progress.go`
- `internal/cli/query.go`
- `internal/cli/reprocess.go`
- `internal/cli/root.go`
- `internal/cli/scan.go`
- `internal/cli/start.go`
- `internal/cli/status.go`
- `internal/cli/stop.go`
- `internal/cli/threat.go`
- `internal/cli/upgrade.go`
- `internal/cli/upgrade_test.go`
- `internal/cli/vault.go`
- `internal/cli/wire.go`
- `internal/daemon/lifecycle.go`
- `internal/daemon/process_unix.go`
- `internal/engine/engine.go`
- `internal/engine/reenrich.go`
- `internal/upgrade/daemon.go`
- `internal/vault/registry.go`

## Key Files

| File | Symbols |
|------|---------|
| `` | Duration, Join, WalkDir, fmt, Sscanf, ... |
| `cmd/go-rag/main.go` | main, err |
| `external-call::dep:github.com/spf13/cobra` | github.com/spf13/cobra |
| `internal/cli/add.go` | cmd, closure@16, newAddCmd |
| `internal/cli/audit.go` | closure@25, newAuditCmd, cmd |
| `internal/cli/chunk.go` | closure@61, newChunkCmd, c, getChunkResponseOut, newChunkGetCmd, ... |
| `internal/cli/commands.go` | version, newVersionCmd, closure@16 |
| `internal/cli/dashboard.go` | embColor, ollamaColor, dimStr, info, vp, ... |
| `internal/cli/dirs.go` | dirEntry, newDirsCmd, Files, Dir, closure@25, ... |
| `internal/cli/enrich.go` | newEnrichCmd, closure@19 |
| `internal/cli/eval.go` | cmd, newEvalCmd |
| `internal/cli/eval_gen.go` | newEvalGenCmd, closure@32, cmd |
| `internal/cli/files.go` | Path, Chunks, Status, newFilesCmd, Type, ... |
| `internal/cli/health.go` | newHealthCmd, closure@56, defaultAddr, defaultAddr, cmd |
| `internal/cli/migrate.go` | parts, flag, newMigrateCmd, d, s, ... |
| `internal/cli/poison.go` | closure@91, cmd, newPoisonReleaseCmd, closure@54, newPoisonRescanCmd, ... |
| `internal/cli/progress.go` | filled, s, path, pct, status, ... |
| `internal/cli/query.go` | newQueryCmd, Score, preview, ChunkIndex, Chunk, ... |
| `internal/cli/reprocess.go` | cmd, closure@19, newReprocessCmd |
| `internal/cli/root.go` | Execute, version |
| `internal/cli/scan.go` | cmd, closure@21, newScanCmd |
| `internal/cli/start.go` | closure@14, newStartCmd, cmd |
| `internal/cli/status.go` | statusInfo, ModelCounts, EmbeddingConvention, embedded, db, ... |
| `internal/cli/stop.go` | closure@14, newStopCmd |
| `internal/cli/threat.go` | closure@30, closure@84, newThreatRemoveCmd, cmd, newThreatListCmd, ... |
| `internal/cli/upgrade.go` | cmd, newUpgradeCmd, closure@28, version |
| `internal/cli/upgrade_test.go` | f, cmd, t, TestUpgradeCmdFlagsAndUse |
| `internal/cli/vault.go` | info, closure@63, Model, newVaultCreateCmd, newVaultCmd, ... |
| `internal/cli/wire.go` | base, openDB |
| `internal/daemon/lifecycle.go` | Status, probeHealth, dbPath, resp, client, ... |
| `internal/daemon/process_unix.go` | proc, pid, isProcessRunning, err |
| `internal/engine/engine.go` | rc, cfg, db, NewWithDB, ec, ... |
| `internal/engine/reenrich.go` | String |
| `internal/upgrade/daemon.go` | DaemonRunning, pid, err |
| `internal/vault/registry.go` | Path, name |

## Entry Points

- `internal/cli/query.go::newQueryCmd`
- `internal/cli/dashboard.go::printVaultsOverview`
- `internal/cli/dashboard.go::printVaultDetail`

## Connected Communities

- **engine +19 dirs** (42 cross-edges)
- **engine +12 dirs** (29 cross-edges)
- **cli +13 dirs** (21 cross-edges)
- **engine +7 dirs** (17 cross-edges)
- **reader +8 dirs** (17 cross-edges)
- **daemon +15 dirs** (17 cross-edges)
- **engine +13 dirs** (13 cross-edges)
- **reader +19 dirs** (7 cross-edges)
- **eval +6 dirs** (6 cross-edges)
- **pipeline +4 dirs** (4 cross-edges)
- **pipeline +2 dirs** (3 cross-edges)
- **engine +1 dirs · ResetChunk** (2 cross-edges)
- **embed · ForRole** (2 cross-edges)
- **watcher +3 dirs** (2 cross-edges)
- **index +1 dirs · Search** (2 cross-edges)
- **. +1 dirs · processAlive** (2 cross-edges)
- **engine · ImportThreatSource** (2 cross-edges)
- **daemon +5 dirs** (2 cross-edges)
- **config +2 dirs · ApplyEnvOverrides** (1 cross-edges)
- **reader +7 dirs** (1 cross-edges)
- **cli · documentOut** (1 cross-edges)
- **engine +3 dirs** (1 cross-edges)
- **daemon +2 dirs** (1 cross-edges)
- **storage +2 dirs** (1 cross-edges)
- **. +3 dirs** (1 cross-edges)
- **embed/modelbundle +3 dirs** (1 cross-edges)
- **engine · newQueryCaches** (1 cross-edges)
- **upgrade +1 dirs** (1 cross-edges)
- **upgrade** (1 cross-edges)
- **engine +2 dirs · Score** (1 cross-edges)
- **config +2 dirs · pipeline** (1 cross-edges)
- **engine +1 dirs · Watch** (1 cross-edges)
- **engine +2 dirs · Get** (1 cross-edges)
- **audit +4 dirs** (1 cross-edges)
- **cli · chunkOut** (1 cross-edges)
- **audit** (1 cross-edges)
- **. +1 dirs · stripInlineEmphasis** (1 cross-edges)

## How to Explore

```
get_communities with id: "community-222"
smart_context with task: "understand cli +7 dirs", format: "gcx"
find_usages with id: "internal/cli/query.go::newQueryCmd", format: "gcx"
```

_`format: "gcx"` returns the [GCX1 compact wire format](../../docs/wire-format.md) — round-trippable, ~27% fewer tokens than JSON. Drop it for JSON output; agents using `@gortex/wire` or the Go `github.com/gortexhq/gcx-go` package decode either._
