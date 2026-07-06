package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
)

// HTTPHandler returns an http.Handler serving MCP over HTTP (Streamable HTTP,
// request/response style):
//
//	POST /mcp         -> one JSON-RPC request; writes the JSON-RPC response
//	GET  /mcp/health  -> 200 "ok" (used by `go-rag start`'s startup poll)
//
// Auth (spec 045 US2): when the server has a backing store (daemon mode), every
// request is validated through the unified auth.Validate — a gorag_ key or
// gorags_ session works identically here and on REST/gRPC, and the loopback
// bypass (US5) applies. The token parameter is retained for caller
// compatibility but is no longer authoritative; a pre-upgrade mcp.token is
// imported as a real key by the daemon's bootstrap path (US4). When no store is
// available (stdio/test mode), auth is disabled — matching the prior empty-token
// behaviour. After `initialize`, the response carries an Mcp-Session-Id which
// clients (and the stdio proxy) echo back on subsequent requests.
func (s *Server) HTTPHandler(token string) http.Handler {
	store := s.authStore()
	_ = token // retained for caller compatibility; auth is store-driven (spec 045)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if store != nil {
			if _, err := store.Validate(r); err != nil {
				audit.Log(audit.AuthFailEvent("mcp", "missing or invalid bearer"))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Resolve the Principal once here (Validate re-runs; the cost is one hash
		// lookup) so the admin-gated auth tools can enforce scope. Empty/zero on
		// the bypass path (admin) and when auth is disabled.
		var p auth.Principal
		if store != nil {
			if got, err := store.Validate(r); err == nil {
				p = got
			}
		}
		resp := s.handle(req, p)
		if resp == nil {
			// Notification (no id) — no response body per MCP.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Mint a session id on initialize so proxying clients can echo it.
		if req.Method == "initialize" && r.Header.Get("Mcp-Session-Id") == "" {
			w.Header().Set("Mcp-Session-Id", "gorag-session")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

// authStore builds the credential store from the server's backing engine or DB
// when present (daemon mode). Returns nil in stdio/test mode (New(dbPath) with
// no open DB), where auth is disabled.
func (s *Server) authStore() *auth.Store {
	if s.eng != nil {
		if db := s.eng.DB(); db != nil {
			return auth.NewStore(db)
		}
	}
	if s.db != nil {
		return auth.NewStore(s.db)
	}
	return nil
}
