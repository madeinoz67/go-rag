package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// init/add/query live in init.go, add.go, query.go; status in status.go; config in
// config_cli.go. Only version and scan remain here (scan is a stub until US4).

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
