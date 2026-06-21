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
			bindExternal, _ := cmd.Flags().GetBool("bind-external")
			addrs := daemon.Addrs{MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr, BindExternal: bindExternal}
			// Pre-validate at the start layer too, so a bad bind fails immediately
			// with the actionable error on the console (SC-002) rather than after
			// the detached 5s health probe buries it in the daemon log. serve
			// re-validates as the authoritative chokepoint (can't be bypassed).
			if err := daemon.ValidateBind(addrs, bindExternal); err != nil {
				return err
			}
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
			suffix := ""
			if bindExternal {
				suffix = " [EXTERNAL binding — see daemon log]"
			}
			fmt.Printf("go-rag started (pid %d) — %s%s\n", pid, bound, suffix)
			return nil
		},
	}
	cmd.Flags().String("mcp-addr", "127.0.0.1:7878", "MCP listen address (loopback by default)")
	cmd.Flags().String("rest-addr", "127.0.0.1:7879", "REST listen address (loopback); empty disables REST")
	cmd.Flags().String("grpc-addr", "127.0.0.1:7880", "gRPC listen address (loopback); empty disables gRPC")
	cmd.Flags().Bool("bind-external", false, "allow non-loopback bind addresses (exposes the vault on the network; no TLS)")
	return cmd
}
