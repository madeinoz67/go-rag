package auth

import (
	"net"
	"net/http"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// bypass.go implements the spec 045 US5 loopback bypass: a request from the
// loopback peer when NO credentials have ever been minted authenticates as a
// bypass Principal (local "just works", matching go-rag's git-like friction
// target). The moment any credential exists — or the peer is non-loopback —
// the bypass is off and the surface is fail-closed.
//
// Why empty-stores rather than empty-config: the operator minting their first
// API key or bootstrapping the admin is the unambiguous signal that they intend
// to enforce auth. Before that signal, blocking loopback would only punish the
// local quickstart. After it, even loopback must present a credential (an
// exposed port forwarded from elsewhere must not silently bypass).

// storesEmpty reports whether NO credential has ever been minted — the
// necessary condition for the loopback bypass (spec 045 US5). BOTH an admin
// user AND an API key count as "armed": the admin is created at `init`, so a
// runnable vault always has one, which means the bypass only ever applies to a
// truly bare vault (the direct-local fresh start before any bootstrap). This
// deliberately NARROWS the bypass window to nothing in practice — and that is
// the point: the loopback peer cannot identify the principal (the proxy in
// front of go-rag, the operator's browser via fetch(), or local malware are all
// loopback), so the bypass must not fire on any vault that has been initialized
// for use. Revoking all API keys cannot re-arm it because the admin persists.
//
// Fail-closed on a Pebble read error: treat "we could not tell" as NOT empty,
// so a storage fault does not silently arm the bypass.
func (s *Store) storesEmpty() bool {
	if s == nil || s.db == nil {
		return true
	}
	if has, err := anyKey(s, storage.PrefixAuthAPIKey); err != nil || has {
		return false
	}
	if has, err := anyKey(s, storage.PrefixAuthAdmin); err != nil || has {
		return false
	}
	return true
}

// anyKey reports whether at least one record exists under prefix (stops at the
// first hit — O(1) in the common case, not a full scan).
func anyKey(s *Store, prefix byte) (bool, error) {
	var found bool
	err := s.db.PrefixScanByte(prefix, func(_, _ []byte) bool {
		found = true
		return false // stop at first
	})
	return found, err
}

// isLoopback reports whether the request originated on the local machine.
func isLoopback(r *http.Request) bool {
	host := peerHost(r.RemoteAddr)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// peerHost strips the port from an addr as it appears in RemoteAddr (host:port
// or [::1]:port), returning the bare host.
func peerHost(addr string) string {
	// IPv6 bracketed: [::1]:1234
	if addr != "" && addr[0] == '[' {
		if i := lastIndex(addr, ']'); i >= 0 {
			return addr[1:i]
		}
	}
	if i := lastIndex(addr, ':'); i >= 0 {
		// Only strip when there's a single colon (host:port). IPv6 without
		// brackets has multiple colons and is handled by net.ParseIP on the
		// full string in the caller — leave it intact here.
		if countByte(addr, ':') == 1 {
			return addr[:i]
		}
	}
	return addr
}

// bypassPrincipal is the Principal returned on a successful bypass.
func bypassPrincipal() Principal {
	return Principal{Subject: "loopback", Mode: ModeAdmin, Source: SourceBypass}
}

func lastIndex(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func countByte(s string, b byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			n++
		}
	}
	return n
}
