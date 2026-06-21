package eval

import (
	"context"
	"testing"
)

// TestDeterministicEmbedder_RoleAware proves the offline embedder distinguishes
// query-role and document-role text once the instruction prefix is applied (audit
// H07, research D5). The DeterministicEmbedder hashes the FULL input text, so
// prefixing the same base text differently per role yields different vectors — no
// embedder code change is required (constitution Principle V: the role signal is
// applied at the prefix boundary, not inside the embedder). This is how the eval
// harness exercises the query-vs-document mechanism deterministically in CI;
// the real quality gain (recall/NDCG) is a manual step against a live model.
func TestDeterministicEmbedder_RoleAware(t *testing.T) {
	em := NewDeterministicEmbedder()
	base := "what is retrieval-augmented generation"

	q, err := em.Embed(context.Background(), []string{"search_query: " + base})
	if err != nil || len(q) != 1 {
		t.Fatalf("query embed: %v len=%d", err, len(q))
	}
	d, err := em.Embed(context.Background(), []string{"search_document: " + base})
	if err != nil || len(d) != 1 {
		t.Fatalf("document embed: %v len=%d", err, len(d))
	}
	if sameVec(q[0], d[0]) {
		t.Fatal("query- and document-prefixed vectors must differ (role-aware mechanism)")
	}

	// Determinism: identical prefixed input reproduces the same vector.
	q2, _ := em.Embed(context.Background(), []string{"search_query: " + base})
	if !sameVec(q[0], q2[0]) {
		t.Fatal("identical prefixed input must reproduce the same vector")
	}
}

func sameVec(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
