package cli

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/spf13/cobra"
)

// newEnrichCmd re-runs document auto-tag + summary enrichment (spec 029) over
// documents not yet enriched (pre-feature) or whose enrichment failed — the
// back-fill path. It uses the configured local model; prints a notice and exits
// when enrichment is disabled (the default).
func newEnrichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enrich",
		Short: "Re-run document auto-tag + summary enrichment (back-fill)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if !cfg.EffectiveEnrichmentEnabled() {
				fmt.Println("Enrichment is disabled (enrichment_enabled=false). Set enrichment_enabled=true and an enrichment_model, then re-run.")
				return nil
			}
			eng := engine.NewWithDB(cfg, db)
			res, err := eng.ReEnrich(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("Re-enriched: %d document(s) (%d errors)\n", res.New, res.Errors)
			return nil
		},
	}
}
