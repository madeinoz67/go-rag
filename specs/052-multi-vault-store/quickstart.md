# Quickstart — Multi-Vault Unified Store (v2.0)

**Feature**: specs/052-multi-vault-store | **Date**: 2026-07-13

Multi-vault validation guide. Implementation detail lives in `tasks.md`.

---

## Prerequisites
- Built binary: `make build` → `./bin/go-rag`. Local Ollama running.
- A fresh unified store (no legacy per-vault DBs, OR use the migration below).

---

## Scenario 1 — one daemon, two vaults (spec US1)
```sh
TMPDB=$(mktemp -d)/store
./bin/go-rag init --db-path "$TMPDB"
./bin/go-rag start --db-path "$TMPDB"   # one daemon, ALL vaults
# add to "work" vault — self-registered on first write
echo "# Work doc" > /tmp/work.md
./bin/go-rag add --vault work /tmp/work.md
# add to "default" vault
echo "# Default doc" > /tmp/default.md
./bin/go-rag add --vault default /tmp/default.md
# query "work" — returns work.md only
./bin/go-rag query --vault work "work" --mode keyword
# query "default" — returns default.md only (isolation)
./bin/go-rag query --vault default "default" --mode keyword
```

## Scenario 2 — cross-vault query (spec US2)
```sh
# query across BOTH vaults — returns hits from both, ranked by RRF
./bin/go-rag query --vault default,work "doc" --mode keyword
```

## Scenario 3 — vault lifecycle (spec US3)
```sh
./bin/go-rag vault rename work projects   # instant (metadata-only)
./bin/go-rag query --vault projects "work" # results present (data didn't move)
./bin/go-rag vault clear projects          # instant (range-tombstone)
./bin/go-rag query --vault projects "work" # empty
```

## Scenario 4 — migration from legacy per-vault DBs (spec US5)
```sh
# with existing ~/.go-rag/vaults/default + ~/.go-rag/vaults/work
./bin/go-rag start   # the on-open migration (v3→v4) rewrites keys into the unified store
./bin/go-rag status  # both vaults present, correct counts
ls ~/.go-rag/vaults/ # legacy dirs archived (.prev), not deleted
```

## Scenario 5 — browser (Interceptor)
Open the console, use the vault picker to switch between vaults; run a cross-vault query (multi-
select); verify results from both. Confirm rename/clear work from the Vaults view (051 reframed).

---

## Teardown
```sh
./bin/go-rag stop; rm -rf "$TMPDB" /tmp/work.md /tmp/default.md
```
