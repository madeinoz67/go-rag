package engine

import (
	"sort"

	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// MigrationPlan is the read-only preview of what Engine.Migrate would do, without
// re-embedding anything (audit H24 / spec 028). It is computed purely from stored
// metadata — pipeline.EmbeddingModelStats (per-model counts, the same reader
// Migrate gates on, so the preview and execution agree — FR-008) plus
// CorpusProfile (dimensionality distribution + consistency). It never constructs
// an Embedder, flushes caches, reprocesses, or refreshes the baseline, so it is
// strictly read-only (FR-003) and succeeds with no embedding backend (FR-004).
type MigrationPlan struct {
	TargetModel string       // cfg.EmbeddingModel — the model a real migrate re-embeds onto
	Total       int          // total tracked embeddings (EmbeddingModelStats sum)
	StaleTotal  int          // embeddings whose model != TargetModel (a real migrate re-embeds these)
	Sources     []ModelCount // one row per stored model, sorted by model for determinism
	Dimensions  []DimCount   // stored dimensionality distribution (CorpusProfile), sorted by dim
	Consistent  bool         // single model+dim+convention (CorpusProfile)
	Estimate    Estimate     // labelled-approximate cost summary (FR-005)
}

// ModelCount is one row of the per-model breakdown in a MigrationPlan.
type ModelCount struct {
	Model string
	Count int
	Stale bool // Model != TargetModel
}

// DimCount is one row of the stored dimensionality distribution in a MigrationPlan.
type DimCount struct {
	Dim   int
	Count int
}

// Estimate is the labelled-approximate cost of a migration (FR-005). It is an
// effort proxy (the count of embeddings to regenerate + the kind of change), NOT
// a wall-clock prediction — predicting time needs the live backend, which the
// dry-run must not require (FR-004). The Note always states this.
type Estimate struct {
	StaleEmbeddings int    // == MigrationPlan.StaleTotal
	ModelChange     bool   // the corpus majority model differs from the target
	MixedCorpus     bool   // the corpus is not single-model/dim/convention
	Note            string // always: an estimate, not a time guarantee
}

// estimateNote is the constant effort-proxy disclaimer carried on every plan.
const estimateNote = "estimate — an effort proxy (re-embedding count), not a time guarantee"

// MigratePlan is the engine-bound preview: it delegates to MigratePlanFor with
// the configured target model. Read-only; used by the transports and by Migrate
// (FR-008: Migrate calls this first so preview and execution share one path).
func (e *Engine) MigratePlan() (*MigrationPlan, error) {
	return MigratePlanFor(e.db, e.cfg.EmbeddingModel)
}

// MigratePlanFor computes a read-only migration plan from stored metadata alone
// (no Engine required). It mirrors CorpusProfile's package-level shape so the CLI
// — which opens a DB but does not construct an Engine — can preview a migration.
// Read-only and backend-free (FR-003/FR-004): it reaches only EmbeddingModelStats
// + CorpusProfile, both Pebble prefix scans. Deterministic (FR-007): Sources and
// Dimensions are sorted, so repeated calls on an unchanged corpus are identical.
func MigratePlanFor(db *storage.DB, target string) (*MigrationPlan, error) {
	stats := pipeline.EmbeddingModelStats(db)
	prof := CorpusProfile(db)

	plan := &MigrationPlan{
		TargetModel: target,
		Consistent:  prof.Consistent,
	}

	// Sources + Total + StaleTotal from EmbeddingModelStats (the same reader Migrate
	// gates on → preview == execution). Sorted by model for determinism.
	models := make([]string, 0, len(stats))
	for m := range stats {
		models = append(models, m)
	}
	sort.Strings(models)
	for _, m := range models {
		stale := m != target
		plan.Sources = append(plan.Sources, ModelCount{Model: m, Count: stats[m], Stale: stale})
		plan.Total += stats[m]
		if stale {
			plan.StaleTotal += stats[m]
		}
	}

	// Dimensions from CorpusProfile (stored dim distribution). Sorted by dim.
	dims := make([]int, 0, len(prof.DimCounts))
	for d := range prof.DimCounts {
		dims = append(dims, d)
	}
	sort.Ints(dims)
	for _, d := range dims {
		plan.Dimensions = append(plan.Dimensions, DimCount{Dim: d, Count: prof.DimCounts[d]})
	}

	plan.Estimate = Estimate{
		StaleEmbeddings: plan.StaleTotal,
		ModelChange:     prof.MajorityModel != "" && prof.MajorityModel != target,
		MixedCorpus:     !prof.Consistent,
		Note:            estimateNote,
	}
	return plan, nil
}
