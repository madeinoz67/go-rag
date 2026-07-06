package auth

import (
	"errors"
	"net/http"
	"strings"
)

// Mode constants define a credential's scope. Handlers enforce Mode at the
// engine/handler layer (read-only keys cannot ingest; write-only keys cannot
// query; admin can manage tokens).
const (
	ModeRead  = "read"  // queries only
	ModeWrite = "write" // ingest + queries
	ModeAdmin = "admin" // full access, including token management
)

// Source constants describe how a request was authenticated. Carried on
// Principal.Source for audit and for the loopback-bypass path (spec 045 US5).
const (
	SourceAPIKey  = "apikey"  // a gorag_ API key
	SourceSession = "session" // a gorags_ session token (UI login)
	SourceBypass  = "bypass"  // loopback peer + empty credential stores
)

// Principal is the authenticated caller. Validate returns it; transport
// middleware puts it in context.Context for handlers to enforce Mode.
type Principal struct {
	Subject string // APIKey.ID (e.g. "gorag_ab12cd34") or AdminUser.Username
	Mode    string // ModeRead | ModeWrite | ModeAdmin
	Source  string // SourceAPIKey | SourceSession | SourceBypass
}

// Maximum accepted bearer length (DoS guard — the contract's 4096-byte cap).
const maxBearerLen = 4096

// Errors from Validate. Transports collapse all of these into an identical 401
// + audit.AuthFailEvent so a probe cannot distinguish the failure reason.
var (
	ErrNoCredential    = errors.New("no bearer credential")
	ErrUnknownCredential = errors.New("invalid bearer credential")
)

// ValidateToken is the transport-agnostic core. It dispatches on the credential
// prefix to the right validator:
//
//	gorags_ → Session (admin login)   → Principal{Mode: admin, Source: session}
//	gorag_  → APIKey                   → Principal{Mode: <key.Mode>, Source: apikey}
//
// gorags_ MUST be tested before gorag_ — "gorags_" starts with "gorag_", so a
// longest-first match is required or every session token is misrouted. Returns
// ErrUnknownCredential (or ErrNoCredential for an empty token) on any failure.
func (s *Store) ValidateToken(token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrNoCredential
	}
	if len(token) > maxBearerLen {
		return Principal{}, ErrUnknownCredential
	}
	switch {
	case strings.HasPrefix(token, sessionPrefix):
		sess, err := ValidateSession(s, token)
		if err != nil {
			return Principal{}, ErrUnknownCredential
		}
		return Principal{Subject: sess.User, Mode: ModeAdmin, Source: SourceSession}, nil
	case strings.HasPrefix(token, apikeyPrefix):
		key, err := ValidateAPIKey(s, token)
		if err != nil {
			return Principal{}, ErrUnknownCredential
		}
		return Principal{Subject: key.ID, Mode: key.Mode, Source: SourceAPIKey}, nil
	}
	return Principal{}, ErrUnknownCredential
}

// Validate parses the Bearer credential off r's Authorization header and
// resolves it to a Principal. It performs NO body parsing (DoS guard). On
// failure the caller MUST emit audit.AuthFailEvent(transport, detail) and
// return 401. The loopback-bypass path (spec 045 US5) is added in Phase 8;
// until then Validate is fail-closed: no Bearer → ErrNoCredential.
func (s *Store) Validate(r *http.Request) (Principal, error) {
	return s.ValidateToken(bearerFromHeader(r))
}

// bearerFromHeader extracts the opaque token from the Authorization header
// (case-insensitive scheme). Returns "" when absent or malformed.
func bearerFromHeader(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const scheme = "Bearer "
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(h[len(scheme):])
}
