package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/mcp"
	"github.com/spf13/cobra"
)

// newServeCmd is the hidden long-running daemon invoked (detached) by `start`.
// It owns the Pebble database for its lifetime and serves MCP over HTTP.
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Run the MCP daemon (internal; used by 'start')",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, _ := cmd.Flags().GetString("mcp-addr")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			token := daemon.ReadToken(dbPath)
			srv := mcp.NewWithDB(dbPath, db, cfg)
			httpSrv := &http.Server{Addr: addr, Handler: srv.HTTPHandler(token)}

			// Graceful shutdown on SIGTERM/SIGINT (from `go-rag stop`).
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
				<-sig
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpSrv.Shutdown(ctx)
			}()

			fmt.Fprintf(os.Stderr, "go-rag daemon serving MCP on %s\n", addr)
			err = httpSrv.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	cmd.Flags().String("mcp-addr", ":7878", "MCP listen address")
	return cmd
}
