package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/engine"
)

// chunk_batch_test.go (package cli) proves spec 038 over the CLI: `chunk batch
// <id> [<id>...]` resolves up to 100 ids in one call, one JSON result per id in
// request order, with per-id tolerance (missing → error="not found", exit 0) and
// a >100 non-zero-exit. Parity-identical to the REST/gRPC/MCP projections (same
// engine method, same DTO shape — spec 035/037's cross-transport argument).
//
// The CLI query surface has no chunk_id, so live ids are resolved via the engine
// directly; FTS indexing is async (H16/spec 018) so the lookup polls.

func TestCLI_ChunkBatch(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()

	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.Flags().Set("ollama-url", srv.URL)
	_ = initCmd.Flags().Set("model", "test-model")
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	var content strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&content, "paragraph %d: authentication tokens session handling for chunk batch.\n", i)
	}
	docPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(docPath, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	addCmd := newAddCmd()
	if err := addCmd.RunE(addCmd, []string{docPath}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Resolve two live chunk ids via the engine (keyword read of the FTS index);
	// poll until the async FTS commit is visible.
	cfg, db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	eng := engine.NewWithDB(cfg, db)
	var live0, live1 string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, qerr := eng.Query(context.Background(), "default", engine.QueryRequest{Query: "authentication", Mode: "keyword", K: 5, IncludeQuarantined: true})
		if qerr == nil && len(res.Hits) > 0 {
			live0 = res.Hits[0].ChunkID
			if c, e := eng.GetChunk("default", live0); e == nil && c.Chunk.NextChunkID != "" {
				live1 = c.Chunk.NextChunkID
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	eng.Close()
	db.Close()
	if live0 == "" || live1 == "" {
		t.Fatal("setup: could not resolve two live chunk ids within 5s")
	}

	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00"
	// `chunk batch live0 live1 missing live0` → 4 results, in order: ok, ok,
	// not-found, ok (duplicate positional, no dedup).
	out := captureStdout(t, func() {
		bc := newChunkBatchCmd()
		_ = bc.Flags().Set("format", "json")
		if err := bc.RunE(bc, []string{live0, live1, missing, live0}); err != nil {
			t.Errorf("chunk batch: %v", err)
		}
	})
	var resp batchResponseOut
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("chunk batch json: %v\nraw: %s", err, out)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("len=%d want 4", len(resp.Results))
	}
	// order + per-position content
	want := []struct {
		id  string
		err string
	}{{live0, ""}, {live1, ""}, {missing, "not found"}, {live0, ""}}
	for i, w := range want {
		if resp.Results[i].ChunkID != w.id {
			t.Errorf("results[%d].chunk_id=%q want %q", i, resp.Results[i].ChunkID, w.id)
		}
		if resp.Results[i].Error != w.err {
			t.Errorf("results[%d].error=%q want %q", i, resp.Results[i].Error, w.err)
		}
	}
	// live positions carry a chunk + document; missing position omits both.
	if resp.Results[0].Chunk == nil || resp.Results[0].Document == nil {
		t.Error("live[0]: chunk + document should be projected")
	}
	if resp.Results[2].Chunk != nil || resp.Results[2].Document != nil {
		t.Error("missing[2]: chunk + document should be omitted")
	}

	// >100 args → non-zero exit + clear message.
	big := make([]string, engine.MaxBatchGetChunks()+1)
	for i := range big {
		big[i] = live0
	}
	bc := newChunkBatchCmd()
	if err := bc.RunE(bc, big); err == nil {
		t.Error(">100 args: should exit non-zero")
	}
}
