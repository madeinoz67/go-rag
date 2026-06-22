package engine

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
// is a lock-free atomic add, so calling it from the pipeline's background
// workers is safe and introduces no lock-ordering risk against pipeMu/idxMu.
func (e *Engine) markIndexChanged() {
	if e.epoch != nil {
		e.epoch.Add(1)
	}
}

// indexEpoch returns the current epoch value (0 before any mutation).
func (e *Engine) indexEpoch() uint64 {
	if e.epoch == nil {
		return 0
	}
	return e.epoch.Load()
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
