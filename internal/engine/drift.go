package engine

import (
	"context"
	"fmt"
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

// computeDriftVerdict compares the persisted baseline to the live config + live
// Ollama version and returns the verdict. US1 (this impl) does the hard-drift
// (model/dim/convention) comparison; US2 adds the soft version comparison;
// US3 adds first-boot backfill. With no baseline (empty corpus, or a pre-H11
// corpus before backfill lands) the verdict is n/a — nothing to compare.
func (e *Engine) computeDriftVerdict(ctx context.Context) DriftVerdict {
	v := DriftVerdict{Verdict: VerdictNA, ConfiguredModel: e.cfg.EmbeddingModel}
	if pre := e.cfg.Prefixer(); pre != nil {
		v.LiveConvention = pre.Convention()
	}
	if em := e.embedderOrOllama(); em != nil {
		v.LiveDim = em.Dimensions()
	}
	// Live Ollama version (populated for status display; US2 compares it).
	// "" for an empty OllamaURL (offline/injected embedder); "unknown" on
	// unreachable — both cause the version comparison to be skipped.
	v.LiveVersion = ollamaVersion(ctx, e.cfg.OllamaURL)

	base, ok := LoadBaseline(e.db)
	if !ok {
		// US3: backfill a baseline for a pre-H11 corpus (embeddings present, no
		// baseline yet) from the stored majority profile + the live Ollama
		// version — no re-ingestion (FR-007). An empty corpus has nothing to
		// backfill, so the verdict stays n/a.
		if prof := CorpusProfile(e.db); prof.Total > 0 {
			base = &CorpusBaseline{
				Model:         prof.MajorityModel,
				Dim:           prof.MajorityDim,
				Convention:    prof.MajorityConvention,
				OllamaVersion: v.LiveVersion,
			}
			if err := SaveBaseline(e.db, base); err != nil {
				return v // couldn't persist; n/a rather than crash
			}
		} else {
			return v // empty corpus → n/a
		}
	}
	v.BaselineModel = base.Model
	v.BaselineDim = base.Dim
	v.BaselineConvention = base.Convention
	v.BaselineVersion = base.OllamaVersion

	// Hard drift: model / dim / convention mismatch. (Dim is skipped when the
	// live dim is unknown — 0 — which it is until the embedder's first response;
	// the model check already catches a swap at boot.)
	var reasons []string
	if base.Model != "" && v.ConfiguredModel != "" && base.Model != v.ConfiguredModel {
		reasons = append(reasons, fmt.Sprintf("model: %s vs %s", base.Model, v.ConfiguredModel))
	}
	if base.Dim != 0 && v.LiveDim != 0 && base.Dim != v.LiveDim {
		reasons = append(reasons, fmt.Sprintf("dim: %d vs %d", base.Dim, v.LiveDim))
	}
	if base.Convention != v.LiveConvention {
		reasons = append(reasons, fmt.Sprintf("convention: %q vs %q", base.Convention, v.LiveConvention))
	}
	if len(reasons) > 0 {
		v.Verdict = VerdictHardDrift
		v.Hard = true
		v.Reasons = reasons
		return v
	}

	// Soft drift (US2): Ollama-server version change — full-string compare.
	// Skipped when either side is unknown/empty (offline/injected embedder or
	// unreachable Ollama). Hard drift already returned above, so reaching here
	// means model/dim/convention all match — hard-wins-over-soft by structure.
	if knownVersion(v.BaselineVersion) && knownVersion(v.LiveVersion) &&
		v.BaselineVersion != v.LiveVersion {
		v.Verdict = VerdictVersionWarning
		v.Hard = false
		v.Reasons = append(v.Reasons,
			fmt.Sprintf("ollama-version: %s vs %s", v.BaselineVersion, v.LiveVersion))
		return v
	}

	// If a comparison was attempted but the live version was unknown (Ollama
	// unreachable while a baseline exists), surface that distinctly; otherwise
	// the profile match is clean.
	if v.LiveVersion == "unknown" && v.BaselineVersion != "" {
		v.Verdict = VerdictUnknown
	}
	if v.Verdict == VerdictNA {
		v.Verdict = VerdictClean
	}
	return v
}

// knownVersion reports whether a version string is a real, comparable value
// (not "" for offline/injected embedders, nor "unknown" for unreachable Ollama).
func knownVersion(s string) bool { return s != "" && s != "unknown" }
