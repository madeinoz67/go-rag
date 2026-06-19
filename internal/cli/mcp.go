package cli

import (
	"os"

	"github.com/madeinoz67/go-rag/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "Run the MCP server over stdio (for AI agents)",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return mcp.New(dbPath).Serve(os.Stdin, os.Stdout)
		},
	}
}
