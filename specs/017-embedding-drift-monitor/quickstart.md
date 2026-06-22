# Quickstart — Embedding Drift Monitoring Validation (H11)

> Phase 1 output. Runnable, end-to-end scenarios that prove the feature works.
> Implementation bodies belong in `tasks.md`, not here. Scenarios that need the
> daemon run against a started daemon on an **isolated DB** with non-default ports
> (per CLAUDE.md §Constraints — a bare `go-rag start` targets the live vault).
> Scenario 6 (gates) uses no Ollama.

## Prerequisites

- Go 1.26+ toolchain; `CGO_ENABLED=0` builds cleanly (`make build`).
- A local Ollama with **two** distinct embedding models pulled (e.g. `nomic-embed-text` and
  `mxbai-embed-large`) for scenarios 1–3; needed for the live version-pinning check.
- Isolated daemon (cache-style features share the long-lived engine; start one on isolated ports):
  `go-rag --db-path "$DB" start --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880`.
  Stop with `go-rag --db-path "$DB" stop`.

## Scenario 1 — Hard drift detected at boot (FR-004, SC-001) 🎯 MVP

```bash
DB=$(mktemp -d)/vault
go-rag --db-path "$DB" init
go-rag --db-path "$DB" config set embedding_model nomic-embed-text
go-rag --db-path "$DB" add <some-file>
go-rag --db-path "$DB" status        # baseline recorded under nomic-embed-text

# Reconfigure to a different model WITHOUT re-embedding (simulate a config change the corpus wasn't migrated for):
go-rag --db-path "$DB" config set embedding_model mxbai-embed-large

go-rag --db-path "$DB" start --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
# → boot log shows: hard drift (model: nomic-embed-text vs mxbai-embed-large)
go-rag --db-path "$DB" status        # Drift: hard-drift (model mismatch); Baseline: nomic-embed-text…
curl -s http://127.0.0.1:17879/health # → {"ok":true,"ready":false,"drift_verdict":"hard-drift",…}
```

**Expected**: the boot log + `status` report the model mismatch before any query; `/health` body shows
`ready: false` with `ok: true` (liveness OK, readiness NOT READY). Queries are refused by H03.

## Scenario 2 — Ollama-version change is a soft warning (FR-005, SC-002)

Build a corpus (baseline records the live Ollama version). Simulate a version change by editing the
baseline's recorded version (or restarting against a different Ollama build), then boot:

```bash
go-rag --db-path "$DB" status        # baseline: … ollama=0.x.y @<ts>; live version differs
# → boot log shows: version drift (baseline 0.x.y vs live 0.a.b)
curl -s http://127.0.0.1:17879/health # → {"ok":true,"ready":true,"drift_verdict":"version-warning",…}
curl -s -X POST http://127.0.0.1:17879/v1/query -d '{"query":"…","mode":"keyword","k":5}'  # query still succeeds
```

**Expected**: version change warns at boot + status, but `ready: true` and queries succeed (soft drift
does not refuse).

## Scenario 3 — Migrate refreshes the baseline (FR-002, SC-003)

```bash
# From scenario 1's drifted state, remediate:
go-rag --db-path "$DB" migrate       # re-embeds under mxbai-embed-large + refreshes the baseline
go-rag --db-path "$DB" status        # baseline now records mxbai-embed-large + current version; drift: clean
curl -s http://127.0.0.1:17879/health # → {"ok":true,"ready":true,"drift_verdict":"clean",…}
```

**Expected**: after `migrate`, the baseline reflects the new model + current Ollama version
(`recorded_at` advances), drift is `clean`, and `ready: true`.

## Scenario 4 — Pre-H11 corpus is backfilled on first boot (FR-007, SC-003)

A vault created before H11 has no baseline record. On the first H11 boot:

```bash
# (vault with embeddings but no baseline — e.g. an existing pre-017 vault copy)
go-rag --db-path "$OLD_DB" status     # baseline appears (backfilled from the stored majority + live version), no re-ingest
```

**Expected**: the baseline is backfilled (model/dim/convention from the existing embedding majority +
live Ollama version + now) without any re-embedding; drift verdict is `clean` (the corpus matches what
it was built under).

## Scenario 5 — Ollama unreachable at boot is safe (FR-006, SC-005)

```bash
# Stop Ollama, then start the daemon:
go-rag --db-path "$DB" start --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
go-rag --db-path "$DB" status        # live_ollama_version: unknown; model/convention verdict still computed
```

**Expected**: the daemon starts (no crash), the version check is skipped, `live_ollama_version` reports
`unknown`, and the model/convention drift checks still run.

## Scenario 6 — Build + test gates green (constitution §Dev Workflow)

```bash
CGO_ENABLED=0 go build ./...
go vet ./...
go test -race -cover ./...   # incl. H11 tests: baseline write/refresh/backfill, drift verdict
                             # (hard/soft/clean/unknown/n-a), readiness flag, status surface, offline-skip
```

**Expected**: all green. The dedicated hard-drift-readiness test (build a corpus under model A,
reconfigure to B, assert `Health.Ready==false && Health.OK==true` at boot) must pass — this is the
clarified posture-A gate.

## Scenario 7 — No quality regression (FR-010, SC-006)

```bash
make test-eval              # H02 harness; offline deterministic embedder → version check skipped
```

**Expected**: recall@10 unchanged (the version check is skipped on the offline embedder path; no
re-embedding, no quality change).

## Done

When scenarios 1–7 pass with build/vet/test/eval green, H11 meets every FR and SC and is ready for the
audit-backlog checkbox.
