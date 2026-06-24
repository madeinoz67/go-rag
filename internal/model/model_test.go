package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateID_DeterministicAndOrderIndependent(t *testing.T) {
	m1 := map[string]any{"a": 1, "b": 2, "page": 3}
	m2 := map[string]any{"page": 3, "b": 2, "a": 1} // same data, diff map order

	id1 := GenerateID("hello world", "text/plain", m1)
	id2 := GenerateID("hello world", "text/plain", m2)
	if id1 != id2 {
		t.Fatalf("GenerateID must be order-independent: %q != %q", id1, id2)
	}
	if id1 == "" {
		t.Fatal("GenerateID must be non-empty")
	}
}

func TestGenerateID_DistinctFromContentHash(t *testing.T) {
	raw := []byte("hello world")
	id := GenerateID(string(raw), "text/plain", map[string]any{})
	ch := ContentHash(raw)

	if id == ch {
		t.Fatal("identity hash must differ from raw-bytes content hash")
	}
	c1, c2 := ContentHash(raw), ContentHash(raw)
	if c1 != c2 {
		t.Fatal("ContentHash must be deterministic")
	}
	if ContentHash([]byte("different")) == ch {
		t.Fatal("distinct content must produce distinct ContentHash")
	}
}

// TestChunk_SectionContext_PreFeatureShape (H23/spec 025, US3-2): a chunk record
// written before the feature (no section_context key) unmarshals cleanly with a
// nil SectionContext — no parse error, so old vaults load without migration.
func TestChunk_SectionContext_PreFeatureShape(t *testing.T) {
	pre := `{"id":"x","document_id":"d","content":"hi","chunk_index":0,"total_chunks":1,"start_char_idx":0,"end_char_idx":2,"token_count":1,"created_at":"2026-01-01T00:00:00Z"}`
	var c Chunk
	if err := json.Unmarshal([]byte(pre), &c); err != nil {
		t.Fatalf("pre-feature chunk must unmarshal: %v", err)
	}
	if c.SectionContext != nil {
		t.Errorf("pre-feature chunk SectionContext = %v, want nil", c.SectionContext)
	}
}

// TestChunk_SectionContext_RoundTrip: a chunk with a breadcrumb round-trips through
// JSON, and a nil SectionContext is omitted (omitempty) so heading-less chunks
// serialize identically to the pre-feature shape.
func TestChunk_SectionContext_RoundTrip(t *testing.T) {
	c := Chunk{ID: "x", SectionContext: []string{"A", "B"}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Chunk
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.SectionContext) != 2 || back.SectionContext[0] != "A" || back.SectionContext[1] != "B" {
		t.Errorf("round-trip SectionContext = %v, want [A B]", back.SectionContext)
	}
	empty, _ := json.Marshal(Chunk{ID: "y"})
	if strings.Contains(string(empty), "section_context") {
		t.Errorf("nil SectionContext should be omitted; got %s", empty)
	}
}
