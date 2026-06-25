package cli

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/engine"
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

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			// --redact opts into the engine's PII redaction (bound when
			// PIIRedactEnabled), so the CLI matches the daemon's redaction behaviour.
			if redactOn, _ := cmd.Flags().GetBool("redact"); redactOn {
				cfg.PIIRedactEnabled = true
			}

			// Route through the engine so the cfg-driven pipeline features fire
			// consistently with the daemon: poisoning detection, redaction,
			// near-dup clustering, AND document enrichment (spec 029) when
			// enrichment_enabled. (The raw-pipeline path the CLI used before bound
			// none of these, so `go-rag add` never enriched.)
			eng := engine.NewWithDB(cfg, db)
			res, err := eng.Add(context.Background(), path, glob)
			eng.Close() // drain async embed + enrich + index
			if err != nil {
				return err
			}

			fmt.Printf("Processed: %d new, %d skipped, %d errors\n", res.New, res.Skipped, res.Errors)
			if res.New > 0 {
				fmt.Println("Embedding/indexing completed.")
			}
			return nil
		},
	}
	cmd.Flags().Bool("recursive", true, "recurse into subdirectories")
	cmd.Flags().String("glob", "", "file pattern filter (e.g. \"*.pdf\")")
	cmd.Flags().Bool("dry-run", false, "show what would be added without ingesting")
	cmd.Flags().Bool("redact", false, "redact detected secrets/PII before indexing (opt-in)")
	return cmd
}
