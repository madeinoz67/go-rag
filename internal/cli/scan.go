package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/watcher"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for changes (use --watch to run continuously)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			watch, _ := cmd.Flags().GetBool("watch")
			pollSec, _ := cmd.Flags().GetInt("poll-interval")

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			root := "."
			if len(cfg.WatchDirs) > 0 && cfg.WatchDirs[0] != "" {
				root = cfg.WatchDirs[0]
			}

			em := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel)
			pl := pipeline.New(db, chunk.NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap), em, index.NewFTS(), index.NewVector(), cfg.Prefixer())
			defer pl.Close()
			cd := watcher.New(db, pl)

			if watch {
				fmt.Printf("Watching %s (poll every %ds). Ctrl-C to stop.\n", root, pollSec)
				return cd.Watch(context.Background(), root, "*", time.Duration(pollSec)*time.Second)
			}

			changes, err := cd.ScanOnce(context.Background(), root, "*")
			if err != nil {
				return err
			}
			for _, c := range changes {
				switch c.Kind {
				case "NEW", "MODIFIED":
					fmt.Printf("[ADDED] %s\n", c.Path)
				case "DELETED":
					fmt.Printf("[DELETED] %s\n", c.Path)
				case "SKIPPED":
					// quiet on no-op
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("watch", false, "watch for changes continuously")
	cmd.Flags().Int("poll-interval", 60, "polling interval in seconds")
	cmd.Flags().Bool("once", true, "scan once and exit")
	return cmd
}
