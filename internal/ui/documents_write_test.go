package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// documents_write_test.go (spec 050 / T013,T015,T017,T018) proves the console's
// first write surface: POST /api/documents (add), DELETE /api/documents/{id}
// (remove), POST /api/documents/{id}/reingest (reingest) — happy paths, parity
// with the engine/CLI write path, the guard (401 without Bearer), and the error
// matrix (empty path, unknown id, vanished source, invalid body). Hermetic: a
// local fake embedder lets engine.Add/Reprocess run offline (no Ollama).

// uiFakeEmbed is a minimal hermetic embed.Embedder so the engine's lazy ingest
// pipeline can chunk + embed without an Ollama backend (mirrors parity_test's
// fakeEmbed). Keyword queries read BM25 only, so the vector content is not
// load-bearing for these tests.
type uiFakeEmbed struct{ dims int }

func (f *uiFakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.dims == 0 {
		f.dims = 2
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1.0, 0.0}
	}
	return out, nil
}
func (f *uiFakeEmbed) Dimensions() int { return 2 }
func (f *uiFakeEmbed) Model() string   { return "fake" }

// newWriteTestEngine returns an engine whose lazy ingest pipeline can run
// offline (a fake embedder is injected via NewWithEmbedder, so Add/Reprocess do
// not reach for Ollama). Mirrors newTestEngine's storage wiring.
func newWriteTestEngine(t *testing.T) *engine.Engine {
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
	eng := engine.NewWithEmbedder(cfg, db, &uiFakeEmbed{})
	t.Cleanup(eng.Close)
	return eng
}

// writeFile writes content to a temp path and returns the absolute path.
func writeUIFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return p
}

// bearerRequest sends method with an optional JSON body + bearer and returns the
// response.
func bearerRequest(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

// firstDocID fetches the first document id from the list (fatals if none).
func firstDocID(t *testing.T, srvURL, token string) string {
	t.Helper()
	resp := bearerGet(t, srvURL+"/api/documents?page_size=10", token)
	defer resp.Body.Close()
	var list documentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Documents) == 0 {
		t.Fatal("no documents in list")
	}
	return list.Documents[0].ID
}

// --- US1: add documents by server-side path (T013) ---

// TestUIWrite_AddDocument: POST /api/documents 200 + ingestSummaryDTO; the doc
// appears in GET /api/documents; parity — the same path re-added via the engine
// is an idempotent no-op (content-addressed), proving UI add ≡ engine.Add.
func TestUIWrite_AddDocument(t *testing.T) {
	eng := newWriteTestEngine(t)
	src := writeUIFile(t, "# Tariff deficit. Solar battery charge deadline peak window.\n")
	srvURL, tok := authedDocServer(t, eng)

	resp := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: src})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, want 200", resp.StatusCode)
	}
	var sum ingestSummaryDTO
	if err := json.NewDecoder(resp.Body).Decode(&sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if sum.New != 1 || sum.Errors != 0 {
		t.Errorf("add summary: got %+v, want new=1 errors=0", sum)
	}
	if sum.Path != src {
		t.Errorf("summary path: got %q, want %q", sum.Path, src)
	}

	// The doc appears in the list.
	docID := firstDocID(t, srvURL, tok)
	if docID == "" {
		t.Fatal("added doc has empty id")
	}

	// Parity: re-adding the SAME path via the engine directly is an idempotent
	// no-op (content-addressed) — UI add and engine.Add target the same identity.
	again, err := eng.Add(context.Background(), "default", src, "")
	if err != nil {
		t.Fatalf("engine re-add: %v", err)
	}
	if again.New != 0 || again.Skipped != 1 {
		t.Errorf("engine re-add: got new=%d skipped=%d, want new=0 skipped=1 (idempotent parity)", again.New, again.Skipped)
	}
}

// TestUIWrite_AddDocument_Errors: 400 empty/whitespace path; 400 invalid body;
// 401 without Bearer.
func TestUIWrite_AddDocument_Errors(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)

	// 400 empty path.
	resp := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: "   "})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty path: got %d, want 400", resp.StatusCode)
	}

	// 400 invalid body (malformed JSON).
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/api/documents", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	bad, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad body req: %v", err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid body: got %d, want 400", bad.StatusCode)
	}

	// 401 without Bearer.
	noTok := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", "", addRequestDTO{Path: "/x"})
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: got %d, want 401", noTok.StatusCode)
	}
}

// --- US2: remove a document (T015) ---

// TestUIWrite_RemoveDocument: DELETE /api/documents/{id} 204; doc + chunks gone
// from the list; source file on disk unchanged (index-only, FR-011); 404 unknown
// id; 401 without Bearer.
func TestUIWrite_RemoveDocument(t *testing.T) {
	eng := newWriteTestEngine(t)
	src := writeUIFile(t, "# Remove me. Peak tariff deficit battery inverter.\n")
	wantBytes, _ := os.ReadFile(src)
	srvURL, tok := authedDocServer(t, eng)

	// Add via the UI first.
	resp := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: src})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, want 200", resp.StatusCode)
	}
	docID := firstDocID(t, srvURL, tok)

	// DELETE → 204.
	del := bearerRequest(t, http.MethodDelete, srvURL+"/api/documents/"+docID, tok, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d, want 204", del.StatusCode)
	}

	// Doc is gone from the list.
	list := bearerGet(t, srvURL+"/api/documents?page_size=10", tok)
	var lr documentsListResponse
	json.NewDecoder(list.Body).Decode(&lr)
	list.Body.Close()
	for _, d := range lr.Documents {
		if d.ID == docID {
			t.Errorf("doc %s still listed after delete", docID)
		}
	}

	// Source file on disk is unchanged (index-only, FR-011).
	gotBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("source file vanished after delete (FR-011): %v", err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Errorf("source file mutated by delete: got %q, want %q", gotBytes, wantBytes)
	}

	// 404 unknown id.
	unknown := bearerRequest(t, http.MethodDelete, srvURL+"/api/documents/not-a-real-id", tok, nil)
	unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id delete: got %d, want 404", unknown.StatusCode)
	}

	// 401 without Bearer.
	noTok := bearerRequest(t, http.MethodDelete, srvURL+"/api/documents/"+docID, "", nil)
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer delete: got %d, want 401", noTok.StatusCode)
	}
}

// --- US3: reingest a document (T017) ---

// TestUIWrite_ReingestDocument: POST /api/documents/{id}/reingest 200 + summary
// after a source change; 404 unknown id; 404 source-not-found when the source
// vanished; 401 without Bearer.
func TestUIWrite_ReingestDocument(t *testing.T) {
	eng := newWriteTestEngine(t)
	src := writeUIFile(t, "# Original content. Solar tariff deficit.\n")
	srvURL, tok := authedDocServer(t, eng)

	add := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: src})
	add.Body.Close()
	if add.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, want 200", add.StatusCode)
	}
	docID := firstDocID(t, srvURL, tok)

	// Change the source, then reingest → 200 + summary (re-derived chunks).
	if err := os.WriteFile(src, []byte("# Changed content. New tariff structure and battery schedule.\n"), 0o644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	re := bearerRequest(t, http.MethodPost, srvURL+"/api/documents/"+docID+"/reingest", tok, nil)
	defer re.Body.Close()
	if re.StatusCode != http.StatusOK {
		t.Fatalf("reingest: status %d, want 200", re.StatusCode)
	}
	var sum ingestSummaryDTO
	if err := json.NewDecoder(re.Body).Decode(&sum); err != nil {
		t.Fatalf("decode reingest summary: %v", err)
	}
	if sum.New != 1 {
		t.Errorf("reingest summary: got new=%d, want 1 (re-derived)", sum.New)
	}

	// 404 unknown id.
	unknown := bearerRequest(t, http.MethodPost, srvURL+"/api/documents/not-a-real-id/reingest", tok, nil)
	unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id reingest: got %d, want 404", unknown.StatusCode)
	}

	// 401 without Bearer.
	noTok := bearerRequest(t, http.MethodPost, srvURL+"/api/documents/"+docID+"/reingest", "", nil)
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer reingest: got %d, want 401", noTok.StatusCode)
	}
}

// TestUIWrite_ReingestSourceVanished: reingest of a doc whose source file was
// deleted → 404 "source not found" (distinct from a successful empty reingest).
func TestUIWrite_ReingestSourceVanished(t *testing.T) {
	eng := newWriteTestEngine(t)
	src := writeUIFile(t, "# Vanishing source. Peak overnight tariff.\n")
	srvURL, tok := authedDocServer(t, eng)

	add := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: src})
	add.Body.Close()
	if add.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d", add.StatusCode)
	}
	docID := firstDocID(t, srvURL, tok)

	// Remove the source file, then reingest → 404 source not found.
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	re := bearerRequest(t, http.MethodPost, srvURL+"/api/documents/"+docID+"/reingest", tok, nil)
	defer re.Body.Close()
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("vanished source reingest: got %d, want 404", re.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(re.Body).Decode(&body)
	if body["error"] != "source not found" {
		t.Errorf("vanished source error: got %v, want \"source not found\"", body["error"])
	}
}

// --- US4: guard + index-only + observability (T018) ---

// TestUIWrite_AllRoutesGuarded: every write route 401s without a Bearer on an
// initialized vault (FR-005). remove/reingest never modify the source file
// (index-only, FR-011) is pinned in the US2/US3 tests above.
func TestUIWrite_AllRoutesGuarded(t *testing.T) {
	eng := newWriteTestEngine(t)
	// authedDocServer initializes the vault (admin → bypass off), but we hit the
	// routes WITHOUT a token to prove the guard.
	src := writeUIFile(t, "# Guard test. Solar battery tariff.\n")
	_ = src
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)

	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/documents", addRequestDTO{Path: src}},
		{http.MethodDelete, "/api/documents/some-id", nil},
		{http.MethodPost, "/api/documents/some-id/reingest", nil},
	}
	for _, c := range cases {
		resp := bearerRequest(t, c.method, srv.URL+c.path, "", c.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without bearer: got %d, want 401", c.method, c.path, resp.StatusCode)
		}
	}
}

// TestUIWrite_NoSetCookie: no write route ever emits Set-Cookie (Bearer-only,
// CSRF-free — the same invariant the auth + read surfaces hold).
func TestUIWrite_NoSetCookie(t *testing.T) {
	eng := newWriteTestEngine(t)
	src := writeUIFile(t, "# No cookie. Tariff deficit solar.\n")
	srvURL, tok := authedDocServer(t, eng)

	add := bearerRequest(t, http.MethodPost, srvURL+"/api/documents", tok, addRequestDTO{Path: src})
	add.Body.Close()
	if c := add.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("add emitted Set-Cookie: %q", c)
	}

	docID := firstDocID(t, srvURL, tok)
	del := bearerRequest(t, http.MethodDelete, srvURL+"/api/documents/"+docID, tok, nil)
	del.Body.Close()
	if c := del.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("delete emitted Set-Cookie: %q", c)
	}
}
