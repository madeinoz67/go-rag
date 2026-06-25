package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// errEmbedder is an embed.Embedder whose Embed always fails. MigratePlan must
// never call it (FR-004: succeeds with no backend) — if it did, this test would
// surface the error. Model/Dimensions are set so the engine constructs cleanly.
type errEmbedder struct {
	model string
	dim   int
}

func (e errEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("no embedding backend")
}
func (e errEmbedder) Dimensions() int { return e.dim }
func (e errEmbedder) Model() string   { return e.model }

// TestMigratePlan_ReadOnly_NoMutation (H24/spec 028, FR-003 / SC-001): the plan
// is strictly read-only — the corpus profile is byte-identical before and after.
func TestMigratePlan_ReadOnly_NoMutation(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	plantEmbedding(t, db, "stale1", "beta", 8) // a stale minority
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})

	before := CorpusProfile(db)
	if _, err := eng.MigratePlan(); err != nil {
		t.Fatalf("MigratePlan: %v", err)
	}
	after := CorpusProfile(db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("MigratePlan mutated the corpus profile\nbefore=%+v\nafter =%+v", before, after)
	}
}

// TestMigratePlan_NoBackendRequired (FR-004 / SC-002): the plan is metadata-only
// — it succeeds even when the embedding backend always errors (it never embeds).
func TestMigratePlan_NoBackendRequired(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	// Re-open the engine with an embedder that always fails. MigratePlan must
	// still succeed and report the plan.
	eng := NewWithEmbedder(cfg, db, errEmbedder{model: "alpha", dim: 4})
	plan, err := eng.MigratePlan()
	if err != nil {
		t.Fatalf("MigratePlan must succeed with no backend, got %v", err)
	}
	if plan.TargetModel != "alpha" || plan.Total == 0 {
		t.Fatalf("plan should reflect the stored corpus, got %+v", plan)
	}
}

// TestMigratePlan_Deterministic (FR-007): repeated calls on an unchanged corpus
// return identical plans (Sources/Dimensions are sorted).
func TestMigratePlan_Deterministic(t *testing.T) {
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	plantEmbedding(t, db, "stale1", "beta", 8)
	plantEmbedding(t, db, "stale2", "gamma", 8)
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4})

	first, _ := eng.MigratePlan()
	for i := 0; i < 10; i++ {
		again, _ := eng.MigratePlan()
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("MigratePlan is not deterministic\nfirst=%+v\nagain =%+v", first, again)
		}
	}
}

// TestMigratePlan_EstimateAndBreakdown (FR-002/FR-005 / SC-003, US2): the plan
// carries an actionable, honestly-labelled estimate. Mixed corpus → stale count,
// per-source stale flags, dim distribution, consistency=false, estimate set;
// clean corpus → zero stale, consistent.
func TestMigratePlan_EstimateAndBreakdown(t *testing.T) {
	// --- mixed corpus (alpha majority + beta minority), target = alpha ---
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup()
	plantEmbedding(t, db, "stale1", "beta", 8)
	plantEmbedding(t, db, "stale2", "beta", 8)
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "alpha", dim: 4}) // target = alpha

	plan, _ := eng.MigratePlan()
	if plan.TargetModel != "alpha" {
		t.Errorf("TargetModel = %q, want alpha", plan.TargetModel)
	}
	if plan.StaleTotal != 2 {
		t.Errorf("StaleTotal = %d, want 2 (the two beta embeddings)", plan.StaleTotal)
	}
	if plan.Consistent {
		t.Error("mixed corpus must report Consistent=false")
	}
	beta := findSource(plan.Sources, "beta")
	if beta == nil || beta.Count != 2 || !beta.Stale {
		t.Errorf("beta source should be {Count:2, Stale:true}, got %+v", beta)
	}
	alpha := findSource(plan.Sources, "alpha")
	if alpha == nil || alpha.Stale {
		t.Errorf("alpha source should be Stale:false (== target), got %+v", alpha)
	}
	if !hasDim(plan.Dimensions, 8, 2) {
		t.Errorf("Dimensions should include {dim:8, count:2}, got %+v", plan.Dimensions)
	}
	if plan.Estimate.StaleEmbeddings != 2 || !plan.Estimate.MixedCorpus {
		t.Errorf("Estimate wrong, got %+v", plan.Estimate)
	}
	if plan.Estimate.Note != estimateNote {
		t.Errorf("Estimate.Note must carry the estimate disclaimer, got %q", plan.Estimate.Note)
	}

	// --- clean corpus (single model/dim), target == stored model ---
	db2, cfg2, cleanup2 := ingestUnder(t, testEmbedder{model: "alpha", dim: 4})
	defer cleanup2()
	eng2 := NewWithEmbedder(cfg2, db2, testEmbedder{model: "alpha", dim: 4})
	clean, _ := eng2.MigratePlan()
	if clean.StaleTotal != 0 {
		t.Errorf("clean corpus StaleTotal = %d, want 0", clean.StaleTotal)
	}
	if !clean.Consistent {
		t.Error("clean corpus must report Consistent=true")
	}
	if clean.Estimate.StaleEmbeddings != 0 || clean.Estimate.MixedCorpus {
		t.Errorf("clean estimate wrong, got %+v", clean.Estimate)
	}
}

// TestMigratePlan_PreviewMatchesExecution (FR-008 / SC-005): the stale set the
// preview reports is exactly what Migrate acts on — the shared MigratePlan drives
// Migrate's gate. Before migrate the plan shows stale work; after a real migrate
// (under the target model) the plan shows zero stale.
func TestMigratePlan_PreviewMatchesExecution(t *testing.T) {
	// Corpus embedded under "oldmodel"; target switches to "newmodel" → all stale.
	db, cfg, cleanup := ingestUnder(t, testEmbedder{model: "oldmodel", dim: 4})
	defer cleanup()
	cfg.EmbeddingModel = "newmodel"
	eng := NewWithEmbedder(cfg, db, testEmbedder{model: "newmodel", dim: 4})
	defer eng.Close() // drain async-after-ACK re-embedding before the deferred db.Close

	before, _ := eng.MigratePlan()
	if before.StaleTotal == 0 {
		t.Fatal("preview must show stale work before a migrate (gate would proceed)")
	}
	staleBefore := before.StaleTotal

	// Real migrate: re-embeds everything onto newmodel (testEmbedder, no network).
	sum, err := eng.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if sum == nil || sum.New == 0 {
		t.Fatalf("Migrate re-embedded nothing; summary %+v", sum)
	}

	// After: every embedding is now newmodel → preview shows zero stale.
	after, _ := eng.MigratePlan()
	if after.StaleTotal != 0 {
		t.Errorf("after migrate, StaleTotal = %d (was %d before); preview must show 0", after.StaleTotal, staleBefore)
	}
	if !after.Consistent {
		t.Errorf("after migrate the corpus should be consistent, got %+v", after)
	}
}

// --- helpers ---

func findSource(srcs []ModelCount, model string) *ModelCount {
	for i := range srcs {
		if srcs[i].Model == model {
			return &srcs[i]
		}
	}
	return nil
}

func hasDim(dims []DimCount, dim, count int) bool {
	for _, d := range dims {
		if d.Dim == dim && d.Count == count {
			return true
		}
	}
	return false
}
