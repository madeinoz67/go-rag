package engine

import (
	"container/list"
	"sync"
	"sync/atomic"
)

// CacheStats is the observable state of one cache, surfaced by Status() for the
// result cache and the query-embedding cache (audit H06/spec 016). Hit rate is
// derived by the consumer as Hits/(Hits+Misses) (guard against divide-by-zero
// when both are 0).
type CacheStats struct {
	Enabled  bool
	Size     int
	Capacity int
	Hits     uint64
	Misses   uint64
}

// entry is one node in an LRU's doubly-linked list.
type entry[K comparable, V any] struct {
	key K
	val V
}

// LRU is a bounded, concurrency-safe, exact-match LRU cache. It is the shared
// shape for the result cache (V = *QueryResult) and the query-embedding cache
// (V = [][]float32). A capacity <= 0 disables the cache: Get always reports a
// miss and Put is a no-op — this is how the global kill-switch
// (config QueryCacheEnabled=false) and a per-cache disable (capacity 0) are
// both expressed, with no special cases at the call sites.
//
// The cache is in-process memory: empty on construction, never persisted, empty
// again on restart (FR-007). It stores already-computed values and never
// changes what a caller sees — a hit is byte-identical to a cold computation
// (FR-008/transparency) — so enabling it only changes latency.
type LRU[K comparable, V any] struct {
	cap    int
	mu     sync.Mutex
	ll     *list.List
	index  map[K]*list.Element
	hits   atomic.Uint64
	misses atomic.Uint64
}

// NewLRU returns an LRU with the given capacity. capacity <= 0 yields a disabled
// cache (Enabled() == false; Get always misses, Put is a no-op).
func NewLRU[K comparable, V any](capacity int) *LRU[K, V] {
	return &LRU[K, V]{
		cap:   capacity,
		ll:    list.New(),
		index: make(map[K]*list.Element),
	}
}

// Enabled reports whether the cache stores anything. A nil receiver is disabled.
func (c *LRU[K, V]) Enabled() bool { return c != nil && c.cap > 0 }

// Get returns the cached value for key, bumping it to most-recently-used on a
// hit. ok is false on a miss or when the cache is disabled. Hits/misses counters
// are cumulative across the cache's lifetime and survive Flush.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	var zero V
	if !c.Enabled() {
		c.misses.Add(1)
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		c.ll.MoveToFront(el)
		c.hits.Add(1)
		return el.Value.(*entry[K, V]).val, true
	}
	c.misses.Add(1)
	return zero, false
}

// Put stores val under key, evicting the least-recently-used entry when the
// cache is at capacity. A Put on an existing key updates the value and bumps it
// to most-recently-used. No-op when disabled.
func (c *LRU[K, V]) Put(key K, val V) {
	if !c.Enabled() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		el.Value.(*entry[K, V]).val = val
		c.ll.MoveToFront(el)
		return
	}
	c.index[key] = c.ll.PushFront(&entry[K, V]{key: key, val: val})
	if c.ll.Len() > c.cap {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.index, oldest.Value.(*entry[K, V]).key)
		}
	}
}

// Flush drops every entry. Counters are preserved (they are cumulative
// lifetime stats for Status(), not a current-size measure).
func (c *LRU[K, V]) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.index = make(map[K]*list.Element)
}

// Len returns the current number of cached entries (0 when disabled/nil).
func (c *LRU[K, V]) Len() int {
	if !c.Enabled() {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Stats returns the observable cache state for Status().
func (c *LRU[K, V]) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	if c.cap <= 0 {
		return CacheStats{Enabled: false, Capacity: c.cap, Hits: c.hits.Load(), Misses: c.misses.Load()}
	}
	return CacheStats{
		Enabled:  true,
		Size:     c.Len(),
		Capacity: c.cap,
		Hits:     c.hits.Load(),
		Misses:   c.misses.Load(),
	}
}
