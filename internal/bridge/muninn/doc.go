// Package muninn implements the optional MuninnDB bridge (spec 060).
//
// The bridge promotes go-rag document chunks into a local MuninnDB vault as
// content-addressed engrams, so that retrieved reference material also lives in
// the operator's long-term memory (go-rag retrieves, MuninnDB remembers).
//
// It is an OPT-IN, LOOPBACK-ONLY egress exception to the local-first principle
// (constitution Principle I): background, never a core operation, off by
// default. A down MuninnDB never blocks ingest/query — promotion runs on a
// decoupled worker (the embedProc pattern) fed by a non-blocking enqueue from
// the ingest pipeline's processJob (the enrichment seam).
//
// v1 is STATELESS: no new go-rag keyspace, no migration. UPSERT on
// idempotent_id (muninndb #556 / PR #659) is the correctness layer — re-promoting
// an unchanged chunk is a strict server-side no-op, so the bridge needs no local
// promoted-chunk registry.
//
// See specs/060-muninn-bridge/{spec,plan,research,data-model}.md and the RFC at
// docs/RFC-bridge-muninndb/bridge-muninn.md.
package muninn
