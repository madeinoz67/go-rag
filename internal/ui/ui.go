// Package ui is the management-console transport adapter for go-rag (spec 046).
// It is a fourth loopback HTTP transport, a peer to internal/rest, internal/grpc,
// and internal/mcp, served on --ui-addr (default 127.0.0.1:7881). It embeds a
// vendored SPA (Alpine.js + hand-written CSS, distributed via go:embed — no
// Node/Vite/Tailwind build) and projects internal/engine exactly as the other
// transports do. Auth is the spec 045 Bearer-session system: every route
// delegates to auth.Validate, so a credential works identically across all four
// transports and the loopback bypass on a bare vault behaves the same here.
//
// Slice 0 ships the shell, the auth flow, and one real view (Dashboard). The
// other seven sidebar views render placeholder panels — each becomes its own
// spec later. The UI makes no independent auth or business-logic decision.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/engine"
)

// webFS holds the embedded SPA tree (templates + static css/js/vendor). go:embed
// is the codebase's first embed.FS use; the prior precedent (internal/rest
// openapi.go) is the byte-slice pattern. Vendored libs are pinned in
// web/static/vendor/VERSIONS.txt.
//
//go:embed web
var webFS embed.FS

// Server is the UI transport adapter over the engine facade. It mirrors
// rest.Server: an engine, the legacy daemon token, and a spec 045 auth store
// built from eng.DB().
type Server struct {
	eng   *engine.Engine
	token string
	store *auth.Store
}

// New returns a UI adapter backed by eng. The auth store is built from eng.DB()
// when present so the spec 045 /login flow can verify the admin password and
// mint gorags_ sessions. Empty token = auth disabled (local/test only).
func New(eng *engine.Engine, token string) *Server {
	s := &Server{eng: eng, token: token}
	if eng != nil {
		if db := eng.DB(); db != nil {
			s.store = auth.NewStore(db)
		}
	}
	return s
}

// Handler returns the http.Handler serving the console (Go 1.22+ pattern mux).
// Static assets and /login are public (the login page must render pre-auth);
// every other route is bearer-guarded via auth.Validate.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Public.
	mux.Handle("GET /static/", noCache(http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(webFS, "web/static"))))))
	mux.HandleFunc("POST /login", s.handleLogin)
	// The shell is served publicly: it is the login page (no data lives in the
	// HTML — the Alpine gate shows login vs app client-side based on whether a
	// token is held). Guarding it would return 401 JSON on an initialized vault
	// and the browser could never load the form. All data is behind guarded
	// /api/* routes, which 401 until the operator authenticates.
	mux.HandleFunc("GET /{$}", s.handleShell)
	mux.HandleFunc("GET /api/dashboard/stats", s.guard(s.handleDashboardStats))
	mux.HandleFunc("GET /api/documents", s.guard(s.handleDocumentsList))                              // spec 047 US1
	mux.HandleFunc("GET /api/documents/search", s.guard(s.handleDocumentsSearch))                     // spec 047 US3
	mux.HandleFunc("POST /api/documents", s.guard(s.handleDocumentAdd))                               // spec 050 US1 (write)
	mux.HandleFunc("DELETE /api/documents/{id}", s.guard(s.handleDocumentRemove))                     // spec 050 US2 (write)
	mux.HandleFunc("POST /api/documents/{id}/reingest", s.guard(s.handleDocumentReingest))            // spec 050 US3 (write)
	mux.HandleFunc("GET /api/documents/{id}", s.guard(s.handleDocumentDetail))                        // spec 047 US2
	mux.HandleFunc("GET /api/documents/{id}/chunks", s.guard(s.handleDocumentChunks))                 // spec 047 US2
	mux.HandleFunc("GET /api/documents/{id}/chunks/{chunkID}/context", s.guard(s.handleChunkContext)) // spec 047 US2
	mux.HandleFunc("POST /api/query", s.guard(s.handleQuery))                                         // spec 048
	mux.HandleFunc("GET /api/bridge-ops/stats", s.guard(s.handleBridgeOpsStats))                      // spec 049
	mux.HandleFunc("GET /api/bridge-ops/activity", s.guard(s.handleBridgeOpsActivity))                // spec 049
	mux.HandleFunc("GET /api/observability/metrics", s.guard(s.handleObservabilityMetrics))           // spec 054 US1
	mux.HandleFunc("GET /api/observability/audit", s.guard(s.handleObservabilityAudit))               // spec 054 US2
	mux.HandleFunc("GET /api/quarantine/list", s.guard(s.handleQuarantineList))                       // spec 053 US1
	mux.HandleFunc("GET /api/quarantine/{id}/detail", s.guard(s.handleQuarantineDetail))              // spec 053 US2
	mux.HandleFunc("POST /api/quarantine/{id}/release", s.guard(s.handleQuarantineRelease))           // spec 053 US3
	mux.HandleFunc("POST /api/quarantine/{id}/reset", s.guard(s.handleQuarantineReset))               // spec 053 US3
	mux.HandleFunc("POST /api/quarantine/rescan", s.guard(s.handleQuarantineRescan))                  // spec 053 US3
	mux.HandleFunc("GET /api/vaults", s.guard(s.handleVaultsList))                                    // spec 051 US1
	mux.HandleFunc("POST /api/vaults", s.guard(s.handleVaultCreate))                                  // spec 051 US2
	mux.HandleFunc("POST /api/vaults/{name}/rename", s.guard(s.handleVaultRename))                    // spec 051 US4
	mux.HandleFunc("POST /api/vaults/{name}/clear", s.guard(s.handleVaultClear))                      // spec 051 US5
	mux.HandleFunc("DELETE /api/vaults/{name}", s.guard(s.handleVaultDelete))                         // spec 051 US5
	mux.HandleFunc("GET /api/placeholder/{view}", s.guard(s.handlePlaceholder))
	mux.HandleFunc("POST /logout", s.guard(s.handleLogout))
	return mux
}

// principalCtxKey is the context key under which guard stores the authenticated
// Principal (same shape as rest.principalCtxKey; the Principal type is shared
// via internal/auth).
type principalCtxKey struct{}

func principalFromContext(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(auth.Principal)
	return p, ok
}

// guard wraps a handler with unified bearer auth (spec 045 US2). It delegates to
// auth.Validate so the loopback bypass + every credential work identically to
// REST/gRPC/MCP and a revoke propagates here too. 401 + audit on failure; the UI
// never emits Set-Cookie (Bearer-in-header only — CSRF-free).
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil { // test-only: engine without a backing DB
			h(w, r)
			return
		}
		p, err := s.store.Validate(r)
		if err != nil {
			audit.Log(audit.AuthFailEvent("ui", "missing or invalid bearer"))
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), principalCtxKey{}, p)))
	}
}

// handleShell serves the SPA at exactly GET /. The same index.html is returned
// whether or not the caller is authenticated — the Alpine auth-gate decides
// client-side which view (login vs app) to render from whether a token is held.
// The guard has already admitted the request (valid credential, or loopback
// bypass on a bare vault), so reaching here means the operator may see the shell.
func (s *Server) handleShell(w http.ResponseWriter, _ *http.Request) {
	b, err := webFS.ReadFile("web/templates/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "shell missing")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// --- spec 045 auth surface (lifted from internal/rest/auth.go, "ui" transport) ---
//
// Contract invariant: NO handler on this surface ever emits Set-Cookie (Bearer
// only). TestAuth_NoSetCookieEver pins it for REST; the UI holds the same line.

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "auth unavailable")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// VerifyPassword collapses "no such user" and "bad password" into one timing
	// envelope (timing-neutral decoy hash) — surface an identical 401, never a
	// status-code oracle.
	if _, err := auth.VerifyPassword(s.store, req.Username, req.Password); err != nil {
		audit.Log(audit.AuthFailEvent("ui", "bad login"))
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	username := req.Username
	if username == "" {
		username = auth.DefaultAdminUsername
	}
	tok, sess, err := auth.MintSession(s.store, username, peerIP(r), auth.DefaultSessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not mint session")
		return
	}
	audit.Log(audit.AuthEvent("login", username, "ui", nil))
	writeJSON(w, http.StatusOK, loginResponse{Token: tok, ExpiresAt: sess.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if p, ok := principalFromContext(r.Context()); ok {
		if p.Source == auth.SourceSession {
			if raw := bearerOf(r); raw != "" {
				_ = auth.RevokeSession(s.store, raw)
			}
		}
		audit.Log(audit.AuthEvent("logout", p.Subject, "ui", nil))
	}
	w.WriteHeader(http.StatusNoContent)
}

// noCache wraps a handler so the browser always revalidates the response.
// Embedded console assets (static + shell) change with each binary but are
// served at stable URLs — without this header the browser serves a stale
// cached copy after a daemon restart (spec 053: stale CSS hid the matched-
// phrase highlight rule until a hard refresh).
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		h.ServeHTTP(w, r)
	})
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

// vaultHeader is the request-scoped vault selector. The web client pins it on
// every /api/* fetch so the handler targets the operator's chosen vault.
const vaultHeader = "X-Go-Rag-Vault"

// vaultFromRequest resolves the target vault for a request. The operator may
// pin it per-request via the X-Go-Rag-Vault header or the ?vault= query param
// (header wins when both are present). Absent/empty → "default".
func vaultFromRequest(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(vaultHeader)); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("vault")); v != "" {
		return v
	}
	return "default"
}

// bearerOf extracts the raw bearer token from the Authorization header (best
// effort — used to identify the caller's own session for logout).
func bearerOf(r *http.Request) string {
	v := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(v) < len(p) || !strings.EqualFold(v[:len(p)], p) {
		return ""
	}
	return strings.TrimSpace(v[len(p):])
}

// peerIP strips the port from r.RemoteAddr for the audit/created-ip trail.
func peerIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

// mustSub returns a sub-FS rooted at dir, panicking on failure. embed contents
// are fixed at compile time, so a failure here is a build-tree bug.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("ui: embed sub " + dir + ": " + err.Error())
	}
	return sub
}
