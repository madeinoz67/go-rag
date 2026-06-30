# DRAFT v2 — Comment for MuninnDB issue #556 (UPSERT write mode)

> ⚠️ **DRAFT for Stephen's review. Do NOT post yet.** · v2 drafted 2026-06-30.
> **Supersedes v1** (`draft-issue-556-upsert.md`) for posting — v1 retained alongside so you can diff.
> **Provenance:** synthesized by a judge-panel workflow (3 drafts × 2 judges × synthesis × adversarial verify) applying the 7 RedTeam edits. Spine: Draft 1 (surgical); grafted: Draft 2's "transport replay, not a cognitive access" framing + explicit retraction of "reuse resolves both"; Draft 3's semantics-before-proto ordering. Verify agent: **PASS** (3/3 critical assumptions resolved in-text, 7/7 edits applied, 0 new contradictions, 0 regressed strengths).
> **Diff vs v1:** `diff docs/RFC-bridge-muninndb/draft-issue-556-upsert.md docs/RFC-bridge-muninndb/draft-issue-556-upsert-v2.md`

---

## Suggested comment (copy from here)

Thanks for filing this — UPSERT mode is the bridge's #1 upstream dependency, so happy to put some proto shape thoughts down before any implementation work, as you suggested. Working backwards from the semantics we need (and a couple of threat-model concerns we hit while thinking it through) into the proto shape, rather than the other way round.

### What we need it for

The go-rag bridge continuously promotes document chunks into MuninnDB as engrams. Chunks are content-addressed (`chunk_id = sha256(...)`), so re-promoting the same chunk after a daemon restart, a state-store wipe, or a re-ingest must **update the existing engram**, not create a duplicate. Today we carry a local Pebble state store for idempotency; a server-side UPSERT keyed on a client value would let us demote that store to a perf cache and make "no duplicate engrams" a server-enforced dedup against the same key string instead of a client-side best-effort.

One framing note before the rest: a chunk re-promoted byte-for-byte across a restart is **transport replay, not a cognitive access**. It is not a human or agent re-encountering the memory; it is the bridge re-sending bytes it already sent. MuninnDB's product *is* the Hebbian/Ebbinghaus/Bayesian learning layer, so the UPSERT semantics have to be careful not to forge learning signals from replays. That distinction drives most of the choices below — in particular the three-case contract that follows.

### Semantics (hard requirements, please confirm / correct)

1. Uniqueness is **per vault**: one engram per `(vault, idempotency_key)` — the correct blast-radius boundary (reframed per the threat model below).
2. **UPSERT + new key** → create a **fresh** engram with no inherited cognitive state — no inherited `access_count`, no inherited Hebbian weight, no inherited decay clock.
3. **UPSERT + existing key, identical content** → **no-op**: do **not** touch `access_count`, do **not** bump Hebbian weight, do **not** refresh the Ebbinghaus decay clock. Re-promotion is transport replay, not a cognitive access — accumulating here would forge the Hebbian signal and inflate weight by daemon-restart count.
4. **UPSERT + existing key, changed content** → open question (see Q5), but our lean is **reset / relearn**: changed content is a new memory and should not inherit `access_count`, the Ebbinghaus decay clock, or the entity / co-occurrence / `Link` edges. Treating it as an update-in-place would corrupt recall ranking.
5. **APPEND** (default) ignores `idempotency_key` → today's behaviour preserved, fully backwards-compatible at the wire level.
6. Response reports create vs update-changed vs update-identical (we need the three-way split for metrics + the no-op in case 3).

Net summary: **UPSERT is a storage-level identity operation, never a cognitive reinforcement.** Learning signals come from real access, contradiction, supersession — not from re-sent bytes.

We're stating (3) as a hard requirement rather than a soft "open Q3" because the obvious-but-wrong default is "UPSERT preserves cognitive state and accumulates" — and that default forges the Hebbian signal MuninnDB exists to measure.

### Threat model & atomicity (worth pinning down before implementation)

A few hard things this shape does **not** solve on its own, and that I think are worth naming up front so we don't ship a guarantee the storage layer can't back:

- **Identity-by-key-STRING is not semantic dedup.** UPSERT collapses on the exact key the caller supplies — two different writers who produce the same key string (hash collision, squatting, or independent tools that happen to use the same scheme) land on the same engram. Per-vault scoping is **namespacing, not authorization**: a vault is an unauthenticated caller-supplied namespace, not a capability. So the no-duplicate guarantee holds only for a **trusted single-writer**. The threats this leaves open are **key squatting**, **replay**, **cross-writer collision**, and **Hebbian-inflation-via-re-promotion** (case 3 above already closes the last for a single writer, but it reopens if a malicious co-writer can drive case-4 resets). For any multi-writer or shared-vault deployment I'd suggest writer-namespaced keys (e.g. `(vault, writer_id, key)`) or an ETag / version precondition on update. We are explicitly *not* asking MuninnDB to solve multi-writer authorization for v1 — we're asking that the docstring state the single-writer scope plainly. On phrasing: I'd softly suggest "server-enforced dedup against the same key string" rather than the broader "server-enforced guarantee," so the threat surface is visible.
- **Pebble has no native unique constraint or secondary index.** Enforcing "one engram per `(vault, key)`" therefore means a maintained forward index (`(vault, idempotency_key) → engram_id`) plus a read-before-write inside the batch — roughly 2x write amplification and a TOCTOU window between the index read and the write unless both land in the same Pebble batch. Could you say how you want uniqueness enforced: single-writer serialization, an explicit CAS, or a forward-index check folded inside the Pebble write batch? I don't have a strong opinion, I just want the invariant to have an actual primitive behind it before we lean on it — it determines whether concurrent UPSERTs to the same key are safe at the storage layer or whether the bridge needs to keep serializing its own writes regardless.

### Open questions for the proto

1. **Field name & durability:** is the existing MBP `idempotency_key` a durable per-`(vault, engram)` content-key index, or a request-scope dedup receipt? If durable, #560 and #556 converge on one field; if it is a request-scope receipt, UPSERT needs a genuinely new durable field and the two issues do *not* collapse — they just share an enum. **I'm retracting any earlier "reuse resolves both #560 and #556" claim until this is confirmed.** Picking one name and sticking to it — `idempotency_key` here — until you tell us what the existing MBP field actually is.
2. **Atomicity primitive:** single-writer serialization vs CAS vs forward-index-in-batch (see the atomicity paragraph above).
3. **Empty key under UPSERT:** reject as `INVALID_ARGUMENT`, or treat as APPEND? We'd prefer reject (fail loud).
4. **Key ownership & collision policy:** who owns a key, and what happens when two independent writers collide on the same `(vault, key)` — last-writer-wins, reject, or writer-namespaced isolation? (Tied to the threat model.)
5. **Changed-content behaviour (our Q3, promoted to a hard leaning):** reset / relearn vs update-in-place — specifically what happens to `access_count`, the Ebbinghaus decay clock, and the entity / co-occurrence / `Link` edges. Our lean is reset (case 4 above); carrying forward corrupts recall ranking.
6. **`engram_id` stability under UPSERT:** keep the existing id, or reissue on update? Affects every downstream caller that cached the id — and is in slight tension with the reset-on-change lean in case 4 (a reset engram with a stable id is a slightly odd object).
7. **UPSERT vs `evolve` / `contradicts` / `supersedes`:** when does UPSERT update in place vs trigger an `evolve` (preserving history)? Our default would be: identical content = no-op, changed content = `evolve`-style supersede rather than a silent overwrite — if the maintainer's mental model is "UPSERT-changed == evolve," the cognitive-state preservation falls out naturally and we'd update our lean. This is the question most worth resolving in design.
8. **BatchWrite partial-failure semantics:** with 50 items in one batch, what are the per-item outcomes if item 42 of 50 collides or fails? A single aggregate response makes item-42 failure unreportable and the no-duplicate invariant unrecoverable on partial failure, so per-item results would help.
9. **Migration / backfill:** what happens to pre-existing engrams that have no `idempotency_key` — are they addressable by UPSERT at all, or do they sit outside the keyed space until backfilled?
10. **Batch granularity:** request-level mode (whole batch one mode) vs per-item mode. The bridge always promotes a batch in a single mode, so request-level is sufficient **for us** — but per-item is strictly more general if other consumers would mix.

### Proposed proto shape (a consequence of the above, not the headline)

Assuming Q1 lands on a durable key field, the shape falls out. The goal is to (a) wire the existing MBP `idempotency_key` field through the gRPC/REST engine (#560) and (b) add a write mode that keys on it — **if** that MBP field is already a durable per-`(vault, engram)` content-key index. If `idempotency_key` in MBP is a request-scope dedup receipt (short-TTL, meant to absorb retries within one request), then UPSERT needs a genuinely new durable field rather than a reuse, and #560 and #556 do not collapse to one field — happy to defer to your call on which it is. One field name throughout this comment either way: `idempotency_key`.

```protobuf
// Reuse the existing MBP field name wherever it is in fact a durable index (#560 wires it through).
enum WriteMode {
  WRITE_MODE_UNSPECIFIED = 0;  // defaults to APPEND — today's behaviour
  APPEND  = 1;  // current default — always create (ignores idempotency key)
  UPSERT  = 2;  // create-or-update keyed by idempotency_key, scoped per vault
}

message WriteRequest {
  // ...existing fields...
  WriteMode write_mode      = N;  // defaults to APPEND = today's behaviour
  string    idempotency_key = N;  // client-supplied; scoped to vault; ignored under APPEND
}

message BatchWriteRequest {
  // ...existing fields...
  WriteMode write_mode = N;  // request-level: whole batch shares one mode
  // (per-item mode is the more general alternative — see open question 10)
}

message WriteResponse {
  string  engram_id    = N;  // resolved id
  // 'created' alone cannot distinguish update-changed from update-identical,
  // and the no-op semantics in case 3 above need that distinction.
  enum Outcome {
    OUTCOME_UNSPECIFIED  = 0;
    CREATED              = 1;  // case 2: fresh engram, no inherited cognitive state
    UPDATED_CHANGED      = 2;  // case 4: content differed — see Q5 for reset semantics
    UPDATED_IDENTICAL    = 3;  // case 3: no-op — access_count and Hebbian weight untouched
  }
  Outcome outcome       = N;
}
```

`WriteMode` as an enum with `APPEND` as the explicit default (`UNSPECIFIED = 0`) is deliberate: it makes backwards compatibility a wire-level invariant. Existing callers who send no mode continue to get today's behaviour. We'd resist a bare `bool upsert` on those grounds, but defer to your proto conventions.

### Dependency note

Reads like #560 (`idempotency_key` in MBP types but not wired through gRPC/REST) should land first — and the field shape above is deliberately written to match the existing MBP `idempotency_key` **if** that field is genuinely a durable content-key index. If it's a request-scope dedup receipt instead, then #560 and #556 don't fully converge and UPSERT needs its own durable field — either way we'd like to land on a single key field rather than a parallel one. Happy to help on either if useful.

### Client-responsibilities note (so future readers don't get burned)

One thing the "server guarantee" framing can obscure: **key stability across the bridge's own version changes is the client's job, not the server's.** If we ever bump our SHA-256 scheme, swap the chunker, or switch from path-addressed to content-addressed keys, the new keys won't match the old ones and UPSERT will silently create clean duplicates of the same logical document — because from the server's view it *is* a new key string. The server-enforced dedup is against *the key string the client hands it* — nothing more, and "no duplicate engrams" is a guarantee about the same key string, not about the same logical document. Flagging this so anyone who later demotes their local store on the strength of this guarantee knows they still own key-scheme stability on their side.

### What this unblocks (beyond the bridge)

- Any bulk-ingestion workflow gets dedup for free (you noted UPSERT/BatchForget are general needs, not bridge-specific).
- Lets clients treat MuninnDB as the source of truth for "does this keyed thing exist yet" without a `FindByMetadata` round-trip per write — *provided* the atomicity question has a real answer.

Looking forward to getting aligned on the semantics first; the proto is the easy part once the three-case contract and the atomicity primitive are settled.

<!-- End of suggested comment -->

---

## What changed vs v1 — the 7 RedTeam edits

- **EDIT 1:** Removed 'preserve access_count and Hebbian weight (accumulate)'; restated as three explicit cases in Semantics — new key=CREATE fresh; identical content=NO-OP (do not touch access_count/Hebbian/decay clock); changed content=lean RESET/relearn, no inherited access_count/decay-clock/edges.
- **EDIT 2:** Added a dedicated Threat model paragraph: UPSERT is identity-by-key-STRING not semantic dedup; per-vault scoping is namespacing not authorization (vault = unauthenticated caller namespace, not capability); guarantee holds only for trusted single-writer; names key squatting, replay, cross-writer collision, Hebbian-inflation-via-re-promotion; proposes writer-namespaced keys or ETag/version precondition; softens phrasing to 'server-enforced dedup against the same key string'.
- **EDIT 3:** Atomicity paragraph states Pebble has no native unique constraint/secondary index, so uniqueness needs a maintained forward index + read-before-write inside the batch (~2x write amplification, TOCTOU window), and asks the maintainer explicitly: single-writer serialization, CAS, or forward-index check inside the write batch.
- **EDIT 4:** Open questions expanded to 10 items, adding key ownership/authorization (Q4), collision policy between independent writers (Q4), engram_id stability under UPSERT (Q6), Ebbinghaus decay clock + entity/co-occurrence/Link edge behaviour on content change (Q5), UPSERT-vs-evolve/contradicts/supersedes interaction (Q7), BatchWrite partial-failure per-item semantics (Q8), and migration/backfill of pre-existing keyless engrams (Q9).
- **EDIT 5:** Resolved the naming self-contradiction by using ONE name (idempotency_key) throughout proto and prose; explicitly retracts the 'reuse resolves both #560 and #556' claim in Q1 and the proto intro, and states that if the existing MBP field is a request-scope dedup receipt (short-TTL), UPSERT needs a genuinely new durable field and the two issues do not collapse.
- **EDIT 6:** WriteResponse expanded with an Outcome enum (CREATED / UPDATED_CHANGED / UPDATED_IDENTICAL); comment in the proto explains that 'created' alone cannot distinguish update-changed from update-identical and that the no-op semantics in case 3 need the three-way split.
- **EDIT 7:** Added a dedicated Client-responsibilities note stating that key stability across the bridge's own version changes (SHA-256 scheme bump, chunker swap, path-vs-content addressing change) is the CLIENT's job, that such a bump silently creates clean duplicates of the same logical document, and that the server-enforced dedup is against the key string only — not the logical document.

---

## Stephen's pre-post checklist (v2)

- [ ] Tone check — still collaborative peer, not demanding (EDIT 2 softened "server-enforced guarantee" → "dedup against the same key string")
- [ ] Field name is single throughout (`idempotency_key`) — no `idempotent_id` contradiction remains (EDIT 5)
- [ ] The three-case semantics (new / identical-no-op / changed-reset) read the way you want them enforced (EDIT 1)
- [ ] Threat-model + atomicity paragraphs are worded the way you want to land them with the maintainer (EDITs 2, 3)
- [ ] Decide whether to press Q5 (changed-content reset) as a hard lean or soften it back to an open question
- [ ] Verify the issue number + repo (`github.com/scrypster/muninndb` #556) before posting
- [ ] Decide whether to also volunteer help on #560 explicitly
- [ ] Remove this checklist + the DRAFT v2 header before posting

## Verify attestation

Adversarial verify agent (`pass: true`):
- **A_accumulate** (21/32 agents, sev 5): ✅ resolved — three-case contract, "transport replay" framing, no-op on identical, lean reset on changed; v1 self-contradiction eliminated.
- **B_isolation** (8/32 agents, sev 5): ✅ resolved — threat-model paragraph; "namespacing, not authorization"; single-writer scope; four threats named.
- **C_idempotency_id** (4/32 agents): ✅ resolved — durability made conditional; "reuse resolves both" retracted; one name throughout.
- **Edits:** 7/7 applied, 0 missing. **New contradictions:** 0. **Strengths regressed:** 0.
