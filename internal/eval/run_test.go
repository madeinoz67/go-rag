package eval

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// repoGolden returns the absolute path to a file under the repo's committed
// testdata/golden directory, regardless of where `go test` sets cwd.
func repoGolden(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "testdata", "golden", rel)
}

func TestEvalRunner_EndToEnd_CommittedGolden(t *testing.T) {
	ctx := context.Background()
	em := NewDeterministicEmbedder()
	cfg, db, cleanup, err := ProvisionCorpus(ctx, repoGolden(t, "corpus"), em, "")
	if err != nil {
		t.Fatalf("ProvisionCorpus: %v", err)
	}
	defer cleanup()

	golden, err := LoadGolden(repoGolden(t, "v1.jsonl"))
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	r := NewEvalRunner(cfg, db, em)
	run1, err := r.Run(ctx, golden, "hybrid", 10, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run1.QueriesRun != len(golden) || run1.QueriesSkipped != 0 {
		t.Fatalf("scored=%d skipped=%d, want %d/0", run1.QueriesRun, run1.QueriesSkipped, len(golden))
	}
	// Reproducibility: a second run is identical (SC-004).
	run2, _ := r.Run(ctx, golden, "hybrid", 10, true)
	if run1.Metrics != run2.Metrics {
		t.Fatalf("non-reproducible: %#v != %#v", run1.Metrics, run2.Metrics)
	}
	// Sanity: with a clean 6-chunk corpus, every relevant chunk is retrievable.
	if run1.Metrics.RecallAt10 != 1.0 {
		t.Fatalf("recall@10 = %v, want 1.0", run1.Metrics.RecallAt10)
	}
	if run1.Metrics.MRR < 0.5 {
		t.Fatalf("MRR = %v, want > 0.5 (retrieval should rank relevant highly)", run1.Metrics.MRR)
	}
}

func TestEvalRunner_ReadOnly(t *testing.T) {
	// FR-006: an Evaluation Run MUST NOT mutate the vault. Snapshot the chunk
	// count before and after; it must be identical.
	ctx := context.Background()
	em := NewDeterministicEmbedder()
	cfg, db, cleanup, err := ProvisionCorpus(ctx, repoGolden(t, "corpus"), em, "")
	if err != nil {
		t.Fatalf("ProvisionCorpus: %v", err)
	}
	defer cleanup()

	golden, _ := LoadGolden(repoGolden(t, "v1.jsonl"))
	before := countChunks(t, db)

	if _, err := NewEvalRunner(cfg, db, em).Run(ctx, golden, "hybrid", 10, true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	after := countChunks(t, db)
	if before != after {
		t.Fatalf("vault mutated by eval: chunk count %d -> %d", before, after)
	}
}

func TestEvalRunner_SkipsZeroRelevant(t *testing.T) {
	// FR-008: a golden query with no labeled relevant chunks is skipped, not
	// scored (and does not crash a divide-by-zero average).
	ctx := context.Background()
	em := NewDeterministicEmbedder()
	cfg, db, cleanup, err := ProvisionCorpus(ctx, repoGolden(t, "corpus"), em, "")
	if err != nil {
		t.Fatalf("ProvisionCorpus: %v", err)
	}
	defer cleanup()

	golden := []GoldenQuery{
		{ID: "z1", Query: "anything", Relevant: nil},
		{ID: "z2", Query: "how does chunking work", Relevant: firstRelevant(t)},
	}
	r, err := NewEvalRunner(cfg, db, em).Run(ctx, golden, "hybrid", 10, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.QueriesRun != 1 || r.QueriesSkipped != 1 {
		t.Fatalf("scored=%d skipped=%d, want 1/1", r.QueriesRun, r.QueriesSkipped)
	}
}

func TestEvalRunner_SkipsStaleLabels(t *testing.T) {
	// FR-008: a query whose labeled relevant chunk_id does not exist in the vault
	// (stale label) is skipped with a reason.
	ctx := context.Background()
	em := NewDeterministicEmbedder()
	cfg, db, cleanup, err := ProvisionCorpus(ctx, repoGolden(t, "corpus"), em, "")
	if err != nil {
		t.Fatalf("ProvisionCorpus: %v", err)
	}
	defer cleanup()

	golden := []GoldenQuery{
		{ID: "s1", Query: "stale query", Relevant: []string{"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}},
	}
	r, err := NewEvalRunner(cfg, db, em).Run(ctx, golden, "hybrid", 10, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.QueriesRun != 0 || r.QueriesSkipped != 1 {
		t.Fatalf("scored=%d skipped=%d, want 0/1", r.QueriesRun, r.QueriesSkipped)
	}
}

func TestCompare_GatePassAndFail(t *testing.T) {
	// T018: the gate fails only when recall@10 drops beyond tolerance.
	run := &EvaluationRun{Metrics: MetricSet{RecallAt10: 0.80, MRR: 0.6, NDCGAt10: 0.7}}
	base := &Baseline{Metrics: MetricSet{RecallAt10: 0.82, MRR: 0.6, NDCGAt10: 0.7}} // 2pt drop

	if c := Compare(run, base, 2.0); !c.Pass {
		t.Fatalf("drop exactly at tolerance should PASS, got %+v", c)
	}
	if c := Compare(run, base, 1.0); c.Pass {
		t.Fatalf("drop beyond tolerance should FAIL, got %+v", c)
	}
	// Improvement always passes.
	better := &Baseline{Metrics: MetricSet{RecallAt10: 0.70}}
	if c := Compare(run, better, 0.5); !c.Pass {
		t.Fatalf("improvement should PASS, got %+v", c)
	}
	// No baseline → always pass.
	if c := Compare(run, nil, 1.0); !c.Pass {
		t.Fatalf("nil baseline should PASS, got %+v", c)
	}
}

func TestBaseline_RoundTrip(t *testing.T) {
	bl := &Baseline{Mode: "deterministic-hash", Embedder: "deterministic-hash", Metrics: MetricSet{RecallAt10: 0.9, MRR: 0.5}}
	p := filepath.Join(t.TempDir(), "baseline.json")
	if err := bl.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadBaseline(p)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if loaded.Metrics != bl.Metrics {
		t.Fatalf("round-trip mismatch: %#v != %#v", loaded.Metrics, bl.Metrics)
	}
}

// countChunks counts stored chunk records — used to assert eval never writes.
func countChunks(t *testing.T, db *storage.DB) int {
	t.Helper()
	n := 0
	if err := db.PrefixScanByte(storage.PrefixChunk, func(_, _ []byte) bool {
		n++
		return true
	}); err != nil {
		t.Fatalf("scan chunks: %v", err)
	}
	return n
}

// firstRelevant returns one real chunk_id from the committed golden set, so the
// "skip" tests have at least one scoreable query alongside the skipped ones.
func firstRelevant(t *testing.T) []string {
	t.Helper()
	golden, err := LoadGolden(repoGolden(t, "v1.jsonl"))
	if err != nil || len(golden) == 0 {
		t.Fatal("need committed golden set for fixture")
	}
	return golden[0].Relevant
}
