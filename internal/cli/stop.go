package cli

import (
	"fmt"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running MCP daemon",
		RunE: func(_ *cobra.Command, _ []string) error {
			pid, _ := daemon.ReadPID(dbPath)
			if err := daemon.Stop(dbPath); err != nil {
				return err
			}
			fmt.Printf("go-rag stopped (pid %d)\n", pid)
			return nil
		},
	}
}
