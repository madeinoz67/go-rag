# Security Policy

go-rag is **alpha** software maintained by a single person. This policy is written to
be honest about that rather than to promise more than it can deliver.

## Reporting a vulnerability

**Please report privately, not in a public issue.**

Use GitHub's private vulnerability reporting:
[**Report a vulnerability**](https://github.com/madeinoz67/go-rag/security/advisories/new).
It creates a private advisory only you and the maintainer can see, and it handles
coordinated disclosure and CVE requests if it gets that far.

If you can, include:

- The version or commit you tested (`go-rag version`)
- Which surface is affected — the CLI, MCP (`:7878`), REST (`:7879`), gRPC (`:7880`),
  or the web console (`:7881`)
- What an attacker gains, and what access they need to start (loopback only? a valid
  API key? an admin session?)
- The smallest reproduction you can manage

Reports are acknowledged and worked on a **best-effort** basis. No response time is
promised that cannot be honored. If something is being actively exploited, say so in
the report and it will be treated accordingly.

Please give a reasonable chance to ship a fix before disclosing publicly. Credit in
the advisory is gladly given — say how you want to be credited, or that you would
rather not be.

## Scope

**Supported version: the latest release.** go-rag is pre-1.0 and fixes are not
backported to older tags. If you are on an older version, the first step is
`go-rag upgrade`.

In scope — anything that lets someone read, write, or destroy documents or vault
data they should not reach:

- Authentication and authorization on any transport (`gorag_` API keys, the admin
  login, `gorags_` bearer sessions, the legacy `mcp.token` import path)
- Cross-vault access, or a request scoped to one vault acting outside it
- Privilege escalation, including a read-only key reaching write or admin operations
- Data corruption or loss (the unified store, schema migrations, vault clear/delete)
- The self-upgrade path — release resolution, SHA-256 checksum verification, or the
  atomic binary replacement
- Remote code execution, SSRF, or path traversal (document paths, watch directories,
  `--db-path`)
- Secrets leaking through logs, errors, API responses, or the web console

Out of scope:

- **Unauthenticated access to a fresh, not-yet-configured vault on loopback.** This
  is a deliberate onboarding choice so a first run works with zero ceremony; it
  closes as soon as the vault is initialized. Initialize auth on any install where
  it matters.
- Anything that requires an attacker to already have filesystem or OS-level access
  to the host — go-rag is local-first, single-operator software and does not defend
  against a compromised machine.
- Missing TLS or hardening headers. Every transport binds to loopback by default
  and ships no TLS. If you rebind to another interface or put the daemon behind a
  reverse proxy, transport security is yours to provide.
- Denial of service through sheer volume against an instance you control.
- Findings from automated scanners with no demonstrated impact.

## Known weaknesses

go-rag is alpha and has rough edges already known about. Some are tracked as public
issues; [`docs/internals/keyspace-registry.md`](../docs/internals/keyspace-registry.md)
documents the storage invariants that must hold, and
[`.specify/memory/constitution.md`](../.specify/memory/constitution.md) the core
engineering principles behind them. If you find something already tracked, a comment
on that issue is more useful than a new report — but if you think it is more severe
than it was rated, say so privately. Re-rating severity beats defending it.
