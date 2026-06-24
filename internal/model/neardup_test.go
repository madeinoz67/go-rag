package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestChunk_NearDup_PreFeatureShape (H20/spec 026, US3-2): a chunk record written
// before the feature (no near_dup key) unmarshals cleanly with nil NearDup — no
// parse error, so old vaults load without migration.
func TestChunk_NearDup_PreFeatureShape(t *testing.T) {
	pre := `{"id":"x","document_id":"d","content":"hi","chunk_index":0,"total_chunks":1,"start_char_idx":0,"end_char_idx":2,"token_count":1,"created_at":"2026-01-01T00:00:00Z"}`
	var c Chunk
	if err := json.Unmarshal([]byte(pre), &c); err != nil {
		t.Fatalf("pre-feature chunk must unmarshal: %v", err)
	}
	if c.NearDup != nil {
		t.Errorf("pre-feature chunk NearDup = %v, want nil", c.NearDup)
	}
}

// TestChunk_NearDup_RoundTrip: a chunk with near-dup info round-trips through JSON;
// nil NearDup is omitted (omitempty) so pre-feature serialization is preserved.
func TestChunk_NearDup_RoundTrip(t *testing.T) {
	c := Chunk{ID: "x", NearDup: &NearDupInfo{Siblings: []string{"a"}, Similarity: 0.9}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Chunk
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.NearDup == nil || back.NearDup.Similarity != 0.9 {
		t.Errorf("round-trip NearDup = %+v, want similarity=0.9", back.NearDup)
	}
	empty, _ := json.Marshal(Chunk{ID: "y"})
	if strings.Contains(string(empty), "near_dup") {
		t.Errorf("nil NearDup should be omitted; got %s", empty)
	}
}
