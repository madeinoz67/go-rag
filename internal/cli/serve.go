package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/engine"
	goraggrpc "github.com/madeinoz67/go-rag/internal/grpc"
	"github.com/madeinoz67/go-rag/internal/mcp"
	"github.com/madeinoz67/go-rag/internal/rest"
	"github.com/spf13/cobra"
)

// newServeCmd is the hidden long-running daemon invoked (detached) by `start`.
// It owns the single Pebble database for its lifetime and serves all three
// transports — MCP, REST, gRPC — in one process, each on its own loopback port.
// All three are adapters over a single *engine.Engine, so they return identical
// results (cross-transport parity, spec 003 FR-002/003).
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Run the go-rag daemon (internal; used by 'start')",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mcpAddr, _ := cmd.Flags().GetString("mcp-addr")
			restAddr, _ := cmd.Flags().GetString("rest-addr")
			grpcAddr, _ := cmd.Flags().GetString("grpc-addr")
			bindExternal, _ := cmd.Flags().GetBool("bind-external")

			// Loopback-by-default contract (spec 007, FR-001/003): refuse to bind
			// any enabled transport to a non-loopback address unless the user opted
			// in with --bind-external. This RunE is the single chokepoint — every
			// bind path (direct serve, start→serve, future config-sourced) flows
			// through it — so gating here is necessary and sufficient. Runs before
			// openDB/listeners, so a rejection opens no DB and no listener.
			if err := daemon.ValidateBind(daemon.Addrs{
				MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr,
			}, bindExternal); err != nil {
				return err
			}

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			token := daemon.ReadToken(dbPath)
			eng := engine.NewWithDB(cfg, db)
			// Drain the engine's background ingest workers (async-after-ACK writes)
			// before the database closes on shutdown. Runs before the deferred
			// db.Close() above (LIFO defer order).
			defer eng.Close()

			// MCP (HTTP/JSON-RPC) — always on; its /mcp/health is the daemon's
			// startup/health probe target.
			mcpSrv := &http.Server{
				Addr:    mcpAddr,
				Handler: mcp.NewWithDB(dbPath, db, cfg).HTTPHandler(token),
			}

			// REST (stdlib net/http) — optional; empty addr disables it.
			var restSrv *http.Server
			if restAddr != "" {
				restSrv = &http.Server{Addr: restAddr, Handler: rest.New(eng, token).Handler()}
			}

			// gRPC (grpc-go) — optional. Build the server always (cheap) but only
			// bind a listener when an address is configured.
			grpcSrv := goraggrpc.NewServer(eng, token)
			var grpcLis net.Listener
			if grpcAddr != "" {
				grpcLis, err = net.Listen("tcp", grpcAddr)
				if err != nil {
					return fmt.Errorf("gRPC listen %s: %w", grpcAddr, err)
				}
			}

			// stopAll drains every started listener. Idempotent via sync.Once so the
			// signal goroutine and the post-error path can both call it safely.
			var stopOnce sync.Once
			stopAll := func() {
				stopOnce.Do(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = mcpSrv.Shutdown(ctx)
					if restSrv != nil {
						_ = restSrv.Shutdown(ctx)
					}
					if grpcLis != nil {
						grpcSrv.GracefulStop()
					}
				})
			}

			// Graceful shutdown on SIGTERM/SIGINT (from `go-rag stop`).
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
				<-sig
				stopAll()
			}()

			bound := "MCP " + mcpAddr
			if restAddr != "" {
				bound += ", REST " + restAddr
			}
			if grpcAddr != "" {
				bound += ", gRPC " + grpcAddr
			}
			fmt.Fprintf(os.Stderr, "go-rag daemon serving %s\n", bound)
			// Exposure warning (spec 007 FR-005): when external binding is
			// authorized and at least one transport is actually non-loopback, say
			// so loudly — once, at boot. All-loopback + --bind-external stays silent.
			if w := daemon.ExternalBindWarning(daemon.Addrs{
				MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr,
			}); w != "" {
				fmt.Fprintln(os.Stderr, w)
			}

			// Start each listener in its own goroutine; collect their exit errors.
			errCh := make(chan error, 3)
			n := 1 // MCP always
			go func() { errCh <- mcpSrv.ListenAndServe() }()
			if restSrv != nil {
				n++
				go func() { errCh <- restSrv.ListenAndServe() }()
			}
			if grpcLis != nil {
				n++
				go func() { errCh <- grpcSrv.Serve(grpcLis) }()
			}

			// First listener to stop (signal-driven shutdown, or a fatal bind/serve
			// error) triggers a drain of the rest, then we collect their exits.
			first := <-errCh
			stopAll()
			for i := 1; i < n; i++ {
				<-errCh
			}

			// http.ErrServerClosed is the expected clean-shutdown return; a real
			// error (e.g. port in use) propagates.
			if first != nil && !errors.Is(first, http.ErrServerClosed) {
				return first
			}
			return nil
		},
	}
	cmd.Flags().String("mcp-addr", "127.0.0.1:7878", "MCP listen address (loopback by default)")
	cmd.Flags().String("rest-addr", "127.0.0.1:7879", "REST listen address (loopback); empty disables REST")
	cmd.Flags().String("grpc-addr", "127.0.0.1:7880", "gRPC listen address (loopback); empty disables gRPC")
	cmd.Flags().Bool("bind-external", false, "allow non-loopback bind addresses (exposes the vault on the network; no TLS)")
	return cmd
}
