package ui

// memory_graph.go (spec 060 US3) is the console's Memory & Graph view — a
// read-only projection of the bridged MuninnDB vault (the engrams go-rag
// promoted). It retires the last placeholder. UI-only: no new engine capability
// beyond Engine.Bridge(); the handlers read through the bridge's Client.
//
// Routes (all spec 045 Bearer-guarded):
//
//	GET  /api/memory-graph/browse            Activate-driven engram list (US3)
//	GET  /api/memory-graph/engrams/{id}      one engram's detail (US3)
//	GET  /api/memory-graph/status            bridge health + promotion/backfill stats
//	POST /api/memory-graph/backfill/{action} pause | resume (FR-014)
//
// When the bridge is disabled or MuninnDB is unreachable, every route returns
// 200 with a `degraded: true` marker (never a 5xx crash) so the view renders a
// clean empty/degraded state. No plaintext key ever appears in any response.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/bridge/muninn"
)

// browseTimeout / detailTimeout bound MuninnDB reads so a slow/down server never
// stalls the console.
const (
	browseTimeout = 10 * time.Second
	detailTimeout = 10 * time.Second
)

// memoryGraphBrowseRow is one row of the Activate browse list.
type memoryGraphBrowseRow struct {
	EngramID   string   `json:"engram_id"`
	Concept    string   `json:"concept"`
	Score      float32  `json:"score"`
	Tags       []string `json:"tags,omitempty"`
	LastAccess int64    `json:"last_access,omitempty"`
}

type memoryGraphBrowseResponse struct {
	Vault    string                 `json:"vault"`
	Rows     []memoryGraphBrowseRow `json:"rows"`
	Degraded bool                   `json:"degraded"`
}

// handleMemoryGraphBrowse — GET /api/memory-graph/browse?q=<phrases>&limit=<n>.
// Streams an Activate recall over the target vault.
func (s *Server) handleMemoryGraphBrowse(w http.ResponseWriter, r *http.Request) {
	br := s.eng.Bridge()
	if br == nil {
		writeJSON(w, http.StatusOK, memoryGraphBrowseResponse{Degraded: true})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	var phrases []string
	if q != "" {
		phrases = strings.Fields(q)
	}

	ctx, cancel := context.WithTimeout(r.Context(), browseTimeout)
	defer cancel()
	stream, err := br.Browse(ctx, phrases, limit)
	if err != nil {
		writeJSON(w, http.StatusOK, memoryGraphBrowseResponse{Vault: br.TargetVault(), Degraded: true})
		return
	}
	rows := make([]memoryGraphBrowseRow, 0, limit)
	for a := range stream {
		rows = append(rows, memoryGraphBrowseRow{
			EngramID: a.EngramID, Concept: a.Concept, Score: a.Score,
			Tags: a.Tags, LastAccess: a.LastAccess,
		})
		if len(rows) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, memoryGraphBrowseResponse{Vault: br.TargetVault(), Rows: rows})
}

// memoryGraphEngramDetail is the GET /api/memory-graph/engrams/{id} payload.
type memoryGraphEngramDetail struct {
	ID          string   `json:"id"`
	Concept     string   `json:"concept"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
	AccessCount int64    `json:"access_count"`
	Stability   float32  `json:"stability"`
	State       string   `json:"state"`
	CreatedAt   int64    `json:"created_at"`
}

// handleMemoryGraphEngram — GET /api/memory-graph/engrams/{id}. 404 if the
// engram is absent, 503 (degraded) if MuninnDB is unreachable.
func (s *Server) handleMemoryGraphEngram(w http.ResponseWriter, r *http.Request) {
	br := s.eng.Bridge()
	id := r.PathValue("id")
	if br == nil || id == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), detailTimeout)
	defer cancel()
	e, err := br.ReadEngram(ctx, id)
	if err != nil {
		// NotFound (a stale id) vs unavailable (MuninnDB down) — distinguish by
		// the error text so a deleted engram renders 404, not a degraded 503.
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, memoryGraphEngramDetail{})
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, memoryGraphEngramDetail{
		ID: e.ID, Concept: e.Concept, Content: e.Content, Tags: e.Tags,
		AccessCount: e.AccessCount, Stability: e.Stability, State: e.State,
		CreatedAt: e.CreatedAt,
	})
}

// handleMemoryGraphStatus — GET /api/memory-graph/status. The bridge health +
// promotion/backfill snapshot (FR-017). When disabled, returns a minimal
// disabled marker.
func (s *Server) handleMemoryGraphStatus(w http.ResponseWriter, _ *http.Request) {
	br := s.eng.Bridge()
	if br == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "degraded": true})
		return
	}
	writeJSON(w, http.StatusOK, memoryGraphStatusDTO(br.Status()))
}

// memoryGraphStatusDTO projects muninn.BridgeStatus → JSON (no key ever leaks).
func memoryGraphStatusDTO(st muninn.BridgeStatus) map[string]any {
	return map[string]any{
		"enabled":      st.Enabled,
		"healthy":      st.Healthy,
		"endpoint":     st.Endpoint,
		"source_vault": st.SourceVault,
		"target_vault": st.TargetVault,
		"promoted":     st.Promoted,
		"skipped":      st.Skipped,
		"failed":       st.Failed,
		"circuit_open": st.CircuitOpen,
		"backfill":     st.Backfill,
	}
}

// handleMemoryGraphBackfillAction — POST /api/memory-graph/backfill/{action}
// where action ∈ {pause, resume} (FR-014). 404 on an unknown action; 409 if the
// bridge is disabled.
func (s *Server) handleMemoryGraphBackfillAction(w http.ResponseWriter, r *http.Request) {
	// Validate the action first — an unknown action is a 404 regardless of whether
	// the bridge is enabled (it's a routing-level invalidity, like a bad path).
	action := r.PathValue("action")
	if action != "pause" && action != "resume" {
		writeError(w, http.StatusNotFound, "unknown action")
		return
	}
	br := s.eng.Bridge()
	if br == nil {
		writeError(w, http.StatusConflict, "bridge disabled")
		return
	}
	if action == "pause" {
		br.Pause()
	} else {
		br.Resume()
	}
	writeJSON(w, http.StatusOK, memoryGraphStatusDTO(br.Status()))
}
