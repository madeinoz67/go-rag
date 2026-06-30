# Research — GetChunk RPC (spec 035)

> Phase 0 output of `/speckit-plan`. Resolves the three planner questions from
> `spec.md` and affirms the Constitution gate, **grounded in the codebase**.
> Every claim cites `file:line` evidence gathered by the research workflow
> (5 readers → 3 adversarial verifiers, all `confirmed`).

**Method.** A workflow fanned five readers over independent surfaces
(`internal/engine`, `internal/storage`, `internal/model`, `proto/gorag.proto` +
the four transport adapters, `internal/storage/migrate`), then three verifiers
adversarially re-checked the load-bearing claims against the actual code. All
three verdicts: **confirmed**. One secondary rationale was corrected by a
verifier (REST URL shape — see R1.D).

---

## R1 — Vault contract (planner Q1)

### Decision
**OMIT the `vault` field from `GetChunk`.** The proto request is
`GetChunkRequest { string chunk_id = 1; }` and the engine method is
`GetChunk(chunkID string) (*ChunkResult, error)` — arity- and wire-identical to
`ReleaseChunk` / `ResetChunk`. The bridge backlog's `string vault = 2` is
**dropped** from `GetChunkRequest`. gRPC, CLI, and MCP carry no vault field;
REST has no per-vault URL segment.

### Rationale (three converging lines of evidence)
1. **API parity (Constitution V).** Every existing chunk-scoped RPC and every
   read RPC takes *only* a content-addressed ID — none carries a vault:
   - `ReleaseChunk(chunkID string) error`, `ResetChunk(chunkID string) error` — `internal/engine/poison.go:60,79`
   - `Query(ctx, QueryRequest)` — `QueryRequest` has **no** `Vault` field (`internal/engine/types.go:18-43`)
   - `ReleaseChunkRequest { string chunk_id = 1; }` / `ResetChunkRequest { string chunk_id = 1; }` — `proto/gorag.proto:130-135`
   Adding a vault field to `GetChunk` alone would break the symmetry Principle V exists to protect.
2. **Engine model.** The engine is **single-vault-per-process**. `engine.Open(base)`
   calls `storage.Open(<base>/data)`, binding exactly one Pebble handle (one
   vault) for the engine's lifetime (`internal/engine/helpers.go:18-34`); the
   `Engine` struct has **no field to hold a vault selector** (`internal/engine/engine.go:35-36`).
   A vault parameter on `GetChunk` would be semantically dead — there is nothing to select.
3. **Vault semantics.** Vaults are filesystem directories
   (`vault.Path(name) = $GO_RAG_VAULT_ROOT/<name>`, default `~/.go-rag/vaults/<name>`),
   chosen **once at process start** via `--vault` / `--db-path` in
   `cli/root.go` `PersistentPreRunE` (`root.go:32-39,54-56`). `ListVaults`
   (`internal/engine/config.go:59-71`) is the *only* vault-bearing RPC, and it
   works precisely because it **bypasses `e.db`** — opening sibling vaults
   transiently via `Open(vault.Path(n))` then closing immediately. It is
   directory enumeration, not a per-call read-path selector.

The backlog's `vault` field is an artifact of designing the proto before reading
the engine convention. The engine wins (Constitution: code before prompts; PRD
single-vault/single-writer model).

### Alternatives considered
- **`vault` required.** Rejected — the engine cannot honour it; it would be a
  field the server ignores, which is worse than absent.
- **`vault` optional-with-default.** Rejected — same dead-parameter problem, and
  it implies a per-call selector that does not exist in any sibling RPC.

### R1.D — REST URL shape (verifier correction)
The bridge backlog proposed `GET /api/vaults/{vault}/chunks/{chunk_id}`. **This
is rejected** — a verifier confirmed it breaks the existing REST convention
(`internal/rest/server.go:42-63`): every route is `/v1/<resource>` (e.g.
`POST /v1/poison/{id}/release`, `POST /v1/poison/{id}/reset`); there is **no**
`/api/` base path and **no** per-vault segment on any route (`GET /v1/vaults` is
the `ListVaults` *listing*). The `GetChunk` REST route is therefore
**`GET /v1/chunks/{id}`**, matching the `/v1/poison/{id}/...` precedent. The OMIT
decision and single-vault reasoning stand; only this part of the backlog's
rationale was wrong.

---

## R2 — Chunk & DocumentMeta message shapes (planner Q2)

### Decision
`GetChunkResponse { Chunk chunk = 1; DocumentMeta document = 2; }`, where:

- **`Chunk`** is a 1:1 projection of `model.Chunk` (`internal/model/model.go:81-128`)
  for identity / content / position / pagination / linked-list / sidecars.
- **`DocumentMeta`** is a **net-new** message projecting `model.Document`
  (`model.go:24-49`) + a flattened view of `*EnrichInfo` (spec 029). No existing
  full-Document projection exists — `QueryHit` deliberately carries only a thin
  slice (`file_path` / `summary` / `enrichment_status`).
- **Reuse the already-defined proto messages** `Poisoning` (`proto:101`),
  `NearDup` (`proto:96`), `PoisoningSignals` (`proto:112`) — do **not** redefine
  them. `QueryHit` (`proto:80-94`, `internal/engine/types.go:55-90`) is the
  canonical field template.

### Field projections

**`Chunk`** (see `data-model.md` for the full table):
`chunk_id`, `document_id`, `content`, `chunk_index`, `total_chunks`,
`page_number` (= `Chunk.PageNumber`, 0 = not paginated — mirrors `QueryHit.page`),
`start_char`, `end_char`, `token_count`, `previous_chunk_id`, `next_chunk_id`,
`poisoning` (reuse `Poisoning`), `section_context` (`repeated string`),
`near_dup` (reuse `NearDup`), `kind`, `created_at`.

**`DocumentMeta`**:
`id`, `content_hash` (the change-detection hash — **distinct from `id`**, powers
PRD §7.2 idempotent re-add; must stay separate), `source_id`, `source_path`,
`file_path`, `file_name`, `file_type`, `mime_type`, `chunk_count`, `file_size`,
`status`, `ingested_at`, `updated_at`, `tags`, `summary`, `enrichment_status`,
`enrichment_model`, `enrichment_at`.

### Key mappings called out in the spec
| Spec asks for | Maps to | Source |
|---|---|---|
| page | `Chunk.page_number` | `model.Chunk.PageNumber` (= `QueryHit.page`) |
| section context | `Chunk.section_context` | `model.Chunk.SectionContext []string` (= `QueryHit.section_context`) |
| poisoning verdict | `Chunk.poisoning` | `model.Chunk.Poisoning *PoisonVerdict` → existing `Poisoning` msg |
| enrichment status | `Document.enrichment_status` | `model.Document.Enrichment.Status` (read off the **document**, not the chunk) |

### Omissions from v1 (with reasons)
- **`model.Chunk.Caption` (`*CaptionInfo`)** — no existing proto message, no
  `QueryHit` projection. Avoid inventing `message Caption` for a point-lookup;
  add later if a consumer needs it. (`Kind` is kept as a cheap scalar.)
- **`model.Document.Metadata` (`map[string]any`)** — lossy in proto3 (only
  `map<string,string>`); not identity, not on `QueryHit`. The bridge caller can
  fetch it separately if ever needed.
- **`model.Embedding.Vector`** — `json:"-"`, lives in chromem-go not Pebble
  (`model.go`); never returned by any transport (PRD §6.5). No field for it.

### Alternatives considered
- **Flatten into one ranked-hit row (like `QueryHit`).** Rejected — `GetChunk`
  is a join (chunk + its parent document), and the explicit `Chunk`/`DocumentMeta`
  split keeps the join legible and lets a caller ignore the document when it
  only wants the chunk.
- **A single new `Caption` message.** Rejected for v1 (see omission).

---

## R3 — Read path & latency (planner Q3)

### Decision
`GetChunk` is **two Pebble point reads** (optionally three), composing the
**existing** helpers `lookupChunk` + `lookupDoc` (`internal/engine/helpers.go:56-79`):

1. `GET #1`: `e.db.GetWithPrefix(storage.PrefixChunk=0x03, []byte(chunkID))` →
   JSON-unmarshal `model.Chunk`. Absent key → `(nil, false, nil)`.
2. `GET #2`: `e.db.GetWithPrefix(storage.PrefixDocument=0x02, []byte(chunk.DocumentID))`
   → JSON-unmarshal `model.Document`. `chunk.DocumentID` is carried **inline** on
   every stored chunk (`model.go:82`), so the parent key is known after `GET #1`
   with **no scan**.
3. *(optional)* `GET #3`: if `source_path` is included,
   `GetWithPrefix(storage.PrefixSource=0x01, []byte(document.SourceID))` →
   `model.Source`. Still a constant-time point read; no scan.

This exact two-Get sequence is already proven by `ListChunks`
(`internal/eval/run.go:205-217`) — `GetChunk` is that inner pattern minus the
`PrefixScan` wrapper.

### Latency
Constant-time in the scan sense: **no scan, no document-list iteration, no index
walk; latency does not scale with corpus size.** (Pedantically a Pebble point Get
over an LSM-tree is O(log N) in key count — the intended, affirmed meaning is
"corpus-size-independent relative to any scan", which holds.) Adversarial
verdict: **confirmed**.

### Edge: orphan chunk (GET #1 hits, GET #2 misses)
If the chunk exists but its parent document was deleted/stale, `GetChunk`
**succeeds with a populated chunk and an empty/zero `DocumentMeta`** — matching
`ListChunks`' tolerant `FilePath=""` behaviour. The contract is stated
explicitly in `contracts/get-chunk.md`.

### Alternatives considered
- **A single read that returns only the chunk (no document).** Rejected —
  FR-005 / Story 2 require the parent document metadata in the same call.
- **Scan the document's chunk list to find the chunk.** Rejected — violates
  FR-007 (constant-time) and pays a full scan for no reason; the chunk key is a
  pure function of `chunk_id`.

---

## R4 — Constitution gate: no migration (storage discipline / schema evolution)

### Decision
**`migrate.ExpectedVersion` stays at `1`. No migration is added.**
`defaultMigrations` is unchanged.

### Evidence
- `const ExpectedVersion uint64 = 1` — `internal/storage/migrate/migrate.go:22`.
- Sole registered migration: `{Version:1, "introduce schema-version key (v0→v1 bootstrap)", Up: v1Bootstrap}` (`migrate.go:46-48`); `v1Bootstrap` is a no-op body that only establishes the `0xFF` schema-version meta key (`v1_bootstrap.go`), which sits **outside** the `0x01–0x15` data-prefix range (`storage.go:11-31`).
- `engine.Open` (`helpers.go:18-34`) is the sole production caller of `migrate.RunMigrations(db.Pebble())` (migration-on-open, FR-013).

### Why the gate holds
`GetChunk` is storage-**read-only**. It composes two existing point-read helpers
into one engine method and projects results into new RPC message shapes:
1. **No new prefix** — `0x02` and `0x03` both predate spec 035.
2. **No new value encoding** — JSON is the established chunk/document encoding (confirmed at three write sites: `pipeline.storeDocument` `pipeline.go:361-377`, `engine.putChunk` `poison.go:101-108`, read in `eval.ListChunks`).
3. **No new key construction** — the only key form is `[]byte{prefix} || key` via `GetWithPrefix` (`db.go:83-86`).

`Run`'s contiguity validator (`migrate.go:75-84`) requires `[1..N]` with no gaps,
so a `Migration` entry is the **only** way to bump `ExpectedVersion` — the two
are a coupled invariant. Adversarial verdict: **confirmed**.

> **Scope guard for the implementer:** if a future change to `model.Chunk` or
> `model.Document` is bundled into this commit *and* that field is newly
> persisted to disk under a data prefix, **that** triggers a migration — not the
> RPC projection. Scope `GetChunk` strictly as read + project: new messages in
> `proto/gen` + a new engine method, **zero storage/model edits.**

**PR compliance statement:** *No on-disk layout change. `GetChunk` adds no
key-space prefix, value encoding, or key construction; `migrate.ExpectedVersion`
remains 1; no migration is added.*

---

## R5 — Not-found contract (the one genuine gap `GetChunk` must close)

### Decision
Introduce a new sentinel **`engine.ErrNotFound`** (`internal/engine/errors.go`
currently has only `ErrInvalid`). The lookup site wraps it:
`if !ok { return fmt.Errorf("%w: chunk %s", engine.ErrNotFound, chunkID) }`.
Each transport's error mapper is extended to surface it natively:

| Transport | Not-found mapping | File |
|---|---|---|
| gRPC | `codes.NotFound` | extend `toStatusErr` (`internal/grpc/engine_adapter.go:14-19`) |
| REST | `http.StatusNotFound` (404) | extend `writeEngineErr` (`internal/rest/server.go:182-187`) |
| MCP | JSON-RPC `-32001` ("chunk not found") — *not* the `-32603` Internal bucket | `internal/mcp/server.go:104,689` |
| CLI | non-zero exit + `chunk not found: <id>` to stderr (existing cobra convention) | `internal/cli/` |

### Why this is required (not optional)
Today `ReleaseChunk` / `ResetChunk` return a bare `fmt.Errorf("chunk not found: %s", chunkID)`
(`poison.go:60,79`) which is **not** wrapped in `ErrInvalid`, so `toStatusErr` /
`writeEngineErr` mis-map it to gRPC `Internal` / HTTP `500`. For a pure lookup
like `GetChunk`, "not found" is a **normal client outcome** (FR-002, the backlog's
`NOT_FOUND`), so it must map to a real not-found status.

### Recommendation
**Back-fill `ErrNotFound` into `ReleaseChunk` / `ResetChunk` at the same time** —
it fixes the same latent 500-instead-of-404 bug on those RPCs for free and keeps
the chunk-scoped family consistent. The minimum is `GetChunk`; the back-fill is
recommended. This is the single deviation from "pure new surface" and is flagged
explicitly in `tasks.md`.

---

## R6 — Cross-transport parity (Constitution V)

All four transports are thin projections of the one new `engine.GetChunk` method,
so parity holds by construction — exactly as it does for `Query` / `ReleaseChunk`
today. Concrete touch-points (mirror the `ReleaseChunk`/`ResetChunk` precedent)
are enumerated in `contracts/get-chunk.md`.

**One CI invariant to respect:** the parity test `T035`
(`internal/rest/server.go:39-42`) asserts the `routes` table matches
`openapi.yaml` exactly — the new `GET /v1/chunks/{id}` route must be added to
**both** in the same commit or CI fails.

---

## Open risks / implementer decisions

1. **`source_path` (3rd point read).** `file_path` (relative, on `Document`,
   no extra read) already satisfies FR-005's "source file path". `source_path`
   (absolute, from `Source`, prefix `0x01`) is a *bonus* the bridge's
   `ActivateWithRAG` benefits from and costs one constant-time Get. **Recommendation:
   include it** (3 reads, still no scan, gate holds). If minimalism is preferred,
   surface `source_id` only and let the caller resolve.
2. **`kind` on `Chunk`.** New vs `QueryHit`; non-identity sidecar, cheap scalar.
   Include for completeness (drop only if strict `QueryHit` parity is demanded).
3. **MCP not-found code.** `-32001` is within the JSON-RPC 2.0 reserved
   `-32000..-32099` server-error range — safe. Confirm the MCP client tolerates
   server-defined codes (it should; the range is reserved for exactly this).
4. **Proto regeneration** is a manual `buf`/`protoc` step; generated package is
   `github.com/madeinoz67/go-rag/proto/gen;goragpb` (`proto:14`).
5. **Backlog delta.** The bridge backlog (`docs/RFC-bridge-muninndb/go-rag-bridge-backlog.md`
   BL-001) is now out of sync with this plan on two points: (a) `vault` field
   dropped from `GetChunkRequest`; (b) REST path is `/v1/chunks/{id}`, not
   `/api/vaults/{vault}/chunks/{chunk_id}`. The bridge consumer must be updated
   to match — call this out when the bridge work starts.
