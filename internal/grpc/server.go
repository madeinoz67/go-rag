// Package grpc is the gRPC transport adapter for go-rag (MuninnDB-parity stack:
// grpc-go). It implements goragpb.GoragServer as a thin projection of the shared
// internal/engine facade — adapters add no independent logic, so gRPC returns
// identical results to REST and MCP.
package grpc

import (
	"context"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/engine"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
	grpcc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

// NewServer builds a *grpc.Server with bearer auth (when token != "") and the
// Gorag service registered. The caller owns Serve/GracefulStop.
func NewServer(eng *engine.Engine, token string) *grpcc.Server {
	srv := grpcc.NewServer(grpcc.UnaryInterceptor(bearerInterceptor(token)))
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
