package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/spf13/cobra"
)

// newMigrateCmd re-embeds documents whose embeddings use a different model than
// the configured one. --dry-run (H24/spec 028) previews the read-only migration
// plan and exits without re-embedding; a real migrate renders the SAME plan then
// proceeds. The plan comes from engine.MigratePlanFor (metadata-only, no backend).
func newMigrateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Re-embed all documents to the current embedding model",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			// The plan is computed once, read-only, from stored metadata (no embedding,
			// no backend). --dry-run renders it and exits; a real migrate renders the
			// same plan, then proceeds when there is stale work.
			plan, err := engine.MigratePlanFor(db, cfg.EmbeddingModel)
			if err != nil {
				return err
			}
			renderMigrationPlan(plan)
			if dryRun {
				return nil
			}
			if plan.StaleTotal == 0 {
				return nil
			}

			fmt.Printf("Re-embedding %d stale embedding(s) to %s...\n", plan.StaleTotal, cfg.EmbeddingModel)
			em := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel)
			p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(db.Pebble()), index.NewVector(), cfg.Prefixer())
			p.OnProgress = progressBar
			res, err := p.ReprocessAll(context.Background())
			p.Close() // drain async embedding
			if err != nil {
				return err
			}
			fmt.Printf("Migrated: %d files re-embedded (%d unsupported, %d errors)\n", res.New, res.Unsupported, res.Errors)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview the migration plan and exit without re-embedding")
	return cmd
}

// renderMigrationPlan prints the read-only migration preview (H24/spec 028):
// target model, per-source counts with stale markers, the stored dimensionality
// distribution + consistency flag, and a labelled-approximate estimate.
func renderMigrationPlan(p *engine.MigrationPlan) {
	fmt.Printf("Target embedding model: %s\n", p.TargetModel)
	if p.Total == 0 {
		fmt.Println("No tracked embeddings yet — nothing to migrate.")
		return
	}
	fmt.Printf("Total tracked embeddings: %d (stale: %d)\n", p.Total, p.StaleTotal)
	for _, s := range p.Sources {
		flag := ""
		if s.Stale {
			flag = "  <- stale (will be re-embedded)"
		}
		fmt.Printf("  %d embeddings on %s%s\n", s.Count, s.Model, flag)
	}
	if len(p.Dimensions) > 0 {
		parts := make([]string, 0, len(p.Dimensions))
		for _, d := range p.Dimensions {
			parts = append(parts, fmt.Sprintf("%dd×%d", d.Dim, d.Count))
		}
		fmt.Printf("Dimensionality: %s (%s)\n", strings.Join(parts, ", "), consistencyLabel(p.Consistent))
	}
	if p.StaleTotal == 0 {
		fmt.Println("All embeddings already use the target model.")
		return
	}
	fmt.Printf("Estimate: ~%d embedding(s) to regenerate. %s\n", p.Estimate.StaleEmbeddings, p.Estimate.Note)
}

func consistencyLabel(consistent bool) string {
	if consistent {
		return "consistent"
	}
	return "MIXED"
}
