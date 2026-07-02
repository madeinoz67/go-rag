package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// writeFakeEmbedding seeds a PrefixEmbedding record for a chunk (simulates a
// prior embed by the background embedder, which newTestPipeline does not run).
// The JSON shape matches embedRecord {Model, Convention, Vector}.
func writeFakeEmbedding(t *testing.T, p *Pipeline, chunkID, model string) {
	t.Helper()
	rec := []byte(`{"Model":"` + model + `","Convention":"","Vector":[1.0,0.1]}`)
	if err := p.db.SetWithPrefix(storage.PrefixEmbedding, []byte(chunkID), rec); err != nil {
		t.Fatalf("write embedding: %v", err)
	}
}

// hasEmbedding checks if a PrefixEmbedding record exists for chunkID.
func hasEmbedding(p *Pipeline, chunkID string) bool {
	_, ok, _ := p.db.GetWithPrefix(storage.PrefixEmbedding, []byte(chunkID))
	return ok
}

// TestReingest_PreservesEmbeddings (spec 043 / BL-010 US2, T016): a Reprocess
// of an unchanged doc with matching-model embeddings preserves them —
// preserveEmbeds copies the old PrefixEmbedding to the new cid, so the
// embedding survives the delete+re-ingest. Without preserveEmbeds, the
// embedding would be deleted by DeleteDoc and never restored (no embedder
// processor runs in newTestPipeline).
func TestReingest_PreservesEmbeddings(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	dir := t.TempDir()
	content := ""
	for i := 0; i < 60; i++ {
		content += "the quick brown fox jumps over the lazy dog. "
	}
	path := filepath.Join(dir, "doc.txt")
	writeFile(t, path, content)
	p.Ingest(context.Background(), dir, "*")

	// Seed embeddings (simulate a prior embed; model "fake" matches the fakeEmbed).
	docID := docIDForPath(t, p, path)
	for _, c := range p.chunksOfDoc(docID) {
		writeFakeEmbedding(t, p, c.ID, "fake")
	}

	// Reprocess (delete + re-ingest the unchanged doc).
	p.Reprocess(context.Background(), dir, "*")

	// The new chunk IDs are the same (content-addressed, unchanged content).
	// preserveEmbeds should have copied the old PrefixEmbedding → embeddings exist.
	newDocID := docIDForPath(t, p, path)
	if newDocID == "" {
		t.Fatal("doc not found after Reprocess")
	}
	for _, c := range p.chunksOfDoc(newDocID) {
		if !hasEmbedding(p, c.ID) {
			t.Errorf("chunk %s: PrefixEmbedding missing after re-ingest (preserveEmbeds failed)", c.ID)
		}
	}
}

// TestReingest_DoesNotPreserveStaleEmbeddings (spec 043 / BL-010 US2, T016):
// when the old embedding's model differs from the current embedder's model,
// preserveEmbeds must NOT copy it (the vector is stale — a model change means
// the dimensions/semantics changed).
func TestReingest_DoesNotPreserveStaleEmbeddings(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	dir := t.TempDir()
	content := ""
	for i := 0; i < 60; i++ {
		content += "the quick brown fox jumps over the lazy dog. "
	}
	path := filepath.Join(dir, "doc.txt")
	writeFile(t, path, content)
	p.Ingest(context.Background(), dir, "*")

	// Seed embeddings with a DIFFERENT model ("stale-model" ≠ fakeEmbed's "fake").
	docID := docIDForPath(t, p, path)
	for _, c := range p.chunksOfDoc(docID) {
		writeFakeEmbedding(t, p, c.ID, "stale-model")
	}

	// Reprocess.
	p.Reprocess(context.Background(), dir, "*")

	// The stale embeddings should NOT have been copied (model mismatch).
	newDocID := docIDForPath(t, p, path)
	for _, c := range p.chunksOfDoc(newDocID) {
		if hasEmbedding(p, c.ID) {
			t.Errorf("chunk %s: stale PrefixEmbedding was copied (model gate failed)", c.ID)
		}
	}
}
