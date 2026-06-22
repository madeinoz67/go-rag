package engine

// cache_safety_test.go (internal package `engine`) proves the H06/spec 016
// caches are concurrency-safe under -race (US4) and empty on restart (FR-007).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestCache_ConcurrentSafe hammers the result cache with concurrent queries
// while ingest goroutines bump the index epoch. Under `go test -race` any
// unprotected access fails the build; the sanity check confirms activity.
func TestCache_ConcurrentSafe(t *testing.T) {
	e := newResultCacheEngine(t, 16)
	addDoc(t, e, "alpha retrieval document about searching ranking relevance")

	// Pre-create ingest files so goroutines don't touch testing.T internals.
	var paths []string
	for i := 0; i < 4; i++ {
		dir := t.TempDir()
		p := filepath.Join(dir, fmt.Sprintf("d%d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("concurrent doc %d about topic number %d", i, i)), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	var wg sync.WaitGroup
	// Readers: concurrent queries (cache Get/Put under the cache mutex).
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			queries := []string{"alpha", "retrieval", "ranking"}
			for i := 0; i < 50; i++ {
				_, _ = e.Query(context.Background(), QueryRequest{Query: queries[n%3], Mode: "keyword", K: 5})
			}
		}(g)
	}
	// Writers: concurrent ingests (atomic epoch bumps + storeDocument).
	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			_, _ = e.Add(context.Background(), path)
		}(p)
	}
	wg.Wait()

	st := e.resultCache.Stats()
	if st.Hits+st.Misses == 0 {
		t.Fatalf("no query activity recorded; concurrency test did not exercise the cache")
	}
}

// TestCache_RestartEmpty proves the caches are in-process only: a fresh engine
// starts empty, Close flushes them, and a new engine instance starts empty again
// (nothing persists — the value type is not written to Pebble anywhere).
func TestCache_RestartEmpty(t *testing.T) {
	e := newResultCacheEngine(t, 8)
	if e.resultCache.Len() != 0 || e.embedCache.Len() != 0 {
		t.Fatalf("fresh engine caches non-empty: result=%d embed=%d", e.resultCache.Len(), e.embedCache.Len())
	}

	addDoc(t, e, "alpha retrieval document about searching")
	if _, err := e.Query(context.Background(), QueryRequest{Query: "alpha", Mode: "hybrid", K: 5}); err != nil {
		t.Fatal(err)
	}
	if e.resultCache.Len() == 0 {
		t.Fatalf("result cache not warmed by a query")
	}

	// Close flushes both caches (they are stale relative to a re-seed).
	e.Close()
	if e.resultCache.Len() != 0 || e.embedCache.Len() != 0 {
		t.Fatalf("caches not flushed by Close: result=%d embed=%d", e.resultCache.Len(), e.embedCache.Len())
	}

	// A brand-new engine instance starts empty (caches are never persisted).
	e2 := newResultCacheEngine(t, 8)
	if e2.resultCache.Len() != 0 || e2.embedCache.Len() != 0 {
		t.Fatalf("new engine instance caches non-empty (caches must not persist)")
	}
}
