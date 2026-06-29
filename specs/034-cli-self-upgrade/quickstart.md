# Quickstart — CLI Self-Upgrade Validation

**Feature**: 034-cli-self-upgrade · **Spec**: [spec.md](./spec.md) · **Contracts**: [contracts/cli-commands.md](./contracts/cli-commands.md) · **Data model**: [data-model.md](./data-model.md)

Runnable scenarios that prove the feature works end-to-end. These are validation/run steps —
implementation detail belongs in `tasks.md`.

> **Isolation rule (project gotcha):** the default `dbPath` is the global vault
> (`~/.go-rag/vaults/default`), and default daemon ports may collide with a live instance.
> Every scenario below uses a throwaway DB path and non-default ports so it never touches the
> user's real running daemon.

---

## Prerequisites

- A go-rag build with a real version injected: `make build` with `-ldflags` setting the version
  to a tag (e.g. `v1.2.0`). A `dev` build disables version checks.
- A controlled release feed for upgrade tests: a local file server (or a mock of
  `latestVersionFn`, as MuninnDB does) serving a newer `go-rag` asset + `checksums.txt`. Point
  the upgrade at it via the test hook / `--release-url` test flag.
- Two throwaway paths: `TMPDB=$(mktemp -d)` and a temp install dir for the binary.

---

## Scenario 1 — Check-only is non-destructive (P2)

**Proves**: `--check` reports versions without modifying the binary or sending identifying data.

```sh
HASH_BEFORE=$(sha256sum $(which go-rag) | awk '{print $1}')
go-rag upgrade --check --release-url "$MOCK_FEED" ; echo "exit=$?"
HASH_AFTER=$(sha256sum $(which go-rag) | awk '{print $1}')
```

**Expected**: prints current + latest + verdict; exit `1` when newer exists, `0` when current;
`$HASH_BEFORE == $HASH_AFTER` (binary byte-identical).

---

## Scenario 2 — Full in-place self-upgrade (P1)

**Proves**: the binary is atomically replaced and reports the new version; the prior binary is
retained.

```sh
go-rag upgrade --yes --release-url "$MOCK_FEED" --db-path "$TMPDB" \
  --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880
go-rag version          # reports the NEW tag
test -f "$(which go-rag).prev" && echo "prev retained ✓"
```

**Expected**: step lines all `✓`; `go-rag version` reports the new tag; `go-rag.prev` exists.

---

## Scenario 3 — Failed checksum leaves binary untouched (P1, safety)

**Proves**: integrity failure aborts before the swap (VR-1).

```sh
HASH_BEFORE=$(sha256sum $(which go-rag) | awk '{print $1}')
# feed a deliberately-wrong checksums.txt for the asset
go-rag upgrade --yes --release-url "$BAD_CHECKSUM_FEED" ; echo "exit=$?"
HASH_AFTER=$(sha256sum $(which go-rag) | awk '{print $1}')
```

**Expected**: aborts at `Verifying ✗`; exit ≠ 0; `$HASH_BEFORE == $HASH_AFTER`; no temp file left.

---

## Scenario 4 — Offline is clean (edge case)

**Proves**: no network ⇒ clear error, core ops unaffected.

```sh
go-rag upgrade --check --release-url "http://127.0.0.1:1/nope" ; echo "exit=$?"
# core ops still work offline on the isolated DB:
go-rag add "$TMPDB" some-doc.txt
go-rag query "$TMPDB" "term"
```

**Expected**: check reports unreachable, exit ≠ 0; the `add`/`query` still succeed.

---

## Scenario 5 — Rollback restores the prior binary (P3)

**Proves**: `--rollback` restores the previous version offline.

```sh
# after Scenario 2 (binary is now at NEW, prev at OLD)
go-rag upgrade --rollback
go-rag version          # reports the OLD tag again
```

**Expected**: `go-rag version` reports the prior tag; completes without network.

---

## Scenario 6 — Schema migrates safely on first open (P2, core coupling)

**Proves**: the 1:1 binary↔schema coupling — a newer binary auto-migrates an older store on open,
and a failed/interrupted migration never bricks the store.

**Setup**: create a store under an OLD schema version (e.g. version `0` — no schema key, or
inject `S=1` for a binary expecting `E=2`).

```sh
# binary expects schema E=2; store on disk is at S=1 (or 0)
go-rag query "$TMPDB_MIG" "term"   # first open triggers migration v1→v2
# verify the 0xFF|schema_ver key now reads 2
go-rag status "$TMPDB_MIG" | grep -i schema      # reports schema version 2
```

**Expected**: a one-line `migrating store schema …` notice; query returns correct results;
`go-rag status` reports schema version `2`.

**Idempotent / crash variant**: kill the binary (`kill -9`) mid-migration, then re-open:

```sh
go-rag query "$TMPDB_MIG" "term" & PID=$!; sleep 0.2; kill -9 $PID 2>/dev/null
go-rag query "$TMPDB_MIG" "term"   # re-open replays the un-advanced migration
```

**Expected**: second open completes; store readable; schema version ends at `2`; no data loss.

---

## Scenario 7 — Refuse to misread a newer-schema store (FR-015)

**Proves**: an older binary does not silently misread a forward-migrated store.

```sh
# store migrated to schema 2 by a newer binary; now open with an OLDER binary (expects ≤1)
OLD_GO_RAG query "$TMPDB_MIG" "term" ; echo "exit=$?"
```

**Expected**: clear error ("store schema 2 is newer than this binary supports"); exit ≠ 0; no
partial/incorrect results.

---

## What is NOT validated here

- Implementation code, migration function bodies, full test suites → `tasks.md`.
- Windows self-replace (unsupported in v1 — prints URL and exits).
- Live daemon handoff (out of scope; daemon is stop→swap→restart).
