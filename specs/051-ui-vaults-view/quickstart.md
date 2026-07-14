# Quickstart — Vaults Management View

**Feature**: specs/051-ui-vaults-view | **Date**: 2026-07-14

## Prerequisites
Built binary, isolated store via `serve --db-path <tmp>` (the daemon serves all vaults from one db).

## Scenario 1 — list vaults + active marker (US1)
Start the daemon. Open the console → Vaults. The `default` vault lists (marked active — it matches
the shell's picker). The shell vault picker now offers every vault (not just `default`).

## Scenario 2 — create a vault (US2)
Create "archive" (confirmed). It appears in the list + the picker. `GET /api/vaults` includes it
(documents 0). Creating a duplicate or a bad name (e.g. "Bad Name!") is refused with a reason.

## Scenario 3 — switch the active vault, live (US3)
With documents in `default` and `archive`, switch the picker to `archive`. The Documents list now
shows `archive`'s documents; `default`'s are gone from the view. Switch back — `default`'s documents
return. No daemon restart; sub-200ms.

## Scenario 4 — rename (US4)
Rename "archive" to "drafts" (confirmed). The list + picker show "drafts"; a query under "drafts"
returns what "archive" held (data identity preserved).

## Scenario 5 — clear + delete (US5)
Clear "drafts" (confirmed) — its document count drops to 0, but it stays listed + writable. Delete
"drafts" (confirmed) — it is gone from the list + picker. Attempt to delete `default` — refused
(400).

## Scenario 6 — browser (Interceptor)
Login → Vaults sidebar item → list renders → create dialog → switch via picker → rename/clear/delete
confirm dialogs. No console errors; no Node artifacts. The Operations/Dashboard counts update to the
active vault after a switch.

## Teardown
Stop daemon, rm tmp.
