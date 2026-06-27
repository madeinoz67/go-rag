package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStatus_AfterIngest(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()

	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.Flags().Set("ollama-url", srv.URL)
	_ = initCmd.Flags().Set("model", "m")
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("some test content for the status command check"), 0o644); err != nil {
		t.Fatal(err)
	}
	addCmd := newAddCmd()
	if err := addCmd.RunE(addCmd, []string{filepath.Join(dir, "a.txt")}); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		sc := newStatusCmd()
		_ = sc.Flags().Set("json", "true")
		_ = sc.RunE(sc, nil)
	})
	var env struct {
		Daemon   string     `json:"daemon"`
		Database statusInfo `json:"database"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("status --json must be valid JSON: %v\nraw: %s", err, out)
	}
	info := env.Database
	if info.Documents != 1 {
		t.Errorf("documents: want 1, got %d", info.Documents)
	}
	if info.Chunks < 1 {
		t.Errorf("chunks: want >=1, got %d", info.Chunks)
	}
	if info.Health != "OK" {
		t.Errorf("health: want OK, got %s", info.Health)
	}
	if info.StorageBytes <= 0 {
		t.Errorf("storage bytes: %d", info.StorageBytes)
	}
}

func TestStatus_DegradedWhenOllamaDown(t *testing.T) {
	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.Flags().Set("ollama-url", "http://127.0.0.1:1") // unreachable
	_ = initCmd.Flags().Set("model", "m")
	_ = initCmd.RunE(initCmd, nil)

	out := captureStdout(t, func() {
		sc := newStatusCmd()
		_ = sc.Flags().Set("json", "true")
		_ = sc.RunE(sc, nil)
	})
	var env struct {
		Daemon   string     `json:"daemon"`
		Database statusInfo `json:"database"`
	}
	_ = json.Unmarshal([]byte(out), &env)
	if env.Database.Health != "degraded" {
		t.Fatalf("unreachable Ollama must report degraded, got %s", env.Database.Health)
	}
}
