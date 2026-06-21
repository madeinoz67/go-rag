package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOpenAPI_RouteParity (T035) asserts every implemented REST route is declared
// in openapi.yaml and vice-versa — no undocumented routes, no dead spec entries.
func TestOpenAPI_RouteParity(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(OpenAPI(), &spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}

	specRoutes := map[string]bool{}
	for path, methods := range spec.Paths {
		for m := range methods {
			if m == "parameters" { // path-level params key, not an HTTP method
				continue
			}
			specRoutes[strings.ToUpper(m)+" "+path] = true
		}
	}

	implRoutes := map[string]bool{}
	for _, r := range routes {
		implRoutes[r.method+" "+r.path] = true
	}

	for r := range implRoutes {
		if !specRoutes[r] {
			t.Errorf("implemented route %q is missing from openapi.yaml", r)
		}
	}
	for r := range specRoutes {
		if !implRoutes[r] {
			t.Errorf("openapi.yaml declares %q but it is not implemented", r)
		}
	}
}

// TestOpenAPI_Served verifies GET /openapi.yaml returns the embedded contract.
func TestOpenAPI_Served(t *testing.T) {
	srv := httptest.NewServer(New(nil, "").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "openapi:") {
		t.Error("served spec does not look like an OpenAPI document")
	}
	if string(body) != string(OpenAPI()) {
		t.Error("served spec differs from the embedded contract")
	}
}
