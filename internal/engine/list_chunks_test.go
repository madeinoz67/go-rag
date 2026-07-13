package engine

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// list_chunks_test.go (package engine) proves spec 047 Slice 1:
//   - Engine.ListChunks filters by document_id, orders by (chunk_index, id),
//     paginates via the opaque chunk cursor.
//   - ListDocuments' new Tags filter (R3) narrows by match-any tag.
//
// Chunks are inserted directly under prefix 0x03 with crafted indices for
// determinism (ListChunks reads chunks, so direct insertion is the hermetic
// setup — no ingest pipeline needed).

// putChunk writes a chunk record under prefix 0x03 for docID at 0-based idx,
// with a deterministic id "<docID>#<idx>".
func putChunk(t *testing.T, e *Engine, docID string, idx int) {
	t.Helper()
	ws := engineWS(e)
	c := model.Chunk{
		ID:          docID + "#" + itoa(idx),
		DocumentID:  docID,
		Content:     "chunk " + itoa(idx) + " of " + docID,
		ChunkIndex:  idx,
		TotalChunks: 10,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal chunk %s: %v", c.ID, err)
	}
	if err := e.db.Set(keys.ChunkKey(ws, c.ID), raw); err != nil {
		t.Fatalf("putChunk %s: %v", c.ID, err)
	}
}

// chunkIndices collects the 0-based indices of a chunk slice (test helper).
func chunkIndices(chunks []model.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = itoa(c.ChunkIndex)
	}
	return out
}

// TestListChunks_PaginationAndOrder covers spec 047: per-document isolation,
// (chunk_index ASC, id ASC) ordering from out-of-order inserts, multi-page
// iteration, unknown-doc empty result, default page_size. [R1; data-model.md]
func TestListChunks_PaginationAndOrder(t *testing.T) {
	e := newCacheEngine(t)
	// docA has 5 chunks inserted out of order; docB has 2.
	putChunk(t, e, "docA", 3)
	putChunk(t, e, "docA", 0)
	putChunk(t, e, "docA", 4)
	putChunk(t, e, "docA", 1)
	putChunk(t, e, "docA", 2)
	putChunk(t, e, "docB", 0)
	putChunk(t, e, "docB", 1)

	// (a) docA, page_size=2 → 2+2+1, ordered 0..4, every chunk once, empty token at end.
	var got []string
	tok := ""
	for page := 0; page < 5; page++ { // guard against infinite loop
		res, err := e.ListChunks("docA", ListChunksRequest{PageSize: 2, PageToken: tok})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		got = append(got, chunkIndices(res.Chunks)...)
		if res.NextPageToken == "" {
			break
		}
		tok = res.NextPageToken
	}
	want := []string{"0", "1", "2", "3", "4"}
	if len(got) != len(want) {
		t.Fatalf("paged: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("paged[%d]=%q want %q", i, got[i], want[i])
		}
	}

	// (b) docB → exactly its 2 chunks (isolation: docA's 5 excluded).
	res, err := e.ListChunks("docB", ListChunksRequest{})
	if err != nil {
		t.Fatalf("docB: %v", err)
	}
	if len(res.Chunks) != 2 || res.Chunks[0].ChunkIndex != 0 || res.Chunks[1].ChunkIndex != 1 {
		t.Errorf("docB: got %v want 2 chunks [0 1]", chunkIndices(res.Chunks))
	}

	// (c) unknown doc → empty result, no error, empty token.
	res, err = e.ListChunks("nope", ListChunksRequest{})
	if err != nil || len(res.Chunks) != 0 || res.NextPageToken != "" {
		t.Errorf("unknown doc: res=%+v err=%v want empty/no-error", res, err)
	}

	// (d) default page_size (0 → 50): docA's 5 chunks in one page.
	res, err = e.ListChunks("docA", ListChunksRequest{PageSize: 0})
	if err != nil || len(res.Chunks) != 5 {
		t.Errorf("default page_size: got %d chunks err=%v want 5", len(res.Chunks), err)
	}
}

// TestListChunks_InvalidInput covers the validation rules + the chunk-token codec.
func TestListChunks_InvalidInput(t *testing.T) {
	e := newCacheEngine(t)
	cases := []struct {
		name string
		doc  string
		req  ListChunksRequest
	}{
		{"empty doc id", "", ListChunksRequest{}},
		{"whitespace doc id", "  ", ListChunksRequest{}},
		{"page_size negative", "docA", ListChunksRequest{PageSize: -1}},
		{"page_size too large", "docA", ListChunksRequest{PageSize: 999}},
		{"malformed token", "docA", ListChunksRequest{PageToken: "!!!"}},
	}
	for _, c := range cases {
		if _, err := e.ListChunks(c.doc, c.req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err=%v want ErrInvalid", c.name, err)
		}
	}
	// codec round-trip.
	tok := encodeChunkPageToken(3, "docA#3")
	idx, id, err := decodeChunkPageToken(tok)
	if err != nil || idx != 3 || id != "docA#3" {
		t.Errorf("codec round-trip: tok=%q → (%d,%q,%v)", tok, idx, id, err)
	}
}

// putDocWithTags writes a document with the given enrichment tags (spec 029 / 047 R3).
func putDocWithTags(t *testing.T, e *Engine, id string, tags []string) {
	t.Helper()
	ws := engineWS(e)
	d := model.Document{
		ID:          id,
		FilePath:    id + ".txt",
		FileName:    id + ".txt",
		FileType:    "text",
		ContentHash: id,
		Status:      "embedded",
		IngestedAt:  docBase,
		Enrichment:  &model.EnrichInfo{Tags: tags, Status: model.EnrichStatusDone, Model: "test"},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if err := e.db.Set(keys.DocumentKey(ws, id), raw); err != nil {
		t.Fatalf("putDocWithTags %s: %v", id, err)
	}
}

// TestListDocuments_TagFilter covers spec 047 R3: match-any tag filtering,
// multi-tag union, no-match empty, and nil/empty Tags = no filter. [R3]
func TestListDocuments_TagFilter(t *testing.T) {
	e := newCacheEngine(t)
	putDocWithTags(t, e, "t1", []string{"security", "auth"})
	putDocWithTags(t, e, "t2", []string{"security"})
	putDocWithTags(t, e, "t3", []string{"networking"})
	putDoc(t, e, "t4", docBase, "embedded") // un-enriched (no tags)

	// match-any single: tags=[security] → t1, t2.
	res, err := e.ListDocuments(ListDocumentsRequest{Tags: []string{"security"}})
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	ids := docIDs(res.Documents)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t2" {
		t.Errorf("tags=[security]: ids=%v want [t1 t2]", ids)
	}

	// match-any union across multiple: tags=[auth,networking] → t1, t3.
	res, err = e.ListDocuments(ListDocumentsRequest{Tags: []string{"auth", "networking"}})
	if err != nil {
		t.Fatalf("auth+net: %v", err)
	}
	ids = docIDs(res.Documents)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t3" {
		t.Errorf("tags=[auth,networking]: ids=%v want [t1 t3]", ids)
	}

	// no match → empty.
	res, err = e.ListDocuments(ListDocumentsRequest{Tags: []string{"nonexistent"}})
	if err != nil || len(res.Documents) != 0 {
		t.Errorf("no-match: docs=%d err=%v want 0", len(res.Documents), err)
	}

	// nil Tags → no filter (all 4 docs).
	res, err = e.ListDocuments(ListDocumentsRequest{Tags: nil})
	if err != nil || len(res.Documents) != 4 {
		t.Errorf("nil tags: docs=%d err=%v want 4", len(res.Documents), err)
	}
}
