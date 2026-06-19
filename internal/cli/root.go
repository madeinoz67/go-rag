// Package cli implements the go-rag command-line interface (PRD §5).
//
// Built on spf13/cobra. The root command wires global flags and registers the
// six subcommands defined in commands.go: init, add, scan, query, status, config.
package cli

import (
	"github.com/spf13/cobra"
)

// Global flags applied to every subcommand.
var (
	dbPath  string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "go-rag",
	Short: "Local RAG database — ingest, index, and query your documents",
	Long: `go-rag is a single-binary local RAG (Retrieval-Augmented Generation) database.

Point it at a directory of PDFs, Word documents, images, and markdown files and it
builds a searchable vector database that answers questions grounded in your local
content — zero external dependencies beyond a local Ollama instance for embeddings.

Full specification: PRD_RAG_Database.md`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		printDashboard()
		return nil
	},
}

// Execute runs the root command. version is injected from main via ldflags.
func Execute(version string) error {
	rootCmd.Version = version
	rootCmd.SetVersionTemplate("go-rag version {{.Version}}\n")

	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "./.go-rag", "path to the database directory")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")

	rootCmd.AddCommand(
		newVersionCmd(version),
		newInitCmd(),
		newAddCmd(),
		newScanCmd(),
		newQueryCmd(),
		newStatusCmd(),
		newConfigCmd(),
		newFilesCmd(),
		newDirsCmd(),
		newStartCmd(),
		newStopCmd(),
		newServeCmd(),
		newReprocessCmd(),
		newMigrateCmd(),
		newMCPCmd(),
	)
	return rootCmd.Execute()
}
