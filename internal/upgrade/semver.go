package upgrade

import (
	"strconv"
	"strings"
)

// parseSemver parses "vX.Y.Z" or "X.Y.Z" into (major, minor, patch). Pre-release
// ("-alpha") and build ("+build") suffixes are stripped. Returns ok=false if the
// string is not a clean three-part numeric version. Ported from MuninnDB's
// cmd/muninn/upgrade.go parseSemver.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, false
	}
	if patch, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, false
	}
	return major, minor, patch, true
}

// NewerVersionAvailable reports whether latest is strictly greater than current
// (both "vX.Y.Z"). Returns false on any parse failure to avoid false-positive
// upgrades, and false when current is empty or "dev" (dev build — check disabled).
func NewerVersionAvailable(current, latest string) bool {
	if current == "" || latest == "" || current == "dev" {
		return false
	}
	cMaj, cMin, cPat, ok1 := parseSemver(current)
	lMaj, lMin, lPat, ok2 := parseSemver(latest)
	if !ok1 || !ok2 {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}
