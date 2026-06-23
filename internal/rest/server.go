// Package rest is the REST (HTTP/JSON) transport adapter for go-rag, built on
// the Go stdlib net/http ServeMux (zero added dependencies). Like the gRPC and
// MCP adapters, it is a thin projection of the shared internal/engine facade —
// adapters add no independent logic, so REST returns identical results to gRPC
// and MCP.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/observe"
)

// Server is the REST transport adapter over the engine facade.
type Server struct {
	eng   *engine.Engine
	token string // empty => auth disabled (local development)
}

// New returns a REST adapter backed by eng. token enables bearer auth (matching
// MCP/gRPC); empty disables it for trusted loopback use.
func New(eng *engine.Engine, token string) *Server { return &Server{eng: eng, token: token} }

// route is one REST route. routes is the authoritative route table: Handler
// wires each entry to its handler, and the OpenAPI parity test (T035) asserts
// this table matches the served spec exactly. Add a route here AND to
// openapi.yaml; the test catches drift in either direction.
type route struct {
	method string
	path   string
	auth   bool // true ⇒ bearer-guarded
}

var routes = []route{
	{"GET", "/health", false},
	{"GET", "/metrics", false}, // H17/spec 020: scraped Prometheus endpoint (unauth, loopback)
	{"GET", "/openapi.yaml", false},
	{"POST", "/v1/query", true},
	{"GET", "/v1/status", true},
	{"POST", "/v1/add", true},
	{"POST", "/v1/scan", true},
	{"POST", "/v1/reprocess", true},
	{"POST", "/v1/migrate", true},
	{"GET", "/v1/files", true},
	{"GET", "/v1/dirs", true},
	{"GET", "/v1/config", true},
	{"PUT", "/v1/config", true},
	{"GET", "/v1/vaults", true},
	{"GET", "/v1/poison", true},               // H04/spec 019: list flagged chunks
	{"POST", "/v1/poison/{id}/release", true}, // H04/spec 019: false-positive override
	{"POST", "/v1/poison/{id}/reset", true},   // H04/spec 019: undo a release
	{"POST", "/v1/poison/rescan", true},       // H04/spec 019: re-score the corpus
}

// Handler returns the http.Handler serving the REST API (Go 1.22+ pattern mux),
// built from the routes table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range routes {
		h := s.handlerFor(r.method, r.path)
		if h == nil {
			panic(fmt.Sprintf("rest: no handler wired for %s %s", r.method, r.path))
		}
		if r.auth {
			h = s.guard(h)
		}
		mux.HandleFunc(r.method+" "+r.path, h)
	}
	return mux
}

// handlerFor resolves a route table entry to its handler method.
func (s *Server) handlerFor(method, path string) http.HandlerFunc {
	switch method + " " + path {
	case "GET /health":
		return s.handleHealth
	case "GET /metrics":
		return s.handleMetrics
	case "GET /openapi.yaml":
		return s.handleOpenAPI
	case "POST /v1/query":
		return s.handleQuery
	case "GET /v1/status":
		return s.handleStatus
	case "POST /v1/add":
		return s.handleAdd
	case "POST /v1/scan":
		return s.handleScan
	case "POST /v1/reprocess":
		return s.handleReprocess
	case "POST /v1/migrate":
		return s.handleMigrate
	case "GET /v1/files":
		return s.handleFiles
	case "GET /v1/dirs":
		return s.handleDirs
	case "GET /v1/config":
		return s.handleConfigGet
	case "PUT /v1/config":
		return s.handleConfigSet
	case "GET /v1/vaults":
		return s.handleVaults
	case "GET /v1/poison":
		return s.handlePoisonList
	case "POST /v1/poison/{id}/release":
		return s.handlePoisonRelease
	case "POST /v1/poison/{id}/reset":
		return s.handlePoisonReset
	case "POST /v1/poison/rescan":
		return s.handlePoisonRescan
	}
	return nil
}

// guard wraps a handler with bearer-token auth (skipped when token is empty).
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && !checkBearer(r, s.token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r)
	}
}

// handleHealth is the unified liveness/readiness probe (GET /health). Reports
// liveness (ok) + readiness (ready) — ready is false on hard embedding drift
// (audit H11/spec 017), so clients/orchestrators reading the body do not route
// query traffic; ok stays true while the process is up. Identical to the gRPC
// Health RPC (both call engine.Health). Unauthenticated so probes don't need a
// token. HTTP stays 200 on drift (liveness) — readiness is in the body, not the
// status code, to avoid restart-loops if /health is used as a liveness probe.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := s.eng.Health(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 h.OK,
		"ready":              h.Ready,
		"storage_open":       h.StorageOpen,
		"embedder_reachable": h.EmbedderReachable,
		"drift_verdict":      h.DriftVerdict,
	})
}

// handleMetrics serves the scraped Prometheus /metrics endpoint (H17/spec 020).
// Unauthenticated + loopback-only (like /health); disabled when metrics_enabled=false.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.eng.Config().EffectiveMetricsEnabled() {
		http.NotFound(w, r)
		return
	}
	observe.MetricsHandler().ServeHTTP(w, r)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

// writeEngineErr maps an engine error to the right HTTP status: client-input
// errors (ErrInvalid) → 400, everything else (storage/index/embedder faults) →
// 500. nil is a no-op.
func writeEngineErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, engine.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// checkBearer reports whether the request carries the expected bearer token.
func checkBearer(r *http.Request, token string) bool {
	v := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(v, prefix) {
		return false
	}
	return strings.TrimSpace(v[len(prefix):]) == token
}
