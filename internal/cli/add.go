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

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Add files or directories to the database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			glob, _ := cmd.Flags().GetString("glob")
			if glob == "" {
				glob = "*"
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			_ = dryRun // MVP: always ingest (dry-run listed in contracts; full impl in polish)

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			em := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel)
			p := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector(), cfg.Prefixer())
			p.OnProgress = progressBar
			res, err := p.Ingest(context.Background(), path, glob)
			p.Close() // drain async embedding+indexing
			if err != nil {
				return err
			}

			fmt.Printf("Processed: %d new, %d skipped, %d unsupported, %d errors\n", res.New, res.Skipped, res.Unsupported, res.Errors)
			if res.New > 0 {
				fmt.Println("Embedding/indexing completed.")
			}
			return nil
		},
	}
	cmd.Flags().Bool("recursive", true, "recurse into subdirectories")
	cmd.Flags().String("glob", "", "file pattern filter (e.g. \"*.pdf\")")
	cmd.Flags().Bool("dry-run", false, "show what would be added without ingesting")
	return cmd
}
