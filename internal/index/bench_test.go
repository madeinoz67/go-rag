package index

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkRetrieval_Hybrid measures hybrid (vector + BM25, RRF) query latency over
// a 100-chunk index, guarding the <500ms hybrid top-5 budget (PRD §10.1).
func BenchmarkRetrieval_Hybrid(b *testing.B) {
	fts := NewFTS()
	vec := NewVector()
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("c%d", i)
		fts.Index(id, map[string]string{"body": fmt.Sprintf("document chunk number %d about retrieval topics", i)})
		vec.Add(id, []float32{float32(i % 10), 0.1})
	}
	embed := func(_ context.Context, _ []string) ([][]float32, error) {
		return [][]float32{{1.0, 0.1}}, nil
	}
	r := NewRetrieval(fts, vec, embed)

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Search(ctx, "document retrieval topics", 5, ModeHybrid, nil); err != nil {
			b.Fatal(err)
		}
	}
}
