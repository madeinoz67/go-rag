// Package grpc is the gRPC transport adapter for go-rag (MuninnDB-parity stack:
// grpc-go). It implements goragpb.GoragServer as a thin projection of the shared
// internal/engine facade — adapters add no independent logic, so gRPC returns
// identical results to REST and MCP.
package grpc

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	grpcc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Adapter implements goragpb.GoragServer over the shared engine facade. RPCs not
// yet implemented (US2/US3 operations) fall through to UnimplementedGoragServer.
type Adapter struct {
	goragpb.UnimplementedGoragServer
	eng *engine.Engine
}

// New returns a GoragServer adapter backed by eng.
func New(eng *engine.Engine) *Adapter { return &Adapter{eng: eng} }

// grpcKeepaliveParams probes idle connections so a stalled-but-connected
// WatchDocuments client (connected but not reading — its HTTP/2 flow-control
// window stays closed) is detected + dropped within Time+Timeout. Its stream
// ctx then cancels and the handler's defer unsub() runs (spec 040 audit: bounds
// the wedge the Send-decouple can't self-heal — without this the server never
// probes idle connections, so a non-reading client would hold the subscriber
// indefinitely). Tunable constants.
var grpcKeepaliveParams = keepalive.ServerParameters{
	Time:    30 * time.Second, // PING an idle connection after 30s
	Timeout: 10 * time.Second, // close it if no PONG within 10s (stalled client)
}

// grpcKeepalivePolicy guards against abusive client PING frequency while
// permitting keepalive without an open stream (the bridge's long-lived watch).
var grpcKeepalivePolicy = keepalive.EnforcementPolicy{
	MinTime:             10 * time.Second,
	PermitWithoutStream: true,
}

// NewServer builds a *grpc.Server with bearer auth (when token != ""), gRPC
// keepalive enforcement (spec 040 audit follow-up), and the Gorag service
// registered. The caller owns Serve/GracefulStop.
func NewServer(eng *engine.Engine, token string) *grpcc.Server {
	_ = token
	var store *auth.Store
	if eng != nil {
		if db := eng.DB(); db != nil {
			store = auth.NewStore(db)
		}
	}
	srv := grpcc.NewServer(
		grpcc.KeepaliveParams(grpcKeepaliveParams),
		grpcc.KeepaliveEnforcementPolicy(grpcKeepalivePolicy),
		grpcc.UnaryInterceptor(authInterceptor(store)),
		grpcc.StreamInterceptor(authStreamInterceptor(store)),
	)
	goragpb.RegisterGoragServer(srv, New(eng))
	return srv
}

// authStreamInterceptor is the streaming-RPC counterpart to authInterceptor.
// It covers server-streaming RPCs (e.g. WatchDocuments, spec 040) which the
// unary interceptor does NOT see — without it, a streaming RPC bypassed auth
// entirely (spec 045 red-team finding A).
func authStreamInterceptor(store *auth.Store) grpcc.StreamServerInterceptor {
	return func(srv any, ss grpcc.ServerStream, _ *grpcc.StreamServerInfo, handler grpcc.StreamHandler) error {
		if store == nil {
			return handler(srv, ss)
		}
		token := bearerFromGRPCMetadata(ss.Context())
		if _, err := store.ValidateTokenOrBypass(token, peerIsLoopback(ss.Context())); err != nil {
			audit.Log(audit.AuthFailEvent("grpc", "missing or invalid bearer token (stream)"))
			return status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return handler(srv, ss)
	}
}

// authInterceptor validates the bearer credential on every unary RPC through the
// single auth.Validate (spec 045 US2). When store is nil (no engine DB — tests),
// auth is disabled, matching the prior empty-token behaviour. The loopback bypass
// (US5) is honoured so local dev with no credentials minted just works.
func authInterceptor(store *auth.Store) grpcc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpcc.UnaryServerInfo, handler grpcc.UnaryHandler) (any, error) {
		if store == nil {
			return handler(ctx, req)
		}
		token := bearerFromGRPCMetadata(ctx)
		if _, err := store.ValidateTokenOrBypass(token, peerIsLoopback(ctx)); err != nil {
			audit.Log(audit.AuthFailEvent("grpc", "missing or invalid bearer token")) // H18 audit
			return nil, status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return handler(ctx, req)
	}
}

// bearerFromGRPCMetadata extracts the opaque token from gRPC metadata. Accepts
// "authorization: Bearer <token>" (matching REST/MCP) and falls back to a bare
// "authorization" or "x-api-key" value (legacy clients); the bare form is then
// tried via the legacy raw-hash lookup path in ValidateToken.
func bearerFromGRPCMetadata(ctx context.Context) string {
	const scheme = "Bearer "
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vals := md.Get("authorization"); len(vals) > 0 {
		v := vals[0]
		// Case-insensitive scheme match — aligns with REST/MCP (spec 045 red-team
		// finding LOW: a lowercase "bearer " was being treated as a bare legacy
		// value and misrouted to ValidateAPIKeyRaw).
		if len(v) >= len(scheme) && strings.EqualFold(v[:len(scheme)], scheme) {
			return strings.TrimSpace(v[len(scheme):])
		}
		return v // bare token (legacy)
	}
	if vals := md.Get("x-api-key"); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// peerIsLoopback reports whether the gRPC client connected from the local machine.
// A host:port peer is loopback when the IP is. A peer that is NOT host:port
// (a Unix-domain socket or an in-process pipe such as grpc's bufconn) is local
// by construction, so it counts as loopback too. The empty-store guard in
// ValidateTokenOrBypass is the real safety net: even a misclassified peer cannot
// bypass once any credential exists.
func peerIsLoopback(ctx context.Context) bool {
	pr, ok := peer.FromContext(ctx)
	if !ok || pr.Addr == nil {
		return false // unknown → fail-closed
	}
	host, _, err := net.SplitHostPort(pr.Addr.String())
	if err != nil {
		return true // not host:port → UDS / in-process pipe → local
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
