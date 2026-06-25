package cli

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/engine"
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
		RunE: func(_ *cobra.Command, args []string) error {
			path := args[0]
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			// Route through the engine so the cfg-driven pipeline features fire
			// consistently with the daemon: poisoning detection, redaction,
			// near-dup clustering, AND document enrichment (spec 029) when enabled.
			eng := engine.NewWithDB(cfg, db)
			res, err := eng.Reprocess(context.Background(), path)
			eng.Close() // drain async embed + enrich + index
			if err != nil {
				return err
			}
			fmt.Printf("Reprocessed: %d new, %d skipped, %d errors\n", res.New, res.Skipped, res.Errors)
			return nil
		},
	}
	return cmd
}
