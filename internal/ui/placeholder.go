package ui

import (
	"net/http"
	"strings"
)

// placeholderViews maps the seven non-Dashboard sidebar items (kebab-case) to
// the spec number that will eventually implement them. This is the seam: each
// later view-spec replaces handlePlaceholder's JSON marker with a real handler.
//
// Dashboard is NOT here — it is the one real view in Slice 0 (handleDashboardStats).
// Memory & Graph (053) and half of Bridge Ops (049) stay bridge-blocked until
// the MuninnDB bridge lands (their issues #560 → #556).
var placeholderViews = map[string]string{
	"documents":     "047",
	"query":         "048",
	"bridge-ops":    "049",
	"vaults":        "050",
	"observability": "051",
	"settings":      "052",
	"memory-graph":  "053",
}

// handlePlaceholder serves the standard placeholder marker for a not-yet-built
// view. The client (Alpine) reads {view, title, status, future_spec} and renders
// the placeholder panel. 404 on an unknown view name.
func (s *Server) handlePlaceholder(w http.ResponseWriter, r *http.Request) {
	view := r.PathValue("view")
	spec, ok := placeholderViews[view]
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"view":        view,
		"title":       capitalize(strings.ReplaceAll(view, "-", " ")),
		"status":      "planned",
		"future_spec": spec,
	})
}

// capitalize uppercases the first byte of s (ASCII; sufficient for the fixed
// view-name set). Avoids the deprecated strings.Title (golangci-lint flags it).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
