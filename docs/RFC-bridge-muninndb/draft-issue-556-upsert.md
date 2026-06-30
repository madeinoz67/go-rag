# DRAFT — Comment for MuninnDB issue #556 (UPSERT write mode)

> ⚠️ **DRAFT for Stephen's review. Do NOT post yet.** · Drafted 2026-06-30.
> **Context:** go-rag × MuninnDB bridge. UPSERT (#556) is our #1 upstream dependency. It is blocked behind #560 (the `idempotent_id` wiring bug). This comment proposes a proto shape so we can align **before** anyone writes implementation code, per the maintainer's suggested kickoff.
> **Reviewer checks before posting:** tone (peer, not demanding), proto shape correctness vs existing MBP types, whether to also volunteer help on #560, which specific open questions to press.

---

## Suggested comment (copy from here)

Thanks for filing this — UPSERT mode is the bridge's #1 upstream dependency, so happy to put some proto shape thoughts down before any implementation work, as you suggested.

### What we need it for

The go-rag bridge continuously promotes document chunks into MuninnDB as engrams. Chunks are content-addressed (`chunk_id = sha256(...)`), so re-promoting the same chunk after a daemon restart, a state-store wipe, or a re-ingest must **update the existing engram**, not create a duplicate. Today we carry a local Pebble state store for idempotency; a server-side UPSERT keyed on a client value would let us demote that store to a perf cache and make "no duplicate engrams" a server guarantee instead of a client-side best-effort.

### Proposed proto shape

The field already exists in the MBP types as `idempotent_id`, so the goal is to (a) wire that same field through the gRPC/REST engine (#560) and (b) add a write mode that keys on it. Aligning on one field name resolves both cleanly.

```protobuf
// Reuse the existing MBP field name wherever possible (#560 wires it through).
enum WriteMode {
  WRITE_MODE_UNSPECIFIED = 0;
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
  // (per-item mode is the more general alternative — see open question 2)
}

message WriteResponse {
  string  engram_id    = N;  // resolved id
  bool    created      = N;  // false ⇒ existing engram was updated
}
```

### Semantics we're assuming (please confirm / correct)

1. Uniqueness is **per vault**: one engram per `(vault, idempotency_key)`.
2. **UPSERT + existing key** → update content/tags/metadata; **preserve `access_count` and Hebbian weight** (accumulate, don't reset). This is our RFC open question Q3 — we currently assume accumulate.
3. **UPSERT + new key** → create.
4. **APPEND** (default) ignores `idempotency_key` → today's behaviour preserved, fully backwards-compatible.
5. Response tells the caller whether it was a create vs an update (we need this for metrics + the "updated vs promoted" split).

### Open questions for the proto

1. **Field name:** `idempotency_key` vs reusing the existing MBP `idempotent_id` verbatim? We'd prefer whatever matches the MBP type so #560 and #556 land on one identical field — happy to defer to your preference.
2. **Batch granularity:** request-level mode (whole batch one mode) vs per-item mode. The bridge always promotes a batch in a single mode, so request-level is sufficient **for us** — but per-item is strictly more general if other consumers would mix.
3. **`access_count` on UPSERT** (our Q3): reset or accumulate? We assume accumulate so re-promotion counts as an access for Hebbian weight.
4. **Empty key under UPSERT:** reject as `INVALID_ARGUMENT`, or treat as APPEND? We'd prefer reject (fail loud).

### Dependency note

Reads like #560 (`idempotent_id` in MBP types but not wired through gRPC/REST) should land first — and the field shape above is deliberately written to match the existing MBP `idempotent_id` so the two issues converge on a single key field rather than introducing a parallel one. Happy to help on either if useful.

### What this unblocks (beyond the bridge)

- Any bulk-ingestion workflow gets dedup for free (you noted UPSERT/BatchForget are general needs, not bridge-specific).
- Lets clients treat MuninnDB as the source of truth for "does this keyed thing exist yet" without a `FindByMetadata` round-trip per write.

Looking forward to getting aligned on the shape.

<!-- End of suggested comment -->

---

## Stephen's pre-post checklist

- [ ] Tone check — peer/collaborative, not presumptive about their internal proto conventions
- [ ] Confirm `WriteMode` enum + field numbering approach matches their style (they may prefer a `bool upsert` or a string mode)
- [ ] Decide whether to explicitly volunteer help on #560 (currently offered softly)
- [ ] Decide whether to press Q3 (`access_count` reset vs accumulate) hard or leave soft
- [ ] Verify the issue number endpoints / repo (`github.com/scrypster/muninndb`) before posting
- [ ] Remove this checklist + the DRAFT header before posting
