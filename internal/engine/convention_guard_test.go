package engine_test

// convention_guard_test.go covers the H07 prefix-convention mismatch guard
// (US3): a query whose prefix convention differs from the corpus majority is
// refused, a mixed-convention corpus is refused, and status surfaces the
// convention. The guard reads the profile from 0x04 records, so these tests
// stage records directly with controlled conventions.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// storeEmbed writes one 0x04 embedding record with the given model/convention.
func storeEmbed(t *testing.T, db *storage.DB, id, model, convention string, vec []float32) {
	t.Helper()
	rec := struct {
		Model      string    `json:"model,omitempty"`
		Convention string    `json:"convention,omitempty"`
		Vector     []float32 `json:"vector"`
	}{Model: model, Convention: convention, Vector: vec}
	b, _ := json.Marshal(rec)
	if err := db.SetWithPrefix(storage.PrefixEmbedding, []byte(id), b); err != nil {
		t.Fatalf("store embed: %v", err)
	}
}

// TestQuery_RefusesConventionMismatch_LegacyCorpus: a legacy (unprefixed) corpus
// queried with prefixes enabled MUST be refused — never silently scored across a
// half-prefixed corpus (FR-006, US3 acceptance #1).
func TestQuery_RefusesConventionMismatch_LegacyCorpus(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	storeEmbed(t, db, "c1", "nomic-embed-text", "", []float32{0.1, 0.2}) // legacy unprefixed

	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text" // prefixer resolves convention "nomic"
	eng := engine.NewWithEmbedder(cfg, db, &recordingEmbed{})
	t.Cleanup(eng.Close)

	_, err = eng.Query(context.Background(), engine.QueryRequest{Query: "q", Mode: "semantic", K: 5})
	if err == nil {
		t.Fatal("query against a legacy corpus with prefixes on must be refused")
	}
	if !strings.Contains(err.Error(), "convention") {
		t.Fatalf("error must mention convention; got: %v", err)
	}
}

// TestQuery_RefusesMixedConventionCorpus: a corpus with more than one convention
// is mid-re-embed and MUST be refused (the dim guard cannot exclude the minority,
// which shares dimensionality) — FR-006.
func TestQuery_RefusesMixedConventionCorpus(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	storeEmbed(t, db, "c1", "nomic-embed-text", "nomic", []float32{0.1, 0.2})
	storeEmbed(t, db, "c2", "nomic-embed-text", "", []float32{0.3, 0.4}) // legacy minority

	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text"
	eng := engine.NewWithEmbedder(cfg, db, &recordingEmbed{})
	t.Cleanup(eng.Close)

	_, err = eng.Query(context.Background(), engine.QueryRequest{Query: "q", Mode: "semantic", K: 5})
	if err == nil {
		t.Fatal("query against a mixed-convention corpus must be refused")
	}
	if !strings.Contains(err.Error(), "mixed prefix conventions") {
		t.Fatalf("error must mention mixed conventions; got: %v", err)
	}
}

// TestStatus_SurfacesConvention: status reports the stored majority convention,
// the configured prefix mode, and the resolved role prefixes (SC-003).
func TestStatus_SurfacesConvention(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	storeEmbed(t, db, "c1", "nomic-embed-text", "nomic", []float32{0.1, 0.2})

	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "nomic-embed-text"
	eng := engine.NewWithDB(cfg, db)

	st, err := eng.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.EmbeddingConvention != "nomic" {
		t.Errorf("EmbeddingConvention = %q, want nomic", st.EmbeddingConvention)
	}
	if st.ConfiguredPrefix != "auto" {
		t.Errorf("ConfiguredPrefix = %q, want auto", st.ConfiguredPrefix)
	}
	if st.QueryPrefix != "search_query: " {
		t.Errorf("QueryPrefix = %q, want %q", st.QueryPrefix, "search_query: ")
	}
	if st.DocPrefix != "search_document: " {
		t.Errorf("DocPrefix = %q, want %q", st.DocPrefix, "search_document: ")
	}
	if st.EmbeddingConventionDrift {
		t.Errorf("uniform-convention corpus must not report convention drift")
	}
}
