# Contract: MuninnDB gRPC Client (outbound)

**Direction**: go-rag → MuninnDB (outbound only; go-rag never serves MuninnDB RPCs).
**Transport**: gRPC, `127.0.0.1:8477` (loopback-only, enforced at dial).
**Authority**: muninndb `proto/muninn/v1/service.proto` @ `e4d6ad21` (verified verbatim, research.md R1).

The bridge vendors the generated `muninn_v1` client stub read-only under `proto/muninn/v1/`. All outbound calls go through one `MuninnClient` interface (transport-agnostic; fakeable in tests).

## Connection

- `grpc.NewClient(endpoint, grpc.WithContextDialer(loopbackDialer), grpc.WithUnaryInterceptor(bearerInterceptor))`.
- **`loopbackDialer`**: refuses any non-loopback (`127.0.0.0/8`, `::1`) address at dial time — defense-in-depth against DNS rebinding (a loopback hostname could otherwise resolve to a public IP). The config-layer `net.ParseIP` check is the first gate.
- **`bearerInterceptor`**: attaches `metadata.Pairs("authorization", "Bearer "+token)` to every outgoing RPC. `token` = `GORAG_BRIDGE_TOKEN` env (the target vault `mk_` key). Mirrors the server-side interceptor shape already in `internal/grpc/server.go`.
- Timeouts: dial `BridgeConnectTimeoutMs` (default 5000); per-RPC `BridgeRequestTimeoutMs` (default 30000).
- Backoff: exponential on `RESOURCE_EXHAUSTED` (1s→2s→4s→8s→16s, max 60s ±20% jitter) and on transient failures (5 retries). Reconnect on connection loss.

## `MuninnClient` interface (v1 surface)

```go
type MuninnClient interface {
    Hello(ctx context.Context) (caps *Capabilities, err error)          // capability + health probe
    Write(ctx context.Context, req *WriteRequest) (id string, createdAt int64, err error)
    BatchWrite(ctx context.Context, vault string, reqs []*WriteRequest) (results []BatchItemResult, err error)
    Read(ctx context.Context, vault, id string) (*Engram, err error)     // detail + the NFR-002 verification probe
    Activate(ctx context.Context, vault string, contextPhrases []string) (<-chan Activation, err error) // browse stream
    Healthy() bool
}
```

`Link` (wikilink Hebbian edges) is v1-disabled (on-query hook stubbed); the interface reserves it for the follow-up.

## WriteRequest (the load-bearing write)

Built by the mapper (data-model.md E4). Verbatim proto fields:

```
concept(1) content(2) tags(3) confidence(4) stability(5) vault(6)
idempotent_id(7) associations(8) embedding(9) memory_type(10) type_label(11) upsert_mode(12)
```

Bridge-set values: `embedding = nil`, `stability = 30.0`, `vault = BridgeTargetVault`, `idempotent_id = "chunk:"+chunkID`, `upsert_mode = true`, plus concept/tags/associations per the mapper. **`idempotent_id` MUST be non-empty when `upsert_mode=true`** (MuninnDB rejects the bare-upsert case fail-loud).

**Response** (`WriteResponse`): `{id(1), created_at(2)}` — **only**. No outcome field. The bridge cannot tell created-vs-no-op from the response; it verifies the no-op via `Read` (NFR-002).

## Semantics the bridge relies on (the correctness contract)

For `(vault, idempotent_id)`:
- key miss ⇒ **CREATED** (fresh engram).
- key hit + byte-identical content ⇒ **left alone** (strict no-op — no `access_count`/weight/decay change). *This is what makes re-promotion safe.*
- key hit + changed content ⇒ **EVOLVED** (new version supersedes; predecessor soft-deleted, kept as history).

The content-addressed bridge only ever produces the first two cases: an unchanged chunk re-promotes identically (left alone); a changed chunk gets a new `chunk_id` (Principle II) ⇒ new `idempotent_id` ⇒ CREATED (never EVOLVED). NFR-002 asserts the "left alone" case via `Read` before/after.

## Read (the verification + view-detail probe)

```
ReadRequest { id(1), vault(2) }
ReadResponse { id, concept, content, confidence, relevance, tags, state, created_at, updated_at, last_access, access_count, stability, memory_type, type_label }
```

NFR-002 cognitive-hygiene test: `Read` after the first promotion captures `access_count`/`updated_at`/`last_access`; after N re-promotions of the unchanged chunk, a second `Read` asserts those three fields are byte-identical.

## Activate (the view browse path)

```
ActivateRequest { vault, context_phrases[], ... }
stream ActivateResponse { engram_id, concept, score, last_access, ... }
```

The Memory & Graph view (Q3 = live target-vault graph) drives its browse list from `Activate` scoped to `BridgeTargetVault`, and fetches per-row detail via `Read`. No `ListEngrams` RPC exists upstream (research.md R5), so v1 is browse/detail-driven.

## Hello (capability + health)

```
HelloRequest { version(1), auth_method(2), token(3) }
HelloResponse { capabilities, server_version, limits (incl. MaxEngramContentBytes) }
```

Probed on start + reconnect. `MaxEngramContentBytes` feeds the mapper's sub-chunking decision (RFC §mapper). A failed `Hello` after retries ⇒ bridge reports degraded (FR-009); all core go-rag ops continue.

## BatchWrite

```
BatchWriteRequest { repeated WriteRequest requests(1) }
BatchWriteItemResult { index(1), id(2), error(3) }
```

Bridge batches `BridgeBatchSize` (≤50) chunks per call. Per-item errors are logged + surfaced via status; the circuit breaker trips on sustained failure.