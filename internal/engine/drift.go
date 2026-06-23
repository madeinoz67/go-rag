package engine

import (
	"context"
	"sync"
)

// Drift verdict constants (audit H11/spec 017).
const (
	VerdictClean          = "clean"           // baseline matches live config + version
	VerdictHardDrift      = "hard-drift"      // model/dim/convention mismatch (readiness NOT READY)
	VerdictVersionWarning = "version-warning" // Ollama-version change (soft; warn, still ready)
	VerdictUnknown        = "unknown"         // Ollama unreachable while a comparison was attempted
	VerdictNA             = "n/a"             // empty corpus / injected offline embedder with no baseline
)

// DriftVerdict is the result of comparing the live embedding profile + live
// Ollama version against the persisted corpus baseline. Computed by
// computeDriftVerdict, cached on the Engine (so /health probes stay O(1)),
// refreshed at boot (serve.go) and after Migrate.
type DriftVerdict struct {
	Verdict            string   // one of the Verdict* constants
	Hard               bool     // true for hard-drift (convenience for the readiness flag)
	BaselineModel      string   // from the persisted baseline (display)
	ConfiguredModel    string   // cfg.EmbeddingModel
	BaselineDim        int      // baseline dim
	LiveDim            int      // live embedder dim (0 if unknown)
	BaselineConvention string   // baseline prefix convention
	LiveConvention     string   // resolved configured convention
	BaselineVersion    string   // baseline Ollama version
	LiveVersion        string   // live Ollama version (""/unknown possible)
	Reasons            []string // human-readable mismatch list, e.g. "model: nomic vs mxbai"
}

// driftVerdict is the cached verdict (RWMutex-guarded). /health reads it under
// the read lock (fast); RefreshDriftVerdict writes it under the write lock.
// driftLiveVersion caches the live Ollama version from the last refresh, reused
// when writing the baseline on first embed (avoids a per-embed fetch).
type driftCache struct {
	mu          sync.RWMutex
	verdict     DriftVerdict
	liveVersion string
}

// RefreshDriftVerdict recomputes the drift verdict and caches it (verdict +
// live Ollama version). Called at boot (serve.go) and after Migrate. Safe to
// call on an engine with no baseline yet (returns n/a / backfill is handled in
// computeDriftVerdict).
func (e *Engine) RefreshDriftVerdict(ctx context.Context) DriftVerdict {
	v := e.computeDriftVerdict(ctx)
	e.drift.mu.Lock()
	e.drift.verdict = v
	e.drift.liveVersion = v.LiveVersion
	e.drift.mu.Unlock()
	return v
}

// currentVerdict returns the cached verdict (the one /health reads). Returns a
// zero verdict (n/a) if RefreshDriftVerdict has never run.
func (e *Engine) currentVerdict() DriftVerdict {
	e.drift.mu.RLock()
	defer e.drift.mu.RUnlock()
	return e.drift.verdict
}

// CachedLiveVersion returns the live Ollama version from the last refresh (used
// when writing the baseline on first embed, to avoid a per-embed fetch). May be
// "" (offline) or "unknown" (unreachable).
func (e *Engine) CachedLiveVersion() string {
	e.drift.mu.RLock()
	defer e.drift.mu.RUnlock()
	return e.drift.liveVersion
}

// computeDriftVerdict compares the persisted baseline to the live config +
// live Ollama version and returns the verdict. US1 fills the hard-drift
// (model/dim/convention) comparison; US2 adds the version (soft) comparison;
// US3 adds the first-boot backfill. This foundational stub returns n/a so the
// engine boots before the comparison logic lands.
func (e *Engine) computeDriftVerdict(_ context.Context) DriftVerdict {
	return DriftVerdict{Verdict: VerdictNA}
}
