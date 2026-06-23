package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ollamaVersion fetches the Ollama server version from {baseURL}/api/version.
//
// It is a property of the Ollama *server*, not of the Embedder interface (whose
// contract is Embed/Model/Dimensions), so it lives here as a free function over
// the configured base URL rather than a method on Embedder (constitution
// Principle V: don't expand the interface needlessly).
//
// Return conventions (so a boot never fails on a version-fetch hiccup):
//   - empty baseURL (injected/offline embedder) → ""  (caller skips the version comparison)
//   - unreachable / non-2xx / parse error        → "unknown" (nil error; caller skips with a note)
//   - success                                     → the version string
//
// Short timeout so a down Ollama never blocks boot (mirrors embedderReachable).
func ollamaVersion(ctx context.Context, baseURL string) string {
	if baseURL == "" {
		return ""
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return "unknown"
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "unknown"
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "unknown"
	}
	if out.Version == "" {
		return "unknown"
	}
	return out.Version
}
