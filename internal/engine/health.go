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
	OK                bool // process alive and storage open
	StorageOpen       bool
	EmbedderReachable bool
}

// Health reports the engine's liveness/readiness. The embedder probe uses a
// short timeout so a down Ollama never makes the health endpoint hang (a
// refused loopback connection returns immediately).
func (e *Engine) Health(ctx context.Context) HealthInfo {
	storageOpen := e.db != nil
	return HealthInfo{
		OK:                storageOpen,
		StorageOpen:       storageOpen,
		EmbedderReachable: e.embedderReachable(ctx),
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
