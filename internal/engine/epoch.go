package engine

import "sync/atomic"

// markIndexChanged advances the engine's index epoch — the invalidation
// counter folded into the result-cache key (audit H06/spec 016). The pipeline
// calls this via its OnChange callback at every shared-index mutation:
//
//   - storeDocument (synchronous FTS add, pre-ACK)
//   - processJob    (asynchronous vector add, post-ACK on a worker goroutine)
//   - DeleteDoc     (synchronous FTS+Vector removal)
//
// Bumping on the asynchronous processJob path is the critical correctness step:
// a vector that lands after the write ACK must invalidate any result cached
// before it landed, or a query would freeze a pre-vector state. markIndexChanged
// uses epochMu only to lazily allocate the per-vault counter; the increment is
// then a lock-free atomic add. epochMu is intentionally separate from idxMu
// because background workers bump epochs and must never participate in the
// pipeMu → idxMu seed path.
func (e *Engine) markIndexChanged(ws [8]byte) {
	e.epochMu.Lock()
	if e.epoch == nil {
		e.epoch = make(map[[8]byte]*atomic.Uint64)
	}
	entry := e.epoch[ws]
	if entry == nil {
		entry = &atomic.Uint64{}
		e.epoch[ws] = entry
	}
	e.epochMu.Unlock()
	entry.Add(1)
}

// indexEpoch returns the current epoch value for one vault (0 before any
// mutation in that vault).
func (e *Engine) indexEpoch(ws [8]byte) uint64 {
	e.epochMu.Lock()
	entry := e.epoch[ws]
	e.epochMu.Unlock()
	if entry == nil {
		return 0
	}
	return entry.Load()
}

// flushCaches drops every cached result and query embedding. Used by Migrate (an
// embedding-model change invalidates all cached results and vectors) and by
// Close (the caches are stale relative to a re-seed). Counters are preserved.
func (e *Engine) flushCaches() {
	if e.resultCache != nil {
		e.resultCache.Flush()
	}
	if e.embedCache != nil {
		e.embedCache.Flush()
	}
}
