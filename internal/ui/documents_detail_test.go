package ui

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// documents_detail_test.go (package ui) proves spec 047 US2: the detail header
// (with source_path), paginated chunks, and the chunk-context window.

// putUIChunk writes a chunk under 0x03 for docID at 0-based idx, with linked-list
// prev/next neighbours assuming a 0..total-1 sequence (so GetChunkContext can walk).
func putUIChunk(t *testing.T, eng *engine.Engine, docID string, idx, total int) {
	t.Helper()
	ws := eng.DB().ResolveVaultPrefix("default")
	c := model.Chunk{
		ID:          docID + "#" + strconvItoa(idx),
		DocumentID:  docID,
		Content:     "chunk " + strconvItoa(idx) + " of " + docID,
		ChunkIndex:  idx,
		TotalChunks: total,
	}
	if idx > 0 {
		c.PreviousChunkID = docID + "#" + strconvItoa(idx-1)
	}
	if idx < total-1 {
		c.NextChunkID = docID + "#" + strconvItoa(idx+1)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	if err := eng.DB().Set(keys.ChunkKey(ws, c.ID), raw); err != nil {
		t.Fatalf("putUIChunk: %v", err)
	}
}

// putUIDocDetail writes a source (0x01) + a document (0x02) that references it,
// with ChunkCount set, so the detail view can resolve source_path and the chunks
// total can be checked against chunk_count.
func putUIDocDetail(t *testing.T, eng *engine.Engine, id, sourceID, sourcePath string, chunkCount int, enriched bool) {
	t.Helper()
	ws := eng.DB().ResolveVaultPrefix("default")
	put := func(key []byte, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := eng.DB().Set(key, raw); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	put(keys.SourceKey(ws, sourceID), model.Source{ID: sourceID, Path: sourcePath, Kind: "directory"})
	d := model.Document{
		ID: id, SourceID: sourceID, FilePath: id + ".md", FileName: id + ".md",
		FileType: "markdown", ContentHash: id, Status: "embedded", ChunkCount: chunkCount,
		IngestedAt: uiDocBase,
	}
	if enriched {
		d.Enrichment = &model.EnrichInfo{Summary: "doc summary", Tags: []string{"x"}, Status: model.EnrichStatusDone, Model: "test"}
	}
	put(keys.DocumentKey(ws, id), d)
}

// strconvItoa is a local int→string (avoid importing strconv solely for it).
func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// TestDocuments_Detail covers US2 (a): detail resolves source_path; 404 unknown;
// un-enriched doc carries no summary (empty state, not error). [FR-004, FR-006]
func TestDocuments_Detail(t *testing.T) {
	eng := newTestEngine(t)
	putUIDocDetail(t, eng, "d1", "src1", "/abs/source/dir", 3, true)
	srvURL, tok := authedDocServer(t, eng)

	// 200 + source_path resolved + enrichment summary present.
	resp := bearerGet(t, srvURL+"/api/documents/d1", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail: got %d want 200", resp.StatusCode)
	}
	var d documentDTO
	json.NewDecoder(resp.Body).Decode(&d)
	if d.SourcePath != "/abs/source/dir" {
		t.Errorf("source_path: got %q want /abs/source/dir", d.SourcePath)
	}
	if d.Summary != "doc summary" {
		t.Errorf("summary: got %q want 'doc summary'", d.Summary)
	}

	// 404 unknown id.
	bad := bearerGet(t, srvURL+"/api/documents/nope", tok)
	bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: got %d want 404", bad.StatusCode)
	}

	// No bearer → 401.
	noTok := bearerGet(t, srvURL+"/api/documents/d1", "")
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: got %d want 401", noTok.StatusCode)
	}
}

// TestDocuments_DetailUnenriched — an un-enriched doc omits summary/tags (empty
// state), not an error. [FR-012]
func TestDocuments_DetailUnenriched(t *testing.T) {
	eng := newTestEngine(t)
	putUIDocDetail(t, eng, "d2", "src2", "/src", 0, false)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/documents/d2", tok)
	defer resp.Body.Close()
	var d documentDTO
	json.NewDecoder(resp.Body).Decode(&d)
	if d.Summary != "" || len(d.Tags) != 0 {
		t.Errorf("unenriched: got summary=%q tags=%v want empty", d.Summary, d.Tags)
	}
	if d.EnrichmentStatus != "" {
		t.Errorf("unenriched: enrichment_status=%q want empty", d.EnrichmentStatus)
	}
}

// TestDocuments_Chunks covers US2 (b): chunk total (across pages) == the
// document's chunk_count; pagination; 401. [FR-005, FR-013]
func TestDocuments_Chunks(t *testing.T) {
	eng := newTestEngine(t)
	const total = 5
	putUIDocDetail(t, eng, "d1", "src1", "/src", total, false)
	for i := 0; i < total; i++ {
		putUIChunk(t, eng, "d1", i, total)
	}
	srvURL, tok := authedDocServer(t, eng)

	// Page through with page_size=2 → 2+2+1, every chunk once, ascending.
	var got []int
	pageToken := ""
	for page := 0; page < 5; page++ {
		url := srvURL + "/api/documents/d1/chunks?page_size=2"
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}
		resp := bearerGet(t, url, tok)
		var list documentChunksResponse
		json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		for _, c := range list.Chunks {
			got = append(got, c.ChunkIndex)
		}
		if list.NextPageToken == "" {
			break
		}
		pageToken = list.NextPageToken
	}
	if len(got) != total {
		t.Fatalf("chunks total: got %d want %d", len(got), total)
	}
	for i := 0; i < total; i++ {
		if got[i] != i {
			t.Errorf("chunks[%d]=%d want %d", i, got[i], i)
		}
	}

	// 401 without bearer.
	noTok := bearerGet(t, srvURL+"/api/documents/d1/chunks", "")
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: got %d want 401", noTok.StatusCode)
	}

	// Unknown doc → empty page (200), not 404 (engine.ListChunks tolerant).
	unk := bearerGet(t, srvURL+"/api/documents/nope/chunks", tok)
	defer unk.Body.Close()
	var list documentChunksResponse
	json.NewDecoder(unk.Body).Decode(&list)
	if unk.StatusCode != http.StatusOK || len(list.Chunks) != 0 {
		t.Errorf("unknown doc chunks: status=%d len=%d want 200/0", unk.StatusCode, len(list.Chunks))
	}
}

// TestDocuments_ChunkContext covers US2 (c): the context window shape + target_index. [FR-005]
func TestDocuments_ChunkContext(t *testing.T) {
	eng := newTestEngine(t)
	const total = 5
	putUIDocDetail(t, eng, "d1", "src1", "/src", total, false)
	for i := 0; i < total; i++ {
		putUIChunk(t, eng, "d1", i, total)
	}
	srvURL, tok := authedDocServer(t, eng)

	// Context for chunk#2 (id d1#2), window=1 → [chunk1, chunk2, chunk3], target_index=1.
	resp := bearerGet(t, srvURL+"/api/documents/d1/chunks/d1%232/context?window=1", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("context: got %d want 200", resp.StatusCode)
	}
	var ctx chunkContextResponse
	json.NewDecoder(resp.Body).Decode(&ctx)
	if len(ctx.Chunks) != 3 {
		t.Fatalf("context len: got %d want 3", len(ctx.Chunks))
	}
	if ctx.TargetIndex != 1 {
		t.Errorf("target_index: got %d want 1", ctx.TargetIndex)
	}
	if ctx.Chunks[ctx.TargetIndex].ChunkIndex != 2 {
		t.Errorf("target chunk: got index %d want 2", ctx.Chunks[ctx.TargetIndex].ChunkIndex)
	}
	if ctx.Document == nil || ctx.Document.ID != "d1" {
		t.Errorf("document: got %+v want d1", ctx.Document)
	}

	// Unknown chunk → 404.
	bad := bearerGet(t, srvURL+"/api/documents/d1/chunks/nope/context", tok)
	bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Errorf("unknown chunk: got %d want 404", bad.StatusCode)
	}

	// Bad window → 400.
	badW := bearerGet(t, srvURL+"/api/documents/d1/chunks/d1%232/context?window=99", tok)
	badW.Body.Close()
	if badW.StatusCode != http.StatusBadRequest {
		t.Errorf("bad window: got %d want 400", badW.StatusCode)
	}
}
