# Quickstart — Adaptive Retrieval Depth & Pool-Size Tuning (H22)

> Phase 1 validation guide. Runnable scenarios that prove the feature works end-to-end against the four user stories and the success criteria. **Not** an implementation reference — code lives in `tasks.md`. See `data-model.md` for entity shapes and the `contracts/` files for the exact transport/config surfaces.

## Prerequisites

- A working local Ollama with an embedding model pulled (e.g. `nomic-embed-text`), OR use the **offline deterministic embedder** via the eval harness (no Ollama needed for the no-regression gate).
- `make build` succeeds → `./bin/go-rag`.
- **Isolated DB for smoke** (per `CLAUDE.md`: the default `dbPath` is the global vault — always point smoke at a tmp path + non-default transport addrs to avoid colliding with a live daemon).

```bash
# one-time: build + make a throwaway vault with a tiny corpus
make build
DB=$(mktemp -d)/smoke
mkdir -p "$DB/corpus" "$DB/corpus/sub"
cat > "$DB/corpus/factoid.md" <<'EOF'
# Limits
The maximum batch size is 512. The default timeout is 30 seconds.
EOF
cat > "$DB/corpus/comparative.md" <<'EOF'
# Caching approaches
Write-through caching writes to cache and store together.
Write-behind caching writes to cache first, store later.
Cache-aside lets the application manage the store.
# Drift approaches
Drift can be detected by schema comparison or by statistical sampling.
EOF
./bin/go-rag init --db-path "$DB" --ollama-url http://localhost:11434 --model nomic-embed-text
./bin/go-rag add --db-path "$DB" "$DB/corpus"
# wait for embeddings to complete, then confirm:
./bin/go-rag status --db-path "$DB" | grep -E "chunks|embeddings"
```

---

## Scenario 1 — Default-OFF is byte-identical (FR-007 / SC-005)

*Proves adaptation is strictly opt-in.*

```bash
# baseline: classifier off (default), no pool override
./bin/go-rag query --db-path "$DB" --format json "what is the max batch size" > off.json
# enable the classifier + re-run the SAME query; results/order MUST be identical
# (factoid recommendation only changes the pool/k when it is safe — but to prove the
# default-off baseline, compare against the with-classifier run too where applicable)
diff <(jq -c '.[].chunk_id' off.json) <(jq -c '.[].chunk_id' off.json) && echo "self-consistent"
```
**Expected**: default-off query returns the same passages in the same order as a pre-H22 build. `make test-eval` is the formal gate (Scenario 5).

---

## Scenario 2 — Tunable pool, same query, no code change (US1 / FR-001 / FR-002)

*Proves an operator can grow/shrink the candidate pool per query and that the default is preserved.*

```bash
# default pool (60)
./bin/go-rag query --db-path "$DB" --format json --mode keyword "compare caching and drift approaches" > pool-default.json
# larger pool — a broad/comparative query that benefits from more candidates
./bin/go-rag query --db-path "$DB" --format json --mode keyword --pool-size 120 "compare caching and drift approaches" > pool-large.json
# smaller pool
./bin/go-rag query --db-path "$DB" --format json --mode keyword --pool-size 20 "max batch size" > pool-small.json
# verify the effective pool is echoed in each response (US3)
jq '.effective_pool' pool-large.json   # → 120
jq '.effective_pool' pool-small.json   # → 20
jq '.effective_pool' pool-default.json # → 60
```
**Expected**: `--pool-size 0` (or omitted) ⇒ `effective_pool == 60` (today's value). `--pool-size N` ⇒ `effective_pool == N`. Recall on the broad query with `pool-size 120` is ≥ the default-pool recall (more candidates surfaced); the factoid query with `pool-size 20` still returns the right chunk.

**Cross-transport parity check (FR-009)** — start an isolated daemon and confirm REST/gRPC/MCP agree:
```bash
./bin/go-rag start --db-path "$DB" --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
sleep 2
# REST
curl -s -X POST http://127.0.0.1:17879/v1/query -d '{"query":"max batch size","mode":"keyword","pool_size":25}' | jq '.effective_pool'   # → 25
# gRPC (via grpcurl against the generated proto)
grpcurl -plaintext -d '{"query":"max batch size","mode":"keyword","pool_size":25}' 127.0.0.1:17880 gorag.Gorag/Query | jq '.effective_pool' # → 25
# MCP (JSON-RPC)
curl -s -X POST http://127.0.0.1:17878 -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"go_rag_query","arguments":{"query":"max batch size","mode":"keyword","pool_size":25}}}' | jq '.result.content[0].text' # effective_pool 25
./bin/go-rag stop --db-path "$DB" --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
```
**Expected**: all three return `effective_pool == 25` and the same hit(s) — cross-transport parity holds.

---

## Scenario 3 — Adaptive depth via the classifier (US2 / FR-005 / FR-006 / SC-002)

*Proves a factoid and a comparative query use different effective depths with the classifier on, and that explicit `k` wins.*

```bash
./bin/go-rag config set --db-path "$DB" adaptive_depth_enabled true
# factoid (no explicit k) → classifier recommends a SHALLOW k (e.g. 3) → small effective pool
./bin/go-rag query --db-path "$DB" --format json "max batch size" > factoid.json
# comparative (no explicit k) → no recommendation → default depth, full pool
./bin/go-rag query --db-path "$DB" --format json "compare the caching and drift approaches across the corpus" > comparative.json
jq -c '{k:.effective_k, pool:.effective_pool}' factoid.json
jq -c '{k:.effective_k, pool:.effective_pool}' comparative.json
# explicit k beats the classifier (FR-006): ask for k=8 on the factoid
./bin/go-rag query --db-path "$DB" --format json --k 8 "max batch size" | jq -c '{k:.effective_k,pool:.effective_pool}' # → effective_k 8
./bin/go-rag config set --db-path "$DB" adaptive_depth_enabled false
```
**Expected**: factoid `effective_k` (3) < comparative `effective_k` (default 5), and factoid `effective_pool` shrinks with the recommended `k` (`k + slack`, clamped to `[floor, ceiling]`) while comparative uses the full configured pool. Explicit `--k 8` overrides the classifier. With `adaptive_depth_enabled=false`, both queries use default depth/pool (Scenario 1).

---

## Scenario 4 — Observability: status shows the knobs (US3 / FR-003 / SC-004)

```bash
./bin/go-rag config set --db-path "$DB" adaptive_depth_enabled true
./bin/go-rag config set --db-path "$DB" pool_size 90
# run a couple of queries to populate utilization...
./bin/go-rag query --db-path "$DB" "max batch size" >/dev/null
./bin/go-rag query --db-path "$DB" --pool-size 30 "caching approaches" >/dev/null
./bin/go-rag status --db-path "$DB" --format json | jq '{pool_size, adaptive_depth_enabled, pool_utilization}'
./bin/go-rag config set --db-path "$DB" adaptive_depth_enabled false
./bin/go-rag config set --db-path "$DB" pool_size 60
```
**Expected**: `pool_size == 90`, `adaptive_depth_enabled == true`, and `pool_utilization.queries >= 2` with non-zero `avg_fetched`/`avg_kept`. After reset, `pool_size == 60` and `adaptive_depth_enabled == false`.

---

## Scenario 5 — No quality regression (FR-010 / SC-003)

*The formal gate — offline, reproducible, runs the real query path.*

```bash
make test-eval
```
**Expected**: recall@10 ≥ the golden baseline (`testdata/golden/baseline.json`, tolerance 2.0) with every new behavior at its default-off posture. This is the single acceptance check that H22 has not regressed existing users.

---

## Scenario 6 — Build/vet/test stay green (Constitution Development & Quality Workflow)

```bash
CGO_ENABLED=0 go build ./... && go vet ./... && go test -race -cover ./...
```
**Expected**: all pass. The pure-Go build gate stays green (Constitution III); no new dependency introduced (FR-008).

---

## Edge cases to spot-check (from `spec.md`)

- **Explicit `k` + classifier on** → explicit wins (Scenario 3). ✓
- **Pool smaller than `k`** → effective pool grown to ≥ `k` (never fewer than requested top-`k`). ✓
- **Empty-after-normalization query** → classifier returns `K:0`; default depth; no crash. ✓
- **Reranker unavailable** → pool still governs the fusion budget; utilization `saturated` reflects the reranker-absent condition. ✓
- **Result cache** → two queries differing only in `pool_size` get distinct cache entries (verify by running the same query twice with different `--pool-size` and confirming both compute, and repeating one hits the cache — R5).

---

## Done-when

All six scenarios pass with their expected outputs, `make test-eval` is green, and the build/vet/test gate is clean. Then proceed to `/speckit-tasks` for the implementation task list.
