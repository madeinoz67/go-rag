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

// TestChunk_SectionLevel_RoundTrip (spec 041 / BL-005): a chunk with a
// section_level round-trips through JSON, and a zero SectionLevel is omitted
// (omitempty) so heading-less / preamble chunks serialize identically to the
// pre-feature shape.
func TestChunk_SectionLevel_RoundTrip(t *testing.T) {
	c := Chunk{ID: "x", SectionLevel: 3}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Chunk
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.SectionLevel != 3 {
		t.Errorf("round-trip SectionLevel = %d, want 3", back.SectionLevel)
	}
	empty, _ := json.Marshal(Chunk{ID: "y"})
	if strings.Contains(string(empty), "section_level") {
		t.Errorf("zero SectionLevel should be omitted; got %s", empty)
	}
}

// TestChunk_ExtractionQuality_RoundTrip (spec 042 / BL-006): method + quality
// round-trip through JSON; zero values are omitted (omitempty) so pre-feature
// chunks serialize identically to the pre-feature shape.
func TestChunk_ExtractionQuality_RoundTrip(t *testing.T) {
	c := Chunk{ID: "x", ExtractionMethod: "mixed", ExtractionQuality: 0.55}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Chunk
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ExtractionMethod != "mixed" || back.ExtractionQuality != 0.55 {
		t.Errorf("round-trip = %q / %v, want mixed / 0.55", back.ExtractionMethod, back.ExtractionQuality)
	}
	empty, _ := json.Marshal(Chunk{ID: "y"})
	if strings.Contains(string(empty), "extraction_") {
		t.Errorf("zero extraction fields should be omitted; got %s", empty)
	}
}

// TestChunk_ContentHash_RoundTrip (spec 043 / BL-010): ContentHash round-trips
// through JSON; a zero ContentHash is omitted (omitempty) so pre-feature chunks
// serialize identically to the pre-feature shape.
func TestChunk_ContentHash_RoundTrip(t *testing.T) {
	c := Chunk{ID: "x", Content: "hi", ContentHash: ContentHash([]byte("hi"))}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Chunk
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ContentHash != c.ContentHash {
		t.Errorf("round-trip ContentHash = %q, want %q", back.ContentHash, c.ContentHash)
	}
	empty, _ := json.Marshal(Chunk{ID: "y"})
	if strings.Contains(string(empty), "content_hash") {
		t.Errorf("zero ContentHash should be omitted; got %s", empty)
	}
}
