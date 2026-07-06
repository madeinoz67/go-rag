package mcp

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/auth"
)

// auth_tools.go renders the spec 045 (US2/R6) auth-management MCP tools. All
// are admin-gated in dispatchAuth. The text surface mirrors the CLI: ids and
// hashes only (never a persisted secret); the one-time secret of a freshly
// created key is returned in the create response and never stored.

func (s *Server) renderAuthList(store *auth.Store) (string, error) {
	keys, err := auth.ListAPIKeys(store)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "no api keys", nil
	}
	var b strings.Builder
	for _, k := range keys {
		expires := "never"
		if k.ExpiresAt != nil {
			expires = k.ExpiresAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "- %s label=%q mode=%s expires=%s enabled=%v\n", k.ID, k.Label, k.Mode, expires, k.Enabled)
	}
	return strings.TrimSpace(b.String()), nil
}

// renderAuthCreate mints a key and returns the full secret string once. The
// caller (agent or operator) MUST capture it now — it is never persisted nor
// re-displayable. mode defaults to read; expires (e.g. "720h") is optional.
func (s *Server) renderAuthCreate(store *auth.Store, args map[string]any) (string, error) {
	label, _ := args["label"].(string)
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = auth.ModeRead
	}
	var expiresAt *time.Time
	if d, ok := args["expires"].(string); ok && d != "" {
		dur, err := time.ParseDuration(d)
		if err != nil {
			return "", fmt.Errorf("expires: %w", err)
		}
		t := time.Now().UTC().Add(dur)
		expiresAt = &t
	}
	display, key, err := auth.CreateAPIKey(store, label, mode, expiresAt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("created %s label=%q mode=%s — secret (copy now, shown once): %s",
		key.ID, key.Label, key.Mode, display), nil
}

func (s *Server) renderAuthRevoke(store *auth.Store, args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return "", fmt.Errorf("id required")
	}
	if err := auth.RevokeAPIKey(store, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("revoked %s", id), nil
}

func (s *Server) renderAuthSessionList(store *auth.Store) (string, error) {
	list, err := auth.ListSessions(store)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no sessions", nil
	}
	var b strings.Builder
	for _, sess := range list {
		fmt.Fprintf(&b, "- %s user=%s expires=%s last_seen=%s ip=%s\n",
			hex.EncodeToString(sess.TokenHash), sess.User,
			sess.ExpiresAt.UTC().Format(time.RFC3339),
			sess.LastSeen.UTC().Format(time.RFC3339), sess.CreatedIP)
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderAuthSessionRevoke(store *auth.Store, args map[string]any) (string, error) {
	hashHex, _ := args["hash"].(string)
	if hashHex == "" {
		return "", fmt.Errorf("hash required")
	}
	hash, err := hex.DecodeString(hashHex)
	if err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	if err := auth.RevokeSessionByHash(store, hash); err != nil {
		return "", err
	}
	return fmt.Sprintf("revoked session %s", hashHex), nil
}
