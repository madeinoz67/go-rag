# Research — Documents Write-Actions (Slice 4)

**Feature**: specs/050-ui-documents-write | **Date**: 2026-07-12

Phase 0 output. Resolves the spec's Open Questions and records the design decisions from
source-inspecting the engine write path + the cross-transport write exposure. Every decision
is grounded in code read this session.

---

## Source grounding (verified before designing)

- `Engine.Add(ctx, path, glob string) (*IngestSummary, error)` — `internal/engine/ingest.go:28`.
  Path-based; glob defaults to `"*"`. Returns `IngestSummary{New, Skipped, Errors, ...}`. Audits
  `IngestEvent("add", ...)`. **No tags parameter** — tags come from enrichment (spec 029).
- `Engine.Reprocess(ctx, path string) (*IngestSummary, error)` — `internal/engine/ingest.go:100`.
  Force-re-ingests a **path** (bypasses SHA-256 dedup); glob `"*"`. Audits `IngestEvent("reprocess", ...)`.
- `Pipeline.DeleteDoc(docID string) error` — `internal/pipeline/delete.go:17`. Takes a
  content-addressed **doc ID**; serializes via the spec 044 per-doc lock (`docLock`); deletes
  the doc + its chunks from Pebble and updates the live in-memory FTS/Vector index
  (`deleteDocLocked`). No engine wrapper exists.
- **Cross-transport write exposure (verified):**
  - **Add** — REST `POST /v1/add` (`handleAdd`), gRPC `Adapter.Add`, MCP `go_rag_add`
    (`renderAdd`), CLI `go-rag add`. **Full parity.**
  - **Reprocess** — REST `POST /v1/reprocess` (`handleReprocess`), gRPC `Adapter.Reprocess`,
    MCP `go_rag_reprocess` (`renderReprocess`), CLI `go-rag reprocess`. **Full parity.**
  - **Delete** — **nowhere.** No REST/gRPC/MCP/CLI surface; only the pipeline method + its test.

**Headline findings**: (1) add/reingest are already fully cross-transport — the UI is a clean
5th adapter over them, no new engine method. (2) remove is a **new operation** with zero
transport exposure; per constitution V + the spec 047 precedent it ships cross-transport here.
(3) reingest targets a **source path** (what `Engine.Reprocess` takes), so the UI resolves the
document's source path from its ID before reingesting.

---

## R1 — Three write routes, all guarded (resolves route shape)

**Decision**: `POST /api/documents` (add), `DELETE /api/documents/{id}` (remove),
`POST /api/documents/{id}/reingest` (reingest). All guarded by `Server.guard`. UI calls the
engine in-process — a 5th adapter.

**Rationale**: REST-verb alignment (POST creates, DELETE removes, POST triggers reingest).
Matches the REST `/v1/add` + `/v1/reprocess` precedent. Path-value `{id}` is the
content-addressed document ID (Go 1.22 pattern mux).

**Alternatives rejected**: (a) one `POST /api/documents/{id}/action` with an action field —
rejected: less REST-honest, harder to gate per-verb. (b) PUT for reingest — rejected: reingest
is an action (re-derive), not an idempotent state-set.

---

## R2 — Add/reingest reuse cross-transport engine methods (resolves the adapter question)

**Decision**: The UI's add handler calls `Engine.Add(ctx, path, glob)`; the reingest handler
resolves the doc's source path then calls `Engine.Reprocess(ctx, path)`. No new engine method
for either.

**Rationale**: both are already on CLI/MCP/REST/gRPC — the UI joins as the 5th adapter over the
same write path the CLI uses. Cross-transport parity is inherited (add ≡ `go-rag add`,
reingest ≡ `go-rag reprocess`). This is the cheapest part of the slice.

**Alternatives rejected**: a UI-local ingest wrapper — rejected: needless; the engine methods
are the canonical surface.

---

## R3 — Remove ships cross-transport (resolves the constitution-V question)

**Decision**: Remove is a **new operation**. Ship it cross-transport: new `Engine.DeleteDoc`
wrapper over `Pipeline.DeleteDoc`, plus CLI (`go-rag delete`), REST (`DELETE /v1/documents/{id}`),
gRPC (`DeleteDocument`), MCP (`go_rag_delete_document`), and proto (`DeleteDocumentRequest`/
`Response` + rpc). Mirrors spec 047's `ListChunks` rollout exactly.

**Rationale**: constitution Principle V — every operation should be cross-transport (MCP-first,
no CLI-only ops). `Pipeline.DeleteDoc` exists but is on no transport; shipping it UI-only would
leave delete transport-asymmetric (a CLI/MCP user couldn't delete). The spec 035–039 / 047
precedent is to ship a new accessor across all transports in one slice.

**Alternatives rejected**: (a) UI-only delete — rejected: violates constitution V (new op left
asymmetric). (b) Defer delete to a separate slice — rejected: it is one of the three core write
actions the spec requires; splitting inflates the spec count without benefit. (c) Engine wrapper
only (no transport projections) — rejected: same constitution-V gap as (a).

---

## R4 — Add is path + optional glob, no tags (resolves add shape)

**Decision**: `POST /api/documents` body `{path, glob?}` → `Engine.Add(ctx, path, glob)` (glob
defaults to `""` → engine uses `"*"`). No tags field.

**Rationale**: `Engine.Add`'s signature is `(ctx, path, glob)` — there is no tags parameter.
Tags are applied by enrichment (spec 029), not at add-time. Exposing glob matches the engine's
directory-ingest capability; defaulting to all keeps the common single-file add simple.

**Alternatives rejected**: (a) add a tags field and ignore it — rejected: misleading. (b) Force
glob UI — rejected: default-all is the common case; glob is advanced.

---

## R5 — Reingest resolves the source path from the doc ID (resolves reingest targeting)

**Decision**: `POST /api/documents/{id}/reingest` — the handler resolves the document's source
path from its ID (via the existing `Engine.GetDocument` / the document store), then calls
`Engine.Reprocess(ctx, sourcePath)`. The operator clicks "Reingest" on a document; the handler
figures out the path.

**Rationale**: `Engine.Reprocess` takes a **path**, not a doc ID. The document's source path is
already surfaced on the Documents detail (spec 047). Resolving it server-side (from the ID in
the URL) is cleaner than requiring the operator to type/re-send the path.

**Alternatives rejected**: (a) `POST /api/documents/reingest` with a path body — rejected: loses
the doc-identity link; the operator would type a path. (b) Reprocess by doc ID (engine change)
— rejected: `Engine.Reprocess` is path-based and cross-transport; changing its signature breaks
parity. Resolve the path from the ID instead.

---

## R6 — Remove is index-only, by doc ID (resolves remove semantics)

**Decision**: `DELETE /api/documents/{id}` → `Engine.DeleteDoc(ctx, id)` → `Pipeline.DeleteDoc(id)`.
Deletes the document + its chunks from the index. **Never touches the source file on disk.**

**Rationale**: removal is "the vault forgets this document," not "delete the file." The source
file is the operator's; the engine never owned it. `Pipeline.DeleteDoc` already does exactly
this (Pebble delete + live-index update). Index-only is safe and reversible (re-add the path).

**Alternatives rejected**: (a) also delete the source file — rejected: the engine must not
delete operator files; out of scope and unsafe. (b) Soft-delete (tombstone) — rejected:
`Pipeline.DeleteDoc` is a hard delete; soft-delete is a separate future concern (spec OQ).

---

## R7 — Confirmation is client-side UX, not server-enforced (resolves confirmation)

**Decision**: Destructive actions (remove, reingest) show a confirmation dialog in the Alpine UI
before the request is sent. The server does not enforce a confirmation token — it executes the
guarded mutation on receipt of an authenticated request.

**Rationale**: confirmation is an operator-intent gate (UX), not an auth/CSRF control. The spec
045 Bearer guard is the security boundary; confirmation prevents fat-finger mistakes. A
server-side confirmation token would add ceremony without security value (a confirmed request
is indistinguishable from an unconfirmed one at the server).

**Alternatives rejected**: server-side confirm token (two-phase) — rejected: ceremony without
security benefit; the guard + dialog suffice for single-operator loopback.

---

## R8 — ACK semantics: add/reingest return a summary; remove returns 204 (resolves responses)

**Decision**: Add + reingest return `IngestSummary` JSON (`{new, skipped, errors}`) on ACK —
the engine ACKs fast (async-after-ACK), embedding continues in the background and surfaces in
Operations (spec 049). Remove returns `204 No Content` on completion (synchronous delete).

**Rationale**: add/reingest follow the existing async-after-ACK model (the summary is available
at ACK; the pending/embedding state is observable separately). Remove is synchronous (the doc
is gone when the handler returns), so 204 is the honest response.

**Alternatives rejected**: (a) block add/reingest until embedding completes — rejected: violates
async-after-ACK (Principle IV); embedding can take seconds-minutes. (b) Return 202 (Accepted)
for add/reingest — rejected: 200 + the summary is the REST `/v1/add` precedent; consistency wins.

---

## R9 — Error mapping (resolves error states)

**Decision**: `path required` (400) on empty/whitespace path; `not found` (404) on unknown doc
ID (remove/reingest); `source not found` (404/409) on reingest of a doc whose source vanished;
`invalid request body` (400) on malformed JSON; engine errors via `writeEngineErr` (existing
helper, same package). All errors are plain, operator-actionable; no silent failures.

**Rationale**: matches the REST `/v1/add` + `/v1/reprocess` error shapes. The operator must be
able to tell a bad path from a missing doc from a vanished source.

**Alternatives rejected**: generic 500 for all — rejected: hostile to operator recovery.

---

## R10 — Parity pinned for all three (resolves the parity test)

**Decision**: Ship cross-transport parity tests: add (UI ≡ `go-rag add` ≡ REST `/v1/add`),
reingest (UI ≡ `go-rag reprocess`), and delete (engine ≡ CLI ≡ REST ≡ gRPC ≡ MCP — the new
operation, tested across all five). Pattern: spec 047's `TestCrossTransport_ListChunksParity`.

**Rationale**: constitution V. The new delete operation MUST be parity-pinned across every
transport (the 047 precedent). Add/reingest parity is structural (same engine methods) but
pinned anyway against the CLI.

**Alternatives rejected**: trust structural parity for add/reingest without a test — rejected:
the repo's standing pattern is to pin parity with a test on every transport surface.
