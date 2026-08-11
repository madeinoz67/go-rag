# Memory protocol — how a finding survives the session

Durable findings from building go-rag go to the **`go-rag` memory vault** — so nothing
important is lost when a session ends. This document is the bar for what belongs there.

> **Vault: `go-rag`, reached via the `muninndb-gorag` MCP server.** go-rag dev memory goes
> through the project-local `muninndb-gorag` connection (registered at local scope in
> `~/.claude.json` under `projects[…/go-rag].mcpServers`, per-project, not committed) — its
> key is scoped to `go-rag`, so its tools (`mcp__muninndb-gorag__*`) default to that vault
> with **no `vault` arg**. Do **not** use the global `muninndb` server for go-rag memory —
> that is the default/LifeOS vault, and its key is scoped there.

## The bar — what qualifies

A proposal must be **durable, non-obvious, and not recoverable elsewhere.**

Propose:

- A **decision and the reason it beat the alternative** — especially PRD/constitution-internal
  choices that will be re-litigated (e.g. "Pebble's prefix-partitioned keyspace over separate
  DBs because a single writer serializes ACK latency" — PRD §6.7; "async-after-ACK because
  <10ms commit beats any embedding latency" — Principle IV).
- A **measured result and the number** ("BM25+vector+rerank RRF over a 30k-chunk corpus
  returns sub-100ms p95" — when measured, not promised).
- An **honest negative** — a thing that did not work, with the evidence that killed it.
  These are the most valuable memories: they stop an idea being re-proposed.
- A **trap** — a thing that looks safe and is not ("the default `dbPath` is the global vault,
  so a bare `go-rag start`/`stop` targets the user's real daemon — always pass `--db-path
  <tmp>` when scripting" — CLAUDE.md; "Pebble's directory lock means only one writer; the
  daemon is the sole writer, every transport is an adapter" — spec 003).
- A **defect pattern**, not a single defect. "Three concurrency reviews found the same
  stale-cache-after-commit bug" is durable; the three instances are in their PRs.
- **Cross-transport parity gotchas** — anything that must stay true across CLI/MCP/REST/gRPC/
  UI (FR-002/003), and the place a drift was caught (e.g. a proto field that lagged the REST
  shape, a console view that read a different engine path than the CLI).
- **Keyspace/migration discipline** — every prefix allocation and every `ExpectedVersion` bump
  (see `docs/internals/keyspace-registry.md`), and why a migration is numbered/idempotent.

Do **not** propose:

- Progress narration ("ran `make test`, it passed").
- A restatement of a diff, commit, PR body, spec section, or PRD clause. Git, the specs, and
  the PRD have those.
- Anything you'd have to look up again anyway to trust it.
- Five variations of one idea. **One concept per memory, atomic.** If it needs "and", it is
  probably two memories.

The bar exists because **a noisy vault is worse than a small one.**

## How to write

Two paths, same vault (`go-rag`):

- **Preferred — the ledger + drain (automatic).** Append a proposal with
  `node "$CLAUDE_PROJECT_DIR/.claude/hooks/memory-propose.mjs"` (validates against the schema;
  refuses a bad batch rather than queueing it). It lands in `.claude/memory-proposals.jsonl`
  and `memory-drain.mjs` flushes it to the go-rag vault on `PreCompact` / `SessionEnd` / `Stop`
  (idempotent, concurrency-safe). This is the path that does not depend on remembering to
  remember — the whole reason the machinery exists.
- **Immediate — the `muninndb-gorag` MCP tools** (`mcp__muninndb-gorag__*`), for a finding
  that must land right now. No `vault` arg; the key is go-rag-scoped.

Either way:

- **Recall first.** Before adding a fact, recall what's related. If the new knowledge
  *corrects, sharpens, or supersedes* an existing memory, `evolve` that one — don't add a
  rival copy. Evolve supersedes and retires the old version; a second `remember` leaves a
  stale duplicate competing in recall.
- **One concept per memory, atomic.** Include `entities` (Pebble, RRF, Embedder, Engine,
  MuninnDB, spec-029, …) and `tags` (`go-rag`, the subsystem) so they recall cleanly.
- **Self-contained.** A memory that only makes sense next to the conversation that produced
  it is not a memory — it's a comment. Write it readable in a year.

## Privacy — this repo ships public

go-rag is released public (`github.com/madeinoz67/go-rag`, homebrew tap, GHCR image). Memories
are an internal artifact but the same discipline as the repo applies: **no secrets, no real
Ollama endpoints behind a real network, no personal data, no customer content** in any memory.
Measurements are welcome ("sub-100ms over 30k chunks"); a real credential or a private
`--rest-addr` of a live install is not.

## How it's wired

The ledger + drain loop lives in `.claude/hooks/`: `memory-schema.mjs` (the one proposal
shape), `memory-propose.mjs` (validates + appends), `memory-ledger.mjs` (paths + lock +
safe-touch), `memory-drain.mjs` (flushes ledger → vault on PreCompact/SessionEnd/Stop),
`memory-freshness.mjs` (SessionStart health), and `ledger-guard.mjs` (catches a bad append
in-session). Hook wiring is in `.claude/settings.json`; the connection env (`MUNINN_MCP_URL`,
`MUNINN_MCP_TOKEN`, `MUNINN_PROPOSAL_VAULT`) is in `.claude/settings.local.json` (gitignored),
and the per-vault key also lives at `~/.muninn/flush-keys/go-rag.token`. For an immediate
write, use the `muninndb-gorag` MCP tools directly.

Intentionally absent: a one-time ledger-repair step (go-rag is greenfield — no pre-schema
legacy) and a cross-surface code-drift guard (that's source-code drift, enforced by the
`.claude/agents/code-reviewer.md` resident reviewer, not memory).
