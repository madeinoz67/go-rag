package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/auth"
)

// apiKeyView — the metadata shape for list/view (spec 057). NEVER carries the
// raw secret: the secret is not persisted (only its SHA-256[:16] hash is), so it
// can only ever leave the process via createAPIKeyResponse below. Revoked keys
// appear with enabled=false (RevokeAPIKey disables, it does not delete — keeping
// the audit trail).
type apiKeyView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Mode      string `json:"mode"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"` // "" when non-expiring
	Enabled   bool   `json:"enabled"`
}

// createAPIKeyResponse — the POST response: the metadata + the raw secret shown
// EXACTLY once (the full gorag_<id8>.<secret> display string from CreateAPIKey).
// This is the only response shape that carries a secret (FR-003).
type createAPIKeyResponse struct {
	apiKeyView
	Secret string `json:"secret"`
}

func toAPIKeyView(k auth.APIKey) apiKeyView {
	expires := ""
	if k.ExpiresAt != nil {
		expires = k.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return apiKeyView{
		ID:        k.ID,
		Label:     k.Label,
		Mode:      k.Mode,
		CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: expires,
		Enabled:   k.Enabled,
	}
}

// handleAPIKeysList — GET /api/settings/auth/api-keys. Metadata only; no secret.
func (s *Server) handleAPIKeysList(w http.ResponseWriter, _ *http.Request) {
	keys, err := auth.ListAPIKeys(s.store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	views := make([]apiKeyView, 0, len(keys))
	for _, k := range keys {
		views = append(views, toAPIKeyView(k))
	}
	writeJSON(w, http.StatusOK, views)
}

// handleAPIKeyCreate — POST /api/settings/auth/api-keys. Returns the secret ONCE.
// Slice 2a creates non-expiring keys (expiresAt nil); expiry-on-create is deferred.
func (s *Server) handleAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
		Mode  string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label required")
		return
	}
	if !validAPIKeyMode(req.Mode) {
		writeError(w, http.StatusBadRequest, "mode must be read|write|admin")
		return
	}
	display, key, err := auth.CreateAPIKey(s.store, req.Label, req.Mode, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResponse{apiKeyView: toAPIKeyView(key), Secret: display})
}

// handleAPIKeyRevoke — DELETE /api/settings/auth/api-keys/{id}. Disables the key
// (Enabled=false); the revoked bearer then fails ValidateAPIKey immediately.
func (s *Server) handleAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := auth.RevokeAPIKey(s.store, id); err != nil {
		if err == auth.ErrUnknownAPIKey {
			writeError(w, http.StatusNotFound, "unknown api key")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validAPIKeyMode(m string) bool {
	switch m {
	case "read", "write", "admin":
		return true
	}
	return false
}
