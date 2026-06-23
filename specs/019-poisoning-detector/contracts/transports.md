# Phase 1 — Interface Contracts: Retrieval Poisoning Defense (H04)

> go-rag exposes four transports over one `Engine` (Constitution V — cross-transport parity).
> This contract fixes how the poisoning verdict is surfaced and how quarantine is managed, so
> that a flagged chunk looks **identical** across CLI/REST/gRPC/MCP (SC-004). Reuses the spec-006
> flag-surfacing shape and the spec-014 opt-in filter bypass.

## 0. Shared verdict shape (all transports)

Every query hit carries:

| Field | Type | Meaning |
|-------|------|---------|
| `poisoning.level` | enum `clean \| suspicious \| quarantine \| released` | Verdict level. |
| `poisoning.score` | float 0..1 | Combined score. |
| `poisoning.signals` | `{repetition, stuffing, instruction}` | Per-signal contribution (transparency). |
| `poisoning.matched_phrases` | []string | Instruction-phrase hits (may be empty). |

A hit is returned **only** if its level is included by the request's quarantine policy.

## 1. Query — quarantine policy (opt-in to excluded chunks)

Default behavior (Q1=A): chunks with `level ∈ {suspicious, quarantine}` are **excluded**.
A request may opt back in.

| Transport | Opt-in flag |
|-----------|-------------|
| CLI | `go-rag query ... --include-quarantined` |
| REST | `GET /query` / `POST /query` with `include_quarantined: true` |
| gRPC | `QueryRequest.include_quarantined` (new bool field) |
| MCP | `query` tool param `include_quarantined` (bool, default false) |

**Parity rule**: a query with the flag unset MUST return byte-identical results to today for
any corpus whose chunks are all `clean` (no regression — SC-006).

## 2. Quarantine management (FR-006 / US2 — the management surface)

Three operations, exposed on all transports, honoring the standing quarantine-management
preference (list, see-why, release; never destroy).

### 2a. List quarantined / flagged chunks

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag poison list [--level suspicious\|quarantine\|all]` |
| REST | `GET /poison?q=...&level=...` |
| gRPC | `ListPoisoned(request)` RPC → stream/page of `{chunk_id, doc_id, level, score, signals, matched_phrases}` |
| MCP | `poison_list` tool |

Returns the verdict + per-signal breakdown + matched phrases per item (the "why was this
flagged" view). Backed by the `0x11` quarantine index (D3) — O(flagged), no full scan.

### 2b. Release a false positive (override — non-destructive)

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag poison release <chunk_id>` |
| REST | `POST /poison/{chunk_id}/release` |
| gRPC | `ReleaseChunk(chunk_id)` RPC |
| MCP | `poison_release` tool |

Sets `level = released` (D6). Chunk re-enters default retrieval. **Idempotent.** Original
score retained in `signals`. Reversible via `reset` (below).

### 2c. Reset (un-release)

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag poison reset <chunk_id>` |
| REST | `POST /poison/{chunk_id}/reset` |
| gRPC | `ResetChunk(chunk_id)` RPC |
| MCP | `poison_reset` tool |

Re-applies the scored level. Non-destructive.

### 2d. Trigger a background re-scan (FR-011, Option A — also auto-fires on threat-list change)

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag poison rescan` |
| REST | `POST /poison/rescan` |
| gRPC | `RescanPoisoning(Empty)` RPC |
| MCP | `poison_rescan` tool |

Fire-and-forget (returns immediately; runs async). The daemon ALSO fires this automatically
(debounced) when the merged threat list or thresholds change. Idempotent; preserves `released`.

## 3. Corpus re-scan (FR-007 / US3)

| Transport | Surface |
|-----------|---------|
| CLI | `go-rag reprocess --poisoning` (rides existing reprocess path) |
| REST | `POST /reprocess` with `poisoning: true` |
| gRPC | existing `Reprocess` RPC extended with `poisoning` flag |
| MCP | existing reprocess tool extended |

Iterates stored chunks, scores, persists verdicts. Idempotent (Constitution II).

## 4. Status / observability

`status` (all transports) gains a poisoning summary:
`poisoning: enabled=true, thresholds suspicious=0.40 quarantine=0.70, flagged=N (quarantine=K,
suspicious=S, released=R), threat_list=<merged-version>, sources=<M enabled / N total>,
last_rescan=<ts>, rescans=<C>`.

## 5. Config (FR-010 / D8 / D9)

`.go-rag/config.json` keys (all optional — sensible defaults):

| Key | Default | Meaning |
|-----|---------|---------|
| `poisoning_enabled` | `true` | Detection default-on (Q2=A); false ⇒ no scoring, chunks treated clean. |
| `poisoning_threshold_suspicious` | `0.40` | score ≥ → `suspicious`. |
| `poisoning_threshold_quarantine` | `0.70` | score ≥ → `quarantine`. |
| `poisoning_phrase_list` | (built-in English list) | Path to override/add instruction phrases (D9). |

## 6. Threat-list management (FR-012/013, D12)

| Op | CLI | REST | gRPC | MCP |
|----|-----|------|------|-----|
| list merged phrases + provenance | `threat list` | `GET /threat` | `ListThreats` | `threat_list` |
| add/remove own phrase | `threat add\|remove <p>` | `POST/DELETE /threat/phrase` | `AddPhrase`/`RemovePhrase` | `threat_add`/`threat_remove` |
| **import source** (file or URL — explicit, one-shot) | `threat import <path\|url>` | `POST /threat/import` | `ImportThreats(req)` | `threat_import` |
| export merged list | `threat export` | `GET /threat/export` | `ExportThreats` | `threat_export` |
| list/enable/disable sources | `threat sources` | `GET/PATCH /threat/sources` | `ListSources`/`ToggleSource` | `threat_sources` |

**Constitution I boundary**: URL fetch happens ONLY inside `import`, as an explicit
user-initiated action. Zero egress at any other time. A successful import that changes the
merged list auto-triggers the FR-011 rescan (debounced).

## 7. gRPC proto deltas (sketch — final field numbers assigned in tasks)

```proto
message QueryRequest  { ...; bool include_quarantined = <N>; }
message QueryHit      { ...; PoisoningVerdict poisoning = <N>; }
message PoisoningVerdict {
  enum Level { CLEAN=0; SUSPICIOUS=1; QUARANTINE=2; RELEASED=3; }
  Level level = 1;
  double score = 2;
  Signals signals = 3;
  repeated string matched_phrases = 4;
}
service Gorag {
  rpc ListPoisoned(ListPoisonedRequest) returns (stream PoisonedItem);
  rpc ReleaseChunk(ChunkID) returns (Ok);
  rpc ResetChunk(ChunkID) returns (Ok);
}
```

**Parity test (SC-004)**: a fixture chunk flagged `quarantine` must surface `level=QUARANTINE,
score, signals` identically on CLI, REST, gRPC, and MCP.
