package engine

import (
	"context"
	"net/http"
	"time"
)

// HealthInfo is the unified liveness/readiness view served by the REST /health
// endpoint and the gRPC Health RPC. Both adapters call Engine.Health so they
// report identical status (parity).
type HealthInfo struct {
	OK                bool // process alive and storage open (liveness)
	StorageOpen       bool
	EmbedderReachable bool

	// Ready (audit H11/spec 017) is readiness — distinct from OK (liveness).
	// false when there is hard embedding drift (model/dim/convention mismatch),
	// so clients/orchestrators do not route query traffic; the process stays up
	// (OK) so the operator can run status/migrate in place. Read from the cached
	// boot verdict (no per-probe fetch).
	Ready        bool
	DriftVerdict string
}

// Health reports the engine's liveness/readiness. The embedder probe uses a
// short timeout so a down Ollama never makes the health endpoint hang (a
// refused loopback connection returns immediately). Readiness reads the cached
// drift verdict (FR-011); call RefreshDriftVerdict at boot / after migrate to
// keep it current.
func (e *Engine) Health(ctx context.Context) HealthInfo {
	storageOpen := e.db != nil
	v := e.currentVerdict()
	return HealthInfo{
		OK:                storageOpen,
		StorageOpen:       storageOpen,
		EmbedderReachable: e.embedderReachable(ctx),
		Ready:             storageOpen && !v.Hard,
		DriftVerdict:      v.Verdict,
	}
}

// embedderReachable pings the configured Ollama base URL once with a short
// timeout. A missing URL or any request/transport error counts as unreachable;
// any non-5xx response counts as reachable.
func (e *Engine) embedderReachable(ctx context.Context) bool {
	if e.cfg.OllamaURL == "" {
		return false
	}
	c, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, e.cfg.OllamaURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < http.StatusInternalServerError
}
