# Contract: UI Quarantine Transport

**Feature**: specs/053-quarantine-management | **Date**: 2026-07-13

## `GET /api/quarantine/list?vault=default`
Returns flagged chunks. **200**: `{chunks: [...], count: N}`. **401**: unauthorized. **200 empty**: `{chunks:[], count:0}` (clean vault — not an error).

## `POST /api/quarantine/{chunkID}/release?vault=default`
Releases a false positive. **204**: released. **404**: unknown chunk. **401**: unauthorized.
(Client-side: confirmation dialog required before sending.)

## `POST /api/quarantine/{chunkID}/reset?vault=default`
Forces a re-scan of one chunk. **204**: reset. **404**: unknown chunk. **401**: unauthorized.

## `POST /api/quarantine/rescan?vault=default`
Vault-wide rescan. **204**: scan triggered. **401**: unauthorized.

## Non-goals
No threshold tuning, no bulk-release, no automated remediation.
