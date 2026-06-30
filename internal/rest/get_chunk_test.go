package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// get_chunk_test.go (package rest) proves spec 035 US3 over REST: GET /v1/chunks/{id}
// returns {chunk, document} (200), 404 for a missing id (FR-002), and 400 for a
// whitespace-only id (FR-009).

func TestREST_GetChunk_HappyNotFoundInvalid(t *testing.T) {
	eng := newEngineWithCorpus(t, "the rest get-chunk endpoint resolves a content-addressed identifier and returns document metadata")
	srv := httptest.NewServer(New(eng, "").Handler())
	defer srv.Close()

	// Obtain a real chunk_id via Query.
	qbody, _ := json.Marshal(map[string]any{"query": "resolves", "mode": "keyword", "k": 5})
	qresp, err := http.Post(srv.URL+"/v1/query", "application/json", bytes.NewReader(qbody))
	if err != nil {
		t.Fatalf("POST /v1/query: %v", err)
	}
	var qout queryResponse
	if err := json.NewDecoder(qresp.Body).Decode(&qout); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	qresp.Body.Close()
	if len(qout.Hits) == 0 {
		t.Fatal("setup: no query hit to obtain a chunk_id")
	}
	id := qout.Hits[0].ChunkID

	// Happy: GET /v1/chunks/{id} → 200 {chunk, document} (FR-001/004/005).
	resp, err := http.Get(srv.URL + "/v1/chunks/" + id)
	if err != nil {
		t.Fatalf("GET /v1/chunks/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out getChunkResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode get-chunk response: %v", err)
	}
	if out.Chunk.ChunkID != id {
		t.Errorf("chunk_id = %q, want %q", out.Chunk.ChunkID, id)
	}
	if out.Chunk.Content == "" {
		t.Error("chunk content is empty")
	}
	if out.Document == nil || out.Document.ID == "" {
		t.Error("US2: parent document must be projected onto the response (FR-005)")
	}
	if out.Document != nil && out.Document.ID == out.Document.ContentHash {
		t.Error("document id and content_hash collapsed — they must be distinct (PRD §7.2)")
	}

	// Not-found → 404 (FR-002).
	nf, err := http.Get(srv.URL + "/v1/chunks/deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00")
	if err != nil {
		t.Fatalf("GET not-found: %v", err)
	}
	defer nf.Body.Close()
	if nf.StatusCode != http.StatusNotFound {
		t.Errorf("not-found status = %d, want 404", nf.StatusCode)
	}

	// Whitespace-only id → 400 (FR-009). "%20%20" decodes to "  " → ErrInvalid.
	inv, err := http.Get(srv.URL + "/v1/chunks/%20%20")
	if err != nil {
		t.Fatalf("GET invalid: %v", err)
	}
	defer inv.Body.Close()
	if inv.StatusCode != http.StatusBadRequest {
		t.Errorf("whitespace-id status = %d, want 400", inv.StatusCode)
	}
}
