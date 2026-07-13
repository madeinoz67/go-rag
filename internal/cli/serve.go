package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/engine"
	goraggrpc "github.com/madeinoz67/go-rag/internal/grpc"
	"github.com/madeinoz67/go-rag/internal/mcp"
	"github.com/madeinoz67/go-rag/internal/observe"
	"github.com/madeinoz67/go-rag/internal/rest"
	"github.com/madeinoz67/go-rag/internal/ui"
	"github.com/spf13/cobra"
)

// shutdownHardDeadline bounds daemon graceful shutdown after SIGTERM. Generous
// beyond the embedder drain (embedproc.drainTimeout=5s) + Pebble/OTel close so
// the common case exits cleanly via the deferred closes; the watchdog only
// forces exit when something hangs. Guarantees `go-rag stop` returns (and the
// Pebble lock releases) within ~this many seconds.
const shutdownHardDeadline = 12 * time.Second

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
			uiAddr, _ := cmd.Flags().GetString("ui-addr")
			bindExternal, _ := cmd.Flags().GetBool("bind-external")

			// Loopback-by-default contract (spec 007, FR-001/003): refuse to bind
			// any enabled transport to a non-loopback address unless the user opted
			// in with --bind-external. This RunE is the single chokepoint — every
			// bind path (direct serve, start→serve, future config-sourced) flows
			// through it — so gating here is necessary and sufficient. Runs before
			// openDB/listeners, so a rejection opens no DB and no listener.
			if err := daemon.ValidateBind(daemon.Addrs{
				MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr, UIAddr: uiAddr,
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
			// Spec 045: bootstrap admin + import any legacy mcp.token against the
			// already-open store (serve owns the single Pebble lock — it cannot
			// re-open). Idempotent; runs before any transport accepts a request.
			bootstrapAuth(auth.NewStore(db), dbPath)
			// spec 030: start the embedder eagerly so the startup scan recovers any
			// pending 0x14 embeddings from a previous crash.
			if err := eng.EnsureEmbedder(vaultName); err != nil {
				return err
			}
			// Drain the engine's background ingest workers (async-after-ACK writes)
			// before the database closes on shutdown. Runs before the deferred
			// db.Close() above (LIFO defer order).
			defer eng.Close()

			// H17/spec 020: init observability (OTel providers/exporters — local by
			// default; OTLP opt-in only). Drain on shutdown so in-flight spans flush.
			if err := observe.Init(cfg); err != nil {
				return err
			}
			defer func() { _ = observe.Shutdown(context.Background()) }()

			// H18/spec 021: structured audit log (local, append-only JSONL). Init +
			// set the global appender; Close drains on shutdown.
			if cfg.EffectiveAuditLogEnabled() {
				ap := cfg.AuditPath
				if ap == "" {
					ap = audit.DefaultPath(cfg.DBPath)
				}
				aud, err := audit.Init(ap, cfg.EffectiveAuditLogMaxBytes())
				if err != nil {
					return err
				}
				audit.SetGlobal(aud)
				defer aud.Close(context.Background())
			}

			// H11/spec 017: compute the embedding-drift verdict at boot and log it
			// (loud-at-startup signal, FR-004/FR-005). Hard drift (model/dim/
			// convention mismatch) makes readiness NOT READY (Health.Ready) while
			// liveness stays OK — the daemon starts degraded so the operator can run
			// migrate in place; soft (ollama-version) change warns but stays ready.
			if v := eng.RefreshDriftVerdict(context.Background(), vaultName); v.Hard || v.Verdict == engine.VerdictVersionWarning {
				detail := v.Verdict
				if len(v.Reasons) > 0 {
					detail += " (" + strings.Join(v.Reasons, "; ") + ")"
				}
				fmt.Fprintf(os.Stderr, "go-rag drift: %s — run `migrate` to re-embed under the current model\n", detail)
			}

			// spec 033: auto-watch cfg.WatchDirs (single-writer-safe — the watcher runs in
			// THIS daemon process, never a second Pebble writer). Set directories via the
			// GO_RAG_WATCH_DIRS env override (e.g. /ingest). The watcher does an initial
			// scan (ingests existing files) then watches for changes (fsnotify + poll).
			watchCtx, watchCancel := context.WithCancel(context.Background())
			defer watchCancel() // stop the watcher on any return (before the eng/db drain)
			if dirs := cfg.WatchDirs; len(dirs) > 0 {
				poll := time.Duration(cfg.PollIntervalSec) * time.Second
				if poll <= 0 {
					poll = 60 * time.Second
				}
				glob := cfg.FileGlob
				if glob == "" {
					glob = "*"
				}
				for _, dir := range dirs {
					if dir == "" {
						continue
					}
					dir := dir
					go func() {
						fmt.Fprintf(os.Stderr, "go-rag daemon watching %s (poll %s)\n", dir, poll)
						if err := eng.Watch(watchCtx, vaultName, dir, glob, poll); err != nil {
							fmt.Fprintf(os.Stderr, "go-rag watch %s ended: %v\n", dir, err)
						}
					}()
				}
			}

			// MCP (HTTP/JSON-RPC) — always on; its /mcp/health is the daemon's
			// startup/health probe target. Backed by the SAME shared engine as
			// REST/gRPC (audit H06/spec 016) so MCP queries hit the cache,
			// go_rag_status reports real cache stats, and the seeded index (H01)
			// is shared across all three transports.
			mcpSrv := &http.Server{
				Addr:    mcpAddr,
				Handler: mcp.NewWithEngine(dbPath, eng, cfg).HTTPHandler(token),
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

			// UI (spec 046) — embedded management console; optional. Empty addr
			// disables it. A fourth loopback HTTP transport over the same engine,
			// auth-gated by the spec 045 Bearer-session system (Bearer only).
			var uiSrv *http.Server
			if uiAddr != "" {
				uiSrv = &http.Server{Addr: uiAddr, Handler: ui.New(eng, token).Handler()}
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
					if uiSrv != nil {
						_ = uiSrv.Shutdown(ctx)
					}
					if grpcLis != nil {
						grpcSrv.GracefulStop()
					}
				})
			}

			// Graceful shutdown on SIGTERM/SIGINT (from `go-rag stop`). Bounded by
			// a hard-deadline watchdog: the deferred engine/db Close can block on
			// a stuck embedder drain (or any future shutdown hang), which would
			// wedge `stop` at its 35s timeout and leave the Pebble lock held.
			// Force-exit after shutdownHardDeadline so the OS releases the lock
			// promptly; the durable 0x14 queue recovers pending work on next start.
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
				<-sig
				go func() {
					time.Sleep(shutdownHardDeadline)
					fmt.Fprintln(os.Stderr, "go-rag daemon: graceful shutdown exceeded hard deadline; forcing exit")
					os.Exit(0)
				}()
				stopAll()
			}()

			bound := "MCP " + mcpAddr
			if restAddr != "" {
				bound += ", REST " + restAddr
			}
			if grpcAddr != "" {
				bound += ", gRPC " + grpcAddr
			}
			if uiAddr != "" {
				bound += ", UI " + uiAddr
			}
			fmt.Fprintf(os.Stderr, "go-rag daemon serving %s\n", bound)
			// Exposure warning (spec 007 FR-005): when external binding is
			// authorized and at least one transport is actually non-loopback, say
			// so loudly — once, at boot. All-loopback + --bind-external stays silent.
			if w := daemon.ExternalBindWarning(daemon.Addrs{
				MCPAddr: mcpAddr, RESTAddr: restAddr, GRPCAddr: grpcAddr, UIAddr: uiAddr,
			}); w != "" {
				fmt.Fprintln(os.Stderr, w)
			}

			// Start each listener in its own goroutine; collect their exit errors.
			errCh := make(chan error, 4)
			n := 1 // MCP always
			go func() { errCh <- mcpSrv.ListenAndServe() }()
			if restSrv != nil {
				n++
				go func() { errCh <- restSrv.ListenAndServe() }()
			}
			if uiSrv != nil {
				n++
				go func() { errCh <- uiSrv.ListenAndServe() }()
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
	cmd.Flags().String("ui-addr", "127.0.0.1:7881", "UI listen address (loopback); empty disables the management console")
	cmd.Flags().Bool("bind-external", false, "allow non-loopback bind addresses (exposes the vault on the network; no TLS)")
	return cmd
}
