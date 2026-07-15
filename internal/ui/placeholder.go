package ui

import (
	"net/http"
	"strings"
)

// placeholderViews maps the sidebar items that are STILL placeholder panels
// (kebab-case) to a short status string the placeholder marker shows the client.
// Built views — Dashboard (046), Documents (047), Query (048), Operations /
// Bridge-Ops (049), Vaults (051), Quarantine (053), Observability (054), Settings
// (055) — each replaced this seam with a real handler + Alpine view and are
// intentionally NOT listed here; handlePlaceholder 404s for them.
//
// Remaining placeholders: Memory & Graph (blocked on the MuninnDB bridge —
// issues #560 → #556). Settings graduated to a real view in spec 055.
var placeholderViews = map[string]string{
	"memory-graph": "blocked",
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
