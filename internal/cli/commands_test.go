package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeOllama returns a fixed 2-dim vector per input text so the full init→add→query
// loop is testable without a real Ollama.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		out := struct {
			Embeddings [][]float32 `json:"embeddings"`
		}{}
		for range req.Input {
			out.Embeddings = append(out.Embeddings, []float32{1.0, 0.0})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestCLI_InitAddQuery(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()

	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	// init
	initCmd := newInitCmd()
	if err := initCmd.Flags().Set("ollama-url", srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.Flags().Set("model", "test-model"); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dbPath, "config.json")); err != nil {
		t.Fatalf("init must create config.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dbPath, "data")); err != nil {
		t.Fatalf("init must create data dir: %v", err)
	}

	// add
	docPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(docPath, []byte("hello world this is a test about golang rag retrieval"), 0o644); err != nil {
		t.Fatal(err)
	}
	addOut := captureStdout(t, func() {
		addCmd := newAddCmd()
		if err := addCmd.RunE(addCmd, []string{docPath}); err != nil {
			t.Errorf("add: %v", err)
		}
	})
	if !contains(addOut, "1 new") {
		t.Fatalf("add should report 1 new, got: %s", addOut)
	}

	// query (json)
	queryOut := captureStdout(t, func() {
		q := newQueryCmd()
		_ = q.Flags().Set("format", "json")
		_ = q.Flags().Set("k", "5")
		if err := q.RunE(q, []string{"hello"}); err != nil {
			t.Errorf("query: %v", err)
		}
	})
	var resp struct {
		Hits         []queryResult `json:"hits"`
		EffectiveK    int           `json:"effective_k"`    // H22/spec 024
		EffectivePool int           `json:"effective_pool"` // H22/spec 024
		EffectiveMode string        `json:"effective_mode"` // H22/spec 024
		RerankFailed  bool          `json:"rerank_failed"`
	}
	if err := json.Unmarshal([]byte(queryOut), &resp); err != nil {
		t.Fatalf("query --format json must produce valid JSON: %v\nraw: %s", err, queryOut)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("query must return at least one result; raw: %s", queryOut)
	}
	if resp.Hits[0].Source != "note.txt" {
		t.Errorf("result source should be note.txt, got %q", resp.Hits[0].Source)
	}
	if resp.EffectivePool != 60 || resp.EffectiveMode != "hybrid" { // H22/spec 024
		t.Errorf("effective triple wrong: k=%d pool=%d mode=%q", resp.EffectiveK, resp.EffectivePool, resp.EffectiveMode)
	}
}

func TestCLI_Dirs(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("ollama-url", srv.URL)
	_ = initCmd.Flags().Set("model", "m")
	_ = initCmd.RunE(initCmd, nil)

	for _, sub := range []string{"Notes", "Projects"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "a.txt"), []byte("a document in "+sub), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	addCmd := newAddCmd()
	_ = addCmd.RunE(addCmd, []string{dir})

	out := captureStdout(t, func() {
		dc := newDirsCmd()
		_ = dc.RunE(dc, nil)
	})
	if !contains(out, "Notes") || !contains(out, "Projects") {
		t.Errorf("dirs should list both subdirectories; got: %s", out)
	}
}

func TestCLI_Files(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("ollama-url", srv.URL)
	_ = initCmd.Flags().Set("model", "m")
	_ = initCmd.RunE(initCmd, nil)

	docPath := filepath.Join(dir, "listed.txt")
	if err := os.WriteFile(docPath, []byte("a document that should appear in the files listing"), 0o644); err != nil {
		t.Fatal(err)
	}
	addCmd := newAddCmd()
	_ = addCmd.RunE(addCmd, []string{docPath})

	out := captureStdout(t, func() {
		fc := newFilesCmd()
		_ = fc.RunE(fc, nil)
	})
	if !contains(out, "listed.txt") {
		t.Errorf("files should list the ingested path; got: %s", out)
	}
}

func TestCLI_AddWithoutInitFails(t *testing.T) {
	saved := dbPath
	dbPath = filepath.Join(t.TempDir(), "no-such-db")
	defer func() { dbPath = saved }()

	addCmd := newAddCmd()
	if err := addCmd.RunE(addCmd, []string{"."}); err == nil {
		t.Fatal("add without init must error (no config)")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Ensure context import is used if extended later.
var _ = context.Background
