package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// batch_get_chunks_test.go (package engine) proves spec 038: BatchGetChunks
// resolves up to 100 chunk_ids in one call, one result per id in request order,
// with per-id tolerance (missing → "not found", call succeeds). US1 happy path +
// US2 boundary/edge. Reuses variedDoc + newCacheEngine/addDoc from the spec-037
// test files (same package).

// allChunkIDs walks the per-document linked list from startID to collect every
// chunk id in document order (head → tail), independently of BatchGetChunks.
func allChunkIDs(t *testing.T, e *Engine, startID string) []string {
	t.Helper()
	head := startID
	for {
		c, err := e.GetChunk("default", head)
		if err != nil {
			t.Fatalf("walk head: %v", err)
		}
		if c.Chunk.PreviousChunkID == "" {
			break
		}
		head = c.Chunk.PreviousChunkID
	}
	var ids []string
	cur := head
	for {
		ids = append(ids, cur)
		c, err := e.GetChunk("default", cur)
		if err != nil {
			t.Fatalf("walk tail: %v", err)
		}
		if c.Chunk.NextChunkID == "" {
			break
		}
		cur = c.Chunk.NextChunkID
	}
	return ids
}

// TestBatchGetChunks_HappyPath covers US1: a batch of live ids (+ one missing)
// resolves in request order, live ids carry full chunks + the parent document,
// the missing id carries Err="not found", and the call succeeds. [US1 #1-3]
func TestBatchGetChunks_HappyPath(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("authentication"))

	q, err := e.Query(context.Background(), "default", QueryRequest{Query: "authentication", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query: err=%v hits=%d", err, len(q.Hits))
	}
	live := allChunkIDs(t, e, q.Hits[0].ChunkID)
	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00"
	req := append(append([]string{}, live...), missing)

	res, err := e.BatchGetChunks("default", req)
	if err != nil {
		t.Fatalf("BatchGetChunks: %v", err)
	}
	if len(res.Results) != len(req) {
		t.Fatalf("len=%d want %d", len(res.Results), len(req))
	}
	for i, it := range res.Results { // request order, 1:1
		if it.ChunkID != req[i] {
			t.Errorf("results[%d].ChunkID=%q want %q (order)", i, it.ChunkID, req[i])
		}
	}
	for i, it := range res.Results {
		if i < len(live) { // live → full chunk + resolved document
			if it.Err != "" {
				t.Errorf("live[%d]: Err=%q want empty", i, it.Err)
			}
			if it.Chunk.ID != live[i] {
				t.Errorf("live[%d]: chunk.ID=%q want %q", i, it.Chunk.ID, live[i])
			}
			if it.Chunk.Content == "" {
				t.Errorf("live[%d]: empty content", i)
			}
			if it.Document.ID == "" {
				t.Errorf("live[%d]: document not resolved", i)
			}
		} else { // missing → "not found"
			if it.Err != "not found" {
				t.Errorf("missing: Err=%q want 'not found'", it.Err)
			}
		}
	}
}

// TestBatchGetChunks_ValidationAndEdges covers US2: cap (>100), empty list,
// empty/whitespace element, duplicates (positional, no dedup), all-missing,
// single-id, and orphan-chunk tolerance. [US2 #1-5; FR-003..007]
func TestBatchGetChunks_ValidationAndEdges(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("tokens"))
	q, err := e.Query(context.Background(), "default", QueryRequest{Query: "tokens", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query: err=%v hits=%d", err, len(q.Hits))
	}
	live := q.Hits[0].ChunkID
	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00"

	// (a) >100 → ErrInvalid (FR-004).
	big := make([]string, MaxBatchGetChunks()+1)
	for i := range big {
		big[i] = live
	}
	if _, err := e.BatchGetChunks("default", big); !errors.Is(err, ErrInvalid) {
		t.Errorf(">100: err=%v want ErrInvalid", err)
	}
	// (b) empty list → ErrInvalid (FR-005).
	if _, err := e.BatchGetChunks("default", []string{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty list: err=%v want ErrInvalid", err)
	}
	// (c) empty/whitespace element → ErrInvalid, no lookup (FR-006).
	for _, bad := range []string{"", "   ", "\t"} {
		if _, err := e.BatchGetChunks("default", []string{live, bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("element %q: err=%v want ErrInvalid", bad, err)
		}
	}
	// (d) duplicates → positional, no de-dup (FR-007).
	res, err := e.BatchGetChunks("default", []string{live, live})
	if err != nil {
		t.Fatalf("duplicates: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("duplicates: len=%d want 2", len(res.Results))
	}
	if res.Results[0].ChunkID != live || res.Results[1].ChunkID != live {
		t.Errorf("duplicates: ids=%q,%q want %q,%q", res.Results[0].ChunkID, res.Results[1].ChunkID, live, live)
	}
	if res.Results[0].Err != "" || res.Results[1].Err != "" {
		t.Error("duplicates: both positions should resolve")
	}
	// (e) all-missing → every result Err="not found", call succeeds.
	res, err = e.BatchGetChunks("default", []string{missing, missing})
	if err != nil {
		t.Fatalf("all-missing: %v", err)
	}
	if res.Results[0].Err != "not found" || res.Results[1].Err != "not found" {
		t.Errorf("all-missing: want both 'not found', got %q/%q", res.Results[0].Err, res.Results[1].Err)
	}
	// (f) single id → one result.
	res, err = e.BatchGetChunks("default", []string{live})
	if err != nil {
		t.Fatalf("single: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].ChunkID != live {
		t.Errorf("single: len=%d id=%q", len(res.Results), res.Results[0].ChunkID)
	}

	// (g) orphan chunk (parent document removed) → chunk still resolves, document
	// zero-valued, no error (FR-008 orphan tolerance, mirrors GetChunk).
	pre, _ := e.BatchGetChunks("default", []string{live})
	docID := pre.Results[0].Document.ID
	if docID == "" {
		t.Fatal("pre-orphan: document should resolve")
	}
	if err := e.db.Delete(keys.DocumentKey(e.db.ResolveVaultPrefix("default"), docID)); err != nil {
		t.Fatalf("delete doc: %v", err)
	}
	res, err = e.BatchGetChunks("default", []string{live})
	if err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if res.Results[0].Err != "" {
		t.Errorf("orphan: should still resolve (Err=%q)", res.Results[0].Err)
	}
	if res.Results[0].Document.ID != "" {
		t.Errorf("orphan: Document.ID=%q want empty", res.Results[0].Document.ID)
	}
}
