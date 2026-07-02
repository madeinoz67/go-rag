// Package grpc is the gRPC transport adapter for go-rag (MuninnDB-parity stack:
// grpc-go). It implements goragpb.GoragServer as a thin projection of the shared
// internal/engine facade — adapters add no independent logic, so gRPC returns
// identical results to REST and MCP.
package grpc

import (
	"context"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	grpcc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
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
	srv := grpcc.NewServer(
		grpcc.KeepaliveParams(grpcKeepaliveParams),
		grpcc.KeepaliveEnforcementPolicy(grpcKeepalivePolicy),
		grpcc.UnaryInterceptor(bearerInterceptor(token)),
	)
	goragpb.RegisterGoragServer(srv, New(eng))
	return srv
}

// bearerInterceptor rejects requests lacking the expected bearer token. When
// token is empty, auth is disabled (local development / trusted loopback).
func bearerInterceptor(token string) grpcc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpcc.UnaryServerInfo, handler grpcc.UnaryHandler) (any, error) {
		if token != "" && !hasBearer(ctx, token) {
			audit.Log(audit.AuthFailEvent("grpc", "missing or invalid bearer token")) // H18 audit
			return nil, status.Error(codes.Unauthenticated, "missing or invalid bearer token")
		}
		return handler(ctx, req)
	}
}

// hasBearer accepts "authorization: Bearer <token>" (matching REST/MCP) or a bare
// token via "authorization" or "x-api-key" metadata.
func hasBearer(ctx context.Context, expected string) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	for _, v := range md.Get("authorization") {
		if v == "Bearer "+expected || v == expected {
			return true
		}
	}
	for _, v := range md.Get("x-api-key") {
		if v == expected {
			return true
		}
	}
	return false
}
