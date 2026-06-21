package cli

import (
	"context"
	"fmt"
	"sort"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/spf13/cobra"
)

// newMigrateCmd re-embeds all documents whose embeddings were made with a different
// model than the currently configured one (T048). After a model change, every
// embedding is stale (you can't mix embedding spaces), so this re-embeds all.
func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Re-embed all documents to the current embedding model",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			current := cfg.EmbeddingModel
			stats := pipeline.EmbeddingModelStats(db)
			fmt.Printf("Current embedding model: %s\n", current)
			if len(stats) == 0 {
				fmt.Println("No tracked embeddings yet — nothing to migrate.")
				return nil
			}

			models := make([]string, 0, len(stats))
			for m := range stats {
				models = append(models, m)
			}
			sort.Strings(models)
			stale := 0
			for _, m := range models {
				flag := ""
				if m != current {
					flag = "  <- stale (will be re-embedded)"
					stale += stats[m]
				}
				fmt.Printf("  %d embeddings on %s%s\n", stats[m], m, flag)
			}

			if stale == 0 {
				fmt.Println("All embeddings already use the current model.")
				return nil
			}

			fmt.Printf("Re-embedding %d stale embedding(s) to %s...\n", stale, current)
			em := embed.NewOllama(cfg.OllamaURL, current)
			p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector(), cfg.Prefixer())
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
}
