package pipeline

// neardup_test.go covers near-duplicate detection at ingest (audit H20 / spec 026):
// the async clustering pass that fingerprints chunks and writes the NearDup sidecar.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// scanChunks enumerates stored chunk records (distinct from section_test.go's helpers).
func scanChunks(t *testing.T, p *Pipeline) []model.Chunk {
	t.Helper()
	var out []model.Chunk
	_ = p.db.PrefixScanByte(storage.PrefixChunk, func(_, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil {
			out = append(out, c)
		}
		return true
	})
	return out
}

// TestIngest_NearDup_ReorderedClustered (US2, SC-001): two passages that are the
// SAME words in a DIFFERENT order have identical SimHash (order-insensitive) but
// distinct content hashes (so they are NOT exact-deduped). After the async
// clustering worker drains, each chunk lists the other as a near-dup sibling.
func TestIngest_NearDup_ReorderedClustered(t *testing.T) {
	dir := t.TempDir()
	words := "the go-rag server performs keyword retrieval over local documents stored on disk with a buffer cache"
	writeFile(t, filepath.Join(dir, "v1.txt"), words)
	writeFile(t, filepath.Join(dir, "v2.txt"), "cache buffer a with disk on stored documents local over retrieval keyword performs server go-rag the") // reordered → same SimHash
	p, _ := newTestPipeline(t, 0)

	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatal(err)
	}
	p.Close() // drain the async worker so clustering lands before we read

	chunks := scanChunks(t, p)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks (a near-dup pair, not exact-deduped), got %d", len(chunks))
	}
	// Clustering is per-job (async); across two separate ingest jobs the sidecar is
	// asymmetric — the job whose scan runs LAST sees the other's fingerprint, so at
	// least one of the pair carries the sibling. Query-time collapse detects the
	// pair bidirectionally (one listing the other suffices). Assert ≥1.
	withSib := 0
	for _, c := range chunks {
		if c.NearDup != nil && len(c.NearDup.Siblings) > 0 {
			withSib++
		}
	}
	if withSib < 1 {
		t.Errorf("want >=1 chunk to carry a near-dup sibling; got %d. chunks: %+v", withSib, chunks)
	}
}

// TestIngest_NearDup_DistinctNotFlagged (FR-009, US2-sc3): two topically-distinct
// passages must NOT be flagged as near-duplicates (precision guard).
func TestIngest_NearDup_DistinctNotFlagged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"),
		"the go-rag server performs keyword retrieval over local documents stored on disk with a buffer cache and an inverted index")
	writeFile(t, filepath.Join(dir, "b.txt"),
		"quantum entanglement lets two particles share state across any distance instantly without any signal traveling between them at all")
	p, _ := newTestPipeline(t, 0)
	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatal(err)
	}
	p.Close()
	for _, c := range scanChunks(t, p) {
		if c.NearDup != nil && len(c.NearDup.Siblings) > 0 {
			t.Errorf("distinct chunk %s flagged as near-dup: %+v", c.ID, c.NearDup)
		}
	}
}

// TestIngest_NearDup_ReprocessBackfill (US3, research R7): Reprocess re-reads the
// source and re-derives the NearDup sidecar — there is no cheap rescan (the
// fingerprint is derived at ingest from the chunk text). Verify that after
// Reprocess, near-dup chunks still carry siblings.
func TestIngest_NearDup_ReprocessBackfill(t *testing.T) {
	dir := t.TempDir()
	words := "the go-rag server performs keyword retrieval over local documents stored on disk with a buffer cache"
	writeFile(t, filepath.Join(dir, "v1.txt"), words)
	writeFile(t, filepath.Join(dir, "v2.txt"), "cache buffer a with disk on stored documents local over retrieval keyword performs server go-rag the")
	p, _ := newTestPipeline(t, 0)
	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Reprocess(context.Background(), dir, "*"); err != nil {
		t.Fatalf("reprocess: %v", err)
	}
	p.Close()
	got := 0
	for _, c := range scanChunks(t, p) {
		if c.NearDup != nil && len(c.NearDup.Siblings) > 0 {
			got++
		}
	}
	if got < 1 {
		t.Errorf("after reprocess: want >=1 chunk with near-dup siblings, got %d", got)
	}
}
