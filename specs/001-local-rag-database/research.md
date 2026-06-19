# Research: Local RAG Database (go-rag v1)

**Phase**: 0 (Outline & Research) | **Date**: 2026-06-19

**Primary source**: `PRD_RAG_Database.md` §9 (library survey) and §11 (open
questions), synthesized 2026-06-19. The PRD already performed the comparative
research; this file consolidates those decisions into the SpecKit Decision /
Rationale / Alternatives format and resolves every §11 open question with a v1
default. No new external research was required.

## Library Choices (PRD §9)

### Storage engine — Pebble
- **Decision**: `cockroachdb/pebble` (embedded LSM-tree).
- **Rationale**: Proven <10ms write-ACK in MuninnDB (the architectural inspiration);
  single embedded instance keeps the binary self-contained; prefix-partitioned
  key-space gives cheap scans and independent index rebuilds.
- **Alternatives**: bbolt (B-tree, weaker write throughput under load), BadgerDB
  (historical stability concerns), SQLite (needs CGo or a pure-Go fork). Rejected:
  worse fit for the async write-heavy RAG workload and the pure-Go constraint.

### Vector store — chromem-go
- **Decision**: `philippgille/chromem-go` (embedded HNSW, optional disk persistence).
- **Rationale**: Zero-dependency, embedded (no separate service), MPL-2.0
  file-level copyleft (compatible). Wrapping it behind our own interface (Principle V)
  makes it swappable.
- **Alternatives**: Qdrant/Milvus (require a separate server — violates single-binary),
  `milvus-io/milvus-sdk-go` (deprecated/archived per PRD §9.4).

### PDF extraction — pdfcpu
- **Decision**: `pdfcpu/pdfcpu` (pure Go, Apache-2.0, actively maintained).
- **Rationale**: Permissive license, full toolkit, active as of Jun 2026 (PRD §B.4).
- **Alternatives**: unipdf (commercial/AGPLv3 — license conflict), `ledongthuc/pdf`
  (reader-only, dormant). OCR fallback deferred to v2 (see OCR below).

### CLI — cobra
- **Decision**: `spf13/cobra`.
- **Rationale**: De facto Go CLI framework; shell completion, help, subcommands for free.
- **Alternatives**: `urfave/cli` (viable; cobra has larger ecosystem and is already in the scaffold).

### File watching — fsnotify
- **Decision**: `fsnotify/fsnotify` (Layer 1, real-time) + periodic SHA-256 polling (Layer 2).
- **Rationale**: Cross-platform OS events; polling survives restarts and catches what
  events miss under load (PRD §7.1).
- **Alternatives**: `radovskyb/watcher` (dormant, polling-only — PRD §9.4).

### Embeddings — Ollama HTTP
- **Decision**: Call Ollama `/api/embed` over HTTP; no Go embedding library.
- **Rationale**: Ollama is the only v1 provider (PRD non-goal N9); the `Embedder`
  interface keeps future providers pluggable. Local, no API keys.
- **Alternatives**: `nlpodyssey/cybertron` (dormant, not production-grade — PRD §9.4).

### RAG orchestration — deferred
- **Decision**: Do NOT adopt `cloudwego/eino` for v1; implement a linear pipeline.
- **Rationale**: The ingest pipeline is a simple linear flow (PRD §4.4); a DAG
  framework is premature. Interface seams leave room to adopt it later.

## Resolved Open Questions (PRD §11)

| Q | Topic | v1 Decision | Rationale |
|---|-------|-------------|-----------|
| Q1 | OCR for images | Defer to v2; images indexed by metadata/filename only | Avoids a heavy CGo OCR dep; preserves pure-Go (Principle III) |
| Q2 | Token counting | Word-based heuristic (~1.3 tokens/word) | "Token" is approximate for chunk sizing; avoids a tokenizer dependency |
| Q3 | Model migration | Re-embed all; document identity survives (ID ≠ embedding) | Content-addressed ID is embedding-agnostic (Principle II) |
| Q4 | chromem-go persistence | Verify + use its disk persistence; persist on write | Enables <1s cold start (PRD §10.1) |
| Q5 | Large PDF memory | Warn >100MB; configurable max file size; skip over max | pdfcpu loads full doc into memory (PRD R4) |
| Q6 | Concurrent add + query | Single-writer; reads see last committed state (eventual consistency) | Pebble snapshot reads; defined, not undefined (Principle IV) |
| Q7 | Cross-doc chunk dedup | None in v1 (document-level dedup only) | Storage cost acceptable; avoids query-precision loss |
| Q8 | Query-result dedup | Collapse to top-1 per document by default (configurable) | Better UX; avoids duplicate hits from one source |
| Q9 | Schema migration | Versioned DB metadata; migrate on open | Backward compatibility across go-rag versions |
| Q10 | File deletion | Hard-delete chunks/embeddings/index entries | Recoverability via re-ingest; keeps storage lean |
| Q11 | Watch recursion depth | Polling safety net covers inotify limits; document `fs.inotify.max_user_watches` | Defense-in-depth change detection (PRD R6) |
| Q12 | Metadata-only change | Re-ingest (metadata is part of the identity hash) | Simpler; metadata changes are rare |
| Q13 | Embedding dim mismatch | Store dimensions; different-dim model → forced full reindex | Prevents index corruption |
| Q14 | MCP tool design | Minimal ~6 tools mirroring the CLI (not 35) | Principle V + simplicity |

## Conclusion

All Technical Context unknowns are resolved by the PRD. No `[NEEDS CLARIFICATION]`
items remain. Proceed to Phase 1 design.
