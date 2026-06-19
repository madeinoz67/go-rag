package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// init/add/query are implemented in init.go, add.go, query.go. The commands below
// (version/scan/status/config) are stubs until their user stories (US2/US3/US4).

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the go-rag version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}
}

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan for changes (use --watch to run continuously)",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("go-rag scan: not yet implemented (see PRD §7)")
			return nil
		},
	}
	cmd.Flags().Bool("watch", false, "watch for changes continuously")
	cmd.Flags().Int("poll-interval", 60, "polling interval in seconds")
	cmd.Flags().Bool("once", true, "scan once and exit")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show database statistics and health",
		RunE: func(_ *cobra.Command, _ []string) error {
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
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("go-rag config: not yet implemented (see PRD §5.7)")
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("go-rag config get: not yet implemented")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("go-rag config set: not yet implemented")
			return nil
		},
	})
	return cmd
}
