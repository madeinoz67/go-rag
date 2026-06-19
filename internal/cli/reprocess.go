package cli

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/spf13/cobra"
)

// newReprocessCmd force-reingests a directory, bypassing content-hash dedup so the
// current reader + embedder apply to already-ingested files (T047). Use after a
// reader or embedding-model change instead of `rm -rf .go-rag`.
func newReprocessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reprocess [path]",
		Short: "Force re-ingest of a directory (applies current reader/embedder; bypasses dedup)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			em := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel)
			p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector())
			p.OnProgress = progressBar
			res, err := p.Reprocess(context.Background(), path, "*")
			p.Close() // drain async embedding/indexing
			if err != nil {
				return err
			}
			fmt.Printf("Reprocessed: %d files (%d unsupported, %d errors)\n", res.New, res.Unsupported, res.Errors)
			return nil
		},
	}
	return cmd
}
