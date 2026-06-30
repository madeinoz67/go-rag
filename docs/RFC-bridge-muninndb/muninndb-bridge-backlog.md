# MuninnDB — Bridge Integration Backlog

> **Author**: Stephen Eaton  
> **Date**: 2026-06-25  
> **Repos**: `github.com/scrypster/muninndb` (MB- items) · `github.com/madeinoz67/go-rag-muninn-bridge` (BR- items)  
> **Source**: Derived from bridge PRD, feature brief, and integration pattern analysis  
> **Labels**: `api` `grpc` `bridge` `idempotency` `streaming` `feat` `enhancement`

> **POST-REVIEW UPDATE (2026-06-30):** The MuninnDB maintainer reviewed the bridge and confirmed **MB-001 / MB-006 (EnsureVault) / MB-008 / MB-009 / MB-011 / MB-012 are already shipped** (as `Read`, auto-create-on-write, `Forget`, `Link`, `Hello`, `Subscribe` respectively) — treat them as resolved, do not rebuild. The real remaining upstream gaps are filed as GitHub **#556** (UPSERT, = MB-003, our #1 blocker), **#557** (BatchForget), **#558** (ListVaults), **#559** (AdjustConfidence+contradiction); **#560** (the `idempotent_id` wiring bug) must land first and #556 depends on it. Per-item status + issue links are now in the **Summary** tables below. **#556:** the UPSERT proto comment was [posted](https://github.com/scrypster/muninndb/issues/556) on 2026-06-30 — draft v2 at [`draft-issue-556-upsert-v2.md`](./draft-issue-556-upsert-v2.md), v1 at [`draft-issue-556-upsert.md`](./draft-issue-556-upsert.md). Full mapping + write invariants: [`bridge-map-post-review.md`](./bridge-map-post-review.md).

Two backlogs in one document because MuninnDB changes and bridge changes are tightly coupled — each MB- item has a corresponding BR- consumer. Both must land together for a pattern to work.

---

## Context: what the bridge currently assumes

The bridge proto stub (`proto/muninn/muninn.proto`) defines four RPCs against MuninnDB:

| RPC | Used for |
|---|---|
| `Remember` | Single engram write |
| `BatchRemember` | Batch sync (up to 50) |
| `Recall` | Idempotency check (text search workaround) |
| `Activate` | `ActivateWithRAG` context expansion |

Every integration pattern beyond basic sync requires MuninnDB API surface that doesn't exist yet. The bridge also has no structured way to initialise, authenticate, negotiate capabilities, or recover from a MuninnDB outage — all of that is currently manual config and hope.

---

## Summary

### MuninnDB API items (go in the `muninndb` repo)

| ID | Title | Priority | Size | Phase | Post-review status (2026-06-30) |
|---|---|---|---|---|---|
| [MB-001](#mb-001) | `GetEngram` by ID | P1 | S | 1 | ✅ Shipped — `Read` (gRPC) |
| [MB-002](#mb-002) | `FindByMetadata` — look up engrams by arbitrary metadata KV | P1 | M | 1 | ❓ Open — not confirmed in review (bridge idempotency fallback depends on this; verify availability) |
| [MB-003](#mb-003) | `BatchRemember` upsert mode with idempotency key | P1 | M | 1 | 🔧 [#556](https://github.com/scrypster/muninndb/issues/556) — proto comment **posted 2026-06-30**; blocked behind [#560](https://github.com/scrypster/muninndb/issues/560) |
| [MB-004](#mb-004) | `PatchEngram` — update tags, metadata, confidence on existing engram | P1 | S | 1 | ❓ Open — not confirmed in review |
| [MB-005](#mb-005) | gRPC `Health` / `Ping` RPC | P1 | S | 1 | ❓ Open — not confirmed in review (may overlap gRPC standard health; verify) |
| [MB-006](#mb-006) | `ListVaults` + `EnsureVault` — vault management over gRPC | P2 | S | 2 | 🔸 Split — `EnsureVault` shipped (auto-create on write, idempotent); `ListVaults` → [#558](https://github.com/scrypster/muninndb/issues/558) |
| [MB-007](#mb-007) | `ListEngrams` — paginated listing with tag and metadata filters | P2 | M | 2 | ❓ Open — not confirmed in review |
| [MB-008](#mb-008) | `DeleteEngram` + `BatchDeleteEngrams` | P2 | S | 2 | 🔸 Split — `DeleteEngram` shipped (`Forget`, `hard:false` soft delete); `BatchDeleteEngrams` → [#557](https://github.com/scrypster/muninndb/issues/557) (BatchForget) |
| [MB-009](#mb-009) | `StrengthenEdge` — write explicit Hebbian association | P2 | S | 2 | ✅ Shipped — `Link` (signature: source, target, rel_type, weight, vault) |
| [MB-010](#mb-010) | `AdjustConfidence` — targeted Bayesian confidence patch | P2 | S | 2 | 🔧 [#559](https://github.com/scrypster/muninndb/issues/559) — with contradiction signaling |
| [MB-011](#mb-011) | `GetServerCapabilities` — version and feature flag negotiation | P2 | S | 2 | ✅ Shipped — `Hello` response (capabilities, server version, limits) |
| [MB-012](#mb-012) | `WatchTriggers` — gRPC stream of semantic trigger fire events | P3 | M | 3 | ✅ Shipped — `Subscribe` (streams `ActivationPush` events) |
| [MB-013](#mb-013) | `GetPredictedNext` — expose predictive activation output | P3 | S | 3 | ❓ Open — not confirmed in review |
| [MB-014](#mb-014) | `BatchPatch` — bulk tag/confidence update across many engrams | P3 | S | 3 | ❓ Open — not confirmed in review |

**Post-review legend:** ✅ = already shipped (do not rebuild). 🔧 = GitHub issue filed, open upstream. 🔸 = partially shipped (item splits across shipped + open). ❓ = not addressed in the 2026-06-30 review — availability unconfirmed, treat as open until verified. Cross-cutting prerequisite: [#560](https://github.com/scrypster/muninndb/issues/560) (`idempotent_id` exists in MBP types but not wired through gRPC/REST engine — only MCP) must land before #556. Full mapping + write invariants + revised pattern enablement: [`bridge-map-post-review.md`](./bridge-map-post-review.md).

### Bridge onboarding items (go in the `go-rag-muninn-bridge` repo)

| ID | Title | Priority | Size | Phase | Post-review status (2026-06-30) |
|---|---|---|---|---|---|
| [BR-001](#br-001) | `bridge muninn init` — guided MuninnDB onboarding wizard | P1 | M | 1 | Open (bridge repo `go-rag-muninn-bridge`) |
| [BR-002](#br-002) | MuninnDB connection lifecycle manager (dial, health, reconnect) | P1 | M | 1 | Open |
| [BR-003](#br-003) | MuninnDB capability negotiation on startup | P2 | S | 2 | Open — capability surface now ships via `Hello` (MB-011) |
| [BR-004](#br-004) | Max engram content size discovery and chunk splitting | P2 | S | 2 | Open |
| [BR-005](#br-005) | Rate-limit-aware `BatchRemember` with adaptive backoff | P2 | S | 2 | Open |
| [BR-006](#br-006) | Bridge state store → MuninnDB idempotency migration path | P3 | M | 3 | ⏸ Blocked behind [#556](https://github.com/scrypster/muninndb/issues/556) / [#560](https://github.com/scrypster/muninndb/issues/560) — idempotency migration needs UPSERT |
| [BR-007](#br-007) | MuninnDB vault pre-flight checks and auto-create | P3 | S | 3 | 🔸 Simplified — `EnsureVault` ships (auto-create on write); pre-flight checks still useful |

**Size key**: S = hours–1 day · M = 2–5 days · L = 1–2 weeks

---

## Phase 1 — Core API primitives

The four RPCs the bridge currently uses assume MuninnDB behaviours that don't exist: idempotent writes, engram reads by ID, tag updates. These items are the minimum surface for the bridge to be production-safe.

---

### MB-001

**`GetEngram` by ID**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge calls `BatchRemember` and receives engram IDs in the response. It has no way to read back what it created — to verify content, check current tags, or read current confidence. `GetEngram` is the missing read primitive. It is also the basis for `ActivateWithRAG`: when MuninnDB returns an engram ID in an `Activate` response, the bridge needs to read the full engram (including `metadata["chunk_id"]`) to know whether to call go-rag for context expansion.

#### Proto

```protobuf
rpc GetEngram(GetEngramRequest) returns (GetEngramResponse);

message GetEngramRequest {
  string vault     = 1;
  string engram_id = 2;
}

message GetEngramResponse {
  Engram engram = 1;
}

message Engram {
  string              id           = 1;
  string              vault        = 2;
  string              concept      = 3;
  string              content      = 4;
  repeated string     tags         = 5;
  map<string, string> metadata     = 6;
  float               confidence   = 7;
  int32               access_count = 8;
  float               decay_factor = 9;  // Current Ebbinghaus decay (0.0–1.0)
  string              created_at   = 10; // RFC3339
  string              updated_at   = 11;
  string              last_accessed_at = 12;
}
```

#### Acceptance criteria

- [ ] Returns full `Engram` struct including metadata map, tags, confidence, and temporal fields
- [ ] Returns `NOT_FOUND` gRPC status for unknown `engram_id`
- [ ] Returns `PERMISSION_DENIED` if the token doesn't have access to the vault
- [ ] `access_count` increments on each `GetEngram` call (counts as an engram access for Hebbian learning)
  - If read-without-activation semantics are preferred, add `bool no_access_record = 3` flag to suppress this
- [ ] REST equivalent: `GET /api/vaults/{vault}/engrams/{id}`
- [ ] Response time < 10ms for a locally running MuninnDB instance

#### Dependencies

None.

#### Bridge integration value

Enables `ActivateWithRAG` (D1). Enables bridge post-promotion verification. Required for BR-006 (state store migration).

---

### MB-002

**`FindByMetadata` — look up engrams by metadata key-value pair**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: M · **Phase**: 1

#### Description

The bridge's idempotency mechanism depends on knowing whether an engram with `metadata["chunk_id"] = <id>` already exists before calling `BatchRemember`. Currently the bridge maintains a local Pebble state store for this mapping. `FindByMetadata` replaces that state store: the bridge queries MuninnDB directly and the need for a local cache disappears. This is the single most important API addition for bridge reliability — if the bridge state store is lost (crash, wipe), `FindByMetadata` is the recovery mechanism.

It is also the foundation for gap detection (E1): the bridge asks "which MuninnDB engrams have `source:go-rag` tag but no valid corresponding `chunk_id` in the go-rag vault?"

#### Proto

```protobuf
rpc FindByMetadata(FindByMetadataRequest) returns (FindByMetadataResponse);

message FindByMetadataRequest {
  string vault        = 1;
  string metadata_key = 2; // e.g. "chunk_id"
  string metadata_val = 3; // e.g. "a1b2c3d4..."
  int32  max_results  = 4; // Default 10; the key should ideally be unique
}

message FindByMetadataResponse {
  repeated Engram engrams = 1;
}
```

#### Acceptance criteria

- [ ] Returns all engrams in the vault where `metadata[metadata_key] == metadata_val`
- [ ] Returns empty list (not error) if no engrams match
- [ ] Response time is acceptable for a metadata lookup — MuninnDB should maintain a secondary index on metadata keys commonly used by the bridge (`chunk_id`, `document_id`, `source`)
- [ ] `metadata_key` is case-sensitive
- [ ] `metadata_val` is exact-match (no partial/prefix matching in v1)
- [ ] Works across all engrams in the vault regardless of when they were created
- [ ] REST equivalent: `GET /api/vaults/{vault}/engrams?metadata_key=chunk_id&metadata_val=abc123`
- [ ] Test: promote 100 engrams with distinct `chunk_id` values; `FindByMetadata` returns the correct one for each

#### Implementation note

A secondary index on `metadata["chunk_id"]` in Pebble (key prefix `0x20 | vault | chunk_id → engram_id`) would make this O(1). Without an index, it degrades to a full vault scan. The bridge will call this once per chunk during idempotency checks — a full scan at scale is not acceptable. Index creation on the `chunk_id` key is strongly recommended.

#### Dependencies

MB-001 (`Engram` message type shared in response)

#### Bridge integration value

Replaces bridge state store for idempotency (BR-006). Enables gap detection (E1). Enables crash recovery without full re-sync.

---

### MB-003

**`BatchRemember` upsert mode with idempotency key**

**Type**: `enhancement` `api` `grpc`  
**Priority**: P1 · **Size**: M · **Phase**: 1

#### Description

`BatchRemember` currently always creates new engrams. If the bridge calls it twice with the same chunk content (e.g., after a crash and restart), MuninnDB creates duplicates. The bridge's local state store guards against this, but that guard disappears on wipe. Adding an `idempotency_key` field to `RememberItem` allows MuninnDB to enforce "one engram per key per vault" semantics server-side, making the bridge state store a performance optimisation rather than a correctness requirement.

Two write modes are needed:
- `CREATE_ONLY` — current behaviour; fails if idempotency key already exists
- `UPSERT` — create if new, update concept/content/tags/metadata if exists; never creates a duplicate

#### Proto change

```protobuf
message RememberItem {
  string              concept          = 1;
  string              content          = 2;
  repeated string     tags             = 3;
  map<string, string> metadata         = 4;
  string              idempotency_key  = 5; // Optional; e.g. "chunk:a1b2c3..."
  WriteMode           write_mode       = 6; // Default: CREATE_ONLY
}

enum WriteMode {
  CREATE_ONLY    = 0; // Error if idempotency_key already exists (current behaviour)
  UPSERT         = 1; // Create if new; update concept/content/tags/metadata if exists
  UPDATE_ONLY    = 2; // Error if idempotency_key does not exist
}

message BatchRememberResult {
  string id      = 1;
  string concept = 2;
  bool   created = 3; // true = new engram; false = existing engram updated
  bool   skipped = 4; // true = CREATE_ONLY and key already existed (not an error)
  string error   = 5;
}
```

The `idempotency_key` is scoped to the vault — the same key in different vaults creates two distinct engrams.

#### Acceptance criteria

- [ ] `UPSERT` with an existing `idempotency_key` updates concept, content, tags, metadata — does NOT create a duplicate
- [ ] `UPSERT` with a new `idempotency_key` creates a new engram, `created=true` in response
- [ ] `CREATE_ONLY` with an existing `idempotency_key` sets `skipped=true`, returns existing `id`, no error
- [ ] `UPDATE_ONLY` with a missing `idempotency_key` sets `error="not found"` for that item
- [ ] Omitting `idempotency_key` retains current behaviour (always creates, no dedup)
- [ ] `UPSERT` updates do not reset Ebbinghaus decay or Hebbian edges — only concept/content/tags/metadata change
- [ ] Test: call `BatchRemember(UPSERT)` twice with the same `idempotency_key`; assert exactly one engram exists in the vault
- [ ] Benchmark: upsert batch of 50 completes in < 50ms

#### Dependencies

None — self-contained change to `BatchRemember` semantics.

#### Bridge integration value

Makes the bridge state store optional for correctness (it can remain as a cache). Eliminates duplicate engrams on crash/restart. Required for BR-006.

---

### MB-004

**`PatchEngram` — update tags, metadata, and confidence on an existing engram**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge needs to modify existing engrams without full rewrite:
- Add `"orphaned"` tag when a go-rag document is deleted
- Add `"low-quality"` tag based on extraction quality score
- Add `"contradicted"` tag when two chunks conflict (C1)
- Adjust Bayesian confidence delta when contradictions are detected (C1)
- Update `metadata["promoted_at"]` on re-promotion

A full `Remember` rewrite would reset the engram's Ebbinghaus decay and Hebbian edges — catastrophic for an engram that has built up cognitive weight. `PatchEngram` applies surgical changes without touching cognitive state.

#### Proto

```protobuf
rpc PatchEngram(PatchEngramRequest) returns (PatchEngramResponse);

message PatchEngramRequest {
  string              vault            = 1;
  string              engram_id        = 2;
  repeated string     add_tags         = 3; // Tags to add (deduped)
  repeated string     remove_tags      = 4; // Tags to remove (no-op if absent)
  map<string, string> set_metadata     = 5; // Keys to set/overwrite
  repeated string     delete_metadata  = 6; // Metadata keys to remove
  float               confidence_delta = 7; // Add to current confidence; clamped 0.0–1.0
                                            // Positive = reinforce, negative = contradict
  string              new_concept      = 8; // Optional; empty = no change
}

message PatchEngramResponse {
  Engram updated_engram = 1; // Full engram state after patch
}
```

#### Acceptance criteria

- [ ] Tags in `add_tags` are added; duplicate tags are deduped silently
- [ ] Tags in `remove_tags` that don't exist on the engram are silently ignored (no error)
- [ ] `set_metadata` overwrites existing keys; does not clear keys not mentioned
- [ ] `delete_metadata` removes specified keys; silently ignores missing keys
- [ ] `confidence_delta` is added to current confidence and clamped to `[0.0, 1.0]`
- [ ] `confidence_delta = 0.0` is a no-op on confidence (other fields may still change)
- [ ] Ebbinghaus decay, Hebbian edges, and `access_count` are NOT modified by `PatchEngram`
- [ ] Returns `NOT_FOUND` if `engram_id` does not exist in the vault
- [ ] Returns the full updated `Engram` struct after applying patches
- [ ] REST equivalent: `PATCH /api/vaults/{vault}/engrams/{id}`

#### Dependencies

MB-001 (`Engram` message type in response)

#### Bridge integration value

Enables C1 (contradiction tagging + confidence adjustment). Enables orphan tagging (E3). Enables quality gating without full re-promotion.

---

### MB-005

**gRPC `Health` / `Ping` RPC**

**Type**: `feat` `api` `grpc`  
**Priority**: P1 · **Size**: S · **Phase**: 1

#### Description

The bridge's `BridgeStatus` command and its internal connection health monitor need to check MuninnDB health over gRPC. MuninnDB exposes a REST health endpoint but the bridge communicates primarily over gRPC, and a gRPC health check is faster, more reliable for connection probing, and uses the same channel as the operational RPCs. The bridge also needs to distinguish between "MuninnDB process is alive" and "MuninnDB's cognitive engine is healthy" — these can diverge during index rebuild or compaction.

#### Proto

```protobuf
service MuninnService {
  // Standard gRPC health check — responds immediately if the server is alive.
  rpc Ping(PingRequest) returns (PingResponse);

  // Deeper health check — reports cognitive engine state.
  rpc Health(HealthRequest) returns (HealthResponse);
}

message PingRequest {}
message PingResponse {
  string version = 1; // MuninnDB semver e.g. "0.6.1"
}

message HealthRequest {
  string vault = 1; // Optional; empty = server-level health only
}

message HealthResponse {
  bool   ok             = 1; // False if any critical subsystem is unhealthy
  bool   engine_ready   = 2; // False during index rebuild / compaction
  bool   vault_ready    = 3; // False if the requested vault is unavailable
  string status_message = 4; // Human-readable explanation if !ok
  int64  uptime_seconds = 5;
  map<string, string> subsystem_status = 6; // e.g. "vector_index": "rebuilding"
}
```

#### Acceptance criteria

- [ ] `Ping` responds in < 5ms under normal load — suitable for keepalive probing
- [ ] `Ping.version` returns the running MuninnDB binary version
- [ ] `Health` reflects accurate engine state: `engine_ready=false` during index rebuild
- [ ] `Health` with a non-existent `vault` sets `vault_ready=false` without error (NOT_FOUND)
- [ ] `Health` with an empty `vault` returns server-level health only
- [ ] MuninnDB also implements the standard `grpc.health.v1.Health` service for compatibility with gRPC ecosystem tooling (grpc-health-probe, Kubernetes liveness probes)
- [ ] The bridge uses `Ping` for keepalive (every 30s) and `Health` for `bridge sync status` display

#### Dependencies

None.

#### Bridge integration value

Powers `bridge sync status` MuninnDB health column. Enables the connection manager (BR-002) to distinguish transient from persistent failures.

---

## Phase 2 — Vault management, listing, and cognitive control

Items that give the bridge full lifecycle control over MuninnDB engrams and the association graph.

---

### MB-006

**`ListVaults` + `EnsureVault` — vault management over gRPC**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

At startup the bridge validates that the configured target vault exists in MuninnDB. If it doesn't, it either errors or auto-creates it (depending on `auto_create_vault` config). Neither action is currently possible over gRPC — MuninnDB vault management is CLI-only. `EnsureVault` is an idempotent create-if-not-exists operation, safe to call on every bridge startup.

#### Proto

```protobuf
rpc ListVaults(ListVaultsRequest) returns (ListVaultsResponse);
rpc EnsureVault(EnsureVaultRequest) returns (EnsureVaultResponse);

message ListVaultsRequest {}

message VaultInfo {
  string vault_name    = 1;
  int64  engram_count  = 2;
  string created_at    = 3;
  string last_accessed = 4;
}

message ListVaultsResponse {
  repeated VaultInfo vaults = 1;
}

message EnsureVaultRequest {
  string vault_name = 1;
  string description = 2; // Optional; set on create, ignored if vault already exists
}

message EnsureVaultResponse {
  VaultInfo vault   = 1;
  bool      created = 2; // true = vault was just created; false = already existed
}
```

#### Acceptance criteria

- [ ] `ListVaults` returns all vaults the authenticated token can access
- [ ] `EnsureVault` creates a new vault if `vault_name` does not exist; returns existing vault if it does
- [ ] `EnsureVault` is idempotent — calling it N times with the same name produces one vault
- [ ] `EnsureVault` with an invalid vault name (wrong chars, too long) returns `INVALID_ARGUMENT`
- [ ] REST equivalents: `GET /api/vaults` · `POST /api/vaults` (with idempotency)
- [ ] Bridge calls `EnsureVault` on startup for each configured `target_vault` before beginning sync

#### Dependencies

None.

#### Bridge integration value

Enables BR-007 (vault pre-flight checks). Removes manual vault creation from bridge deployment process.

---

### MB-007

**`ListEngrams` — paginated listing with tag and metadata filters**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: M · **Phase**: 2

#### Description

The bridge needs to enumerate MuninnDB engrams for three operations:
- **Gap detection (E1)**: list all engrams tagged `"go-rag"` and cross-reference against go-rag vault contents
- **Orphan scan**: list engrams tagged `"go-rag"` with a `source_path` that no longer exists in go-rag
- **Reconciliation after crash**: compare MuninnDB engram set against bridge state store to identify what was or wasn't promoted

`Recall` is unsuitable — it's semantic search, not enumeration. A paginated listing endpoint with filter support is needed.

#### Proto

```protobuf
rpc ListEngrams(ListEngramsRequest) returns (ListEngramsResponse);

message ListEngramsRequest {
  string          vault        = 1;
  repeated string require_tags = 2; // AND — engram must have all of these
  repeated string exclude_tags = 3; // NOT — engram must have none of these
  string          metadata_key = 4; // Filter by metadata key presence/value
  string          metadata_val = 5; // Exact match; empty = key presence only
  int32           page_size    = 6; // Default 100, max 1000
  string          page_token   = 7;
  string          sort_by      = 8; // "created_at" | "updated_at" | "access_count" | "confidence"
  bool            sort_desc    = 9; // Default true
}

message ListEngramsResponse {
  repeated Engram engrams         = 1;
  string          next_page_token = 2; // Empty if last page
  int32           total_count     = 3; // Total matching (not just this page)
}
```

#### Acceptance criteria

- [ ] `require_tags = ["go-rag"]` returns only engrams that have that tag
- [ ] `require_tags` with multiple values applies AND semantics (all must be present)
- [ ] `exclude_tags` filters out engrams that have any of those tags
- [ ] `metadata_key` without `metadata_val` returns engrams that have that key (regardless of value)
- [ ] `metadata_key` + `metadata_val` filters to exact match
- [ ] `sort_by = "created_at"` returns engrams in creation order
- [ ] Pagination is stable — adding engrams during a paginated scan does not cause duplicates or skips
- [ ] `total_count` is accurate (not estimated)
- [ ] REST equivalent: `GET /api/vaults/{vault}/engrams?tag=go-rag&sort=created_at`

#### Dependencies

MB-001 (`Engram` message type in response)

#### Bridge integration value

Enables gap detection (E1). Enables orphan scan. Enables full reconciliation for BR-006.

---

### MB-008

**`DeleteEngram` + `BatchDeleteEngrams`**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

When `orphan_policy: "delete"` is configured, the bridge must remove engrams whose source go-rag documents have been deleted. There is currently no delete API. This also handles cleanup when a go-rag vault is dropped entirely — the bridge can bulk-delete all engrams tagged with that vault name.

Deletion must be soft-delete: Ebbinghaus decay handles engram fade naturally, but an explicit delete by the bridge must be definitive. However, MuninnDB should retain the engram's entity graph edges for a configurable grace period (default 7 days) to avoid stranding associated engrams.

#### Proto

```protobuf
rpc DeleteEngram(DeleteEngramRequest) returns (DeleteEngramResponse);
rpc BatchDeleteEngrams(BatchDeleteEngramsRequest) returns (BatchDeleteEngramsResponse);

message DeleteEngramRequest {
  string vault     = 1;
  string engram_id = 2;
}

message DeleteEngramResponse {
  bool deleted = 1; // false if engram was already gone (idempotent)
}

message BatchDeleteEngramsRequest {
  string          vault      = 1;
  repeated string engram_ids = 2; // Max 200
}

message BatchDeleteEngramResult {
  string engram_id = 1;
  bool   deleted   = 2;
  string error     = 3;
}

message BatchDeleteEngramsResponse {
  repeated BatchDeleteEngramResult results = 1;
  int32 deleted_count                      = 2;
}
```

#### Acceptance criteria

- [ ] `DeleteEngram` is idempotent — deleting an already-deleted engram returns `deleted=false`, not an error
- [ ] Deleted engrams do not appear in `Recall`, `Activate`, or `ListEngrams` results immediately
- [ ] Deleting an engram removes it from all FTS and vector indexes
- [ ] Deleted engram's Hebbian edges are retained for `edge_grace_days` (default 7) to allow associated engrams to stabilise; configurable in MuninnDB config
- [ ] `BatchDeleteEngrams` processes all items; per-item errors don't abort the batch
- [ ] REST: `DELETE /api/vaults/{vault}/engrams/{id}` · `POST /api/vaults/{vault}/engrams/batch-delete`
- [ ] Test: delete 50 engrams; verify none appear in subsequent `ListEngrams` or `Recall` calls

#### Dependencies

None.

#### Bridge integration value

Enables `orphan_policy: "delete"`. Enables vault cleanup when go-rag vaults are dropped.

---

### MB-009

**`StrengthenEdge` — write an explicit Hebbian association between two engrams**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

MuninnDB builds Hebbian edges implicitly through co-activation — engrams activated together repeatedly form associations automatically. The bridge's E2 pattern (Obsidian backlinks → Hebbian edges) needs to write edges explicitly: when an Obsidian note links `[[authentication]]` to `[[JWT]]`, that relationship is known immediately and should not require dozens of co-activations to emerge. `StrengthenEdge` seeds explicit associations that are then refined by Hebbian learning over time.

The bridge workaround — firing two rapid sequential `Activate` calls with both concepts — is fragile: it depends on MuninnDB's co-activation detection window and adds latency proportional to the number of wikilinks per document.

#### Proto

```protobuf
rpc StrengthenEdge(StrengthenEdgeRequest) returns (StrengthenEdgeResponse);

message StrengthenEdgeRequest {
  string vault       = 1;
  string engram_id_a = 2;
  string engram_id_b = 3;
  float  weight      = 4; // Strength increment (0.0–1.0); added to existing edge weight
  string reason      = 5; // "wikilink" | "co-citation" | "explicit" — audit trail
}

message StrengthenEdgeResponse {
  float resulting_weight = 1; // Combined edge weight after increment
  bool  edge_created     = 2; // true = new edge; false = existing edge strengthened
}
```

The edge is bidirectional (undirected). The `reason` field is stored as edge metadata for future analytics.

#### Acceptance criteria

- [ ] Creates a new Hebbian edge if none exists between the two engrams; `edge_created=true`
- [ ] Strengthens existing edge if one already exists; `edge_created=false`
- [ ] `resulting_weight` reflects the post-operation edge weight, clamped to `[0.0, 1.0]`
- [ ] Edge appears in MuninnDB's BFS association traversal phase of `Activate`
- [ ] Edge is subject to Ebbinghaus decay if not re-activated — consistent with naturally-formed edges
- [ ] Returns `NOT_FOUND` if either `engram_id` does not exist in the vault
- [ ] REST: `POST /api/vaults/{vault}/edges`

#### Dependencies

None — self-contained graph operation.

#### Bridge integration value

Directly implements E2 (Obsidian backlinks → Hebbian edges). Removes the fragile co-activation workaround. Seeds the association graph with structured human knowledge before Hebbian learning refines it.

---

### MB-010

**`AdjustConfidence` — targeted Bayesian confidence patch**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

`PatchEngram` (MB-004) includes `confidence_delta` as one field in a general patch operation. `AdjustConfidence` is a dedicated RPC for confidence-only adjustments, intended for high-frequency bridge operations like contradiction detection (C1) where only the confidence needs to change and the overhead of a general patch is unnecessary.

More importantly, MuninnDB's contradiction detection (C1) needs the ability to signal that two specific engrams are in tension with each other — not just adjust one engram's confidence, but record the contradiction as a semantic relationship between the pair.

#### Proto

```protobuf
rpc AdjustConfidence(AdjustConfidenceRequest) returns (AdjustConfidenceResponse);

message AdjustConfidenceRequest {
  string vault               = 1;
  string engram_id           = 2;
  float  delta               = 3; // Positive = reinforce; negative = contradict; clamped 0.0–1.0
  string reason              = 4; // "contradiction" | "corroboration" | "bridge-explicit"
  string contradicted_by_id  = 5; // Optional: the other engram in a contradiction pair
                                   // MuninnDB records a "contradicts" edge between them
}

message AdjustConfidenceResponse {
  float new_confidence = 1;
  bool  contradiction_edge_created = 2; // If contradicted_by_id was set and edge is new
}
```

#### Acceptance criteria

- [ ] `delta > 0` raises confidence; `delta < 0` lowers confidence; result clamped to `[0.0, 1.0]`
- [ ] `contradicted_by_id` creates a typed `"contradicts"` edge between the two engrams (distinct from Hebbian `"associated"` edges)
- [ ] The `"contradicts"` edge is visible in MuninnDB's entity/association graph
- [ ] `Activate` and `Recall` surface low-confidence engrams with a confidence warning in the response
- [ ] Returns `NOT_FOUND` if `engram_id` or `contradicted_by_id` does not exist

#### Dependencies

MB-009 (`StrengthenEdge` edge infrastructure — contradiction edges share the same storage layer with a different type label)

#### Bridge integration value

Directly implements C1 (contradiction detection with Bayesian confidence adjustment). Records contradiction relationships for MuninnDB's BFS traversal to surface.

---

### MB-011

**`GetServerCapabilities` — version and feature flag negotiation**

**Type**: `feat` `api` `grpc`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

MuninnDB is actively developed. The bridge needs to know at runtime which RPCs are available so it can degrade gracefully rather than fail hard when it connects to an older MuninnDB version. This is especially important in homelab setups where MuninnDB and the bridge may be updated independently.

#### Proto

```protobuf
rpc GetServerCapabilities(GetServerCapabilitiesRequest) returns (GetServerCapabilitiesResponse);

message GetServerCapabilitiesRequest {}

message GetServerCapabilitiesResponse {
  string version             = 1; // Semver: "0.6.1"
  repeated string rpcs       = 2; // List of available RPC names: ["Remember","BatchRemember",...]
  int32  max_batch_size      = 3; // Current BatchRemember limit (default 50)
  int32  max_engram_content  = 4; // Max content bytes per engram (bridge uses this for splitting)
  int32  max_tags_per_engram = 5;
  bool   entity_graph_enabled = 6; // StrengthenEdge / contradiction edges available
  bool   triggers_enabled    = 7;  // WatchTriggers available
  map<string, string> feature_flags = 8; // Freeform capability flags
}
```

#### Acceptance criteria

- [ ] `rpcs` lists every currently available RPC name as a string
- [ ] `max_engram_content` reflects the actual enforced content byte limit
- [ ] `max_batch_size` reflects the actual BatchRemember item limit
- [ ] Response is stable and fast (< 5ms) — the bridge calls this on startup and caches it for the session
- [ ] New capabilities added in future MuninnDB versions appear in `feature_flags` before getting promoted to first-class fields

#### Dependencies

None.

#### Bridge integration value

Powers BR-003 (capability negotiation). Allows bridge to adapt to different MuninnDB versions. Provides `max_engram_content` value for BR-004 (chunk splitting).

---

## Phase 3 — Event subscription and advanced operations

Items that unlock the proactive/reverse-flow patterns and bulk operations.

---

### MB-012

**`WatchTriggers` — gRPC stream of semantic trigger fire events**

**Type**: `feat` `api` `grpc` `streaming`  
**Priority**: P3 · **Size**: M · **Phase**: 3

#### Description

MuninnDB's semantic trigger system fires when a subscribed context becomes relevant. The bridge's A1 pattern (reverse activation) depends on receiving these trigger events: when a trigger fires for an engram tagged `source:go-rag`, the bridge issues a go-rag query for fresh document context and injects it into the active agent's context window.

Currently there is no way to subscribe to trigger events over gRPC — they are delivered via the MCP push mechanism only, which is not accessible to the bridge.

#### Proto

```protobuf
rpc WatchTriggers(WatchTriggersRequest) returns (stream TriggerEvent);

message WatchTriggersRequest {
  string          vault          = 1;
  repeated string filter_tags    = 2; // Only receive triggers for engrams with these tags
                                       // Empty = all triggers
}

message TriggerEvent {
  string          engram_id       = 1;
  string          concept         = 2;
  float           activation_score = 3; // How strongly the trigger fired (0.0–1.0)
  repeated string context_matched = 4; // Context strings that triggered it
  map<string,string> metadata     = 5; // Full engram metadata (incl. chunk_id, source_path)
  int64           timestamp_ms    = 6;
}
```

#### Acceptance criteria

- [ ] Bridge receives `TriggerEvent` within 500ms of MuninnDB determining a trigger has fired
- [ ] `filter_tags = ["go-rag"]` limits events to engrams sourced from go-rag
- [ ] Stream survives MuninnDB internal state changes (index rebuilds, compaction) without disconnecting
- [ ] Multiple clients can hold concurrent `WatchTriggers` streams without interference
- [ ] Trigger events are delivered at-least-once; the bridge should handle occasional duplicates idempotently
- [ ] Empty `filter_tags` receives all trigger events for the vault (use carefully — can be high volume)
- [ ] Bridge uses this to fire `go-rag.Query(concept)` and inject results into agent context

#### Dependencies

None — this is a new delivery mechanism for MuninnDB's existing trigger engine.

#### Bridge integration value

Directly implements A1 (semantic trigger → go-rag query). Transforms the bridge from a push-only promoter into a bidirectional bridge where MuninnDB can drive go-rag queries.

---

### MB-013

**`GetPredictedNext` — expose predictive activation output**

**Type**: `feat` `api` `grpc`  
**Priority**: P3 · **Size**: S · **Phase**: 3

#### Description

MuninnDB's predictive activation phase learns sequential patterns across `Activate` calls and predicts which engrams will be needed next. This prediction currently influences the internal ACTIVATE pipeline but is not exposed externally. The bridge's A2 pattern (predictive pre-fetch) needs this output to know which go-rag chunks to pre-warm in go-rag's query cache.

#### Proto

```protobuf
rpc GetPredictedNext(GetPredictedNextRequest) returns (GetPredictedNextResponse);

message GetPredictedNextRequest {
  string vault       = 1;
  int32  max_results = 2; // Default 5
}

message PredictedEngram {
  string  engram_id        = 1;
  string  concept          = 2;
  float   prediction_score = 3; // Confidence of the prediction (0.0–1.0)
  map<string,string> metadata = 4; // Includes chunk_id and source_path if go-rag sourced
}

message GetPredictedNextResponse {
  repeated PredictedEngram predictions = 1;
  string                   vault       = 2;
}
```

#### Acceptance criteria

- [ ] Returns the top N engrams predicted to be needed based on recent activation history
- [ ] `prediction_score` reflects the model's confidence — low scores should not trigger expensive pre-fetches
- [ ] Returns empty list (not error) if insufficient activation history to make predictions
- [ ] Bridge filters returned predictions to those with `metadata["source"] = "go-rag"` before issuing go-rag queries
- [ ] Bridge applies a `min_prediction_score` threshold (configurable, default `0.6`) before pre-fetching
- [ ] REST: `GET /api/vaults/{vault}/predictions`

#### Dependencies

None — this exposes an existing internal computation.

#### Bridge integration value

Directly implements A2 (predictive pre-fetch). Pre-warmed go-rag query cache means near-zero latency on predicted retrievals.

---

### MB-014

**`BatchPatch` — bulk tag and metadata update across many engrams**

**Type**: `feat` `api` `grpc`  
**Priority**: P3 · **Size**: S · **Phase**: 3

#### Description

The bridge's orphan scan (when a go-rag vault is dropped or a document deleted) needs to tag potentially hundreds of engrams as `"orphaned"` at once. Calling `PatchEngram` (MB-004) per engram is unacceptably slow for large vaults. `BatchPatch` applies the same patch operation to a list of engram IDs in one call.

#### Proto

```protobuf
rpc BatchPatch(BatchPatchRequest) returns (BatchPatchResponse);

message BatchPatchRequest {
  string vault             = 1;
  repeated string engram_ids = 2; // Max 500
  // The same patch applied to all engram_ids:
  repeated string add_tags         = 3;
  repeated string remove_tags      = 4;
  map<string, string> set_metadata = 5;
  repeated string delete_metadata  = 6;
  float confidence_delta           = 7;
}

message BatchPatchResult {
  string engram_id = 1;
  bool   patched   = 2;
  string error     = 3;
}

message BatchPatchResponse {
  repeated BatchPatchResult results = 1;
  int32 patched_count               = 2;
  int32 failed_count                = 3;
}
```

#### Acceptance criteria

- [ ] All engrams in `engram_ids` receive the same patch atomically where possible
- [ ] Per-item failures don't abort the batch — error is recorded per item
- [ ] Max 500 engrams per call; requests above this return `INVALID_ARGUMENT`
- [ ] `patched_count + failed_count == len(engram_ids)` always
- [ ] REST: `POST /api/vaults/{vault}/engrams/batch-patch`

#### Dependencies

MB-004 (`PatchEngram` — shares patch semantics)

#### Bridge integration value

Makes orphan tagging efficient at scale. Enables bulk quality tagging after vault re-sync.

---

## Bridge onboarding items

Changes to the bridge daemon itself that handle the MuninnDB connection lifecycle cleanly.

---

### BR-001

**`bridge muninn init` — guided MuninnDB onboarding wizard**

**Type**: `feat` `cli`  
**Priority**: P1 · **Size**: M · **Phase**: 1

#### Description

The bridge currently requires manual editing of `bridge.yaml` to configure MuninnDB. This is error-prone and gives no feedback until the daemon crashes at runtime. `bridge muninn init` is an interactive wizard that:

1. Prompts for MuninnDB gRPC address (default `127.0.0.1:8477`)
2. Dials the connection and calls `Ping` (MB-005) — fails fast with a clear error if unreachable
3. Prompts for API token (optional; skips if MuninnDB running without auth)
4. Calls `GetServerCapabilities` (MB-011) and displays which features are available
5. Lists existing vaults (`ListVaults`, MB-006); prompts user to select or create a target vault
6. Calls `EnsureVault` (MB-006) for the selected vault
7. Writes validated settings to `bridge.yaml` under the `muninn:` key
8. Confirms: "MuninnDB at 127.0.0.1:8477 (v0.6.1) — vault 'go-rag' ready"

```
$ bridge muninn init

Connecting to MuninnDB...
  Address [127.0.0.1:8477]: ↵
  ✓ Connected — MuninnDB v0.6.1

API token (press Enter to skip): ↵
  ✓ Authenticated (no-auth mode)

Server capabilities:
  ✓ BatchRemember (upsert mode)     ✓ PatchEngram
  ✓ FindByMetadata                  ✓ StrengthenEdge
  ✗ WatchTriggers                   (upgrade to v0.7+ for A1 pattern)

Existing vaults: [default]
Target vault name [go-rag]: ↵
  ✓ Vault 'go-rag' created

Configuration written to ~/.bridge/bridge.yaml

Run 'bridge sync start' to begin.
```

#### Acceptance criteria

- [ ] Wizard exits with a non-zero code and a clear error message if MuninnDB is unreachable
- [ ] Wizard warns (not errors) when a required capability is missing, and lists the bridge patterns that won't function
- [ ] `bridge muninn init --non-interactive` accepts all values as flags for scripted deployment
- [ ] Running `bridge muninn init` when a MuninnDB config already exists prompts to overwrite rather than silently replacing
- [ ] All config values written by the wizard are valid and usable without restart (daemon picks up on next `bridge sync start`)

#### Dependencies

MB-005 (Ping), MB-006 (ListVaults/EnsureVault), MB-011 (GetServerCapabilities)

---

### BR-002

**MuninnDB connection lifecycle manager**

**Type**: `feat` `internal`  
**Priority**: P1 · **Size**: M · **Phase**: 1

#### Description

The bridge daemon currently opens a single gRPC connection to MuninnDB at startup and panics if it drops. A proper connection lifecycle manager handles:
- Initial dial with configurable timeout and retry
- Keepalive pings every 30s using `Ping` (MB-005)
- Health checks using `Health` (MB-005) on a configurable interval
- Automatic reconnection with exponential backoff (1s → 2s → 4s → max 60s) on connection loss
- Backpressure: when MuninnDB is unhealthy, pause promotion queue consumption without dropping items
- Metrics: `bridge_muninn_connection_healthy` gauge and `bridge_muninn_reconnect_total` counter

```go
type MuninnConnection struct {
    addr        string
    token       string
    conn        *grpc.ClientConn
    client      muninnpb.MuninnServiceClient
    healthy     atomic.Bool
    backoff     BackoffPolicy
    capabilities *CapabilitiesCache
}

func (c *MuninnConnection) Watch(ctx context.Context) // background health monitor
func (c *MuninnConnection) WaitHealthy(ctx context.Context) error // blocks until healthy
func (c *MuninnConnection) Capabilities() *ServerCapabilities // cached from MB-011
```

#### Acceptance criteria

- [ ] Bridge does not crash when MuninnDB restarts — reconnects within `max_reconnect_wait_s` (default 60s)
- [ ] `bridge_muninn_connection_healthy = 0` within 5s of MuninnDB going down
- [ ] `bridge_muninn_connection_healthy = 1` within 5s of MuninnDB coming back up
- [ ] Promotion queue pauses during MuninnDB outage — no items are dropped
- [ ] Items queued during outage are promoted when connection recovers
- [ ] Reconnect uses exponential backoff: does not storm a recovering MuninnDB instance
- [ ] All MuninnDB gRPC calls go through the connection manager — no ad-hoc `grpc.Dial` calls in sync workers

#### Dependencies

MB-005 (Ping/Health RPCs used for keepalive and health monitoring)

---

### BR-003

**MuninnDB capability negotiation on startup**

**Type**: `feat` `internal`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

After BR-002 establishes the connection, the bridge calls `GetServerCapabilities` (MB-011) and stores the result. Every sync worker and pattern handler checks capabilities before calling an RPC, and degrades gracefully if a capability is absent.

```go
type CapabilitySet struct {
    HasFindByMetadata    bool
    HasPatchEngram       bool
    HasStrengthenEdge    bool
    HasWatchTriggers     bool
    HasBatchUpsert       bool
    MaxBatchSize         int
    MaxEngramContentBytes int
}

func (c *CapabilitySet) Warn(logger) // logs missing capabilities and which patterns they affect
func (c *CapabilitySet) Require(cap string) error // errors if cap is absent
```

Bridge sync start output when capabilities are missing:

```
WARN MuninnDB v0.5.3 — missing capabilities:
  FindByMetadata:  bridge state store required for idempotency (BR-006 migration skipped)
  StrengthenEdge:  E2 pattern (Obsidian backlinks) disabled
  WatchTriggers:   A1 pattern (semantic triggers) disabled
  Upgrade MuninnDB to v0.7+ to enable these patterns.
```

#### Acceptance criteria

- [ ] Capability check runs on every daemon startup, not just `bridge muninn init`
- [ ] Missing P1 capabilities (FindByMetadata, BatchUpsert) log `WARN` but do not prevent daemon start — bridge falls back to state-store-based idempotency
- [ ] Missing P3 capabilities (WatchTriggers) log `INFO` with the affected pattern name
- [ ] Capability cache is invalidated and refreshed on reconnect
- [ ] `bridge sync status` includes a "MuninnDB capabilities" section listing enabled/disabled patterns

#### Dependencies

MB-011 (GetServerCapabilities), BR-002 (connection lifecycle provides the check point)

---

### BR-004

**Max engram content size discovery and automatic chunk splitting**

**Type**: `feat` `internal`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

MuninnDB enforces a maximum content size per engram. The bridge PRD notes this as an open question (Q3). The sync worker must not attempt to promote a chunk that exceeds this limit — doing so wastes a BatchRemember call and produces an error for that item. The limit is discovered from `GetServerCapabilities` (MB-011) and applied in the sync worker before batch construction.

When a chunk exceeds the limit, the worker splits it at sentence boundaries into sub-chunks, each promoted as a separate engram sharing the parent concept but suffixed `[a]`, `[b]`, etc. Both sub-engram IDs are stored under the parent `chunk_id` key in the bridge state store.

```go
func splitForMuninn(chunk Chunk, maxBytes int) []RememberItem {
    if len(chunk.Content) <= maxBytes {
        return []RememberItem{mapChunk(chunk)}
    }
    // Split at sentence boundary near maxBytes
    parts := splitAtSentenceBoundary(chunk.Content, maxBytes)
    items := make([]RememberItem, len(parts))
    for i, part := range parts {
        items[i] = mapChunk(chunk)
        items[i].Concept = fmt.Sprintf("%s [%s]", chunk.Concept, string(rune('a'+i)))
        items[i].Content = part
        items[i].Metadata["sub_chunk_index"] = strconv.Itoa(i)
        items[i].Metadata["sub_chunk_total"] = strconv.Itoa(len(parts))
    }
    return items
}
```

#### Acceptance criteria

- [ ] Sync worker reads `MaxEngramContentBytes` from the capabilities cache on startup
- [ ] Any chunk with `len(content) > MaxEngramContentBytes` is split before `BatchRemember`
- [ ] Sub-chunks share the parent `chunk_id` in metadata and are stored together in the bridge state store
- [ ] Sub-chunk suffixes use `[a]`, `[b]`, `[c]` in the concept label
- [ ] Split occurs at sentence boundary (period + space or newline), not mid-word
- [ ] `bridge sync status` shows a `"chunks split"` counter per vault
- [ ] If `MaxEngramContentBytes` is not reported by MuninnDB (older version), bridge uses conservative default of 40KB

#### Dependencies

MB-011 (provides `MaxEngramContentBytes`), BR-003 (reads from capabilities cache)

---

### BR-005

**Rate-limit-aware `BatchRemember` with adaptive backoff**

**Type**: `feat` `internal`  
**Priority**: P2 · **Size**: S · **Phase**: 2

#### Description

MuninnDB may rate-limit `BatchRemember` calls (especially on the default vault shared with other MCP clients). The bridge must handle `RESOURCE_EXHAUSTED` gRPC status gracefully — not by crashing, not by silent drop, but by backing off and retrying with the full batch intact.

The bridge already has a retry policy in the PRD (§11.1). This item implements it specifically for rate limit responses, which have different semantics from network errors: the server is healthy and the request is valid, just throttled.

```go
func (w *SyncWorker) batchRememberWithBackoff(
    ctx context.Context,
    items []RememberItem,
    vault string,
) (*BatchRememberResponse, error) {
    backoff := newRateLimitBackoff()
    for {
        resp, err := w.muninn.BatchRemember(ctx, &BatchRememberRequest{
            Vault: vault, Items: items,
        })
        if status.Code(err) == codes.ResourceExhausted {
            sleep := backoff.Next()
            w.metrics.rateLimitHits.Inc()
            w.logger.Warn("MuninnDB rate limited", "retry_in", sleep)
            select {
            case <-time.After(sleep):
                continue
            case <-ctx.Done():
                return nil, ctx.Err()
            }
        }
        return resp, err
    }
}
```

Rate limit backoff: 1s, 2s, 4s, 8s, 16s, max 60s. Jitter ±20%.

#### Acceptance criteria

- [ ] `RESOURCE_EXHAUSTED` response causes backoff and retry — never a dropped item or error to the promotion queue
- [ ] `bridge_muninn_rate_limit_total` Prometheus counter increments on each rate limit hit
- [ ] After 6 retries (max ~90s total wait), the batch is marked failed and moves to dead-letter log
- [ ] Rate limit backoff does not block the sync worker goroutine — uses `select` with context cancellation
- [ ] Adaptive: if rate limits hit > 10x per minute, bridge reduces concurrent worker count by 1 (floor: 1)

#### Dependencies

BR-002 (connection lifecycle provides the gRPC client)

---

### BR-006

**Bridge state store → MuninnDB idempotency migration path**

**Type**: `feat` `internal`  
**Priority**: P3 · **Size**: M · **Phase**: 3

#### Description

The bridge currently maintains a local Pebble state store (`~/.bridge/state`) to track `chunk_id → engram_id` mappings for idempotency. Once MB-002 (`FindByMetadata`) and MB-003 (BatchRemember upsert) are available, the state store becomes an optional performance cache rather than a correctness requirement. This item adds a migration command that:

1. Reads all `chunk_id → engram_id` entries from the state store
2. Verifies each engram still exists in MuninnDB via `GetEngram` (MB-001)
3. Removes state store entries for engrams that no longer exist in MuninnDB
4. Optionally: fully vacates the state store and switches to MuninnDB-only idempotency

```
$ bridge state migrate --to-muninn

Reading state store: 14,823 chunk mappings
Verifying against MuninnDB...
  ✓ 14,791 engrams verified
  ✗ 32 engrams not found in MuninnDB (deleted or vault wiped)
  Removing 32 stale entries from state store

Migration complete. State store now used as performance cache only.
Run 'bridge state vacuum' to clear the state store and use MuninnDB as sole source of truth.
```

#### Acceptance criteria

- [ ] `bridge state migrate` runs non-destructively by default — produces a report without making changes
- [ ] `bridge state migrate --apply` makes the changes described in the report
- [ ] `bridge state vacuum` clears the local state store after confirming with the user
- [ ] After vacuum, bridge idempotency relies on MB-003 upsert mode and MB-002 `FindByMetadata`
- [ ] Bridge falls back to state-store mode automatically if `FindByMetadata` is unavailable (older MuninnDB)

#### Dependencies

MB-001 (GetEngram), MB-002 (FindByMetadata), MB-003 (BatchRemember upsert)

---

### BR-007

**MuninnDB vault pre-flight checks and auto-create**

**Type**: `feat` `internal`  
**Priority**: P3 · **Size**: S · **Phase**: 3

#### Description

Every `bridge sync start` should verify that the configured target vault exists in MuninnDB before the first sync attempt. If `auto_create_vault: true` is set, it creates the vault automatically. If `auto_create_vault: false` and the vault doesn't exist, the daemon fails fast with a clear error rather than failing mid-sync on the first `BatchRemember` call.

Pre-flight also validates token permissions: does the configured token have write access to the vault?

```
$ bridge sync start

Pre-flight checks...
  go-rag gRPC 127.0.0.1:7880  ✓ healthy (v1.2.0)
  MuninnDB gRPC 127.0.0.1:8477 ✓ healthy (v0.6.1)
  Vault 'go-rag' in MuninnDB  ✓ exists (14,823 engrams)
  Vault 'security' in MuninnDB ✗ not found
    → auto_create_vault: true — creating...
    ✓ Vault 'security' created

Starting sync daemon...
```

#### Acceptance criteria

- [ ] Pre-flight runs before any sync work begins
- [ ] Missing vault + `auto_create_vault: false` → daemon exits with code 1 and clear message
- [ ] Missing vault + `auto_create_vault: true` → vault created, daemon continues
- [ ] Token permission failure → daemon exits with code 1 (auth error)
- [ ] Pre-flight results are shown in `bridge sync start` output (not just logs)
- [ ] Pre-flight is skipped with `--skip-preflight` flag for fast restarts in known-good environments

#### Dependencies

MB-005 (Health check), MB-006 (ListVaults/EnsureVault), BR-002 (connection lifecycle)

---

## Implementation order

```
Sprint 1 (unblock sync correctness)
  MB-001  GetEngram
  MB-003  BatchRemember upsert
  MB-004  PatchEngram
  MB-005  Health/Ping
  BR-001  bridge muninn init wizard
  BR-002  Connection lifecycle manager

Sprint 2 (vault lifecycle + listing)
  MB-006  ListVaults / EnsureVault
  MB-007  ListEngrams
  MB-008  DeleteEngram / BatchDeleteEngrams
  MB-011  GetServerCapabilities
  BR-003  Capability negotiation
  BR-004  Max content size + chunk splitting
  BR-005  Rate-limit backoff

Sprint 3 (graph and confidence)
  MB-002  FindByMetadata  ← depends on metadata index work
  MB-009  StrengthenEdge
  MB-010  AdjustConfidence
  MB-014  BatchPatch
  BR-006  State store migration
  BR-007  Vault pre-flight

Sprint 4 (event subscription + predictive)
  MB-012  WatchTriggers stream
  MB-013  GetPredictedNext
```

**Note on MB-002 ordering**: `FindByMetadata` requires a secondary index on `metadata["chunk_id"]` in MuninnDB's Pebble store. This is a non-trivial schema addition that should be designed carefully to avoid full-vault scans at scale — hence it's deferred to Sprint 3 despite being listed as P1. The bridge can function with its local state store until MB-002 lands.

---

## Open questions for MuninnDB maintainers

| # | Question | Affects |
|---|---|---|
| Q1 | What is the enforced max content size per engram? | BR-004, chunk splitting logic |
| Q2 | Does `BatchRemember` call the Ebbinghaus/Hebbian engine per item, or batch? Affects whether rapid upserts cause unexpected cognitive state changes | MB-003 |
| Q3 | Is there a metadata secondary index today, or would MB-002 require a schema migration? | MB-002 timeline |
| Q4 | Do `StrengthenEdge` edges decay at the same rate as Hebbian edges? Should explicit edges from wikilinks decay slower? | MB-009 |
| Q5 | Does `GetEngram` count as an access for Hebbian learning, or is it a transparent read? | MB-001 |
| Q6 | What is the current rate limit on `BatchRemember`? Per-second or per-minute? Per vault or per token? | BR-005 |
| Q7 | Is the `WatchTriggers` stream delivery at-least-once or exactly-once? The bridge needs to handle duplicates idempotently either way | MB-012 |
| Q8 | Does MuninnDB expose the standard `grpc.health.v1.Health` service already, or only a custom endpoint? | MB-005 |
