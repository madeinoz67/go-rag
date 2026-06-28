package cli

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/spf13/cobra"
)

// runHealth probes the daemon's always-on MCP health endpoint (GET /mcp/health)
// and returns nil on HTTP 200, an error otherwise. Extracted from newHealthCmd's
// RunE so the probe logic is unit-testable against an httptest.Server (the RunE
// itself os.Exit(1)s on failure, which would kill the test process).
func runHealth(addr string) error {
	url := daemon.HealthURL(addr)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("%s unreachable: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	return nil
}

// newHealthCmd probes the running daemon's always-on MCP health endpoint
// (GET /mcp/health -> 200 "ok"; unauthenticated — see internal/mcp/http.go).
//
// Its primary purpose is the shell-less Docker HEALTHCHECK on the distroless
// runtime image (spec 033): distroless/static has no /bin/sh and no curl, so the
// image's exec-array `HEALTHCHECK CMD ["/go-rag","health"]` invokes this
// subcommand directly. It is also usable ad-hoc: `go-rag health` or
// `go-rag health --addr 127.0.0.1:7878`.
//
// Exit 0 on HTTP 200; exit 1 (with a stderr reason) on connect-refused, timeout,
// or any non-200 response. The probe targets loopback INSIDE the container's
// network namespace, so it succeeds regardless of --bind-external or the host
// port mapping. No auth token is required.
func newHealthCmd() *cobra.Command {
	// GO_RAG_MCP_ADDR (layered over the JSON config per spec 033) overrides the
	// loopback default; falls through to 127.0.0.1:7878 when neither it nor the
	// file config sets an address. Read at flag-registration time, which is
	// correct for a healthcheck (the env is set once per image/container).
	defaultAddr := os.Getenv("GO_RAG_MCP_ADDR")
	if defaultAddr == "" {
		defaultAddr = "127.0.0.1:7878"
	}
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Probe the running daemon's health endpoint (exit 0 if healthy)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, _ := cmd.Flags().GetString("addr")
			if err := runHealth(addr); err != nil {
				// Single clean stderr line for humans; the exit CODE is what Docker's
				// HEALTHCHECK reads. os.Exit (rather than returning the error) avoids
				// both cobra's "Error:" line and main's "go-rag:" line printing.
				fmt.Fprintln(os.Stderr, "go-rag health:", err)
				os.Exit(1)
			}
			fmt.Println("ok")
			return nil
		},
	}
	cmd.Flags().String("addr", defaultAddr, "daemon MCP address to probe (host:port)")
	return cmd
}
