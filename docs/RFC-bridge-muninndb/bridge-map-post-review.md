# go-rag × MuninnDB Bridge — Post-Review Map

> **Author**: Stephen Eaton (consolidated with DAIV) · **Date**: 2026-06-30
> **Source**: MuninnDB maintainer review conversation (2026-06-30) × `bridge-muninn.md` RFC × both bridge backlogs
> **Status**: Living reference. Where this doc and `bridge-muninn.md` disagree, **this doc is current** until the RFC is revised.

---

## TL;DR

The maintainer **approved** the bridge and confirmed the complementary axis — *go-rag retrieves, MuninnDB remembers*. The scope is **smaller than the RFC planned**: six MuninnDB RPCs we assumed we'd have to request are already shipped. The one real upstream dependency (**UPSERT**, #556) had its prerequisite wiring bug (#560) **resolved upstream** — #556 is no longer prerequisite-blocked (still awaiting the maintainer's UPSERT implementation). **Does not gate us** — the bridge is buildable now on the RFC's local state-store + `FindByMetadata` fallback. **All go-rag-side work (BL-001..007) is fully unblocked and independent of the upstream timeline.**

---

## 1. Already shipped — do NOT rebuild

These RFC MB-items assumed missing RPCs. They exist. Re-implementing them is wasted work.

| RFC item | Need | Actual shipped RPC | Notes |
|---|---|---|---|
| **MB-001** | `GetEngram` by ID | **`Read`** (gRPC) | identical semantics |
| **MB-009** | `StrengthenEdge` | **`Link`** | identical signature: `source, target, rel_type, weight, vault` |
| **MB-012** | `WatchTriggers` stream | **`Subscribe`** | already streams `ActivationPush` events in real time |
| **MB-011** | `GetServerCapabilities` | **`Hello`** response | returns capabilities, server version, limits |
| **MB-006** (EnsureVault half) | `EnsureVault` | **auto-create on write** | writing to a new vault name creates it; idempotent — not needed |
| **MB-008** (soft-delete half) | `DeleteEngram` | **`Forget`** (`hard: false`) | soft delete |

**Net:** MB-001, MB-006 (EnsureVault), MB-008, MB-009, MB-011, MB-012 are moot. The bridge's `client.go` calls these by their real names (`Read`, `Link`, `Subscribe`, `Hello`, `Forget`), not the RFC's invented ones.

---

## 2. Open upstream — MuninnDB GitHub issues

The maintainer filed these for the real remaining gaps (2026-06-30). None are bridge-specific — UPSERT and BatchForget are general bulk-ingestion needs any workflow hits.

| Issue | RFC item | What | Ordering / status |
|---|---|---|---|
| **#560** | (cross-cutting) | `idempotent_id` wiring through the gRPC/REST engine paths (was MBP-only) | ✅ **SHIPPED upstream** — the gate below #556 is lifted |
| **#556** | MB-003 | **UPSERT** write mode on `Write`/`BatchWrite` | bridge's **#1 blocker**; prerequisite #560 ✅ cleared — now a single self-contained change, awaiting maintainer |
| #557 | ~MB-008 (batch half) | `BatchForget` on gRPC | open |
| #558 | MB-006 (ListVaults half) | `ListVaults` on gRPC | open |
| #559 | MB-010 | `AdjustConfidence` with contradiction signaling | open |

**Why #560 gated #556 (resolved upstream):** the field UPSERT keys on (`idempotency_key` / `idempotent_id`) existed in the MBP type system but was dead in the gRPC/REST engine. #560 wired it through — the prerequisite is cleared, so #556 can now be a single self-contained UPSERT change. Our #556 proto comment (**posted 2026-06-30**; v2 at `draft-issue-556-upsert-v2.md`, v1 at `draft-issue-556-upsert.md`) was written to match the existing MBP field so #560 + #556 converge on one consistent key.

---

## 3. Write invariants — bind the mapper and client

From the maintainer, verbatim in intent. These amend `bridge-muninn.md` § mapper and transport.

1. **Embeddings: `nil`.** Send `embedding: nil`; let MuninnDB re-embed on write. Unless go-rag's model dimensions **exactly** match MuninnDB's configured dimensions, passing mismatched vectors **silently breaks vector search**. (go-rag's mapper doesn't currently pass embeddings — keep it that way.)
2. **Stability: `30.0`** for document chunks. The default is tuned for conversational memory; reference material decays out of recall faster than expected.
3. **Hebbian weights — attenuated.**
   - `0.1–0.2` for **on-query co-retrieval** — RAG co-retrieval doesn't pass MuninnDB's cognitive filter, so the signal is weaker than co-activation earned inside MuninnDB.
   - `0.6–0.8` for **Obsidian wikilinks** — these are explicitly human-curated edges.
   - Let MuninnDB's own activation path strengthen the ones that turn out to be meaningful.
4. **Transport: gRPC only for v1.** Skip MBP. At ~2s async promotion latency there's no overhead that matters. When MBP is wanted later, the clean path is MuninnDB promoting the frame types to a **public package** so we import them instead of implementing the wire format blind.

---

## 4. Revised integration-pattern enablement

Re-maps RFC § Integration patterns against what's now shipped.

| Pattern | Status after review | Needs |
|---|---|---|
| Document promotion on ingest | ✅ **Unblocked** | default config (change-event sync) |
| **Obsidian backlinks → Hebbian edges** | ✅ **Unblocked — PRIORITY** | `Link` (shipped) + **BL-004** wikilink metadata (ours) — maintainer: "best idea in the RFC" |
| Context expansion (`ActivateWithRAG`) | ✅ **Unblocked** | `Read` (shipped) + **BL-001/002/003** (ours) |
| Semantic trigger → go-rag query (reverse/push) | ✅ **Unblocked** | `Subscribe` (shipped) |
| Knowledge-gap detection | ⚠️ Partial | `ListEngrams` needs #557/#558-class work |
| Contradiction detection | ❌ Blocked | `AdjustConfidence` → #559 |
| Batch UPSERT idempotency | ❌ Blocked (has fallback) | UPSERT → #556 only (#560 prerequisite cleared). **Fallback:** local `0x21` state store + `FindByMetadata` |
| Vault management | ⚠️ Partial | auto-create works; `ListVaults` → #558 |
| Bulk orphan cleanup | ⚠️ Partial | `Forget` works; `BatchForget` → #557 |

---

## 5. Two independent work streams

The bridge splits cleanly. Neither blocks the other.

**Stream A — go-rag side (our repo, unblocked now).** Backlog Phase 1, BL-001..007. All small, all go-rag-internal.
- `035-get-chunk-rpc` = **BL-001** `GetChunk` RPC (fetch a chunk by content-addressed ID). In-flight — spec exists, needs `/speckit-plan`.
- **BL-004 / BL-005 / BL-006** expose wikilinks / `section_heading` / `extraction_quality` in `Chunk.metadata`. These feed the prioritized wikilink → `Link` pipeline (BL-004 is the unblocker for the maintainer's "best idea").
- BL-002/003 (`GetChunkContext`, `BatchGetChunks`) build on BL-001.

**Stream B — MuninnDB coordination (their repo).** #556 (UPSERT) → lets the bridge drop its local `0x21` state store for correctness (it becomes a perf cache). #560 (the idempotent_id wiring prerequisite) ✅ landed upstream; blocked on #556 only. **Our move:** ✅ done — proto comment posted to #556 on 2026-06-30 (`draft-issue-556-upsert-v2.md`); awaiting maintainer response.

---

## 6. What is now stale in `bridge-muninn.md`

(Editig the RFC body is Stephen's call. This list is the diff to apply when he chooses to revise it.)

- **Capability-negotiation table** lists MB-001/009/011/012 as required RPCs — all shipped (call them `Read`/`Link`/`Hello`/`Subscribe`).
- **Mapper table** does not set `stability` → add `stability: 30.0`.
- **Mapper / `writeWikilinkEdges`** does not specify Hebbian weights → add `0.6–0.8` for wikilinks; on-query hook → add `0.1–0.2`.
- **Mapper** must explicitly send `embedding: nil` (state it as an invariant, not an omission).
- **Transport § MBP** → deprioritize; mark gRPC as the v1 transport and MBP as "deferred until MuninnDB publishes frame types as a public package."
- **`muninndb-bridge-backlog.md`** MB-001/006(EnsureVault)/008/009/011/012 → mark SHIPPED.
- **Open questions Q1–Q5** — Q3 (does UPSERT reset `access_count`?) and the three-case semantics were raised in the posted #556 comment (Q5 in the v2 draft).

---

## 7. Next actions (ordered)

> **Update 2026-07-02: Phase 1 is complete.** BL-001/002/003 (`GetChunk` / `GetChunkContext` / `BatchGetChunks`, specs 035/037/038), BL-004 (Wikilinks, spec 036), BL-005 (`section_depth`, spec 041), and BL-006 (`extraction_quality`, spec 042) all shipped to `main`. The original items below are retained for history.

1. ✅ **[ours] DONE** — Stream A Phase 1 complete (BL-001 through BL-006; specs 035/036/037/038/041/042).
2. **[ours → upstream]** ✅ DONE 2026-06-30 — #556 proto comment posted (v2: `draft-issue-556-upsert-v2.md`). [#560](https://github.com/scrypster/muninndb/issues/560) prerequisite ✅ cleared upstream — **awaiting maintainer response on #556 itself**.
3. ✅ **[ours] DONE** — BL-004/005/006 (the wikilink → `Link` pipeline inputs) all shipped. The Hebbian-edge wiring itself is bridge-consumer work (the `go-rag-muninn-bridge` repo), now unblocked by go-rag's Phase 1 surface.
4. **[RFC]** ✅ Applied 2026-07-02 — the §6 stale-list diff applied to `bridge-muninn.md` (mapper `stability: 30.0` + `embedding: nil` invariant, Hebbian weights 0.6–0.8 / 0.1–0.2, MBP deferral, Q3/Q5 upstream annotations). `muninndb-bridge-backlog.md` already reflects post-review SHIPPED status.

**Next:** ✅ v0.3.0 shipped 2026-07-02 (Phase 1 complete). Phase 2 (BL-010/011/013) code-grounded scoping puts all three at **"later"** (see the callout in `go-rag-bridge-backlog.md`) — revisit when real bridge traffic justifies. No v1.0.0 yet (0.3.x continues).

---

*Cross-references: `bridge-muninn.md` (original RFC), `go-rag-bridge-backlog.md`, `muninndb-bridge-backlog.md`, `draft-issue-556-upsert.md` (v1), `draft-issue-556-upsert-v2.md` (v2 — **posted to #556**). Maintainer intelligence stored in MuninnDB vault `default` (7 memories, 2026-06-30).*
