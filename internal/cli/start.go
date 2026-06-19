package cli

import (
	"fmt"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the MCP daemon in the background",
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, _ := cmd.Flags().GetString("mcp-addr")
			if err := daemon.Start(dbPath, addr); err != nil {
				return err
			}
			pid, _ := daemon.ReadPID(dbPath)
			fmt.Printf("go-rag started (pid %d) — MCP on %s\n", pid, addr)
			return nil
		},
	}
	cmd.Flags().String("mcp-addr", ":7878", "MCP listen address")
	return cmd
}
