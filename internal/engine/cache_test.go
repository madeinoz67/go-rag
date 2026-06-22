package engine

import (
	"sync"
	"testing"
)

// TestLRU_HitMissEviction covers the core LRU contract: hits bump to front,
// capacity evicts the least-recently-used, and miss/hit counters accumulate.
func TestLRU_HitMissEviction(t *testing.T) {
	c := NewLRU[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = (%d,%v), want (1,true)", v, ok)
	}
	// "b" is now least-recently-used; inserting "c" evicts "b".
	c.Put("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Fatalf("Get(b) after eviction hit; want miss")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) should survive (was MRU), got (%d,%v)", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = (%d,%v), want (3,true)", v, ok)
	}

	st := c.Stats()
	if st.Hits != 3 || st.Misses != 1 {
		t.Fatalf("Stats hits/misses = %d/%d, want 3/1", st.Hits, st.Misses)
	}
	if st.Size != 2 || st.Capacity != 2 || !st.Enabled {
		t.Fatalf("Stats size/cap/enabled = %d/%d/%v, want 2/2/true", st.Size, st.Capacity, st.Enabled)
	}
}

// TestLRU_PutUpdateMove verifies a Put on an existing key updates the value and
// refreshes its recency (does not grow the size, does not evict).
func TestLRU_PutUpdateMove(t *testing.T) {
	c := NewLRU[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a") // "a" is MRU now
	c.Put("b", 22)
	if c.Len() != 2 {
		t.Fatalf("Len after update = %d, want 2", c.Len())
	}
	// "a" was touched most recently after the "b" update? No: Put("b") makes "b"
	// MRU. So "a" is LRU → next insert evicts "a".
	c.Put("c", 3)
	if _, ok := c.Get("a"); ok {
		t.Fatalf("Get(a) hit after eviction; want miss (a was LRU)")
	}
	if v, ok := c.Get("b"); !ok || v != 22 {
		t.Fatalf("Get(b) = (%d,%v), want (22,true) — update must persist", v, ok)
	}
}

// TestLRU_DisabledNoOp asserts a capacity-0 cache never stores or serves.
func TestLRU_DisabledNoOp(t *testing.T) {
	c := NewLRU[string, int](0)
	c.Put("a", 1)
	if v, ok := c.Get("a"); ok {
		t.Fatalf("Get on disabled cache hit with %d; want miss", v)
	}
	st := c.Stats()
	if st.Enabled {
		t.Fatalf("disabled cache reports Enabled=true")
	}
	// A miss on a disabled cache still counts as a miss (cumulative stat).
	if st.Misses != 1 {
		t.Fatalf("disabled miss counter = %d, want 1", st.Misses)
	}
}

// TestLRU_FlushPreservesCounters verifies Flush empties entries but keeps the
// cumulative hit/miss stats (they are lifetime stats for Status(), not size).
func TestLRU_FlushPreservesCounters(t *testing.T) {
	c := NewLRU[string, int](4)
	c.Put("a", 1)
	c.Get("a") // hit
	c.Get("z") // miss
	hits, misses := c.hits.Load(), c.misses.Load()
	c.Flush()
	if c.Len() != 0 {
		t.Fatalf("Len after Flush = %d, want 0", c.Len())
	}
	if c.hits.Load() != hits || c.misses.Load() != misses {
		t.Fatalf("Flush changed counters: hits %d→%d misses %d→%d", hits, c.hits.Load(), misses, c.misses.Load())
	}
	// Re-Get after flush is a fresh miss.
	if _, ok := c.Get("a"); ok {
		t.Fatalf("Get(a) after Flush hit; want miss")
	}
}

// TestLRU_Concurrent hammers the cache from many goroutines under -race to
// confirm the mutex guards all mutations (a proxy for the query-path concurrency
// the constitution's single-writer/concurrent-reader model requires).
func TestLRU_Concurrent(t *testing.T) {
	c := NewLRU[string, int](64)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := string(rune('A' + (i+off)%26))
				c.Put(k, i)
				_, _ = c.Get(k)
			}
		}(g)
	}
	wg.Wait()
	st := c.Stats()
	if st.Size > 26 {
		t.Fatalf("size %d exceeds key universe 26", st.Size)
	}
	if st.Hits+st.Misses == 0 {
		t.Fatalf("no cache activity recorded")
	}
}
