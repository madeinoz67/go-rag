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

// chunk_context_test.go (package cli) proves spec 037 over the CLI: `chunk
// context <id> --window N` returns the ordered window JSON with the target at
// target_index, parity-identical to the REST/gRPC/MCP projections (same engine
// method, same DTO shape as GetChunk — spec 035's cross-transport argument).
// Covers US1 (window resolves) + the FR-004 window>10 non-zero-exit contract.
//
// The CLI query surface does not expose chunk_id, so the target id is resolved
// via the shared engine directly (package cli imports engine). FTS indexing is
// async (H16/spec 018) — the id lookup polls until the keyword index commits,
// mirroring the engine parity test's waitEmbeddings drain.

func TestCLI_ChunkContext(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()

	dir := t.TempDir()
	saved := dbPath
	dbPath = filepath.Join(dir, ".go-rag")
	defer func() { dbPath = saved }()

	// init + add a multi-chunk doc so the per-document linked list spans
	// several chunks (window=2 → 5 chunks for an interior target).
	initCmd := newInitCmd()
	_ = initCmd.Flags().Set("embedding-provider", "ollama")
	_ = initCmd.Flags().Set("ollama-url", srv.URL)
	_ = initCmd.Flags().Set("model", "test-model")
	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	var content strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&content, "paragraph %d: authentication tokens session handling for chunk context.\n", i)
	}
	docPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(docPath, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	addCmd := newAddCmd()
	if err := addCmd.RunE(addCmd, []string{docPath}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Resolve an interior chunk id via the engine (keyword read of the FTS
	// index); poll until the async FTS commit is visible.
	cfg, db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	eng := engine.NewWithDB(cfg, db)
	var id string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, qerr := eng.Query(context.Background(), "default", engine.QueryRequest{Query: "authentication", Mode: "keyword", K: 5, IncludeQuarantined: true})
		if qerr == nil && len(res.Hits) > 0 {
			id = res.Hits[0].ChunkID
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("setup: keyword query never found the ingested chunk within 5s (async FTS drain?)")
	}
	// Pick the middle chunk of the linked list → interior, so window=2 returns 5.
	head := id
	for {
		c, werr := eng.GetChunk("default", head)
		if werr != nil || c.Chunk.PreviousChunkID == "" {
			break
		}
		head = c.Chunk.PreviousChunkID
	}
	var ordered []string
	cur := head
	for {
		ordered = append(ordered, cur)
		c, werr := eng.GetChunk("default", cur)
		if werr != nil || c.Chunk.NextChunkID == "" {
			break
		}
		cur = c.Chunk.NextChunkID
	}
	eng.Close()
	db.Close()
	if len(ordered) < 5 {
		t.Fatalf("setup: corpus produced %d chunks, need ≥5 for a window=2 interior test", len(ordered))
	}
	id = ordered[len(ordered)/2]

	// `chunk context <id> --window 2 --format json` → ordered window, target at
	// target_index, linked-list contiguity, parent document projected.
	out := captureStdout(t, func() {
		cc := newChunkContextCmd()
		_ = cc.Flags().Set("format", "json")
		_ = cc.Flags().Set("window", "2")
		if err := cc.RunE(cc, []string{id}); err != nil {
			t.Errorf("chunk context: %v", err)
		}
	})
	var resp getContextResponseOut
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("chunk context json: %v\nraw: %s", err, out)
	}
	if len(resp.Chunks) != 5 {
		t.Errorf("window=2: len=%d, want 5 (interior target)", len(resp.Chunks))
	}
	if resp.TargetIndex < 1 || resp.TargetIndex > 3 {
		t.Errorf("interior target_index=%d, want 1..3", resp.TargetIndex)
	}
	if resp.TargetIndex >= 0 && resp.TargetIndex < len(resp.Chunks) &&
		resp.Chunks[resp.TargetIndex].ChunkID != id {
		t.Errorf("target=%q, want %q", resp.Chunks[resp.TargetIndex].ChunkID, id)
	}
	for i := 1; i < len(resp.Chunks); i++ { // linked-list contiguity
		if resp.Chunks[i].PreviousChunkID != resp.Chunks[i-1].ChunkID {
			t.Errorf("broken order at %d: prev=%q want %q", i, resp.Chunks[i].PreviousChunkID, resp.Chunks[i-1].ChunkID)
		}
	}
	if resp.Document == nil || resp.Document.ID == "" {
		t.Error("parent document should be projected (non-orphan chunk)")
	}

	// FR-004: window>10 → non-zero exit + clear message.
	bad := newChunkContextCmd()
	_ = bad.Flags().Set("window", "11")
	if err := bad.RunE(bad, []string{id}); err == nil {
		t.Error("window=11 should exit non-zero")
	}
}
