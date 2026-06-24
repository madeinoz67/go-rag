# Contract — New Config Keys

> Two new persisted config keys (`internal/config/config.go`, stored in `.go-rag/config.json`, PRD §5.7). Both follow the established `Config`-struct / `Default()` / `Effective*()` / `Validate()` / `Get()` / `Set()` pattern. Both are default-OFF-or-neutral so an existing config file changes nothing on upgrade (FR-007/SC-005).

## `pool_size` — configured candidate-pool ceiling

- **Type**: `int`. JSON key: `"pool_size"`.
- **Default**: **60** (today's hardcoded `Retrieval.poolSize`; `internal/index/retrieval.go:63`). `0` ⇒ default 60 (absent-key = default, same backward-compat rule as `rrf_k`).
- **Validation**: `pool_size < 0` is invalid (`Validate()` rejects, mirroring `rrf_k`). `pool_size == 0` is allowed and resolves to 60.
- **Effective accessor**:
  ```go
  const DefaultPoolSize = 60
  func (c Config) EffectivePoolSize() int {
      if c.PoolSize > 0 { return c.PoolSize }
      return DefaultPoolSize
  }
  ```
- **Semantics**: the configured **ceiling** for the candidate pool (data-model.md Entity 2). Per-query override (`--pool-size`) and classifier-derived shrinking both clamp against it. With the classifier off and no override, this is the exact pool used ⇒ byte-identical to pre-H22.
- **Surfaced**: `status` → `pool_size` (the effective value); `config get/set pool_size`.

## `adaptive_depth_enabled` — classifier posture

- **Type**: `bool`. JSON key: `"adaptive_depth_enabled"`.
- **Default**: **false** (default posture = no classification, FR-007; spec US2 scenario 3). Absent key ⇒ false (the `bool` zero value already means off, so unlike the cache/poisoning keys no special backward-compat rule is needed — a pre-H22 config omits it and stays off).
- **Validation**: none (bool).
- **Effective accessor**:
  ```go
  func (c Config) EffectiveAdaptiveDepthEnabled() bool { return c.AdaptiveDepthEnabled }
  ```
- **Semantics**: when true, the engine constructs `index.RuleBasedClassifier{}` and consults it for `k` when the caller hasn't set one (FR-005/FR-006). When false, `e.classifier` is nil and no classification occurs.
- **Surfaced**: `status` → `adaptive_depth_enabled`; `config get/set adaptive_depth_enabled`.

## Wiring checklist (all in `internal/config/config.go` unless noted)

For each key:
1. Field on `Config` struct (with `// H22/spec 024` comment).
2. Value in `Default()` (`PoolSize: 60`; `AdaptiveDepthEnabled` omitted ⇒ false).
3. `const DefaultPoolSize = 60` (next to `DefaultRRFK`).
4. `EffectivePoolSize()` / `EffectiveAdaptiveDepthEnabled()` methods.
5. Guard in `Validate()` (`pool_size < 0` rejected).
6. `case` in `Get()` (return `strconv.Itoa(c.EffectivePoolSize())` / `strconv.FormatBool(...)`).
7. `case` in `Set()` (parse + validate; reject negative pool).
8. Add `"pool_size"`, `"adaptive_depth_enabled"` to `knownConfigKeys` (`internal/engine/config.go`).
9. Add both to the CLI config allowlist (`internal/cli/config_cli.go`).
10. Backward-compat note in `Load()`: **only `pool_size` needs the absent-key rule** (so an old config that omits it resolves to 60, not 0). `adaptive_depth_enabled` needs none (false is the zero value and the desired default).

## Out of scope
- `pool_slack` / `pool_floor` as config keys — deferred (R4). They ship as package constants in `internal/index` and are promoted to config only if the eval harness shows operators need per-corpus tuning.
- Removing or repurposing the dead `rerank_candidates` key — explicitly untouched (R2).
