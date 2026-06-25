# Quickstart — Migration Dry-Run (H24, spec 028)

**Phase 1 output.** A validation runbook proving the dry-run previews correctly
and — critically, because it claims read-only — **changes nothing**. Every
scenario maps to a Success Criterion in [spec.md](./spec.md).

> **Smoke rule (repo `CLAUDE.md`):** the default `dbPath` is the global vault
> (`~/.go-rag/vaults/default`). These checks use a **temporary isolated DB**
> (`--db-path <tmp>`) so they never touch a live daemon or real corpus. The
> dry-run itself needs **no Ollama** (metadata-only) — only the real-migrate
> comparison (SC-005) needs a backend.

---

## Prerequisites

- Go 1.22+, `CGO_ENABLED=0`.
- A local Ollama with an embedding model — **only for SC-005** (the real-migrate
  comparison). SC-001…SC-004 run without it.

## Build gate

```bash
make build vet test
```

**Expected:** green. The new `MigratePlan` path and its tests compile clean.

---

## SC-001 — Preview without re-embedding (read-only)

Seed an isolated DB with embeddings on a model, then change the configured model,
then dry-run — and confirm nothing changed.

```bash
DB=$(mktemp -d)
go run ./cmd/go-rag init --db-path "$DB"            # configures an embedding model
# (ingest a small fixture so embeddings exist; OR use an existing test helper)
go run ./cmd/go-rag migrate --dry-run --db-path "$DB"
# EXPECT: a plan printed (target model, sources, stale_total), exit 0,
#         and the corpus is UNCHANGED.
```

**Pass:** the plan is shown; re-running `status` shows identical embedding counts
before and after; no embedding was generated. The unit test
(`internal/engine/migrate_plan_test.go`) asserts corpus/cache/baseline/epoch are
byte-identical before/after `MigratePlan`.

## SC-002 — Succeeds with no backend (metadata-only)

```bash
# Point Ollama at a dead URL, then dry-run:
go run ./cmd/go-rag migrate --dry-run --db-path "$DB"   # with OllamaURL unreachable
```

**Pass:** the dry-run still returns the full plan (no connection error). The unit
test runs `MigratePlan` against an engine whose embedder is unreachable and
asserts success + correct plan. Proves FR-004.

## SC-003 — Actionable cost estimate

On a **mixed** corpus (embeddings on two models, or two dims):

```bash
go run ./cmd/go-rag migrate --dry-run --db-path "$DB"
```

**Pass:** the output reports `stale_total`, each source model with its count and
stale flag, the dimensionality distribution, `consistent=false`, and an
`estimate` block labelled approximate. On a clean single-model corpus, the same
command reports `stale_total=0` and `consistent=true`. Proves FR-002/FR-005.

## SC-004 — Parity across transports (identical plan, zero mutation)

```bash
go test -race ./internal/engine/    # incl. parity_test.go extension
```

**Pass:** the cross-transport test invokes the preview over CLI/REST/gRPC/MCP on
the same corpus and asserts the returned `MigrationPlan` is byte-identical, and
that each leaves the corpus/caches/baseline/epoch unchanged. Proves FR-006/FR-003.

## SC-005 — Preview matches execution

With Ollama up, on a corpus with stale embeddings:

```bash
PLAN=$(go run ./cmd/go-rag migrate --dry-run --db-path "$DB")   # capture stale_total
go run ./cmd/go-rag migrate --db-path "$DB"                      # real migrate
go run ./cmd/go-rag migrate --dry-run --db-path "$DB"            # now stale_total should be 0
```

**Pass:** the number of embeddings the dry-run reported stale equals the number
the real migrate re-embedded; after migrate, the dry-run reports `stale_total=0`.
Proves FR-008 (preview == execution). The unit test asserts this directly by
comparing `MigratePlan().StaleTotal` to the real `Migrate` result.

---

## Summary of expected outcomes

| SC | Check | Command | Pass condition |
|----|-------|---------|----------------|
| SC-001 | preview is read-only | `migrate --dry-run` + unit test | plan shown; corpus/cache/baseline/epoch unchanged |
| SC-002 | no backend needed | `--dry-run` with Ollama down | full plan returned, no error |
| SC-003 | actionable estimate | `--dry-run` on mixed corpus | stale count + dims + consistency + labelled estimate |
| SC-004 | parity + zero mutation | `go test ./internal/engine/` | identical plan across 4 transports; nothing mutated |
| SC-005 | preview == execution | dry-run → migrate → dry-run | stale count matches re-embedded count; post-migrate 0 |

If all five pass, the operator can see the migration bill before paying it — on
every transport — with a hard guarantee that looking never changes anything.
