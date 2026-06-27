package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
	"github.com/spf13/cobra"
)

// newModelCmd groups model-management subcommands (spec 032).
func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage the bundled embedding model",
	}
	cmd.AddCommand(newModelInstallCmd())
	return cmd
}

// newModelInstallCmd fetches and verifies the bundled pure-Go embedding model.
// Idempotent: a no-op when the model is present and its hash matches. The model is
// global (shared across vaults at ~/.go-rag/models/<id>/). With --force, the local
// copy is removed first to re-download.
func newModelInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download and verify the bundled pure-Go embedding model (spec 032)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			if force {
				if dir, err := modelbundle.ModelDir(); err == nil {
					_ = os.RemoveAll(dir)
				}
			}
			dir, err := modelbundle.EnsureModel(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("Bundled model %s ready at %s\n", modelbundle.ModelID, dir)
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "re-download even if the model is present")
	return cmd
}
