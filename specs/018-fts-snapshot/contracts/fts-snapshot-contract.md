# Contract — FTS Snapshot (Internal; Transparency + Escape) (H16)

> Phase 1 output. H16 is **internal and transparent** — it changes cold-start *latency*, never *results*,
> and adds **no transport/API surface** (no new CLI/REST/gRPC/MCP contract, no new request/response
> fields). So this "contract" is the internal invariant set + the test escape, not a public interface.

## 1. Transparency invariant (the core contract)

For any corpus state, a cold start that **loads the FTS snapshot** MUST produce keyword query results
**byte-identical** to a cold start that **rebuilds the FTS from chunks** (the pre-H16 path).

- Same chunk IDs returned, same BM25 order, same scores (within float epsilon).
- This holds across: snapshot present, snapshot absent (rebuilt), snapshot stale (rebuilt), snapshot
  corrupt (rebuilt), format-version mismatch (rebuilt).
- The snapshot is a pure performance optimization; enabling/disabling it never changes what a caller
  sees (FR-007/SC-001).

This is the single most important assertion and gets a dedicated test (load-snapshot vs forced-rebuild,
diff the hits).

## 2. Self-invalidation contract (never wrong)

The snapshot MUST never be the source of wrong results. It is used **only** when **both** gates pass:

1. **Marker gate**: `snapshot.Marker == Get(0x06/"marker")` — the chunk set has not changed since the
   snapshot was written (D4).
2. **Version gate**: `snapshot.Version == CurrentVersion` — the serialization format is current (D6).

Any failure (stale marker, version mismatch, corrupt/unparseable blob, absent snapshot) ⇒ fall back to
the rebuild from chunks (today's path) and overwrite the snapshot. The caller sees no error from a bad
snapshot — only the (correct) rebuilt results (FR-004/FR-005/FR-006).

## 3. Currency contract (stays current, efficiently)

- A chunk **add** (ingest) or **delete** (delete/watcher) in a session ⇒ the snapshot is refreshed on
  that session's `Close` (one write, not per-chunk) so the next cold start sees the change (FR-003).
- The marker is bumped **lazily once per session** (only the first mutation) and **persisted before the
  chunk is durable** — so a crash between mutation and Close leaves the marker ahead of the snapshot ⇒
  the next cold start rebuilds (FR-009 + crash safety).
- Bulk ingest does **not** write a snapshot per chunk (the perf cliff) — exactly one snapshot write per
  session, on Close (FR-009).

## 4. Test escape (internal, not user-facing)

To make the transparency invariant (§1) testable, an **internal** escape forces the rebuild path (bypass
the snapshot load) so a test can compare snapshot-loaded vs rebuilt results. This is:

- **Not** a CLI flag / REST field / config key (no public surface).
- An internal knob (e.g., an engine/test-only option or an env-var/flag consumed only by tests) that
  disables the snapshot load, forcing `LoadIndex` down the rebuild branch.

Rationale: transparency must be *verifiable*, and verifying it requires running both paths against the
same corpus and diffing. The escape exists for that test and nothing else.

## 5. Out-of-contract (explicitly not exposed)

- No new CLI command, flag, REST endpoint/field, gRPC RPC/field, or MCP tool/field. `status` does not
  report snapshot state by default (it's an internal cache; the operator doesn't need it — though plan
  MAY add a one-line "fts: snapshot/built" hint if useful, non-binding).
- No public "rebuild snapshot" command (Close + the self-invalidation cover every real need; a manual
  rebuild knob is YAGNI).
- No vector-map persistence (FTS-only, locked clarification 2026-06-23).
- No sharded/online-compacted snapshot (YAGNI at local scale).
