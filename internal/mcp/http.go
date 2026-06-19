package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
)

// HTTPHandler returns an http.Handler serving MCP over HTTP (Streamable HTTP,
// request/response style):
//
//	POST /mcp         -> one JSON-RPC request; writes the JSON-RPC response
//	GET  /mcp/health  -> 200 "ok" (used by `go-rag start`'s startup poll)
//
// If token is non-empty, requests must carry `Authorization: Bearer <token>`.
// After `initialize`, the response carries an Mcp-Session-Id which clients
// (and the stdio proxy) echo back on subsequent requests.
func (s *Server) HTTPHandler(token string) http.Handler {
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
		if token != "" && !checkBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp := s.handle(req)
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

// checkBearer reports whether the request carries the expected bearer token.
func checkBearer(r *http.Request, token string) bool {
	v := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(v, prefix) {
		return false
	}
	return strings.TrimSpace(v[len(prefix):]) == token
}
