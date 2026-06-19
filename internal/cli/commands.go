package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NOTE: each command is a stub. Behavior is implemented against the PRD in
// later tasks; the scaffold guarantees the CLI surface and flags compile and
// respond. The "see PRD §x" pointers map each stub to its specification.

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the go-rag version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new RAG database",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("go-rag init: not yet implemented (see PRD §5.2)")
			return nil
		},
	}
	cmd.Flags().String("ollama-url", "http://localhost:11434", "Ollama server URL")
	cmd.Flags().String("model", "", "embedding model name (auto-detected if omitted)")
	cmd.Flags().String("watch-dir", ".", "directory to watch")
	cmd.Flags().Int("chunk-size", 512, "chunk size in tokens")
	cmd.Flags().Int("chunk-overlap", 50, "chunk overlap in tokens")
	return cmd
}

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Add files or directories to the database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("go-rag add %q: not yet implemented (see PRD §5.3)\n", args[0])
			return nil
		},
	}
	cmd.Flags().Bool("recursive", true, "recurse into subdirectories")
	cmd.Flags().String("glob", "", "file pattern filter (e.g. \"*.pdf\")")
	cmd.Flags().Bool("dry-run", false, "show what would be added without ingesting")
	return cmd
}

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for changes (use --watch to run continuously)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("go-rag scan: not yet implemented (see PRD §7)")
			return nil
		},
	}
	cmd.Flags().Bool("watch", false, "watch for changes continuously")
	cmd.Flags().Int("poll-interval", 60, "polling interval in seconds")
	cmd.Flags().Bool("once", true, "scan once and exit")
	return cmd
}

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [query]",
		Short: "Search the database (hybrid retrieval by default)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("go-rag query %q: not yet implemented (see PRD §5.5)\n", args[0])
			return nil
		},
	}
	cmd.Flags().Int("k", 5, "number of results")
	cmd.Flags().String("mode", "hybrid", "retrieval mode: hybrid|semantic|keyword")
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	cmd.Flags().String("source", "", "filter by source file glob")
	cmd.Flags().Float64("threshold", 0.0, "minimum relevance score")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show database statistics and health",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("go-rag status: not yet implemented (see PRD §5.6)")
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or change configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("go-rag config: not yet implemented (see PRD §5.7)")
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("go-rag config get %q: not yet implemented\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("go-rag config set %q=%q: not yet implemented\n", args[0], args[1])
			return nil
		},
	})
	return cmd
}
