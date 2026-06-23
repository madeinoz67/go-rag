package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/spf13/cobra"
)

// newPoisonCmd is the H04/spec 019 quarantine management surface (US2, FR-006):
// list flagged chunks (with the per-signal breakdown so a user can see why each
// was flagged), and release/reset a false positive. Non-destructive — content is
// never deleted; only the verdict level + quarantine-index entry change.
func newPoisonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "poison",
		Short: "Manage chunks flagged as injection-poisoning (list / release / reset)",
	}
	cmd.AddCommand(newPoisonListCmd(), newPoisonReleaseCmd(), newPoisonResetCmd(), newPoisonRescanCmd())
	return cmd
}

// newPoisonRescanCmd re-scores the whole stored corpus against the current
// detector (US3, FR-007, and the US4 T031 manual trigger): pre-feature chunks get
// verdicts and a threshold/detector change applies to the back-catalog, without
// re-ingesting source files. Idempotent.
func newPoisonRescanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rescan",
		Short: "Re-score the whole corpus against the current detector (idempotent; no re-ingest)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			rescored, flagged, err := engine.NewWithDB(cfg, db).RescanPoisoning()
			if err != nil {
				return err
			}
			fmt.Printf("Rescan: %d chunks (re)scored, %d flagged.\n", rescored, flagged)
			return nil
		},
	}
}

func newPoisonListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List chunks flagged as injection-poisoning (excluded from default results)",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			flagged, err := engine.NewWithDB(cfg, db).ListPoisoned()
			if err != nil {
				return err
			}
			if len(flagged) == 0 {
				fmt.Println("No flagged chunks.")
				return nil
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(flagged)
			}
			for i, f := range flagged {
				fmt.Printf("[%d] %s  (level %s, score %.2f)\n", i+1, f.ChunkID, f.Verdict.Level, f.Verdict.Score)
				fmt.Printf("    %s\n", preview(f.Preview, 200))
				if len(f.Verdict.MatchedPhrases) > 0 {
					fmt.Printf("    matched: %s\n", strings.Join(f.Verdict.MatchedPhrases, ", "))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	return cmd
}

func newPoisonReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release <chunk_id>",
		Short: "Release a flagged chunk (false-positive override) — makes it retrievable by default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := engine.NewWithDB(cfg, db).ReleaseChunk(args[0]); err != nil {
				return err
			}
			fmt.Printf("Released %s — now retrievable by default.\n", args[0])
			return nil
		},
	}
}

func newPoisonResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <chunk_id>",
		Short: "Undo a release — re-quarantines the chunk if its score is flagged",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := engine.NewWithDB(cfg, db).ResetChunk(args[0]); err != nil {
				return err
			}
			fmt.Printf("Reset %s — re-evaluated against thresholds.\n", args[0])
			return nil
		},
	}
}
