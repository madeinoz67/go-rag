# Feature Specification: Loopback Bind by Default (H13)

**Feature Branch**: `007-loopback-bind-default`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "look at next task in the backlog" — resolved to backlog item **H13** (`RAG_BOOK_AUDIT_BACKLOG.md`, Phase 1, P0, effort S). Source of truth for the problem detail: `RAG_BOOK_AUDIT.md` §1.7 (line 171), §1.8 (line 195), H13 row (line 240).

> **Scope note.** The audit and backlog refer to the bare `serve` command. The
> current daemon command is `start` (introduced by spec 003's multi-transport
> refactor) and serves three transports — MCP, REST, gRPC — each on its own
> listener. This spec applies the loopback-by-default contract to **every
> listener the daemon opens**, regardless of command name or how many transports
> are enabled.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Never exposed by accident (Priority: P1)

As the single user running go-rag on my own machine, when I start the daemon
with no flags and no saved configuration, it MUST listen only on loopback. My
document vault, my query traffic, and every transport MUST stay invisible to
every other device on my LAN and to the internet — without me having to
remember to configure anything.

**Why this priority**: go-rag's thesis is air-gapped-by-default, friction-free
local storage (constitution Principle I; audit §10.2). A default that binds to
all interfaces silently turns that promise into a lie and exposes the entire
document vault plus plaintext query traffic to the network. This is the
silent-killer the P0 rating points at, and it is the whole point of H13.

**Independent Test**: Start the daemon with defaults and inspect the network
listeners it opens. Every listener is bound to a loopback address; none is
reachable from another machine. This story alone is a shippable, valuable MVP.

**Acceptance Scenarios**:

1. **Given** a fresh start with no config file and no flags, **When** the daemon
   starts, **Then** every transport listener is bound to a loopback address and
   no listener exists on any non-loopback interface.
2. **Given** a previously-saved configuration file that still contains an
   all-interfaces or external bind address, **When** the daemon starts using that
   file, **Then** it still binds only to loopback (the unsafe saved address is
   not silently honored) unless external binding is explicitly authorized.
3. **Given** the daemon is running with default (loopback) binding, **When** a
   second machine on the same network attempts to connect to any transport,
   **Then** the connection cannot be established.

---

### User Story 2 - Deliberate external bind with informed consent (Priority: P2)

As a user who genuinely wants to reach go-rag from another device on my network
(for example, running go-rag on a home server and querying it from a laptop), I
can explicitly opt into non-loopback binding. When I do, the system makes the
security implications unmistakable — it tells me, loudly and at startup, that
the database and transports are now exposed beyond my machine, that traffic is
unencrypted, and that access control is entirely my responsibility.

**Why this priority**: Local-first does not mean local-only forever; a power
user serving a trusted home network is a legitimate use case (the project's own
BESS/Unraid/Home Assistant context is exactly this shape). But it must be an
explicit, eyes-open choice, never a default or a footgun.

**Independent Test**: Start the daemon with an external bind address plus the
explicit opt-in. The daemon starts on the external address and emits the
exposure warning exactly once; without the opt-in, the same address is rejected.

**Acceptance Scenarios**:

1. **Given** the user has configured an external (non-loopback) bind address and
   provided the explicit opt-in, **When** the daemon starts, **Then** it binds
   to that external address and prints a prominent, one-time exposure warning.
2. **Given** the same external address configured WITHOUT the opt-in, **When**
   the daemon starts, **Then** it refuses to start and prints a clear error
   naming the offending address and how to authorize it.
3. **Given** the opt-in is present but every configured address is loopback,
   **When** the daemon starts, **Then** no exposure warning is emitted (nothing
   is actually exposed).

---

### User Story 3 - Fail closed on unsafe configuration (Priority: P3)

As a user carrying an old or hand-edited configuration that asks for an
all-interfaces or external bind, the daemon refuses to start with a single,
actionable error — it does not silently open a network listener, and it does not
start half-way with some transports exposed and others not.

**Why this priority**: This is the defense-in-depth backstop for Story 1. A
misconfigured or stale config file is the exact vector the audit calls out
(loading a saved `config.json` binds to `0.0.0.0`). Fail-closed turns that
silent footgun into a loud, safe stop.

**Independent Test**: Attempt to start with a config specifying an external
address and no opt-in. The daemon exits within ~1 second with one clear error
and creates zero network listeners.

**Acceptance Scenarios**:

1. **Given** a configuration that requests a non-loopback bind for any enabled
   transport and no explicit opt-in, **When** the daemon starts, **Then** it
   exits before opening any listener, with an error that names the offending
   address and the opt-in mechanism.
2. **Given** a configuration with multiple transports where only some are
   external and there is no opt-in, **When** the daemon starts, **Then** it
   rejects the whole startup (no partial exposure) and lists every offending
   address.

---

### Edge Cases

- Bind target is the wildcard all-interfaces address (IPv4 or IPv6): treated as
  external → requires opt-in.
- Bind target is a specific LAN or routable IP: external → requires opt-in.
- Bind target is any address in the IPv4 loopback range (`127.0.0.0/8`), the
  IPv6 loopback (`::1`), or the `localhost` hostname: loopback → allowed by
  default, no warning.
- A transport is disabled entirely (no listener configured for it): the
  external-bind check is not triggered for that transport.
- The user passes the opt-in but every address is loopback: start normally, no
  warning (opt-in is authorization, not a request to bind externally).
- The daemon runs inside a container with bridged networking: loopback semantics
  are the operator's responsibility — out of scope for v1 (see Assumptions).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: By default, every network transport the daemon opens MUST bind
  only to loopback addresses, regardless of whether the bind settings originate
  from built-in defaults, a loaded configuration file, or command-line flags.
- **FR-002**: The system MUST classify any bind target outside the loopback
  family — the all-interfaces wildcard, a specific LAN/public IP, etc. — as an
  external exposure.
- **FR-003**: When an external bind is requested for any enabled transport
  WITHOUT explicit user authorization, the daemon MUST refuse to start. It MUST
  create no network listeners and MUST emit a single, actionable error naming
  every offending address and how to authorize external binding.
- **FR-004**: The system MUST provide an explicit opt-in mechanism that
  authorizes non-loopback binding for the transports the user has configured
  external addresses for.
- **FR-005**: When external binding is explicitly authorized, the daemon MUST
  emit a prominent, persistent warning at startup stating that the database and
  transports are exposed beyond the local machine, that traffic is unencrypted,
  and that access control is the user's responsibility.
- **FR-006**: Loopback classification MUST recognize the loopback address family
  — IPv4 `127.0.0.0/8`, IPv6 `::1`, and the `localhost` hostname — as loopback.
  Every other bind target is external.
- **FR-007**: The loopback-default behavior, the external-opt-in mechanism, and
  the no-TLS warning MUST be documented in user-facing documentation (README and
  command help).
- **FR-008**: A transport that is disabled (no listener) MUST NOT trigger the
  external-bind check.

### Key Entities *(include if feature involves data)*

- **Transport Listener**: a network listener the daemon opens for one of its
  transports. Each has a bind address that the system classifies as loopback or
  external.
- **External-Bind Authorization**: a single user-supplied opt-in that permits
  non-loopback binding for the current run, scoped to whatever the user has
  configured externally.
- **Configuration Source**: the origin of bind settings — built-in defaults, a
  loaded configuration file, or command-line flags. All three sources are subject
  to the same loopback-default / external-opt-in enforcement.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A default start-up with no flags and no configuration file binds
  exclusively to loopback — verifiable by a network-listing check showing zero
  listeners on any non-loopback interface.
- **SC-002**: A configuration containing an all-interfaces or external address,
  started without opt-in, fails to start within 1 second with a single clear
  error and creates zero network listeners.
- **SC-003**: The same external configuration, started with explicit opt-in,
  starts on the configured address and emits the exposure warning exactly once;
  remove the opt-in and it is rejected — the opt-in is the sole determinant.
- **SC-004**: 100% of supported transports honor the loopback-default /
  external-opt-in contract — no transport can bypass it or expose a listener the
  others would have blocked.
- **SC-005**: The exposure warning appears both in start-up output and in
  user-facing documentation, so no user can bind externally without having seen
  the risk.

## Assumptions

- **Loopback definition.** Loopback = IPv4 `127.0.0.0/8`, IPv6 `::1`, and the
  `localhost` hostname. Everything else (wildcard, LAN, public IP) is external.
  This is the industry-standard classification.
- **TLS is out of scope for v1** (constitution "out of scope"; backlog line 80 /
  audit line 289). External binding is therefore permitted via opt-in as
  **plaintext at the user's explicit risk**, with a prominent warning. This spec
  deliberately opens the door that a future TLS spec will lock down: once TLS
  exists, non-loopback binding should require it. That escalation is tracked in
  the backlog ("TLS … flips to P0 if anyone binds non-loopback — see H13").
- **One global opt-in** authorizes external binding for whichever transports the
  user configured externally; per-transport opt-in is not required (an
  implementation choice to confirm at plan time).
- **Threat model unchanged.** go-rag remains a single-user, single-writer local
  tool. H13 hardens against *accidental* exposure, not adversarial network
  attack — RBAC, mTLS, and rate-limiting remain separate backlog items.
- **Containerized deployments.** Inside a container with bridged networking,
  loopback semantics are the operator's responsibility; container networking is
  out of scope for v1.

## Out of Scope (v1)

- TLS / mTLS and certificate management (separate future spec; escalates to P0
  once external binding ships — this spec is the prerequisite).
- RBAC / multi-user authentication (P0 only when go-rag backs a shared
  multi-user server; the backlog names the vault as a namespace, not a security
  boundary).
- Rate limiting and structured audit logging (separate backlog items).
- Non-TCP transports.

## Constitution Alignment

This spec directly enforces **Principle I (Local-First, air-gapped by
construction — "no external surface")**. It does not touch Principles II–V. No
principle is weakened; the async-after-ACK write budget (Principle IV) is
unaffected because network-bind enforcement happens at boot, before any write.
