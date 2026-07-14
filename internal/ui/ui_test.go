package ui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newTestEngine opens a fresh empty engine on a temp DB (no ingestion — the UI
// tests cover the transport/auth/contract surface, not engine math). Mirrors the
// rest/server_test.go scaffolding, minus the pipeline (empty corpus is fine:
// Status() returns Documents:0, EmbeddingsComplete:true).
func newTestEngine(t *testing.T) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake"
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := engine.NewWithDB(cfg, db)
	t.Cleanup(eng.Close) // drain background workers before db.Close (no-op for read-only engines)
	return eng
}

func newUITest(t *testing.T, eng *engine.Engine) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(eng, "").Handler())
	t.Cleanup(srv.Close)
	return srv
}

func bearerGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// login POSTs {username,password} and returns (token, response). token is "" on
// non-200.
func login(t *testing.T, base, user, pw string, wantStatus int) (string, *http.Response) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	resp, err := http.Post(base+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("login want %d, got %d", wantStatus, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	return out.Token, resp
}

// TestShell_AlwaysServed — GET / returns the shell HTML (text/html) on both a
// bare and an initialized vault. The shell is the login page; it must load
// before auth so the Alpine gate can render the login form.
func TestShell_AlwaysServed(t *testing.T) {
	for _, label := range []string{"bare vault", "initialized vault"} {
		t.Run(label, func(t *testing.T) {
			eng := newTestEngine(t)
			if label == "initialized vault" {
				if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "pw"); err != nil {
					t.Fatalf("CreateAdmin: %v", err)
				}
			}
			srv := newUITest(t, eng)
			resp := bearerGet(t, srv.URL+"/", "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET / want 200, got %d", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("GET / Content-Type want text/html*, got %q", ct)
			}
		})
	}
}

// TestGuard_BypassRegime — on a bare loopback vault the spec 045 bypass admits
// guarded API routes; the moment an admin exists, the bypass is disabled and
// the same route 401s without a Bearer. This is the load-bearing security
// invariant (re-derived here for the UI transport; do not flip the expected
// status without re-deriving the argument — see rest/bypass_guard_test.go).
func TestGuard_BypassRegime(t *testing.T) {
	const route = "/api/dashboard/stats"

	// (1) Bare vault — bypass fires on loopback.
	bareSrv := newUITest(t, newTestEngine(t))
	resp := bearerGet(t, bareSrv.URL+route, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bare vault + loopback + no bearer: want 200 (bypass), got %d", resp.StatusCode)
	}

	// (2) Initialized vault — bypass MUST NOT fire.
	initEng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(initEng.DB()), auth.DefaultAdminUsername, "pw"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	initSrv := newUITest(t, initEng)
	resp2 := bearerGet(t, initSrv.URL+route, "")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("initialized vault + loopback + no bearer: want 401 (bypass must NOT fire), got %d", resp2.StatusCode)
	}
}

// TestLogin_MintsSession_NoSetCookie — POST /login on an initialized vault mints
// a gorags_ session, returns 200 with {token, expires_at}, and NEVER emits
// Set-Cookie (Bearer-only, CSRF-free). A bad password collapses to 401.
func TestLogin_MintsSession_NoSetCookie(t *testing.T) {
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)

	tok, resp := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)
	if !strings.HasPrefix(tok, "gorags_") {
		t.Fatalf("token want gorags_ prefix, got %q", tok)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Fatalf("Set-Cookie must never be emitted: got %q", resp.Header.Get("Set-Cookie"))
	}

	// Bad password → 401 (no oracle, no Set-Cookie).
	_, bad := login(t, srv.URL, auth.DefaultAdminUsername, "wrong", http.StatusUnauthorized)
	if bad.Header.Get("Set-Cookie") != "" {
		t.Fatalf("Set-Cookie on failed login: %q", bad.Header.Get("Set-Cookie"))
	}
}

// TestGuardedRoute_RequiresBearer — on an initialized vault the dashboard route
// 401s without a Bearer and 200s with the session token from /login.
func TestGuardedRoute_RequiresBearer(t *testing.T) {
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)

	noTok := bearerGet(t, srv.URL+"/api/dashboard/stats", "")
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer: want 401, got %d", noTok.StatusCode)
	}

	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)
	withTok := bearerGet(t, srv.URL+"/api/dashboard/stats", tok)
	defer withTok.Body.Close()
	if withTok.StatusCode != http.StatusOK {
		t.Fatalf("with bearer: want 200, got %d", withTok.StatusCode)
	}
}

// TestDashboardStats_ProjectsStatus — the DTO carries the engine.Status()
// counts + the derived vault name. documents/chunks/embeddings match the engine
// exactly (cross-transport parity is the same-source guarantee).
func TestDashboardStats_ProjectsStatus(t *testing.T) {
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)

	resp := bearerGet(t, srv.URL+"/api/dashboard/stats", tok)
	defer resp.Body.Close()
	var dto dashboardDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode dto: %v", err)
	}

	want, err := eng.Status("default")
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	if dto.Documents != want.Documents || dto.Chunks != want.Chunks || dto.Embeddings != want.Embeddings {
		t.Fatalf("dashboard != engine: dto=%+v want=%+v", dto, want)
	}
	if dto.EmbeddingsComplete != want.EmbeddingsComplete {
		t.Fatalf("embeddings_complete: dto=%v want=%v", dto.EmbeddingsComplete, want.EmbeddingsComplete)
	}
	if dto.Vault == "" {
		t.Fatalf("vault must be derived (non-empty)")
	}
}

// TestPlaceholder_Routes — the 7 non-dashboard views return the planned marker;
// an unknown view 404s. Dashboard is intentionally absent (it is the real view).
func TestPlaceholder_Routes(t *testing.T) {
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)

	for view, spec := range placeholderViews {
		resp := bearerGet(t, srv.URL+"/api/placeholder/"+view, tok)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("placeholder %s: want 200, got %d", view, resp.StatusCode)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("decode placeholder %s: %v", view, err)
		}
		if m["status"] != "planned" || m["future_spec"] != spec {
			t.Fatalf("placeholder %s: got %v", view, m)
		}
	}

	// Unknown view → 404.
	bad := bearerGet(t, srv.URL+"/api/placeholder/nonsense", tok)
	bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown placeholder: want 404, got %d", bad.StatusCode)
	}
}

// TestSidebar_ViewSet — the placeholder map carries exactly the 7 non-dashboard
// sidebar views with their future spec numbers (the 8th, Dashboard, is real).
func TestSidebar_ViewSet(t *testing.T) {
	// placeholderViews holds ONLY the sidebar items still rendering a placeholder
	// panel. Built views (documents 047, query 048, operations 049, vaults 051,
	// quarantine 053, observability 054) are intentionally absent — handlePlaceholder
	// 404s for them.
	want := map[string]string{
		"settings":     "planned",
		"memory-graph": "blocked",
	}
	if len(placeholderViews) != len(want) {
		t.Fatalf("placeholder view count: got %d, want %d", len(placeholderViews), len(want))
	}
	for k, v := range want {
		if placeholderViews[k] != v {
			t.Errorf("placeholder[%q]: got %q, want %q", k, placeholderViews[k], v)
		}
	}
	// Built views must NOT regress into the placeholder map.
	for _, built := range []string{"documents", "query", "operations", "vaults", "quarantine", "observability"} {
		if _, ok := placeholderViews[built]; ok {
			t.Errorf("built view %q must not be a placeholder", built)
		}
	}
}

// TestNoNodeArtifacts — the console is a vendored SPA with no Node/Vite/Tailwind
// build chain. Walk the repo (from the module root) and assert none of the
// forbidden front-end build artifacts exist anywhere.
func TestNoNodeArtifacts(t *testing.T) {
	repoRoot := findRepoRoot(t)
	forbidden := map[string]bool{
		"package.json":       true,
		"package-lock.json":  true,
		"yarn.lock":          true,
		"pnpm-lock.yaml":     true,
		"vite.config.js":     true,
		"vite.config.ts":     true,
		"tailwind.config.js": true,
		"tailwind.config.ts": true,
		"postcss.config.js":  true,
	}
	var hit []string
	_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := filepath.Base(path)
		if base == ".git" || base == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		if forbidden[base] {
			rel, _ := filepath.Rel(repoRoot, path)
			hit = append(hit, rel)
		}
		return nil
	})
	if len(hit) != 0 {
		t.Fatalf("found Node/Vite/Tailwind build artifacts (spec 046 forbids them): %v", hit)
	}
}

// findRepoRoot walks up from the working dir until it finds go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
