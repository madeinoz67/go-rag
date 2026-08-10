# Quickstart — Spec 060 MuninnDB Bridge

**Purpose**: a runnable validation runbook that proves the feature end-to-end. Not a test suite — those land in `tasks.md`. This is the operator-facing "does it work" walk.

## Prerequisites

- A built go-rag binary on `main` with spec 060 shipped: `make build` → `./bin/go-rag`.
- A local MuninnDB running on loopback gRPC `127.0.0.1:8477` with the UPSERT surface (muninndb ≥ the `#659` merge `e4d6ad21`).
- A MuninnDB `mk_` key for a dedicated target vault (create one in MuninnDB, e.g. vault `go-rag`).
- An **isolated** go-rag test vault (NOT the global default — the repo convention: pass `--db-path /tmp/gorag-bridge` and non-default transport ports to avoid colliding with a live daemon).

## Setup (isolated daemon)

```sh
export GORAG_BRIDGE_TOKEN="mk_<your-go-rag-vault-key>"
./bin/go-rag bridge muninn init --non-interactive \
  --db-path /tmp/gorag-bridge \
  --endpoint 127.0.0.1:8477 \
  --source-vault default \
  --target-vault go-rag \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
./bin/go-rag start --db-path /tmp/gorag-bridge \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
```

`go-rag bridge muninn init` writes the flat `Bridge*` config + flips `BridgeEnabled=true`. Start boots the daemon + the bridgeProc (gated by `EffectiveBridgeEnabled()`).

## US1 — opt-in promotion + cognitive hygiene

1. Add a document:
   ```sh
   ./bin/go-rag add --db-path /tmp/gorag-bridge docs/some.md
   ```
2. **Within seconds**, the document's chunks appear as engrams in MuninnDB vault `go-rag` (verify via a MuninnDB client: `muninn recall` scoped to that vault, or `muninn find-by-entity` for a `go-rag` tag).
3. **No-op assertion (NFR-002)**: capture an engram's `access_count`/`updated_at`/`last_access` via `Read`, then re-ingest the unchanged document:
   ```sh
   ./bin/go-rag add --db-path /tmp/gorag-bridge docs/some.md   # re-promote, identical content
   ```
   Re-`Read` the same engram id; the three fields are byte-identical (UPSERT left it alone). **Expected:** unchanged. A bump here is a cognitive-hygiene regression.
4. **Changed chunk ⇒ new engram**: edit `docs/some.md`, re-add. The changed chunks get new `chunk_id`s ⇒ new `idempotent_id`s ⇒ CREATED in MuninnDB (verify: a new engram id, not the old one mutated). The old engrams remain.

## US2 — auto-backfill, storm-limited, pausable

1. On a fresh test vault with an existing corpus, enable the bridge (the config flip above). The backfill **starts automatically** — confirm via status:
   ```sh
   ./bin/go-rag bridge muninn status --db-path /tmp/gorag-bridge
   # backfill.running=true; promoted climbs toward the corpus total
   ```
2. **Storm-limit**: while backfill runs, run a foreground query:
   ```sh
   ./bin/go-rag query --db-path /tmp/gorag-bridge "some term"
   ```
   Query latency stays within its normal budget (the `BridgeMaxInFlight`/`BridgeRatePerSec` caps hold). Expected: no measurable degradation.
3. **Pause/resume**:
   ```sh
   ./bin/go-rag bridge muninn pause  --db-path /tmp/gorag-bridge   # promotion stops; backfill holds its place
   ./bin/go-rag bridge muninn resume --db-path /tmp/gorag-bridge   # continues; no duplicate engrams
   ```
   Expected: resume completes the backfill with `promoted_total` matching the corpus, zero duplicates (UPSERT no-op on already-promoted chunks).

## US3 — Memory & Graph view (retires the last placeholder)

1. With the bridge enabled + ≥1 doc promoted, open the console in a real browser (Interceptor — mandatory for visual verification):
   `http://127.0.0.1:17881/` → Memory & Graph (9th sidebar item).
2. **Browse renders**: the Activate-driven list shows engrams from vault `go-rag` with concept/score/tags.
3. **Detail renders**: click a row → per-engram detail (content, access_count, associations).
4. **Degraded state**: stop MuninnDB, reload the view → a clear degraded/empty state (not a crash/hang).
5. **Auth guard**: `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:17881/api/memory-graph/browse` (no Bearer) → **401**.

## Resilience spot-checks

- **MuninnDB down, write unaffected**: stop MuninnDB, `./bin/go-rag add ...` → ACKs in the normal `<10ms` budget; the doc is fully queryable in go-rag; bridge status shows `healthy=false, circuit=open`.
- **`go-rag stop` doesn't wedge**: with MuninnDB down + in-flight promotions, `./bin/go-rag stop --db-path /tmp/gorag-bridge` completes within the daemon's stop budget (the bridgeProc drain is bounded — the embedproc lesson). Expected: prompt shutdown.

## Gates (run before declaring done)

```sh
make build && make vet && make test    # the repo standard; -race on storage/concurrency
make lint                              # golangci-lint, the ci.yml gate (0 issues)
```

Plus the spec 060 invariants asserted by tests (land in `tasks.md`): the NFR-002 cognitive-hygiene property (fake `MuninnClient` recording `Read` values across N re-promotions), the circuit-breaker open/closed transitions, and the bounded-drain timeout.

## Cleanup

```sh
./bin/go-rag stop --db-path /tmp/gorag-bridge
rm -rf /tmp/gorag-bridge
# kill any orphaned test daemon by port (the repo gotcha — pkill misses the detached re-exec):
for p in 17878 17879 17880 17881; do lsof -ti :$p | xargs kill -9; done
```
