package cli

import (
	"fmt"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the go-rag daemon (MCP + REST + gRPC) in the background",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mcpAddr, _ := cmd.Flags().GetString("mcp-addr")
			restAddr, _ := cmd.Flags().GetString("rest-addr")
			grpcAddr, _ := cmd.Flags().GetString("grpc-addr")
			addrs := daemon.Addrs{MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr}
			if err := daemon.Start(dbPath, addrs); err != nil {
				return err
			}
			pid, _ := daemon.ReadPID(dbPath)
			bound := mcpAddr
			if restAddr != "" {
				bound += ", REST " + restAddr
			}
			if grpcAddr != "" {
				bound += ", gRPC " + grpcAddr
			}
			fmt.Printf("go-rag started (pid %d) — %s\n", pid, bound)
			return nil
		},
	}
	cmd.Flags().String("mcp-addr", "127.0.0.1:7878", "MCP listen address (loopback)")
	cmd.Flags().String("rest-addr", "127.0.0.1:7879", "REST listen address; empty disables REST")
	cmd.Flags().String("grpc-addr", "127.0.0.1:7880", "gRPC listen address; empty disables gRPC")
	return cmd
}
