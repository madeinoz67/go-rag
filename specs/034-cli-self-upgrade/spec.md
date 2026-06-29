# Feature Specification: CLI Self-Upgrade Mechanism

**Feature Branch**: `034-cli-self-upgrade` *(single-author repo — work commits to `main` per project convention; this slug identifies the spec, not a git branch)*

**Created**: 2026-06-29

**Status**: Draft

**Input**: User description: "I now want an upgrade mechanism like muninndb for the cli, make note for planner to research muninndb source repository when planning on how they achieve." Amended 2026-06-29 (clarify): "we need to account for pebble schema changes and how they're tracked as it's a 1:1; you can't update binary without a possible schema change; again look at how muninn handles this during the upgrade process."

---

## Clarifications

### Session 2026-06-29

- Q: Binary↔schema are 1:1 coupled — when does a vault's on-disk schema migrate
  relative to `go-rag upgrade`? → A: **Lazy migration-on-open.** `go-rag upgrade`
  swaps the binary only; the new binary auto-migrates each vault's Pebble store on
  first open, under the single-writer lock, before serving any operation. The
  existing `migrate` command (re-embedding onto a new model) is a different axis;
  schema migration is automatic on open, not a `migrate` subcommand.

> **Scope correction (this session):** on-disk Pebble schema migration was
> previously listed *out of scope*. That was wrong — the binary and the on-disk
> schema are 1:1 coupled, so **schema versioning + migration is now in scope** as
> part of the upgrade mechanism (FR-012 → FR-016). The Research Note for the
> Planner has been extended with the schema-migration questions MuninnDB's source
> must answer.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Self-upgrade the go-rag binary in place (Priority: P1)

A user who installed go-rag as a single binary wants to move to the newest release
without leaving the terminal, without a package manager, and without manually
finding the right download for their OS and architecture. They run one command;
go-rag discovers the appropriate release, fetches it, verifies it, and replaces
itself. The next `go-rag` invocation reports the new version. Their local vaults,
config, and indexes are untouched by the *upgrade step itself* — any schema work
happens lazily on first open (Story 2).

**Why this priority**: The product thesis (PRD §1) is that a local RAG database
MUST be as frictionless as `git init; git add; git commit`, delivered as ONE
dependency-free binary. Forcing users to hunt releases, pick the right asset,
checksum, and swap binaries by hand re-introduces exactly the friction the project
exists to remove. First-class self-upgrade closes the loop on the single-binary
promise end-to-end.

**Independent Test**: Run the upgrade command against a controlled release feed
pointing at a known newer binary; afterwards the on-disk binary is replaced and
`go-rag version` reports the new version. Deliverable on its own — this story
alone is a viable MVP.

**Acceptance Scenarios**:

1. **Given** the user is on version `v1.2.0` and `v1.3.0` is the latest stable
   release for their OS/arch, **When** they run `go-rag upgrade`, **Then** the CLI
   reports the current and target versions, downloads the correct asset, verifies
   it, replaces the binary, and exits successfully with `go-rag version` now
   reporting `v1.3.0`.
2. **Given** the user is already on the latest stable release, **When** they run
   `go-rag upgrade`, **Then** the CLI reports "already up to date" and makes no
   changes to the binary.
3. **Given** a download that is truncated or fails integrity verification,
   **When** the upgrade runs, **Then** the CLI aborts, leaves the current binary
   completely untouched, and emits a clear non-zero-exit error.
4. **Given** the upgrade replaced the binary and the new binary expects a newer
   schema, **When** the user next opens an existing vault, **Then** no manual
   migration step is required — migration is handled automatically per Story 2.

---

### User Story 2 - Schema auto-migrates safely on first open after upgrade (Priority: P2)

Because the binary and the on-disk Pebble schema are 1:1 coupled, a newer binary
may expect a newer schema than an existing vault has. Rather than force the user to
run a migration tool, the upgraded binary migrates the vault automatically the
first time it is opened — and it does so safely: the vault is snapshotted before
any destructive change, and if migration fails or is interrupted, the prior
version is restored so the vault is never left unreadable.

**Why this priority**: Migration-on-open is what makes a binary upgrade actually
usable against existing data — without it, the upgraded binary would misread or
refuse every pre-existing vault. It is P2 (not P1) because the upgrade itself
(Story 1) delivers value even before a vault is next opened; migration is the
safety-critical bridge from old schema to new.

**Independent Test**: Upgrade the binary, then open a vault created under the prior
schema; confirm the vault is migrated to the new schema, queries return correct
results, and a deliberately-broken migration leaves the vault readable at its prior
version via the snapshot.

**Acceptance Scenarios**:

1. **Given** a vault at schema `v1` and a binary expecting schema `v2`, **When**
   the user opens the vault (e.g., runs a query), **Then** the binary migrates it
   to `v2` automatically and serves correct results with no manual step.
2. **Given** a migration that fails midway (or is interrupted by a crash/kill),
   **When** the vault is next opened, **Then** the prior-version snapshot is
   restored and the vault remains readable at its pre-migration schema, with a
   clear error.
3. **Given** a vault already at the binary's expected schema, **When** it is
   opened, **Then** no migration runs (idempotent no-op) and open latency is
   unaffected.

---

### User Story 3 - Check for a newer version without applying it (Priority: P2)

A user (or a script, or CI) wants to know whether a newer go-rag exists WITHOUT
modifying anything — to decide whether to upgrade later, to pin a release note, or
to gate a workflow. They run a check-only command; it reports current vs latest and
exits without writing to disk.

**Why this priority**: Non-destructive visibility is what makes upgrade safe to
reason about in automation and in air-gapped-adjacent workflows. It is a strict
subset of P1's discovery step and costs little once P1 exists, but it is the
primary way cautious users will first interact with the feature.

**Independent Test**: Run the check-only command; it prints an "up to date" /
"newer version available" verdict, sends no identifying data, and leaves the
binary byte-identical (verified by hash before/after).

**Acceptance Scenarios**:

1. **Given** a newer stable release exists, **When** the user runs the check-only
   command, **Then** it prints the current version, the latest available version,
   and a one-line summary, and exits without modifying the binary.
2. **Given** the host has no network access, **When** the user runs the check-only
   command, **Then** it reports that the release source could not be reached and
   exits with a clear error, without implying the binary is up to date.

---

### User Story 4 - Safe rollback to the previous binary (Priority: P3)

A user upgraded, but the new release misbehaves on their corpus or workflow. They
want to return to the binary they had before the upgrade with one command — no
re-download, no manual file juggling — because go-rag kept the prior binary.

**Why this priority**: Rollback is what makes self-upgrade trustworthy enough to
actually use. It mirrors MuninnDB's safety posture (exact mechanism to be
confirmed by the planner from MuninnDB source). Valuable but secondary: an MVP
without it still delivers the core upgrade value. Note: with lazy migration-on-open
(Story 2), rolling the *binary* back before any vault has been re-opened is trivial
and clean; the subtler case (rolling back after a vault was migrated forward) is an
edge case below.

**Independent Test**: Upgrade from `v1.2.0` → `v1.3.0`, then run the rollback
command; `go-rag version` reports `v1.2.0` again and the restored binary behaves
as the pre-upgrade one did.

**Acceptance Scenarios**:

1. **Given** the user just upgraded from `v1.2.0` to `v1.3.0` and a backup of the
   prior binary was retained, **When** they run the rollback command, **Then** the
   prior binary is restored as the active binary and `go-rag version` reports
   `v1.2.0`.
2. **Given** no prior-binary backup exists (e.g., fresh install, never upgraded),
   **When** the user runs rollback, **Then** the CLI reports that there is nothing
   to roll back to and exits cleanly without erroring destructively.

---

### Edge Cases

- **Interrupted upgrade (network drop, SIGKILL mid-write, power loss):** the
  install MUST never be left in a corrupt or half-written state — the prior binary
  always remains runnable until the new one is fully verified and atomically
  swapped in.
- **Checksum / signature mismatch:** abort, keep the current binary, refuse to
  apply. No partial replacement under any failure path.
- **Running while the daemon is active:** replacing the binary file is safe (the
  running daemon keeps its mapped inode); the new binary takes effect on the next
  daemon start. The CLI MUST warn and advise a restart. Live daemon handoff is
  out of scope for v1.
- **Air-gapped / offline host:** `upgrade` and `--check` fail cleanly with a
  human-readable message; core operations (ingest/index/query) are entirely
  unaffected.
- **No matching release asset for the host GOOS/GOARCH:** clear "no prebuilt
  binary for this platform" message plus a pointer to build from source.
- **Permission denied writing to the install path:** clear error surfacing that
  the user needs write access to the binary's directory (system-wide installs may
  need elevation — transparently solving that is out of scope for v1).
- **Downgrade attempt:** by default refuse to "upgrade" to an older semantic
  version; allow only with an explicit `--allow-downgrade` flag.
- **Pre-releases:** excluded by default; included only with an explicit opt-in
  (e.g., `--pre`).
- **Trust model:** how the downloaded asset is authenticated (checksum-only vs
  signed) follows the MuninnDB approach and the project release trust model — to
  be confirmed by the planner (see Research Note).
- **Unversioned (pre-schema-version) vault opened by a version-aware binary:** the
  first release that introduces schema versioning MUST bootstrap — treat a vault
  with no schema-version record as "v0 / unversioned" and migrate it to v1. This
  is the chicken-and-egg first migration (introducing the version key is itself a
  schema change).
- **Migration interrupted by SIGKILL / crash:** on the next open, the binary MUST
  detect the incomplete migration (via the pre-migration snapshot/journal) and
  recover to a readable prior state rather than presenting a half-migrated store.
- **Migration-on-open vs the cold-start budget:** a one-time migration on first
  open after upgrade may exceed the constitution's normal <1s cold-start target.
  This is an expected, one-time cost and MUST be surfaced to the user (e.g.,
  "migrating vault from schema v1 → v2…") rather than appearing as a hang.
- **Rolling the binary BACK after a vault was migrated FORWARD:** an older binary
  may be unable to read a newer schema. The binary MUST detect this and emit a
  clear error (do not silently misread). Whether downgrade-read compatibility is
  supported is a planner decision grounded in MuninnDB (see Research Note).
- **Single-writer contention during migration:** migration-on-open requires the
  write lock; if another go-rag process (e.g., the daemon) already holds the
  vault's lock, migration MUST NOT proceed silently — it MUST detect the lock and
  error/advice accordingly.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `go-rag upgrade` command that, on
  invocation, resolves the latest release appropriate for the current operating
  system and architecture and replaces the running binary in place.
- **FR-002**: The system MUST verify the integrity of the downloaded binary
  (checksum; signature policy per planner research against MuninnDB + the project
  release trust model) BEFORE replacing the current one. A failed check MUST leave
  the current binary completely untouched.
- **FR-003**: The replacement MUST be atomic from the user's perspective — at no
  point is the installation left with a missing or partially-written binary. A
  crash at any point in the upgrade MUST NOT brick the install.
- **FR-004**: The system MUST preserve the current binary as a restorable backup
  before applying the new one, and MUST provide a documented rollback path (e.g.,
  a `--rollback` flag).
- **FR-005**: The system MUST support a check-only mode (`--check`) that reports
  the current versus latest version without modifying the binary or sending
  identifying data.
- **FR-006**: Upgrade and version-check MUST be strictly opt-in and explicit. The
  system MUST NOT perform automatic background checks or updates, and MUST NOT send
  telemetry or any identifying/user data to the release source. *(Constitution
  Principle I — Local-First.)*
- **FR-007**: The upgrade mechanism MUST be pure Go with `CGO_ENABLED=0`, with NO
  dependency on system package managers (brew / apt / scoop / choco) or any
  external runtime. The binary replaces itself. *(Constitution Principle III —
  Pure Go.)*
- **FR-008**: The system MUST fail safely and clearly when offline, when no
  matching asset exists for the host platform, or when the install path is not
  writable — always leaving the current binary intact and emitting a
  human-readable, non-zero-exit error.
- **FR-009**: The system MUST target stable releases only by default, MUST offer
  an explicit opt-in for pre-releases, and MUST refuse silent downgrades unless
  explicitly requested.
- **FR-010**: When an upgrade is performed while the go-rag daemon is running,
  the CLI MUST warn the user and document that a daemon restart is required for
  the new binary to take effect. Live daemon handoff is explicitly out of scope
  for v1.
- **FR-011** *(research-derived — see Research Note)*: The concrete upgrade AND
  schema-migration mechanism — release/version discovery, atomic self-replacement
  strategy per supported OS, checksum/signature trust model, rollback retention,
  schema-version tracking, migration step registry, and the v0→v1 bootstrap — MUST
  be derived by the planner from the MuninnDB source repository, then reconciled
  against this spec's requirements and the constitution before implementation.
- **FR-012**: Each vault MUST carry a schema-version record that the binary reads
  on open and compares against its own expected schema version. (Exact key
  location — a new reserved key-space prefix vs the existing `0x09` config KV — is
  confirmed by the planner; introducing the record is itself the v0→v1 bootstrap.)
- **FR-013**: On opening a vault whose schema version is older than expected, the
  new binary MUST automatically migrate the Pebble store to the expected version,
  under the single-writer lock, BEFORE serving any operation. *(Migration-on-open
  — per clarification session 2026-06-29.)*
- **FR-014**: Migration MUST be safe and recoverable: each migration step MUST be
  idempotent, and the schema-version key MUST be advanced (durable fsync) only
  AFTER the step succeeds — so a crash mid-migration leaves the version
  un-advanced and the same step replays cleanly on the next open. A re-open of an
  already-migrated store MUST be a no-op. (A Pebble Checkpoint backup is NOT the
  default mechanism; it is reserved as an optional escape hatch for a step that
  cannot be made idempotent.) *(Constitution Principle II — durability,
  SIGKILL-tolerance. Mechanism grounded in MuninnDB migrate.go — see research R8.)*
- **FR-015**: The binary MUST refuse to open — and MUST NOT silently misread — a
  vault whose schema version it cannot migrate (too old to have a migration path,
  or newer than the binary supports). It MUST emit a clear, actionable error.
- **FR-016**: Schema migration MUST remain distinct from the existing `migrate`
  command (which re-embeds the corpus onto a new embedding model — spec 028).
  Schema migration is automatic on open; it MUST NOT overload the `migrate`
  subcommand name.

### Key Entities *(include if feature involves data)*

- **Release Candidate**: a published go-rag release the user could move to —
  defined by its semantic version, the asset URL for a given OS/architecture, its
  integrity checksum (and signature, if the trust model requires), a
  stable-vs-prerelease flag, and human-readable release notes.
- **Installed Binary**: the go-rag binary currently on disk — its reported
  semantic version, its on-disk path, and the OS/arch it was built for.
- **Backup Record**: the immediately-previous binary retained across an upgrade —
  its path, its version, and the retention rule (how many prior versions are kept
  and for how long).
- **Schema Version**: the on-disk schema-version record held by each vault; the
  binary's expected schema version; and the ordered migration-step registry that
  maps each version transition (v(n) → v(n+1)) to the transform that realizes it.
- **Migration Snapshot**: the pre-migration vault backup (Pebble checkpoint/copy)
  retained across a migration so an interrupted or failed migration can be rolled
  back to a readable prior state.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user on any supported OS/architecture can move from an older
  release to the newest stable release with a single command — no manual download,
  no asset selection — in under 30 seconds on a typical residential connection.
- **SC-002**: An interrupted or failed upgrade (network drop, checksum mismatch,
  or SIGKILL mid-write) NEVER leaves the installation unusable — the prior binary
  always remains runnable and `go-rag version` still reports a valid version.
- **SC-003**: Check-only mode returns a correct "up to date" / "newer version
  available" verdict without modifying the binary (byte-identical before and
  after) and without sending any identifying data.
- **SC-004**: The upgrade mechanism introduces zero new runtime dependencies: a
  fresh `CGO_ENABLED=0` build still yields a single self-contained binary, and
  offline core operations (ingest / index / query) behave identically before and
  after an upgrade.
- **SC-005**: Rollback to the immediately previous binary completes in under 10
  seconds and restores the prior version's reported behavior.
- **SC-006**: After upgrade, opening any pre-existing vault automatically migrates
  it to the new schema with no manual step, and the migrated vault returns correct
  query results.
- **SC-007**: A failed or interrupted schema migration NEVER leaves a vault
  unreadable — the prior-version data is always recoverable via the migration
  snapshot, and re-opening an already-migrated vault is a no-op (idempotent).

---

## Research Note for Planner (Phase 0 — Constitution Check gate)

> **Directive from the feature request:** the `/speckit-plan` Phase 0 research
> MUST study the **MuninnDB source repository** to learn how MuninnDB achieves its
> self-upgrade AND schema-migration mechanisms, and derive go-rag's concrete approach from it. This
> spec intentionally specifies the WHAT and the WHY (requirements, safety,
> local-first constraints, migration-on-open trigger) and defers the HOW to that research.

The planner MUST answer, grounded in MuninnDB's source plus the go-rag codebase:

1. **Version / release discovery** — How does MuninnDB discover the latest
   available version? What release source and feed shape does it use, and what is
   the equivalent for go-rag (releases at `github.com/madeinoz67/go-rag`)? How is
   the correct asset selected per GOOS/GOARCH?
2. **Atomic self-replacement** — Exactly how does MuninnDB replace a running
   binary atomically on each supported platform (Unix rename-over-executable vs.
   Windows move-after-exit)? Which pure-Go approach does it use, and what are the
   edge cases per OS?
3. **Integrity / trust model** — Does MuninnDB verify checksums only, or signed
   releases? What is the right trust model for go-rag given its air-gapped,
   local-first ethos and content-addressed identity principle?
4. **Rollback / safety** — How does MuninnDB retain and restore the prior
   binary? Retention policy, naming, and the restore path.

**Schema migration (added this session):**
5. **Schema-version tracking** — How does MuninnDB store and discover the on-disk
   schema version on open? For go-rag: should the version record live in a new
   reserved key-space prefix or in the existing `0x09` config KV (PRD §6.7)? Note
   that introducing this record is itself a schema change requiring the v0→v1
   bootstrap.
6. **Migration step registry** — How does MuninnDB represent and apply ordered,
   version-to-version migration steps? How does it handle multi-version jumps
   (skipping intermediate versions)?
7. **The v0 → v1 bootstrap** — How does MuninnDB handle a store that predates
   schema versioning (the chicken-and-egg first migration)? This is go-rag's
   exact situation on the release that introduces schema versioning.
8. **Migration safety & recovery** — How does MuninnDB snapshot a store before a
   destructive migration, and how does it recover from a SIGKILL/crash mid-write?
   Snapshot retention policy and restore path.
9. **Downgrade compatibility** — Does MuninnDB support running an older binary
   against a store already migrated to a newer schema? What is the right policy
   for go-rag, and how does it interact with `--allow-downgrade` (FR-009)?

**go-rag integration points:**
10. **Integration surface** — Confirm the existing `go-rag version` command
   (`internal/cli/commands.go`) and the daemon lifecycle (`internal/daemon`,
   spec 003) so the new `go-rag upgrade` command, the `--check` mode, and the
   daemon-restart warning integrate cleanly without disrupting the multi-transport
   server.

**In scope (corrected this session):** on-disk Pebble schema versioning and
migration IS part of this feature — migration-on-open (FR-013). The binary and the
schema are 1:1 coupled; an upgrade must account for both.

**Still out of scope for v1:** live daemon handoff during upgrade; transparent
handling of system-wide installs needing elevated privileges; automatic background
update/check. Re-embedding onto a new embedding model remains the existing,
separate `migrate` command and is unchanged by this feature.

---

## Assumptions

- Releases are published as prebuilt static binaries for the supported
  GOOS/GOARCH matrix, hosted on the project release source (GitHub Releases at
  `github.com/madeinoz67/go-rag`). Exact asset-naming and feed format to be
  confirmed by the planner.
- The user has write permission to the directory containing the go-rag binary
  (the normal case for single-user installs). System-wide installs requiring
  elevated privileges are surfaced via a clear error in v1, not solved
  transparently.
- Outbound network access is required ONLY when the user explicitly invokes
  `upgrade` or `--check`. Air-gapped operation is the default and is unaffected by
  this feature.
- Semantic versioning is the versioning scheme, consistent with the `cliff.toml`
  changelog generation referenced in the constitution.
- The concrete binary-upgrade AND schema-migration mechanisms follow MuninnDB's
  proven approach; the planner confirms specifics from MuninnDB source and
  reconciles against this spec and the constitution (Principles I, II, III) before
  any implementation.
- **Migration safety default (not separately clarified):** destructive schema
  migrations are preceded by an automatic vault snapshot with auto-rollback on
  failure (FR-014), defaulting to the safe choice the constitution's durability
  standard (Principle II) implies. Override/review is available at planning.
- **Schema-version key location:** a sensible default exists (new reserved prefix
  or the `0x09` config KV); the planner confirms the best fit from MuninnDB and
  PRD §6.7. No clarification blocks this.
- **One-time cold-start exception:** a migration on first open after upgrade is an
  expected, one-time cost that may exceed the normal <1s cold-start budget; it is
  surfaced to the user rather than treated as a regression.
