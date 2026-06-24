package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/spf13/cobra"
)

// newThreatCmd is the H04/spec 019 threat-list management surface (US4, FR-012/
// 013, D12): import an instruction-phrase source from a file or URL (the URL fetch
// is the ONLY network egress — Constitution I), list sources, and add/remove
// phrases. Importing/adding triggers a rescan so newly-matching chunks are flagged.
func newThreatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "threat",
		Short: "Manage instruction-phrase sources for poisoning detection (import/list/add/remove)",
	}
	cmd.AddCommand(newThreatImportCmd(), newThreatListCmd(), newThreatAddCmd(), newThreatRemoveCmd())
	return cmd
}

func newThreatImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <path|url>",
		Short: "Import an instruction-phrase source from a file or URL (URL fetch is the only network egress)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).ImportThreatSource(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Imported %d phrases from %s (id %s); rescan: %d rescored, %d flagged.\n",
				res.Added, res.Origin, res.ID, res.Rescored, res.Flagged)
			return nil
		},
	}
}

func newThreatListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed instruction-phrase sources (the built-in list is always active)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			srcs, err := engine.NewWithDB(cfg, db).ListThreatSources()
			if err != nil {
				return err
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(srcs)
			}
			if len(srcs) == 0 {
				fmt.Println("No managed sources (using the built-in list only).")
				return nil
			}
			for _, s := range srcs {
				fmt.Printf("%s  enabled=%v  phrases=%d  origin=%s\n", s.ID, s.Enabled, len(s.Phrases), s.Origin)
			}
			return nil
		},
	}
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	return cmd
}

func newThreatAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <phrase>",
		Short: "Add an instruction phrase to the manual (user) source (triggers a rescan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).AddPhrases([]string{args[0]})
			if err != nil {
				return err
			}
			fmt.Printf("Added; user source now %d phrases; rescan: %d rescored, %d flagged.\n",
				res.Added, res.Rescored, res.Flagged)
			return nil
		},
	}
}

func newThreatRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a managed phrase source by id (triggers a rescan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := engine.NewWithDB(cfg, db).RemoveThreatSource(args[0]); err != nil {
				return err
			}
			fmt.Printf("Removed source %s.\n", args[0])
			return nil
		},
	}
}
