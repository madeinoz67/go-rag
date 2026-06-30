package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// get_chunk_test.go (package engine) proves spec 035 US1 at the engine level:
// GetChunk resolves a content-addressed chunk_id to its chunk (matching the
// content seen at ingestion), returns ErrNotFound for a missing id, ErrInvalid
// for empty/whitespace input, and tolerates an orphan chunk (present chunk whose
// parent document was removed) by returning the chunk with a zero Document.

func TestGetChunk_Found_MatchesIngestion(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "the go-rag get-chunk primitive resolves a content-addressed chunk identifier")
	ctx := context.Background()

	q, err := e.Query(ctx, QueryRequest{Query: "primitive", Mode: "keyword", K: 5})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query to obtain a chunk_id failed: err=%v hits=%d", err, len(q.Hits))
	}
	id := q.Hits[0].ChunkID

	res, err := e.GetChunk(id)
	if err != nil {
		t.Fatalf("GetChunk(%s): %v", id, err)
	}
	if res.Chunk.ID != id {
		t.Errorf("chunk id = %q, want %q", res.Chunk.ID, id)
	}
	if res.Chunk.Content != q.Hits[0].Content {
		t.Error("GetChunk content differs from the query hit content (must match ingestion)")
	}
	// The engine read path already resolves the parent document (FR-005); the
	// transport projects it in US2.
	if res.Document.ID == "" {
		t.Error("parent document should be resolved in the same call (FR-005 read path)")
	}
}

func TestGetChunk_Missing_IsErrNotFound(t *testing.T) {
	e := newCacheEngine(t)
	_, err := e.GetChunk("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a missing id, got %v", err)
	}
}

func TestGetChunk_Empty_IsErrInvalid(t *testing.T) {
	e := newCacheEngine(t)
	for _, bad := range []string{"", "   ", "\t\n"} {
		if _, err := e.GetChunk(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("GetChunk(%q) expected ErrInvalid, got %v", bad, err)
		}
	}
}

func TestGetChunk_OrphanChunk_Tolerant(t *testing.T) {
	// A chunk whose parent document record was removed still resolves; the
	// engine returns the chunk with a zero-valued Document (not an error) —
	// matching ListChunks' tolerant FilePath="" behaviour (research.md R3).
	e := newCacheEngine(t)
	addDoc(t, e, "orphan probe text for the get-chunk tolerant read path")
	ctx := context.Background()

	q, _ := e.Query(ctx, QueryRequest{Query: "orphan", Mode: "keyword", K: 5})
	if len(q.Hits) == 0 {
		t.Fatal("setup: no hit for orphan probe")
	}
	id := q.Hits[0].ChunkID
	docID := q.Hits[0].DocumentID

	if err := e.db.DeleteWithPrefix(storage.PrefixDocument, []byte(docID)); err != nil {
		t.Fatalf("remove parent document: %v", err)
	}
	res, err := e.GetChunk(id)
	if err != nil {
		t.Fatalf("orphan GetChunk should succeed, got %v", err)
	}
	if res.Chunk.ID != id {
		t.Errorf("chunk id = %q, want %q", res.Chunk.ID, id)
	}
	if res.Document.ID != "" {
		t.Errorf("orphan chunk should have a zero Document, got id=%q", res.Document.ID)
	}
}
