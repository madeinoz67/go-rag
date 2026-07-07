# Data Model — go-rag UI Console, Slice 0

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Persistence: none

The UI transport is **stateless presentation**. It owns no Pebble key-space, adds
no storage prefixes, ships no migration, and holds no mutable server-side state.
Sessions live in the spec 045 session store (already on Pebble under
`PrefixAuthSession`); the UI only *mints* and *validates* them via `internal/auth`.

**Implication for the constitution's Storage Discipline rule:** no on-disk
schema change → no migration, no `ExpectedVersion` bump, no PRD §6.7 update.
 affirmed by R1/R3 (research.md).

## The single source of truth the UI reads

`engine.Status() (*StatusInfo, error)` (`internal/engine/status.go::Engine.Status`).
The UI never writes; the Dashboard is a read projection. Full `StatusInfo` field
list is in `internal/engine/types.go::StatusInfo`; the Dashboard DTO below is the
subset Slice 0 surfaces.

## Entities (in-memory / wire only)

### `DashboardDTO` — `/api/dashboard/stats` response

Projection of `StatusInfo` + a derived vault. All fields already exist on
`StatusInfo` except `vault` (edge-derived).

| Field | Type | Source | Meaning |
|-------|------|--------|---------|
| `documents` | int | `StatusInfo.Documents` | doc count (PrefixDocument) |
| `chunks` | int | `StatusInfo.Chunks` | chunk count |
| `embeddings` | int | `StatusInfo.Embeddings` | embedding count |
| `dimensions` | int | `StatusInfo.Dimensions` | majority stored dim (0 if empty) |
| `embedding_model` | string | `StatusInfo.EmbeddingModel` | majority stored model |
| `reranker` | string | `StatusInfo.Reranker` | `"disabled"` when unset |
| `ollama_url` | string | `StatusInfo.OllamaURL` | configured Ollama endpoint |
| `embeddings_complete` | bool | `StatusInfo.EmbeddingsComplete` | **index-health flag** (`docs==0 \|\| embs>=chunks`) |
| `drift_verdict` | string | `StatusInfo.DriftVerdict` | `clean\|hard-drift\|version-warning\|unknown\|n/a` |
| `hard_drift` | bool | `StatusInfo.HardDrift` | drift boolean form |
| `embed_pending` | int | `StatusInfo.EmbedPending` | spec 030 backlog |
| `embed_failed` | int | `StatusInfo.EmbedFailed` | failed embed count |
| `enriched_docs` | int | `StatusInfo.EnrichedDocs` | spec 029 enrichment |
| `enrichment_enabled` | bool | `StatusInfo.EnrichmentEnabled` | enrichment on/off |
| `vault` | string | derived | `filepath.Base` of the engine's DBPath relative to `vault.Root()` |

**Validation:** none server-side — it is a faithful projection. The client treats
`embeddings_complete == true && hard_drift == false` as "index healthy."

### `loginRequest` / `loginResponse` — `POST /login` (from spec 045)

Identical to `internal/rest/auth.go::loginRequest`/`loginResponse` (contract
pinned by spec 045 tests; lifted into `internal/ui`):

```go
type loginRequest  struct { Username string `json:"username"`;  Password string `json:"password"` }
type loginResponse struct { Token     string `json:"token"`;     ExpiresAt string `json:"expires_at"` } // RFC3339 UTC
```

### `Principal` (from spec 045, context-injected — not UI-defined)

`auth.Principal{Subject, Mode, Source}`. Sessions always carry `Mode=admin`,
`Source=session`. The UI reads it only to render the authenticated shell; it
makes no authorization decision in Slice 0 (Dashboard is read-only).

## Embedded asset tree (served content, not "data")

```
internal/ui/web/
├── templates/{index.html, _placeholder.html}
└── static/{css/{theme,base,components,utilities}.css, js/app.js, vendor/{alpine,chart,cytoscape}.min.js}
```

This is `//go:embed`-ded into the binary (R4). It is version-controlled source,
not runtime data.

## Route table (authoritative in contracts/ui-transport.md)

| Method+Path | Auth | Handler |
|---|---|---|
| `GET  /` | guard (bypass-enabled) | shell (`index.html`) |
| `GET  /static/*` | public (login-page assets) | `http.FileServer` over embed.FS |
| `POST /login` | public | `handleLogin` (VerifyPassword → MintSession) |
| `POST /logout` | guard (any credential) | `handleLogout` |
| `GET  /api/dashboard/stats` | guard | `handleDashboardStats` → DashboardDTO |
| `GET  /api/placeholder/{view}` | guard | placeholder marker |

## State transitions

None. Slice 0 is read-only with a single state change: unauthenticated ⇄
authenticated (client-side Alpine gate driven by the presence of a `gorags_`
token in memory; server-side the only transition is session mint at `/login` and
session drop at `/logout`/expiry).

## Migrations

**None.** No storage change. (The constitution's migration-contiguity rule is
unaffected.)
