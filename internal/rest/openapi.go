package rest

import (
	_ "embed"
	"net/http"
)

// openapiYAML is the REST contract served at GET /openapi.yaml. It is the
// operational source of truth for the served API; the design copy lives at
// specs/003-rest-grpc-api/contracts/rest-openapi.yaml (hand-synced, v1).
//
//go:embed openapi.yaml
var openapiYAML []byte

// handleOpenAPI serves the REST contract at GET /openapi.yaml (T034). Public
// (no bearer) so clients can discover the API without a token.
func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiYAML)
}

// OpenAPI returns the embedded REST contract bytes (for the parity test, T035).
func OpenAPI() []byte { return openapiYAML }
