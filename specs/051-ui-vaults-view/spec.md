# Feature Specification: go-rag Management Console — Vaults View (management)

**Feature Branch**: `051-ui-vaults-view`

**Created**: 2026-07-13 (revised 2026-07-14 — supersedes the stale read-only draft)

**Status**: Draft

**Input**: User description: *"A management Vaults view for the console. Spec 052 unified the store (one daemon serves all vaults; vault is a per-request parameter; switching is live, no restart) and shipped the lifecycle operations rename/clear/delete. The Vaults view lets the operator list vaults, create a new vault, switch the active vault live, and rename/clear/delete — replacing the read-only placeholder. This supersedes the earlier read-only spec, which predates spec 052."*

## Context & Background

The console's sidebar has a **Vaults** placeholder (view 5, spec 046). An earlier draft of this spec
(spec 051, 2026-07-13) described a *read-only* view, written when each vault was a separate database
served by its own daemon. **That model is obsolete.** Spec 052 (2026-07-13) shipped a **unified store**:
one daemon serves **all** vaults simultaneously — the vault is a per-request parameter (the
`X-Go-Rag-Vault` header), not a process configuration — and added the three lifecycle operations
(rename, clear, delete). The shell already carries a vault picker, but it is hardcoded to a single
vault and never populated from the store.

This spec replaces the placeholder with the **management** surface those capabilities now make
possible: the operator's single place to see every vault, create a new one, switch which corpus the
console is operating on (live, no restart), and rename, clear, or delete a vault — all confirmed,
all inside the authenticated shell.

The view reuses verbatim — and changes none of — the spec 046 shell, the Alpine `goragApp` root, the
4-layer CSS, `go:embed` static serving, the loopback UI transport, the spec 045 Bearer guard, the
no-cache static-asset headers, and the sortable-table convention. The UI is an **adapter over
existing engine methods** (list / create / rename / clear / delete vault; switch is a client-side
state change). It introduces no new transport, no new storage, no on-disk schema change (no
migration, no `ExpectedVersion` bump — Constitution storage discipline), no new auth, and no
Node/build chain.

A note on "switch": because the daemon serves all vaults, switching the active vault is a **live,
client-side** action — it sets the shell's vault picker (the header every `/api/*` call already
carries) and refreshes the current view. There is no daemon restart.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — List vaults and see the active one (Priority: P1) 🎯 MVP

An operator opens the **Vaults** sidebar item and sees every vault, each row showing the vault's name
and document count, with the console's **active** vault (the one the shell's vault picker currently
targets) clearly marked. The shell's vault picker — until now hardcoded to a single vault — is
populated from this list, so every view's vault selector reflects reality.

**Why this priority**: the gate. Without a real vault list, there is nothing to create into, switch
to, rename, or delete.

**Independent Test**: On a store with a known set of vaults, open Vaults; the rows and counts match
`go-rag vault list`; the row matching the console's current vault is marked active.

**Acceptance Scenarios**:

1. **Given** one or more vaults, **When** Vaults opens, **Then** every vault lists with name +
   document count, and the active vault is marked.
2. **Given** the vault list, **When** compared to `go-rag vault list`, **Then** the names + counts
   match.
3. **Given** the shell's vault picker, **When** the list loads, **Then** the picker offers every
   vault (no longer a single hardcoded entry) and defaults to the active one.

---

### User Story 2 — Create a vault (Priority: P1)

An operator creates a new named vault. A dialog collects the name (validated — no invalid
characters, no duplicate, no reserved name); on confirm the vault is created empty and immediately
appears in the list and the picker.

**Why this priority**: creation is the entry point to a new corpus; without it the operator cannot
grow beyond the default vault from the console.

**Independent Test**: Create a vault "archive"; it appears in the list + the picker + `go-rag vault
list`; adding a document to it (Documents view, picker on "archive") stores it under that vault.

**Acceptance Scenarios**:

1. **Given** the create dialog, **When** the operator submits a valid unused name, **Then** the vault
   is created and appears in the list + picker.
2. **Given** the create dialog, **When** the operator submits an invalid name (bad characters, a
   duplicate, or empty), **Then** creation is refused with a clear reason and no vault is created.
3. **Given** a created vault, **When** the operator switches to it and adds a document, **Then** the
   document lands in that vault (isolated from others).

---

### User Story 3 — Switch the active vault, live (Priority: P1)

An operator chooses a vault as the **active** one. The console immediately operates on it — every
view (Dashboard, Documents, Query, Operations, Quarantine) reflects the newly active vault — with no
daemon restart and no full page reload. This is the operator's primary navigation between corpora.

**Why this priority**: with one daemon serving all vaults, switching is the everyday action that
makes multi-vault useful; it must be instant and consistent.

**Independent Test**: With documents in vaults A and B, switch from A to B; the Documents list now
shows B's documents and A's are gone from the view; switch back; A's documents return. No restart.

**Acceptance Scenarios**:

1. **Given** two or more vaults, **When** the operator activates a different vault, **Then** every
   view reflects the new vault immediately (no restart, no reload).
2. **Given** a switch, **When** the operator later returns, **Then** the last-active vault is
   remembered across sessions (the picker choice persists).
3. **Given** the active vault, **When** the operator opens any other view, **Then** that view's data
   is scoped to the active vault (vault isolation holds).

---

### User Story 4 — Rename a vault (Priority: P2)

An operator renames a vault. On confirm, the vault's name changes everywhere — the list, the picker,
and the active-vault marker — and queries under the new name return the same results the old name
would have (the data is untouched; only the name→vault mapping moves).

**Why this priority**: useful for organizing corpora, but the list + create + switch (P1) already
make the view functional.

**Independent Test**: Rename "scratch" to "drafts"; the list + picker show "drafts"; a query under
"drafts" returns the documents "scratch" held; `go-rag vault list` shows "drafts".

**Acceptance Scenarios**:

1. **Given** a vault, **When** the operator renames it to a valid unused name (confirmed), **Then**
   the list + picker update to the new name.
2. **Given** a rename, **When** the renamed vault was the active one, **Then** the active marker
   follows the new name.
3. **Given** a rename, **When** the operator queries under the new name, **Then** the results match
   what the old name held (data identity preserved).

---

### User Story 5 — Clear and delete a vault (Priority: P2)

An operator can **clear** a vault — empty every document/chunk/embedding it holds while keeping the
vault itself registered and immediately re-usable — or **delete** it entirely (clear + unregister,
gone from the list). Both are destructive and require explicit confirmation. The **default** vault
cannot be deleted (it is always present), but it can be cleared.

**Why this priority**: essential for retiring or resetting a corpus, but triage (list/create/switch,
P1) comes first.

**Independent Test**: Clear vault "test"; its document count drops to 0 but it remains listed;
delete vault "old"; it disappears from the list + picker; attempt to delete "default" — refused.

**Acceptance Scenarios**:

1. **Given** a vault, **When** the operator clears it (confirmed), **Then** its contents are gone
   (document count 0) but the vault remains listed + writable.
2. **Given** a non-default vault, **When** the operator deletes it (confirmed), **Then** it is gone
   from the list + picker.
3. **Given** the default vault, **When** the operator attempts to delete it, **Then** the action is
   refused (the default vault is always present).
4. **Given** a clear or delete, **When** initiated, **Then** it does not proceed without explicit
   confirmation.

---

### User Story 6 — Vault-aware, confirmed, shell-consistent (Priority: P2)

Every operation targets a named vault (no cross-vault ambiguity); every destructive action (create,
rename, clear, delete) requires explicit confirmation; the view renders inside the authenticated
shell, degrades gracefully on errors and the single-vault case, and introduces no Node/build chain.

**Why this priority**: a hard invariant (vault-aware; confirmed destructive ops; no Node; single
binary), proven once so the view is safe to ship.

**Independent Test**: Inspect every network call the view issues; attempt a destructive op without
confirming — it does not proceed; confirm no `package.json`/`node_modules` introduced.

**Acceptance Scenarios**:

1. **Given** any operation, **When** issued, **Then** it targets exactly one named vault.
2. **Given** a destructive action, **When** initiated, **Then** it does not proceed without explicit
   confirmation.
3. **Given** the repository, **When** checked, **Then** no Node or front-end build artifacts are
   introduced.
4. **Given** a store with only the default vault, **When** Vaults opens, **Then** a healthy state
   renders (one vault, marked active) — not an error.
5. **Given** a session that expires mid-view, **When** a fetch returns 401, **Then** the shell
   routes back to login (no crash, no silent failure).

---

### Edge Cases

- **Only the default vault exists** — the list shows one row, marked active; create is the path to
  more.
- **Creating a vault that already exists** — refused with a clear "already exists" reason.
- **Renaming to a name that already exists** — refused; the original is untouched.
- **Switching to a vault, then deleting it from another session** — the next view fetch reports it
  gone; the operator is routed back to a valid vault (the default).
- **Clearing the default vault** — allowed (it stays registered); deleting it — refused.
- **Invalid vault names** (bad characters, empty, reserved) — refused at creation/rename with the
  specific rule violated.
- **A vault with zero documents** — shown with count 0; fully functional (writable).
- **Mid-action session expiry** — graceful return to login.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The view MUST list every vault, each row showing the vault name and document count,
  with the console's active vault clearly marked.
- **FR-002**: The shell's vault picker MUST be populated from the vault list (every vault offered;
  the active one selected) — replacing the hardcoded single entry.
- **FR-003**: The operator MUST be able to create a new named vault, with the name validated (no
  invalid characters, no duplicate, no empty/reserved), via a confirmed action; the new vault MUST
  appear in the list and the picker.
- **FR-004**: The operator MUST be able to switch the active vault **live** — the console
  immediately operates on the chosen vault across every view, with no daemon restart and no full page
  reload. The choice MUST persist across sessions.
- **FR-005**: The operator MUST be able to rename a vault (confirmed); the list, the picker, and the
  active marker MUST update to the new name, and data identity MUST be preserved (the same corpus).
- **FR-006**: The operator MUST be able to clear a vault's contents (confirmed, destructive) — the
  vault's documents/chunks/embeddings are removed but the vault stays registered and writable.
- **FR-007**: The operator MUST be able to delete a non-default vault entirely (confirmed,
  destructive) — clear + unregister; the default vault MUST NOT be deletable.
- **FR-008**: Every destructive action (create, rename, clear, delete) MUST require explicit
  confirmation and MUST NOT proceed without it.
- **FR-009**: Every operation MUST target exactly one named vault (vault-aware; no cross-vault
  ambiguity).
- **FR-010**: The view MUST be gated by the existing spec 045 Bearer guard — no new authentication.
- **FR-011**: The view MUST ship inside the single binary via the existing embedded, vendored SPA —
  no Node / Vite / Tailwind build chain.
- **FR-012**: The view MUST render healthy states for the single-default-vault case and degrade
  gracefully on errors and mid-action session expiry — no silent failures.
- **FR-013**: The vault list shown MUST match `go-rag vault list` for the same store (cross-surface
  parity).

### Key Entities *(include if feature involves data)*

- **Vault**: a named corpus — a partition of the unified store — with a document count and a
  name→vault mapping.
- **Active Vault**: the vault the console's shell picker currently targets; the one every view
  operates on. Switching it is a live, client-side action (no restart).
- **Vault Lifecycle**: create (register a new empty vault), rename (move the name→vault mapping,
  data untouched), clear (drop a vault's contents, keep it registered), delete (clear + unregister).

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can open Vaults and see every vault with name + count + active marker
  within 1 second on loopback.
- **SC-002**: An operator can create a vault and have it appear in the list, the picker, and
  `go-rag vault list`.
- **SC-003**: An operator can switch the active vault and see every view reflect it in under 200 ms,
  with no daemon restart.
- **SC-004**: After a rename, queries under the new name return the same results the old name held —
  zero data drift.
- **SC-005**: A cleared vault shows count 0 but remains listed; a deleted non-default vault is gone;
  the default vault cannot be deleted.
- **SC-006**: No create / rename / clear / delete completes without explicit operator confirmation.
- **SC-007**: The vault list matches `go-rag vault list` — zero drift.
- **SC-008**: The view introduces zero new build tooling — a single `make build` still produces one
  binary with no Node chain.

---

## Assumptions

- The view reuses the spec 046 shell, transport, embed serving, 4-layer CSS, Alpine `goragApp` root,
  spec 045 Bearer auth, the no-cache static-asset headers, the sortable-table convention, and the
  quarantine view's confirmed-destructive-action dialog pattern — unchanged.
- The engine surface for management already exists (spec 052): list vaults, create, rename, clear,
  delete. The UI is a 4th adapter over those methods; switch is a client-side state change (the
  `X-Go-Rag-Vault` header every `/api/*` call already carries).
- The unified store (spec 052) means one daemon serves all vaults; switching does not restart the
  daemon.
- Single-operator use; no multi-user or RBAC (PRD N2).
- Desktop-first per `docs/style-guide.md`; mobile is not a target.
- The view refreshes on demand (on view-entry / after each mutation); live streaming is out of scope.

---

## Open Questions (to resolve in plan / tasks)

- **Per-vault identity depth** — the vault list shows name + document count; whether to also surface
  the embedding model / on-disk size (as `go-rag vault list` does) is a plan decision (it may require
  enriching the list accessor). Lean: ship name + count + active marker now (the management essentials);
  model/size can follow.
- **Create mechanic** — whether "create" registers an empty vault immediately or defers creation to
  first write. Lean: register immediately (so the vault appears + is switchable + writable), matching
  `go-rag vault create`.
- **Switch affordance** — a per-row "switch" action vs. clicking the row vs. the picker. Lean: an
  explicit per-row "Switch" action (clear + discoverable), plus the picker remains the quick-switch.
- **Stale-active-vault recovery** — if the active vault is deleted from another session, the next
  fetch fails; the plan decides whether to auto-fall-back to the default vault silently or surface a
  notice. Lean: auto-fall-back to default + a non-blocking notice.
