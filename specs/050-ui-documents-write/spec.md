# Feature Specification: go-rag Management Console — Documents Write-Actions (Slice 4)

**Feature Branch**: `050-ui-documents-write`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: *"Spec 050 — Documents write-actions: the FIRST write surface in the management console (specs 046–049 were all read-only). Make the Documents view actionable: ADD documents by server-side path (matches the go-rag add CLI; file upload is a separate future US), REMOVE a document, and REINGEST a document. Destructive ops require confirmation. Reuse the existing engine write path and spec 045 Bearer guard. Writes ACK fast; the pending/embedding state surfaces in Operations (spec 049)."*

## Context & Background

Specs 046–049 built the console's **read-only** surfaces — the shell, Documents browse, Query
retrieval, and Operations health. Every one deliberately deferred its write-actions to "a
later slice," leaving the console a viewer, not a tool. **This spec is that later slice for
Documents** — the first surface that mutates the vault from the browser. After 050, an
operator can manage a corpus without leaving the console.

The three actions map onto the existing engine write path, exactly as the CLI exposes it:
**Add** by path (`Engine.Add`, `go-rag add`), **Reingest** by source path (`Engine.Reprocess`,
`go-rag reprocess`), and **Remove** by document ID (`Pipeline.DeleteDoc` — present at the
pipeline level, exposed here via a new thin `Engine.DeleteDoc` wrapper, mirroring how spec 049
added `Engine.AuditRead`). The UI becomes a **4th adapter over the same write path the CLI
uses** — cross-transport parity holds by construction, because every transport calls the same
engine methods.

The view reuses verbatim — and changes none of — the spec 046 shell, the Alpine `goragApp`
root, the 4-layer CSS, `go:embed` static serving, the loopback UI transport, and the spec 045
Bearer-session guard. It introduces **no new transport, no new storage, no new auth, no Node
build chain, and no new ingest/embedding logic** — only thin write handlers that call the
existing engine write methods, plus the one `Engine.DeleteDoc` wrapper.

A note on "add": go-rag's add is **path-based** (the daemon reads a path from disk), matching
`go-rag add <path>`. The daemon is single-operator loopback, so the operator types a path the
daemon can read. Browser file-upload (multipart) is a deliberately separate, future US — it
adds upload handling, staging, and size limits out of proportion to this slice.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Add documents by server-side path (Priority: P1)

An operator opens the **Documents** view, chooses "Add", enters a server-side path (a file or
a directory, with an optional glob filter defaulting to all), and submits. The daemon ingests
the path; the write acknowledges fast (the engine's async-after-ACK model: durable Pebble
commit in under 10ms, embedding/indexing on background workers). The newly-ingested document
appears in the Documents list (initially in a pending/embedding state), and the embedding
backlog drains in the Operations view (spec 049) as the background workers catch up. This is
the "put content into my vault from the console" action.

**Why this priority**: the gate to the rest of the write surface and the single most common
write. Without add, remove and reingest have nothing new to act on. This story alone is a
viable MVP.

**Independent Test**: On an isolated vault with a known file on disk, add it via the console;
the document appears in the Documents list and in `go-rag status`; its chunk count matches;
the Operations backlog rises then drains as embedding completes; the result is byte-identical
to `go-rag add <path>` for the same input.

**Acceptance Scenarios**:

1. **Given** a valid path on disk, **When** the operator submits an add, **Then** the document
   ingests, appears in the list, and matches `go-rag add <path>` (parity).
2. **Given** a directory path + glob, **When** submitted, **Then** every matching file under
   the directory ingests (the count matches the CLI for the same path + glob).
3. **Given** an empty or non-existent path, **When** submitted, **Then** a clear error renders
   (400/404-style) — no silent failure, no partial ingest.
4. **Given** an already-ingested path (same content), **When** re-added, **Then** it is a no-op
   (idempotent, content-addressed — matches CLI behaviour).
5. **Given** a successful add, **When** the background embedding finishes, **Then** the
   document's embedding state resolves to complete in the list (without operator action beyond
   refresh).

---

### User Story 2 - Remove a document (Priority: P1)

An operator selects a document and chooses "Remove". A confirmation dialog states what will be
deleted (the document and all its chunks from the index). On confirm, the document is deleted
by its content-addressed ID and disappears from the list. The operator's source file on disk
is **not** touched — removal is index-only (the vault forgets the document; the file remains).

**Why this priority**: the second half of content lifecycle management. A console that can add
but not remove is half a tool. Requires the new `Engine.DeleteDoc` wrapper.

**Independent Test**: Add a document, then remove it via the console; it disappears from the
Documents list and from `go-rag status`; a query that previously matched it returns no hit;
the source file on disk is unchanged.

**Acceptance Scenarios**:

1. **Given** a document, **When** the operator chooses Remove, **Then** a confirmation dialog
   names the document and states the index-only deletion; the action does not proceed without
   explicit confirm.
2. **Given** a confirmed remove, **When** it completes, **Then** the document and its chunks
   are gone from the list, from `go-rag status` counts, and from query results.
3. **Given** a remove, **When** the source file is checked, **Then** it is untouched on disk
   (removal is index-only — no file deletion).
4. **Given** an unknown/already-removed document ID, **When** remove is attempted, **Then** a
   clear not-found error renders (no silent success, no crash).

---

### User Story 3 - Reingest a document (Priority: P2)

An operator selects a document whose source has changed (or whose chunking/embedding should be
re-derived under the current reader/embedder) and chooses "Reingest". A confirmation dialog
states that chunks/embeddings will be re-derived for the document's source path (bypassing
dedup). On confirm, the document is re-ingested from its source path; the list reflects the
updated state once the background work lands.

**Why this priority**: essential for keeping a corpus current after reader/embedder changes or
source edits, but the view is already valuable at P1 (add + remove) without it.

**Independent Test**: Add a document, change its source file on disk, reingest via the console;
the document's chunks reflect the new content; the result matches `go-rag reprocess <path>`.

**Acceptance Scenarios**:

1. **Given** a document, **When** the operator chooses Reingest, **Then** a confirmation dialog
   names the source path and states dedup is bypassed; the action does not proceed without
   explicit confirm.
2. **Given** a confirmed reingest, **When** it completes, **Then** the document's chunks are
   re-derived from the current source, matching `go-rag reprocess <path>` (parity).
3. **Given** a document whose source file no longer exists, **When** reingest is attempted,
   **Then** a clear error renders (the source is gone — distinct from a successful empty
   reingest).

---

### User Story 4 - Writes are auth-guarded, confirmed, observable, and shell-consistent (Priority: P2)

Every write is a guarded mutation: no write route is reachable without a valid spec 045 Bearer
session; destructive operations (remove, reingest) require explicit confirmation; every write
is reflected in the audit log (spec 021) and in the Operations view's activity/backlog (spec
049). The slice introduces no new authentication surface, no Node/build chain, and degrades
gracefully on errors. This is a constraint, proven once so every later write surface inherits
it.

**Why this priority**: not a feature but a hard invariant (writes are guarded + confirmed +
observable + no-Node + single-binary). P2 because the actions function before the invariant is
formally proven, but it must hold before the slice ships.

**Independent Test**: Inspect every write route — all are guarded; attempt a remove/reingest
without confirmation — it does not proceed; after a write, confirm an audit event was logged
and the Operations activity/backlog reflects it; confirm no `package.json`/`node_modules` is
introduced; confirm a session-expired write returns 401 → login (no partial mutation).

**Acceptance Scenarios**:

1. **Given** any write route, **When** called without a valid Bearer session, **Then** it
   returns 401 and no mutation occurs.
2. **Given** a destructive action (remove, reingest), **When** initiated, **Then** it does not
   proceed without an explicit confirmation step.
3. **Given** a completed write (add/remove/reingest), **When** the audit log and Operations
   view are inspected, **Then** each write is recorded (audit event) and reflected (activity /
   backlog).
4. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts
   are introduced.
5. **Given** a session that expires mid-write, **When** the request returns 401, **Then** no
   partial mutation is left (the engine write is atomic — ACK-or-not) and the shell routes to
   login.

---

### Edge Cases

- **Empty / whitespace path on add** — 400 `path required`, no ingest.
- **Non-existent path on add** — a clear error (the daemon cannot read it), no partial ingest.
- **Permission-denied path on add** — a clear error, no crash.
- **Directory with zero matching files (glob)** — a healthy "0 ingested" result, not an error.
- **Path outside the daemon's reasonable reach** (e.g. `/dev/null`, a socket) — a clear error.
- **Re-add of identical content** — idempotent no-op (content-addressed).
- **Remove of an already-removed / unknown ID** — clear not-found, no silent success.
- **Reingest of a document whose source vanished** — clear "source not found" error.
- **Reingest of a path shared by several documents** — all matching documents re-ingest (the
  engine reprocesses the path); the confirmation states this scope.
- **Embedder unreachable during add/reingest** — the write still ACKs (chunks indexed); the
  embeddings pend/fail and surface in Operations (spec 049); the document is queryable by
  keyword, not vector, until the embedder returns.
- **Mid-write session expiry** — 401; no partial mutation (atomic ACK).
- **Operator navigates away mid-embed** — fine; embedding continues on background workers and
  resolves on next refresh.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST provide an Add action that ingests a server-side path (file or
  directory) with an optional glob filter, via the existing `Engine.Add`.
- **FR-002**: The view MUST provide a Remove action that deletes a document by its
  content-addressed ID via a new thin `Engine.DeleteDoc` wrapper over the existing
  `Pipeline.DeleteDoc` — index-only, never touching the source file on disk.
- **FR-003**: The view MUST provide a Reingest action that re-derives a document's
  chunks/embeddings from its source path via the existing `Engine.Reprocess`.
- **FR-004**: Destructive actions (remove, reingest) MUST require explicit confirmation that
  names the target and states the effect before proceeding.
- **FR-005**: Every write route MUST be gated by the existing spec 045 Bearer-session guard —
  no write is reachable unauthenticated.
- **FR-006**: Writes MUST ACK per the engine's async-after-ACK model (fast durable commit;
  embedding/indexing async) and the resulting pending/embedding state MUST be observable in the
  Operations view (spec 049) and the Documents list.
- **FR-007**: Every completed write MUST be recorded in the audit log (spec 021) and reflected
  in the Operations activity/backlog.
- **FR-008**: Write results MUST match the equivalent CLI action byte-for-byte for the same
  input — add ≡ `go-rag add`, reingest ≡ `go-rag reprocess` (cross-transport parity, same
  engine write path).
- **FR-009**: Errors (empty/missing path, unknown ID, vanished source, permission denied,
  embedder-down) MUST surface as clear, operator-actionable states — never silent failures or
  partial mutations.
- **FR-010**: The slice MUST ship inside the single binary via the existing embedded, vendored
  SPA — no Node / Vite / Tailwind build chain.
- **FR-011**: The slice MUST NOT delete or modify source files on disk (removal is index-only;
  add/reingest read source files but never write them).
- **FR-012**: The slice MUST NOT introduce file upload (multipart), bulk/batch operations,
  re-embed-only, scan-trigger, source-level cascades, cross-vault writes, or undo/soft-delete.

### Key Entities *(include if feature involves data)*

- **Add Request**: a server-side path (file or directory) + optional glob; the input to
  `Engine.Add`.
- **Ingest Summary**: the result of an add or reingest — counts of new, skipped, and errored
  documents (`Engine.IngestSummary`).
- **Document Identity**: a content-addressed document ID — the target of a remove
  (`Engine.DeleteDoc`).
- **Source Path**: the on-disk path a document was ingested from — the target of a reingest
  (`Engine.Reprocess`) and the (unchanged) file removal leaves behind.
- **Confirmation**: the explicit operator approval required before a destructive action
  proceeds.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can add a document to an empty vault from the console and have it
  appear in the Documents list within 2 seconds (loopback), with embedding completing in the
  background.
- **SC-002**: An operator can remove a document from the console and confirm it is gone from
  the list, from `go-rag status`, and from query results — while the source file remains on
  disk.
- **SC-003**: 100% of console writes match the equivalent CLI action for the same input —
  zero drift (add ≡ `go-rag add`, reingest ≡ `go-rag reprocess`).
- **SC-004**: No destructive action (remove, reingest) completes without explicit operator
  confirmation.
- **SC-005**: No write route is reachable without a valid session; no write leaves a partial
  mutation on session expiry.
- **SC-006**: Every console write is recorded in the audit log and reflected in the Operations
  view — verifiable by inspecting both after any write.
- **SC-007**: The slice introduces zero new build tooling — a single `make build` still
  produces one binary with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp`
  root, and spec 045 Bearer auth unchanged — exactly as specs 047–049 did.
- **Add is path-based** (the daemon reads a path from disk), matching `go-rag add <path>`.
  Browser file-upload (multipart) is a separate future US, out of scope here.
- `Engine.Add(ctx, path, glob)` and `Engine.Reprocess(ctx, path)` already exist and are what
  the CLI uses; the UI calls them in-process as a 4th adapter.
- Remove needs a new thin `Engine.DeleteDoc(docID)` wrapper over the existing
  `Pipeline.DeleteDoc` — read across the engine boundary, mirroring how spec 049 added
  `Engine.AuditRead`. Plan confirms the exact wrapper shape and that it composes with the
  spec 044 per-document lock.
- Tags are **not** set on add (`Engine.Add` has no tags parameter); tags come from enrichment
  (spec 029). Setting tags at add-time is a separate future concern.
- Reingest operates on the document's **source path** (that is what `Engine.Reprocess` takes),
  which the Documents detail already surfaces.
- Single-operator use; no multi-user or RBAC concerns (PRD N2). The loopback daemon reads
  paths the operator already has disk access to.
- Desktop-first per `docs/style-guide.md`; mobile is not a target.

---

## Open Questions (to resolve in plan / tasks)

- **Add feedback** — whether the list auto-refreshes on ACK or requires manual refresh, and
  whether a transient "embedding in progress" badge shows on newly-added rows until the
  background work lands. Lean: auto-refresh on ACK + transient pending badge.
- **Reingest scope wording** — when a source path is shared by several documents, the
  confirmation dialog should state that all matching documents re-ingest. Lean: include the
  match-count in the confirm text where knowable.
- **Remove confirmation strength** — simple confirm button vs. type-the-name. Lean: simple
  confirm (single-operator loopback, low-risk, index-only + source file preserved).
- **Glob UI** — whether the add form exposes the glob as a text field (advanced) or hides it
  behind a default ("all files"). Lean: expose as an optional field, default empty (= all).
- **Concurrent-write guard** — whether the UI prevents two simultaneous writes to the same
  document (the engine's spec 044 per-doc lock already serializes at the engine level; the UI
  may add client-side disable-on-submit). Lean: client-side disable-on-submit per action.
