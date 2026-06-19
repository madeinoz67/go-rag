package model

import "testing"

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
	if ContentHash(raw) != ContentHash(raw) {
		t.Fatal("ContentHash must be deterministic")
	}
	if ContentHash([]byte("different")) == ch {
		t.Fatal("distinct content must produce distinct ContentHash")
	}
}
