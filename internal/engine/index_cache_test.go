package engine

// index_cache_test.go is in the internal package `engine` (not engine_test) so it
// can assert the shared-index invariants directly: e.indexes() returns the SAME
// *FTS/*Vector pointers across calls (seed-once → no per-query rebuild), the
// single proof that audit H01 / spec 011 actually took effect. Behavioral
// read-after-write / delete / concurrency are also exercised here via an injected
// fake embedder (NewWithEmbedder) so no external Ollama is needed.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// cacheFakeEmb returns a fixed dim-2 vector per text (deterministic, instant) —
// satisfies embed.Embedder so the pipeline embeds hermetically with no Ollama.
type cacheFakeEmb struct{}

func (cacheFakeEmb) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}
func (cacheFakeEmb) Dimensions() int { return 2 }
func (cacheFakeEmb) Model() string   { return "fake" }

// newCacheEngine builds an Engine over a temp DB with the fake embedder injected
// (used for both ingest and query). Close + DB close are registered for cleanup.
func newCacheEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	e := NewWithEmbedder(cfg, db, cacheFakeEmb{})
	t.Cleanup(e.Close) // drains async workers before db.Close
	return e
}

// addDoc writes content to a temp file, ingests it, and waits for embeddings to
// land (async-after-ACK), returning the file path.
func addDoc(t *testing.T, e *Engine, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := e.Add(context.Background(), path); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitEmbedded(t, e)
	return path
}

// waitEmbedded polls Status until the async embedders have drained.
func waitEmbedded(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := e.Status()
		if st.Embeddings > 0 && st.EmbeddingsComplete {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("embeddings did not drain within 5s")
}

// docIDForPath resolves the document ID for an ingested path (PrefixPathDoc).
func docIDForPath(t *testing.T, e *Engine, path string) string {
	t.Helper()
	raw, ok, _ := e.db.GetWithPrefix(storage.PrefixPathDoc, []byte(path))
	if !ok {
		t.Fatalf("no document indexed for path %s", path)
	}
	return string(raw)
}

// hitsEqual compares two hit slices by ChunkID + Score (order-sensitive).
func hitsEqual(a, b []QueryHit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ChunkID != b[i].ChunkID || a[i].Score != b[i].Score {
			return false
		}
	}
	return true
}

// --- US1: cache reused, no per-query rebuild ---

// TestQuery_ReusesSharedIndex (H01 US1, SC-001): indexes() returns the SAME
// shared pointers across calls (seed-once → no per-query rebuild), and repeated
// queries return identical results (FR-008).
func TestQuery_ReusesSharedIndex(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "alpha bravo charlie delta echo foxtrot")

	f1, v1, err := e.indexes()
	if err != nil {
		t.Fatalf("indexes: %v", err)
	}
	f2, v2, _ := e.indexes()
	if f1 != f2 || v1 != v2 {
		t.Fatal("indexes() must return the same shared *FTS/*Vector across calls (seed-once / no per-query rebuild)")
	}

	r1, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(r1.Hits) == 0 {
		t.Fatal("expected >=1 hit for 'alpha'")
	}
	r2, _ := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "keyword", K: 5})
	if !hitsEqual(r1.Hits, r2.Hits) {
		t.Errorf("repeated queries must return identical results: %v vs %v", r1.Hits, r2.Hits)
	}
}

// --- US2: freshness (read-after-write / delete / migrate) ---

// TestQuery_ReadAfterWrite_Ingest (H01 US2, FR-003/FR-004): after a document is
// ingested and its embeddings land, the next query returns it (the live shared
// index reflects embedding completion — no restart, no flush).
func TestQuery_ReadAfterWrite_Ingest(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "the quick brown fox jumps over the lazy dog")
	res, err := e.Query(context.Background(), QueryRequest{Query: "fox", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("read-after-write failed: query found nothing after ingest + embeddings")
	}
}

// TestQuery_AfterDelete_NoPhantomHits (H01 US2, FR-003): deleting a document via
// the cache-aware (*Pipeline).DeleteDoc removes its chunks from the shared index —
// the next query returns no phantom hits.
func TestQuery_AfterDelete_NoPhantomHits(t *testing.T) {
	e := newCacheEngine(t)
	path := addDoc(t, e, "uniquezword to find then delete from the index")

	res, _ := e.Query(context.Background(), QueryRequest{Query: "uniquezword", Mode: "keyword", K: 5})
	if len(res.Hits) == 0 {
		t.Fatal("precondition: doc should be queryable before delete")
	}

	p, err := e.pipeline()
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if err := p.DeleteDoc(docIDForPath(t, e, path)); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}

	after, _ := e.Query(context.Background(), QueryRequest{Query: "uniquezword", Mode: "keyword", K: 5})
	if len(after.Hits) != 0 {
		t.Errorf("delete must remove chunks from the shared index; got %d phantom hits: %+v", len(after.Hits), after.Hits)
	}
}

// TestQuery_AfterMigrate_IndexIntact (H01 US2, FR-002): migrate flows through
// reprocess (cache-aware DeleteDoc + re-ingest via processJob); the shared index
// stays consistent and queryable afterwards (no crash, no stale/empty state).
// (Migrate's "new embeddings reflected" is structurally delete(T above)+ingest
// composition; with a constant fake embedder the vectors are identical, so this
// test asserts consistency rather than a vector-value change.)
func TestQuery_AfterMigrate_IndexIntact(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "migrate consistency corpus document about retrieval")

	if _, err := e.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	res, err := e.Query(context.Background(), QueryRequest{Query: "migrate", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query after migrate: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("index must remain queryable (non-empty) after migrate")
	}
}

// --- US3: concurrency safety + seed-once ---

// TestQuery_ConcurrentSafe_UnderBackgroundIngest (H01 US3, FR-005, SC-004): many
// concurrent queries while a document ingests in the background, under -race, must
// not error or panic. (The background Add is fire-and-forget; the readers hit the
// already-ingested baseline doc.)
func TestQuery_ConcurrentSafe_UnderBackgroundIngest(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "concurrency baseline document for parallel queries")

	// Fire a background ingest concurrently with the readers (errors ignored —
	// it must not corrupt the readers' results either way).
	bgDir := t.TempDir()
	bgPath := filepath.Join(bgDir, "bg.txt")
	if err := os.WriteFile(bgPath, []byte("background ingested document for concurrency"), 0o644); err != nil {
		t.Fatalf("write bg doc: %v", err)
	}
	addDone := make(chan struct{})
	go func() {
		defer close(addDone)
		_, _ = e.Add(context.Background(), bgPath) // returns after sync store + enqueue
	}()

	const q = 32
	var wg sync.WaitGroup
	errs := make(chan error, q)
	for i := 0; i < q; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.Query(context.Background(), QueryRequest{Query: "baseline", Mode: "keyword", K: 5}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()    // readers done
	<-addDone    // background Add returned (sync store complete) before teardown closes the DB
	close(errs)
	for err := range errs {
		t.Errorf("concurrent query errored: %v", err)
	}
}

// TestIndexes_SeedsOnce_NoThunderingHerd (H01 US3, FR-006, SC-004): N concurrent
// first-time indexes() calls against a cold cache all observe the SAME shared
// pointers — the seed ran exactly once, not N times (no thundering herd).
func TestIndexes_SeedsOnce_NoThunderingHerd(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "seed-once hermit document")

	type pair struct {
		f *index.FTS
		v *index.Vector
	}
	var (
		mu       sync.Mutex
		first    pair
		mismatch bool
		wg       sync.WaitGroup
	)
	const n = 32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, v, err := e.indexes()
			if err != nil {
				t.Errorf("indexes: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if first.f == nil {
				first = pair{f, v}
			} else if f != first.f || v != first.v {
				mismatch = true
			}
		}()
	}
	wg.Wait()
	if mismatch {
		t.Error("concurrent indexes() returned different pointers — seed must be shared (no thundering herd)")
	}
}
