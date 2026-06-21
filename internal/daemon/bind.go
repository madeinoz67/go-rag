package daemon

import (
	"fmt"
	"net"
	"strings"
)

// lookupIP resolves a hostname to its IPs. It is a package-level variable so
// tests can stub it, keeping IsLoopbackBind hermetic (no real DNS in unit tests).
var lookupIP = net.LookupIP

// IsLoopbackBind reports whether addr is a loopback bind target.
//
// Loopback = IPv4 127.0.0.0/8, IPv6 ::1, or the "localhost" hostname (spec 007,
// FR-006). Every other target — the all-interfaces wildcard (bare ":port",
// "0.0.0.0", "[::]"), a LAN/public IP, or a non-loopback hostname — is external.
// The function is fail-safe: an unparseable address or an unresolvable hostname
// is treated as external (never silently treated as safe).
func IsLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false // unparseable → fail-safe (external)
	}
	if host == "" {
		return false // bare ":port" → all interfaces
	}
	// "localhost" special-case: avoids a DNS round-trip for the canonical name.
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() // covers all of 127.0.0.0/8 and ::1
	}
	// Hostname: resolve and check whether any returned IP is loopback. Fail-safe
	// on resolve error (treat as external).
	ips, err := lookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			return true
		}
	}
	return false
}

// BindEntry pairs a transport name with its listen address.
type BindEntry struct {
	Name string // "MCP", "REST", or "gRPC"
	Addr string // the listen address, e.g. "127.0.0.1:7878"
}

// enabledBinds returns the enabled (non-empty) transports in MCP, REST, gRPC
// order. A disabled transport (empty addr) is omitted — it opens no listener and
// is not subject to the bind check (FR-008).
func enabledBinds(addrs Addrs) []BindEntry {
	var out []BindEntry
	if addrs.MCPAddr != "" {
		out = append(out, BindEntry{Name: "MCP", Addr: addrs.MCPAddr})
	}
	if addrs.RESTAddr != "" {
		out = append(out, BindEntry{Name: "REST", Addr: addrs.RESTAddr})
	}
	if addrs.GRPCAddr != "" {
		out = append(out, BindEntry{Name: "gRPC", Addr: addrs.GRPCAddr})
	}
	return out
}

// NonLoopbackBinds returns the enabled transports whose addresses are not
// loopback. serve uses this to decide whether to print the exposure warning when
// --bind-external authorizes external binding (spec 007 FR-005).
func NonLoopbackBinds(addrs Addrs) []BindEntry {
	var offenders []BindEntry
	for _, e := range enabledBinds(addrs) {
		if !IsLoopbackBind(e.Addr) {
			offenders = append(offenders, e)
		}
	}
	return offenders
}

// ValidateBind enforces the loopback-by-default contract (spec 007 FR-001/003).
//
// Every enabled transport address MUST be loopback unless allowExternal is true.
// On violation it returns an error naming every offending transport and address,
// with the actionable hint to re-run with --bind-external. When allowExternal is
// true, or when every enabled address is loopback, it returns nil.
func ValidateBind(addrs Addrs, allowExternal bool) error {
	offenders := NonLoopbackBinds(addrs)
	if len(offenders) == 0 {
		return nil // nothing exposed
	}
	if allowExternal {
		return nil // external binding explicitly authorized
	}
	lines := make([]string, len(offenders))
	for i, e := range offenders {
		lines[i] = fmt.Sprintf("  %s %s", e.Name, e.Addr)
	}
	return fmt.Errorf(
		"refusing to bind non-loopback address(es) without --bind-external:\n%s\ngo-rag stays local-only by default. To expose it on purpose, re-run with --bind-external",
		strings.Join(lines, "\n"),
	)
}

// ExternalBindWarning returns the exposure warning text (spec 007 FR-005) when
// addrs has any non-loopback bind, or "" when nothing is exposed. serve prints
// it once at boot when --bind-external authorizes external binding. Keeping the
// text in a pure function makes the warning hermetically testable without
// binding a listener.
func ExternalBindWarning(addrs Addrs) string {
	offenders := NonLoopbackBinds(addrs)
	if len(offenders) == 0 {
		return ""
	}
	lines := make([]string, len(offenders))
	for i, e := range offenders {
		lines[i] = fmt.Sprintf("  %s %s", e.Name, e.Addr)
	}
	return fmt.Sprintf(
		"⚠ go-rag is bound to a non-loopback address. The document vault and all\n"+
			"  transports are reachable from other machines. Traffic is UNENCRYPTED (no TLS).\n"+
			"  Access control is your responsibility. Exposed transports:\n%s\n"+
			"  (allowed by --bind-external)",
		strings.Join(lines, "\n"),
	)
}
