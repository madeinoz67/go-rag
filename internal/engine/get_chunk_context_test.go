package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// get_chunk_context_test.go (package engine) proves spec 037: GetChunkContext
// returns the chunk plus up to `window` neighbours on each side, in document
// order, with TargetIndex pointing at the requested chunk. US1 (happy path,
// independently verified by walking the linked list via GetChunk) + US2 (every
// boundary / edge value). No MuninnDB dependency — pure go-rag read.

// variedDoc builds a multi-paragraph document long enough to span several chunks
// (so the per-document linked list PreviousChunkID/NextChunkID is populated),
// with `term` occurring so a keyword query can locate a chunk. Paragraphs rotate
// distinct topics so the ingestion-time repetition detector does not quarantine
// the chunks (setup queries also pass IncludeQuarantined as a safety net).
func variedDoc(term string) string {
	topics := []string{"retrieval", "indexing", "embeddings", "vectors", "ranking", "fusion", "scoring", "metadata", "poisoning", "wikilinks", "section", "neighbors", "queries", "keywords", "windows", "context"}
	var sb strings.Builder
	for i := 0; i < 80; i++ {
		a := topics[i%len(topics)]
		b := topics[(i*3+5)%len(topics)]
		fmt.Fprintf(&sb, "note %d: %s interacts with %s in the local database; the %s path also carries %s for %s. ", i, a, b, a, b, term)
	}
	return sb.String()
}

func TestGetChunkContext_WindowMatchesLinkedList(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("authentication"))

	q, err := e.Query(context.Background(), QueryRequest{Query: "authentication", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	const window = 2
	id := q.Hits[0].ChunkID

	res, err := e.GetChunkContext(id, window)
	if err != nil {
		t.Fatalf("GetChunkContext(%s, %d): %v", id, window, err)
	}

	// Independently compute the expected window by walking the linked list.
	var prevs []string
	cur := id
	for i := 0; i < window; i++ {
		c, err := e.GetChunk(cur)
		if err != nil {
			t.Fatalf("GetChunk walk (back): %v", err)
		}
		if c.Chunk.PreviousChunkID == "" {
			break
		}
		prevs = append([]string{c.Chunk.PreviousChunkID}, prevs...)
		cur = c.Chunk.PreviousChunkID
	}
	want := append([]string{}, prevs...)
	want = append(want, id)
	cur = id
	for i := 0; i < window; i++ {
		c, err := e.GetChunk(cur)
		if err != nil {
			t.Fatalf("GetChunk walk (forward): %v", err)
		}
		if c.Chunk.NextChunkID == "" {
			break
		}
		want = append(want, c.Chunk.NextChunkID)
		cur = c.Chunk.NextChunkID
	}

	if len(res.Chunks) != len(want) {
		t.Fatalf("window length = %d, want %d (ids got=%v want=%v)", len(res.Chunks), len(want), chunkIDs(res.Chunks), want)
	}
	if res.TargetIndex != len(prevs) {
		t.Errorf("TargetIndex = %d, want %d", res.TargetIndex, len(prevs))
	}
	for i, c := range res.Chunks {
		if c.ID != want[i] {
			t.Errorf("chunks[%d].ID = %q, want %q (broken document order)", i, c.ID, want[i])
		}
	}
	if res.Chunks[res.TargetIndex].ID != id {
		t.Errorf("chunk at TargetIndex = %q, want target %q", res.Chunks[res.TargetIndex].ID, id)
	}
	// Linked-list contiguity within the returned window.
	for i := 1; i < len(res.Chunks); i++ {
		if res.Chunks[i].PreviousChunkID != res.Chunks[i-1].ID {
			t.Errorf("chunks[%d].PreviousChunkID = %q, want %q", i, res.Chunks[i].PreviousChunkID, res.Chunks[i-1].ID)
		}
	}
	// Parent document resolved (FR-009); every chunk carries its content (FR-008).
	if res.Document.ID == "" {
		t.Error("parent document should be resolved in the same call")
	}
	for i, c := range res.Chunks {
		if c.Content == "" {
			t.Errorf("chunks[%d] has empty content — full metadata expected", i)
		}
	}
}

func chunkIDs(cs []model.Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

// --- US2: boundaries + edge values (FR-003..007) ---

// headTail walks the per-document linked list from id to its first (head) and
// last (tail) chunk — independently of GetChunkContext — so boundary tests can
// target the document's first and last chunks by id.
func headTail(t *testing.T, e *Engine, id string) (head, tail string) {
	t.Helper()
	cur := id
	for {
		c, err := e.GetChunk(cur)
		if err != nil {
			t.Fatalf("headTail walk (back): %v", err)
		}
		if c.Chunk.PreviousChunkID == "" {
			head = cur
			break
		}
		cur = c.Chunk.PreviousChunkID
	}
	cur = id
	for {
		c, err := e.GetChunk(cur)
		if err != nil {
			t.Fatalf("headTail walk (fwd): %v", err)
		}
		if c.Chunk.NextChunkID == "" {
			tail = cur
			break
		}
		cur = c.Chunk.NextChunkID
	}
	return head, tail
}

// TestGetChunkContext_Boundaries covers FR-003/FR-005: windowing is correct at
// the first chunk (target_index=0, successors only), the last chunk (predecessors
// only), window=0 (exactly the target ≡ GetChunk), and a window larger than the
// document (the whole document, target at its real index, linked-list contiguity).
func TestGetChunkContext_Boundaries(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("authentication"))

	q, err := e.Query(context.Background(), QueryRequest{Query: "authentication", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	mid := q.Hits[0].ChunkID
	head, tail := headTail(t, e, mid)

	// (a) first chunk, window=5 → target_index=0, successors only.
	res, err := e.GetChunkContext(head, 5)
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if res.TargetIndex != 0 {
		t.Errorf("first chunk TargetIndex=%d, want 0", res.TargetIndex)
	}
	if res.Chunks[res.TargetIndex].ID != head {
		t.Errorf("first chunk target=%q, want head %q", res.Chunks[res.TargetIndex].ID, head)
	}

	// (b) last chunk → predecessors only (target at the end of the window).
	res, err = e.GetChunkContext(tail, 5)
	if err != nil {
		t.Fatalf("last chunk: %v", err)
	}
	if res.TargetIndex != len(res.Chunks)-1 {
		t.Errorf("last chunk TargetIndex=%d, want %d (last)", res.TargetIndex, len(res.Chunks)-1)
	}
	if res.Chunks[res.TargetIndex].ID != tail {
		t.Errorf("last chunk target=%q, want tail %q", res.Chunks[res.TargetIndex].ID, tail)
	}

	// (c) window=0 → exactly [target], target_index=0 (≡ GetChunk, FR-003).
	res, err = e.GetChunkContext(mid, 0)
	if err != nil {
		t.Fatalf("window=0: %v", err)
	}
	if len(res.Chunks) != 1 || res.TargetIndex != 0 || res.Chunks[0].ID != mid {
		t.Errorf("window=0: len=%d idx=%d id=%q, want 1/0/%q", len(res.Chunks), res.TargetIndex, res.Chunks[0].ID, mid)
	}

	// (g) window larger than the document → the whole document, target at its
	// real index, linked-list contiguity preserved.
	res, err = e.GetChunkContext(mid, MaxChunkContextWindow())
	if err != nil {
		t.Fatalf("window>doc: %v", err)
	}
	for i := 1; i < len(res.Chunks); i++ {
		if res.Chunks[i].PreviousChunkID != res.Chunks[i-1].ID {
			t.Errorf("window>doc: broken document order at %d", i)
		}
	}
	if res.Chunks[res.TargetIndex].ID != mid {
		t.Errorf("window>doc: target=%q, want %q", res.Chunks[res.TargetIndex].ID, mid)
	}
}

// TestGetChunkContext_SingleChunkDoc covers a single-chunk document: any window
// returns exactly that one chunk with target_index=0.
func TestGetChunkContext_SingleChunkDoc(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, "a single short sentence about retrieval.") // < one chunk (512/50)
	q, err := e.Query(context.Background(), QueryRequest{Query: "retrieval", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	id := q.Hits[0].ChunkID
	for _, w := range []int{0, 2, MaxChunkContextWindow()} {
		res, err := e.GetChunkContext(id, w)
		if err != nil {
			t.Fatalf("single-chunk window=%d: %v", w, err)
		}
		if len(res.Chunks) != 1 || res.TargetIndex != 0 {
			t.Errorf("single-chunk window=%d: len=%d idx=%d, want 1/0", w, len(res.Chunks), res.TargetIndex)
		}
	}
}

// TestGetChunkContext_InvalidWindowAndID covers FR-004 (window out of range →
// ErrInvalid) and FR-006/FR-007 (missing id → ErrNotFound; empty/whitespace id
// → ErrInvalid, no lookup).
func TestGetChunkContext_InvalidWindowAndID(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("tokens"))
	q, err := e.Query(context.Background(), QueryRequest{Query: "tokens", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	id := q.Hits[0].ChunkID

	// (d) window=11 → ErrInvalid; (e) negative window → ErrInvalid.
	if _, err := e.GetChunkContext(id, MaxChunkContextWindow()+1); !errors.Is(err, ErrInvalid) {
		t.Errorf("window=11: err=%v, want ErrInvalid", err)
	}
	if _, err := e.GetChunkContext(id, -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("window=-1: err=%v, want ErrInvalid", err)
	}
	// (h) empty / whitespace chunk_id → ErrInvalid (FR-007, no lookup).
	for _, bad := range []string{"", "   ", "\t"} {
		if _, err := e.GetChunkContext(bad, 2); !errors.Is(err, ErrInvalid) {
			t.Errorf("chunk_id=%q: err=%v, want ErrInvalid", bad, err)
		}
	}
	// (i) missing chunk_id → ErrNotFound (FR-006; also the cross-vault path).
	if _, err := e.GetChunkContext("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00", 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: err=%v, want ErrNotFound", err)
	}
}

// TestGetChunkContext_OrphanChunkTolerant covers FR-009 / data-model: a chunk
// whose parent document was removed is NOT an error — the window is returned
// with a zero-valued Document. Set up by deleting the document record under the
// chunk (the chunk record under prefix 0x03 is left intact → orphan).
func TestGetChunkContext_OrphanChunkTolerant(t *testing.T) {
	e := newCacheEngine(t)
	addDoc(t, e, variedDoc("orphan"))
	q, err := e.Query(context.Background(), QueryRequest{Query: "orphan", Mode: "keyword", K: 5, IncludeQuarantined: true})
	if err != nil || len(q.Hits) == 0 {
		t.Fatalf("setup query failed: err=%v hits=%d", err, len(q.Hits))
	}
	target := q.Hits[0].ChunkID

	// Sanity: the document resolves before we orphan the chunk.
	res, err := e.GetChunkContext(target, 2)
	if err != nil {
		t.Fatalf("pre-orphan: %v", err)
	}
	if res.Document.ID == "" {
		t.Fatal("pre-orphan: document should resolve")
	}
	docID := res.Document.ID

	// Remove the parent document record → the chunk is now an orphan.
	if err := e.db.Delete(keys.DocumentKey(e.db.ResolveVaultPrefix("default"), docID)); err != nil {
		t.Fatalf("delete doc: %v", err)
	}

	res, err = e.GetChunkContext(target, 2)
	if err != nil {
		t.Fatalf("orphan GetChunkContext: %v (want no error)", err)
	}
	if res.Document.ID != "" {
		t.Errorf("orphan: Document.ID=%q, want empty (zero-valued)", res.Document.ID)
	}
	if res.Chunks[res.TargetIndex].ID != target {
		t.Errorf("orphan: target=%q, want %q", res.Chunks[res.TargetIndex].ID, target)
	}
}
