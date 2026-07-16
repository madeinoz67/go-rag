# Research — Settings: API Keys (Slice 2a, spec 057)

> Phase 0. Grounds the auth-store surface so the build is a UI adapter over
> `s.store` (no engine/storage change) and nails the secret-once safety property.

## R1 — Create returns the secret ONCE, as a display string

**Decision: the UI surfaces `CreateAPIKey`'s first return value (`display`) in the
create response ONLY; it is never logged, never stored, never re-displayed.**

**Rationale (grounded):** `auth.CreateAPIKey(s, label, mode, expiresAt *time.Time)
(display string, _ APIKey, _ error)` returns `display = gorag_<id8>.<secret>` — the
full key the operator copies — plus the `APIKey` struct (which carries the
SHA-256[:16] `StorageHash`, NOT the raw secret). The raw secret is never persisted.
The UI handler returns `{...metadata..., secret: display}` in the create response
and nothing else ever carries it. FR-003 is satisfied structurally.

## R2 — List shape: metadata only; revoked keys visible as enabled=false

**Decision: the list DTO is `{id, label, mode, created_at, expires_at, enabled}`;
`StorageHash` is internal and never exposed; revoked keys appear with
`enabled=false` (rendered distinctly), preserving the audit trail.**

**Rationale (grounded):** `ListAPIKeys(s) → []APIKey` returns every stored key; the
`APIKey` struct has no raw secret. `RevokeAPIKey` sets `Enabled=false` (it does not
delete the record), so revoked keys remain in the list — the UI renders them as
revoked rather than hiding them (audit visibility), satisfying "no longer active."

## R3 — Revoke = disable + reject; unknown id → 404

**Decision: `DELETE` calls `RevokeAPIKey(s.store, id)`; success ⇒ 204;
`ErrUnknownAPIKey` ⇒ 404. Revocation is immediate + irreversible (re-enable is not
exposed).**

**Rationale (grounded):** `RevokeAPIKey(s, id) error` flips `Enabled=false`;
`ValidateAPIKey` then rejects the bearer (the Enabled check). FR-004.

## R4 — Mode validation + label required

**Decision: the handler validates `label` (non-empty) + `mode ∈ {read, write,
admin}` before calling `CreateAPIKey`; `CreateAPIKey` itself re-validates mode
(`validMode`). Invalid ⇒ 400, no key created.**

**Rationale (grounded):** `CreateAPIKey` returns an error on invalid mode
(`"invalid mode %q (want read|write|admin)"`). The UI mirrors the CLI's
label-required + mode-enum contract.

## R5 — Direct adapter over s.store (no engine/storage change)

**Decision: `handleAPIKeysList/Create/Revoke` call `auth.ListAPIKeys`/
`CreateAPIKey`/`RevokeAPIKey(s.store, …)` directly. The UI Server already holds
`s.store *auth.Store` (built from `eng.DB()`).**

**Rationale (grounded):** the store is the same surface the `go-rag auth` CLI uses.
This is a 5th adapter over the auth surface, mirroring how 050/051 adapted engine
writes. No engine/migrate/storage change (the auth store shipped in spec 045).

## R6 — Expiry on create is deferred (Slice 2a ships non-expiring creates)

**Decision: the create dialog takes `label + mode` only (non-expiring keys);
`expires_at` is still DISPLAYED in the list (for keys created with `--expires` via
the CLI). Optional expiry-on-create is a later nicety.**

**Rationale:** keeps the MVP create dialog minimal; `CreateAPIKey` accepts
`expiresAt *time.Time` (nil = non-expiring), so adding expiry later is one field.

## R7 — Constitution compliance (pre-check)

- I (local-first): write surface, but loopback-only + admin-gated; **no egress**;
  the secret transits loopback only (no TLS on loopback, spec 007). ✓
- III (pure Go): vendored SPA, no Node build. ✓
- IV (async-after-ACK): about INGEST; auth writes are synchronous + small (not the
  <10ms ingest-ACK budget). N/A. ✓
- V (extension by interface): UI adapter over the existing `auth.Store`. ✓
- Storage discipline: **no on-disk layout change** — the auth key prefix
  (`PrefixAuthAPIKey`) shipped in spec 045; this slice only reads/writes existing
  records via the store. ✓
- The secret-once + hash-only-storage guarantees are spec 045 invariants; the UI
  preserves them (FR-003).
