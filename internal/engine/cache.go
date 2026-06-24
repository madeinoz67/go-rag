package engine

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/madeinoz67/go-rag/internal/embed"
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

// cacheKey is the exact-match key for the result cache (audit H06/spec 016). It
// captures EVERY input that affects the ranked output, so two queries that would
// produce different results always get different keys (and vice versa). The key
// is hashed (FNV-1a, null-separated, tags sorted) into a short string for the
// LRU map. req.NoCache is deliberately NOT part of the key — it is a per-call
// serve-bypass flag, not a result-shape input.
type cacheKey struct {
	Query         string
	Mode          string
	K             int
	Threshold     float64
	RRFK          int
	FilterSource  string
	FilterType    string
	FilterTags    []string // sorted before hashing; nil/empty both hash the same
	ContextWindow int
	RerankEnabled bool
	RerankModel   string // only populated when RerankEnabled (avoids false key splits)
	// IncludeQuarantined (H04/spec 019): a quarantine-excluded result and an
	// include-quarantined result differ, so the flag is part of the key.
	IncludeQuarantined bool
	// EffK / EffPool (H22/spec 024): the EFFECTIVE depth and candidate pool used
	// for the query (explicit | recommended | default for K; per-query |
	// classifier-derived | config for Pool). Pool was previously constant (60) so
	// it was not a differentiator; once it varies it MUST be in the key or two
	// queries differing only in pool collide. Folding EFFECTIVE (not requested) k
	// means an explicit-k and a classifier-recommended-k that resolve equal share
	// a key (same results) while different effective depths diverge.
	EffK   int
	EffPool int
	Epoch  uint64
}

// hash returns the FNV-1a digest of the key as a hex string. Deterministic for a
// given cacheKey (tags sorted, numbers in canonical string form, null-separated
// so no field can be confused with its neighbor).
func (k cacheKey) hash() string {
	tags := k.FilterTags
	if !sort.StringsAreSorted(tags) {
		dup := append([]string(nil), tags...)
		sort.Strings(dup)
		tags = dup
	}
	h := fnv.New64a()
	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0}) // null separator — unambiguous field boundaries
	}
	write(k.Query)
	write(k.Mode)
	write(strconv.Itoa(k.K))
	write(strconv.FormatFloat(k.Threshold, 'f', -1, 64))
	write(strconv.Itoa(k.RRFK))
	write(k.FilterSource)
	write(k.FilterType)
	for _, t := range tags {
		write(t)
	}
	write(strconv.Itoa(k.ContextWindow))
	write(strconv.FormatBool(k.IncludeQuarantined)) // H04/spec 019: different quarantine policy → different key
	write(strconv.FormatBool(k.RerankEnabled))
	write(k.RerankModel)
	write(strconv.Itoa(k.EffK))   // H22/spec 024: effective depth (explicit|recommended|default)
	write(strconv.Itoa(k.EffPool)) // H22/spec 024: effective candidate pool (per-query|classifier|config)
	write(strconv.FormatUint(k.Epoch, 10))
	return strconv.FormatUint(h.Sum64(), 16)
}

// resultKey builds the result-cache key for a query against the engine's config
// and current index epoch. effRRFK is the already-resolved effective RRF k
// (caller resolves req.RRFK>0 vs config). effK/effPool (H22/spec 024) are the
// already-resolved effective depth and candidate pool, folded in so two queries
// differing only in effective depth/pool get distinct keys. Rerank model is
// folded in only when reranking is enabled for this request, so a no-rerank
// query and a reranker-not-configured query that both skip reranking share a key.
func (e *Engine) resultKey(req QueryRequest, effRRFK, effK, effPool int, epoch uint64) string {
	k := cacheKey{
		Query:              req.Query,
		Mode:               req.Mode,
		K:                  req.K,
		Threshold:          req.Threshold,
		RRFK:               effRRFK,
		ContextWindow:      req.ContextWindow,
		IncludeQuarantined: req.IncludeQuarantined,
		EffK:               effK,
		EffPool:            effPool,
		Epoch:              epoch,
	}
	if req.Filter != nil {
		k.FilterSource = req.Filter.Source
		k.FilterType = req.Filter.Type
		k.FilterTags = req.Filter.Tags
	}
	if e.cfg.RerankModel != "" && !req.NoRerank {
		k.RerankEnabled = true
		k.RerankModel = e.cfg.RerankModel
	}
	return k.hash()
}

// embedFingerprint is the embedding-profile component of the query-embedding
// cache key: the embedder's model + dimensionality + the active prefix
// convention. Two queries under the same profile share embeddings; a
// model/convention change produces a different fingerprint (and so a different
// key), evicting the old vectors by key. em is the query embedder, pre the
// instruction-prefix resolver in effect.
func embedFingerprint(em embed.Embedder, pre *embed.Prefixer) string {
	conv := ""
	if pre != nil {
		conv = pre.Convention()
	}
	return fmt.Sprintf("%s|%d|%s", em.Model(), em.Dimensions(), conv)
}

// embedCacheKey composes the embedding-profile fingerprint and the prefixed
// query text into a single cache key. The NUL byte separates them so no text
// can span the boundary.
func embedCacheKey(profileFP, text string) string {
	return profileFP + "\x00" + text
}
