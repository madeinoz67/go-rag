package muninn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/madeinoz67/go-rag/proto/muninn/v1"
)

// grpcClient is the production Client implementation over gRPC — the repo's
// first outbound gRPC client (research.md R9). It enforces loopback-only at
// dial (defense vs DNS rebinding) and injects the target vault key as a Bearer
// header on every call (unary + stream; Activate is server-streaming). Health is
// tracked from RPC outcomes; the bridgeProc's circuit breaker (processor.go) is
// the real authority on whether to promote.
type grpcClient struct {
	conn   *grpc.ClientConn
	raw    pb.MuninnDBClient
	token  string
	target string

	healthy atomic.Bool
	caps    atomic.Pointer[Capabilities]
}

// Dial connects to MuninnDB at endpoint (loopback-only) using token (the target
// vault mk_ key, read from GORAG_BRIDGE_TOKEN by the caller). It returns a client
// regardless of whether the server is currently up — the bridge degrades — but a
// non-loopback endpoint fails immediately. A best-effort Hello probe populates
// capabilities; probe failure does not fail Dial.
func Dial(ctx context.Context, endpoint, token string) (Client, error) {
	if token == "" {
		return nil, errors.New("bridge: empty MuninnDB token (set GORAG_BRIDGE_TOKEN)")
	}
	conn, err := grpc.NewClient(endpoint,
		grpc.WithContextDialer(loopbackDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // loopback only; remote is refused at dial
		grpc.WithUnaryInterceptor(bearerUnary(token)),
		grpc.WithStreamInterceptor(bearerStream(token)),
	)
	if err != nil {
		return nil, fmt.Errorf("bridge: dial %s: %w", endpoint, err)
	}
	g := &grpcClient{conn: conn, raw: pb.NewMuninnDBClient(conn), token: token, target: endpoint}
	// Best-effort probe. The ctx here is the dial/probe context; a short timeout
	// keeps an unreachable MuninnDB from stashing bridge start.
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if caps, err := g.hello(probeCtx); err == nil {
		g.healthy.Store(true)
		g.caps.Store(caps)
	} else {
		slog.Warn("bridge: MuninnDB hello probe failed (degraded until reachable)", "endpoint", endpoint, "err", err)
	}
	return g, nil
}

// hello calls the MuninnDB Hello RPC and maps capabilities. Kept unexported so the
// public Hello (which also updates health) wraps it.
func (g *grpcClient) hello(ctx context.Context) (*Capabilities, error) {
	resp, err := g.raw.Hello(ctx, &pb.HelloRequest{
		Version: "go-rag-bridge/0", Client: "go-rag", AuthMethod: "bearer", Token: g.token,
	})
	if err != nil {
		return nil, err
	}
	caps := &Capabilities{ServerVersion: resp.ServerVersion}
	if resp.Limits != nil {
		// MuninnDB exposes MaxPayloadMb (MB); the mapper's sub-chunking wants bytes.
		if mb := resp.Limits.MaxPayloadMb; mb > 0 {
			caps.MaxEngramContentBytes = int(mb) * 1024 * 1024
		}
	}
	return caps, nil
}

func (g *grpcClient) Hello(ctx context.Context) (*Capabilities, error) {
	caps, err := g.hello(ctx)
	g.mark(err)
	if err != nil {
		return nil, err
	}
	g.caps.Store(caps)
	return caps, nil
}

func (g *grpcClient) Write(ctx context.Context, p WriteParams) (string, int64, error) {
	resp, err := g.raw.Write(ctx, toProtoWrite(p), grpc.WaitForReady(true))
	g.mark(err)
	if err != nil {
		return "", 0, err
	}
	return resp.ID, resp.CreatedAt, nil
}

func (g *grpcClient) BatchWrite(ctx context.Context, vault string, batch []WriteParams) ([]BatchItemResult, error) {
	reqs := make([]*pb.WriteRequest, len(batch))
	for i, p := range batch {
		p.Vault = vault
		reqs[i] = toProtoWrite(p)
	}
	resp, err := g.raw.BatchWrite(ctx, &pb.BatchWriteRequest{Requests: reqs}, grpc.WaitForReady(true))
	g.mark(err)
	if err != nil {
		return nil, err
	}
	out := make([]BatchItemResult, len(resp.Results))
	for i, r := range resp.Results {
		out[i] = BatchItemResult{Index: int(r.Index), ID: r.Id, Error: r.Error}
	}
	return out, nil
}

func (g *grpcClient) Read(ctx context.Context, vault, id string) (*Engram, error) {
	resp, err := g.raw.Read(ctx, &pb.ReadRequest{ID: id, Vault: vault}, grpc.WaitForReady(true))
	g.mark(err)
	if err != nil {
		return nil, err
	}
	return fromProtoRead(resp), nil
}

// Activate streams a recall browse. Each ActivateResponse frame carries a batch of
// ActivationItem; each item becomes one Activation row on the returned channel.
// The channel closes when the stream ends or errs (the error is delivered as a
// final sentinel on a separate path — callers draining to completion treat close
// as "done"). ctx cancellation aborts the stream.
func (g *grpcClient) Activate(ctx context.Context, vault string, phrases []string, limit int) (<-chan Activation, error) {
	stream, err := g.raw.Activate(ctx, &pb.ActivateRequest{
		Context: phrases, Vault: vault, MaxResults: int32(limit),
	}, grpc.WaitForReady(true))
	if err != nil {
		g.mark(err)
		return nil, err
	}
	out := make(chan Activation, 16)
	go func() {
		defer close(out)
		sent := 0
		for {
			frame, err := stream.Recv()
			if err != nil {
				g.mark(err)
				return
			}
			for _, a := range frame.Activations {
				select {
				case <-ctx.Done():
					return
				case out <- Activation{
					EngramID: a.ID, Concept: a.Concept,
					Score: a.Score, Tags: nil,
				}:
				}
				sent++
				if limit > 0 && sent >= limit {
					return
				}
			}
		}
	}()
	g.healthy.Store(true)
	return out, nil
}

func (g *grpcClient) Healthy() bool { return g.healthy.Load() }

func (g *grpcClient) Close() error { return g.conn.Close() }

// mark updates health from an RPC outcome. Only connection-level failures
// (Unavailable) flip the client unhealthy; application errors (InvalidArgument,
// NotFound, Canceled-from-caller-ctx) leave health alone. This is a coarse signal
// — the bridgeProc circuit breaker is the real authority on whether to promote.
func (g *grpcClient) mark(err error) {
	if err == nil {
		g.healthy.Store(true)
		return
	}
	if status.Code(err) == codes.Unavailable {
		g.healthy.Store(false)
	}
}

// loopbackDialer is the gRPC dialer — refuses any non-loopback address. This is
// the connection-time gate (config.Validate is the first); together they defeat
// DNS rebinding (a loopback hostname that resolves to a public IP is rejected).
func loopbackDialer(ctx context.Context, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("bridge: bad endpoint %q: %w", addr, err)
	}
	if host == "" {
		return nil, fmt.Errorf("bridge: refusing bare port (loopback only): %q", addr)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return nil, fmt.Errorf("bridge: refusing non-loopback endpoint %q", addr)
		}
	} else {
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("bridge: resolve %q: %w", host, err)
		}
		for _, s := range ips {
			if ip := net.ParseIP(s); ip == nil || !ip.IsLoopback() {
				return nil, fmt.Errorf("bridge: refusing non-loopback endpoint %q (resolves to %s)", addr, s)
			}
		}
	}
	d := net.Dialer{}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}

// bearerUnary injects the target vault key as an Authorization: Bearer header on
// every unary RPC (mirrors the server-side interceptor shape in internal/grpc).
func bearerUnary(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), method, req, reply, cc, opts...)
	}
}

// bearerStream is the streaming counterpart (Activate / Subscribe are streams).
func bearerStream(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), desc, cc, method, opts...)
	}
}

// toProtoWrite maps the bridge's WriteParams to the generated WriteRequest. The
// maintainer invariants (research.md R4) are set here: Embedding is never copied
// (nil unless the caller explicitly set one — the mapper leaves it nil); UpsertMode
// is passed through verbatim.
func toProtoWrite(p WriteParams) *pb.WriteRequest {
	assocs := make([]pb.Association, len(p.Associations))
	for i, a := range p.Associations {
		assocs[i] = pb.Association{
			TargetID: a.TargetID, RelType: a.RelType, Weight: a.Weight, Confidence: a.Confidence,
		}
	}
	return &pb.WriteRequest{
		Concept:      p.Concept,
		Content:      p.Content,
		Tags:         p.Tags,
		Confidence:   p.Confidence,
		Stability:    p.Stability,
		Vault:        p.Vault,
		IdempotentID: p.IdempotentID,
		Associations: assocs,
		Embedding:    p.Embedding, // nil by the maintainer invariant
		MemoryType:   p.MemoryType,
		TypeLabel:    p.TypeLabel,
		UpsertMode:   p.UpsertMode,
	}
}

// fromProtoRead maps a MuninnDB ReadResponse to the bridge's Engram.
func fromProtoRead(r *pb.ReadResponse) *Engram {
	if r == nil {
		return nil
	}
	return &Engram{
		ID:          r.ID,
		Concept:     r.Concept,
		Content:     r.Content,
		Tags:        r.Tags,
		AccessCount: int64(r.AccessCount),
		Stability:   r.Stability,
		State:       stateName(r.State),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		LastAccess:  r.LastAccess,
	}
}

// stateName maps the MuninnDB state enum (uint32) to a readable label for the
// console. Unknown values pass through as their decimal so the view never lies.
func stateName(s uint32) string {
	switch s {
	case 0:
		return "active"
	case 1:
		return "soft-deleted"
	case 2:
		return "superseded"
	default:
		return fmt.Sprintf("state(%d)", s)
	}
}
