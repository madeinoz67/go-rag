package rest

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
)

// auth.go is the spec 045 (US3) REST auth surface, mounted at /api/auth/* on
// the REST transport (:7879 now; spec 046 remounts on :7881 for the UI).
//
//	POST   /api/auth/login            {username,password} → {token,expires_at}
//	POST   /api/auth/logout           revokes the calling session
//	GET    /api/auth/session          lists active sessions            (admin)
//	DELETE /api/auth/session/{hash}   revokes a session by hash        (admin)
//
// Contract invariant: NO handler on this surface ever emits a Set-Cookie header
// (Bearer-in-header only — the SPA stores the opaque token in sessionStorage,
// keeping the surface CSRF-free). TestAuth_NoSetCookieEver pins this.

// loginRequest is the body of POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is returned exactly once at login. Token is the opaque
// gorags_<…> string the client must store; it is never persisted server-side.
type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// sessionOut is one row of GET /api/auth/session. The token itself is absent;
// Hash is hex(TokenHash) — the handle DELETE /api/auth/session/{hash} takes.
type sessionOut struct {
	Hash      string `json:"hash"`
	User      string `json:"user"`
	ExpiresAt string `json:"expires_at"`
	LastSeen  string `json:"last_seen"`
	IP        string `json:"ip"`
}

// mountAuth registers the /api/auth/* surface. login is public; logout requires
// any valid credential; session list/revoke require admin mode. No-op when the
// server has no credential store (engine without a DB — tests only).
func (s *Server) mountAuth(mux *http.ServeMux) {
	if s.store == nil {
		return
	}
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.withPrincipal(s.handleLogout))
	mux.HandleFunc("GET /api/auth/session", s.withAdmin(s.handleSessionList))
	mux.HandleFunc("DELETE /api/auth/session/{hash}", s.withAdmin(s.handleSessionRevoke))
}

// withPrincipal wraps a handler that needs an authenticated caller. Uses
// ValidateStrict (no loopback bypass) — the /api/auth/* management surface
// requires a real credential even locally. The Principal is passed to the inner
// handler. 401 + audit on any failure.
func (s *Server) withPrincipal(h func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.store.ValidateStrict(r)
		if err != nil {
			audit.Log(audit.AuthFailEvent("rest", "missing or invalid bearer"))
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r, p)
	}
}

// withAdmin is withPrincipal plus an admin-mode check (an admin API key or any
// session — sessions are always admin). Non-admin collapses to 401, NOT 403,
// so a probe cannot confirm a candidate credential is recognized-but-low-scope
// via a status-code oracle (spec 045 red-team finding LOW).
func (s *Server) withAdmin(h func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return s.withPrincipal(func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		if p.Mode != auth.ModeAdmin {
			audit.Log(audit.AuthFailEvent("rest", "non-admin attempted admin op"))
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r, p)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	// VerifyPassword collapses "no such user" and "bad password" into the same
	// timing envelope (timing-neutral decoy hash) — surface an identical 401.
	if _, err := auth.VerifyPassword(s.store, req.Username, req.Password); err != nil {
		audit.Log(audit.AuthFailEvent("rest", "bad login"))
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
	audit.Log(audit.AuthEvent("login", username, "rest", nil))
	writeJSON(w, http.StatusOK, loginResponse{Token: tok, ExpiresAt: sess.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	// Only a session has something to revoke; an API-key caller has no session
	// record — return 204 either way (idempotent).
	if p.Source == auth.SourceSession {
		if raw := bearerOf(r); raw != "" {
			_ = auth.RevokeSession(s.store, raw)
		}
	}
	audit.Log(audit.AuthEvent("logout", p.Subject, "rest", nil))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionList(w http.ResponseWriter, _ *http.Request, _ auth.Principal) {
	list, err := auth.ListSessions(s.store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sessionOut, 0, len(list))
	for _, sess := range list {
		out = append(out, sessionOut{
			Hash:      hex.EncodeToString(sess.TokenHash),
			User:      sess.User,
			ExpiresAt: sess.ExpiresAt.UTC().Format(time.RFC3339),
			LastSeen:  sess.LastSeen.UTC().Format(time.RFC3339),
			IP:        sess.CreatedIP,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request, _ auth.Principal) {
	hashHex := r.PathValue("hash")
	hash, err := hex.DecodeString(hashHex)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad hash encoding")
		return
	}
	if err := auth.RevokeSessionByHash(s.store, hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bearerOf extracts the raw bearer token from the Authorization header (best
// effort — used only to identify the caller's own session for logout).
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
