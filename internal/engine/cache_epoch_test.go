package engine

// cache_epoch_test.go (internal package `engine`) proves H06/spec 016
// invalidation: the result cache never serves a stale result after the corpus
// changes. The epoch must bump at every shared-index mutation — including the
// asynchronous vector-add in processJob, which a write-ACK-only bump would miss.

import (
	"context"
	"testing"
	"time"
)

// waitForEpoch polls until the index epoch reaches want (or the deadline
// passes). Used by the async-bump test because waitEmbedded returns as soon as
// embeddings are *persisted* (inside the vec.Add loop), which can precede the
// processJob indexChanged() call that fires *after* the loop — so the epoch's
// async advance must be awaited explicitly. If want is never reached (e.g. the
// async bump were removed), the deadline trips and the test fails.
func waitForEpoch(t *testing.T, e *Engine, want uint64) {
	t.Helper()
	ws := e.db.ResolveVaultPrefix("default")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e.indexEpoch(ws) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("epoch never reached %d (got %d) within 5s", want, e.indexEpoch(ws))
}

// waitForEpochStable polls until the index epoch stops advancing between two
// samples — i.e. all pending async indexChanged bumps have drained. Used by
// cache tests that cache a result and re-query the same key: a lingering async
// bump would otherwise advance the epoch between the two queries and turn an
// expected hit into a miss (the same race waitForEpoch guards against).
func waitForEpochStable(t *testing.T, e *Engine) {
	t.Helper()
	ws := e.db.ResolveVaultPrefix("default")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a := e.indexEpoch(ws)
		time.Sleep(30 * time.Millisecond)
		if e.indexEpoch(ws) == a {
			return // quiescent
		}
	}
	t.Fatalf("epoch never stabilized within 5s (still advancing)")
}

// TestEpoch_PerVaultIsolation proves the epoch counter is maintained per vault:
// a mutation in one workspace must not invalidate another workspace's cache key.
func TestEpoch_PerVaultIsolation(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	wsA := e.db.ResolveVaultPrefix("default")
	wsB := e.db.ResolveVaultPrefix("other")

	e.markIndexChanged(wsA)
	e.markIndexChanged(wsA)
	e.markIndexChanged(wsB)

	if got := e.indexEpoch(wsA); got != 2 {
		t.Fatalf("epoch(default) = %d, want 2", got)
	}
	if got := e.indexEpoch(wsB); got != 1 {
		t.Fatalf("epoch(other) = %d, want 1", got)
	}
}

// TestCacheKey_PerVaultIsolation proves identical queries in different
// workspaces hash differently, preventing cross-vault result-cache reuse.
func TestCacheKey_PerVaultIsolation(t *testing.T) {
	wsA := [8]byte{'d', 'e', 'f', 'a', 'u', 'l', 't', 0}
	wsB := [8]byte{'o', 't', 'h', 'e', 'r', 0, 0, 0}
	keyA := cacheKey{Query: "same query", Mode: "keyword", Epoch: 7, WS: wsA}.hash()
	keyB := cacheKey{Query: "same query", Mode: "keyword", Epoch: 7, WS: wsB}.hash()
	if keyA == keyB {
		t.Fatal("cacheKey hash collided across workspaces; want ws-scoped result cache keys")
	}
}

// TestEpoch_IngestInvalidates asserts a cached keyword query reflects a newly-
// ingested document: the epoch bumped at the synchronous FTS add (storeDocument),
// so the stale entry is never served.
func TestEpoch_IngestInvalidates(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	req := QueryRequest{Query: "uniqueingestterm", Mode: "keyword", K: 5}

	// Cold: no such term in the corpus → empty result, cached.
	r0, err := e.Query(context.Background(), "default", req)
	if err != nil {
		t.Fatal(err)
	}
	if len(r0.Hits) != 0 {
		t.Fatalf("precondition: expected 0 hits, got %d", len(r0.Hits))
	}

	// Ingest a document containing the term.
	addDoc(t, e, "this document mentions uniqueingestterm explicitly")

	// Same query: must reflect the new document (not the cached empty result).
	hitsBefore := e.resultCache.Stats().Hits
	r1, err := e.Query(context.Background(), "default", req)
	if err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != hitsBefore {
		t.Fatalf("post-ingest query served from cache; want recomputation (epoch invalidation)")
	}
	if len(r1.Hits) == 0 {
		t.Fatalf("post-ingest query returned 0 hits; want the new document (stale cache served)")
	}
}

// TestEpoch_DeleteInvalidates asserts a cached query reflects a deletion: the
// epoch bumped in DeleteDoc, so the now-deleted hit is not served.
func TestEpoch_DeleteInvalidates(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	path := addDoc(t, e, "deleteme document about deletable content")
	req := QueryRequest{Query: "deletable", Mode: "keyword", K: 5}

	r0, err := e.Query(context.Background(), "default", req)
	if err != nil {
		t.Fatal(err)
	}
	if len(r0.Hits) == 0 {
		t.Fatal("precondition: expected the doc to match")
	}

	// Delete via the engine's pipeline (the same path the watcher uses).
	docID := docIDForPath(t, e, path)
	pipe, err := e.pipeline()
	if err != nil {
		t.Fatal(err)
	}
	if err := pipe.DeleteDoc(docID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}

	// Same query: must reflect the deletion (empty), served as a recomputation.
	hitsBefore := e.resultCache.Stats().Hits
	r1, err := e.Query(context.Background(), "default", req)
	if err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Stats().Hits != hitsBefore {
		t.Fatalf("post-delete query served from cache; want recomputation (epoch invalidation)")
	}
	if len(r1.Hits) != 0 {
		t.Fatalf("post-delete query returned %d hits; want 0 (stale cache served a phantom hit)", len(r1.Hits))
	}
}

// TestEpoch_AsyncVectorBump is the CRITICAL regression test for the asynchronous
// epoch bump (audit H06, research D2). Each ingested document must advance the
// epoch TWICE: once at the synchronous storeDocument (FTS add, pre-ACK) and once
// at the asynchronous processJob (vector add, post-ACK). A write-ACK-only bump —
// the bug this spec exists to prevent — would advance it only once, and a query
// that cached between the two would freeze a pre-vector state. This test fails
// the moment the processJob bump (T007) is removed.
func TestEpoch_AsyncVectorBump(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	epoch0 := e.indexEpoch(e.db.ResolveVaultPrefix("default"))

	// addDoc ACKs after storeDocument (durable writes only; FTS moved async —
	// H16/spec 018). The async processJob adds the vector + FTS postings and
	// bumps the epoch (+1). Await the +1.
	addDoc(t, e, "first document content for the async epoch test")
	waitForEpoch(t, e, epoch0+2) // spec 030: processJob FTS (+1) + embedder vector (+1)

	// A second document advances it by another 2.
	addDoc(t, e, "second document content distinct from the first")
	waitForEpoch(t, e, epoch0+4)
}

// TestEpoch_MigrateFlushesCaches asserts Migrate flushes the result cache (an
// embedding-model change invalidates all cached results). We force a migration
// by making the configured model differ from the stored embeddings' model.
func TestEpoch_MigrateFlushesCaches(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	addDoc(t, e, "migrate document content about re-embedding")
	waitForEpochStable(t, e) // drain async index bumps before querying (CI timing)

	// Warm the result cache with a query (embedder model "fake" matches corpus).
	req := QueryRequest{Query: "migrate", Mode: "keyword", K: 5}
	if _, err := e.Query(context.Background(), "default", req); err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Len() == 0 {
		t.Fatalf("precondition: result cache should hold the warmed entry")
	}

	// Force a real migration: the stored embeddings are model "fake" (the injected
	// embedder), so a different configured model makes EmbeddingModelStats report
	// them all stale and Migrate proceeds past its no-op short-circuit.
	e.cfg.EmbeddingModel = "different-model"

	if _, err := e.Migrate(context.Background(), "default"); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if e.resultCache.Len() != 0 {
		t.Fatalf("result cache not flushed by Migrate (size=%d); want 0", e.resultCache.Len())
	}
}
