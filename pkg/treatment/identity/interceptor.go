package identity

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Policy maps full gRPC method names ("/pkg.Service/Method") to the Role a
// caller must present. The controller derives it from the proto's per-RPC
// role option; this package only enforces it. A method absent from the
// policy is refused for everyone, so an RPC added without a declaration
// fails closed rather than open.
type Policy map[string]Role

type ctxKey struct{}

// WithIdentity stores a resolved identity so handlers do not parse the
// certificate again.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the identity an interceptor resolved, if any.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// resolve prefers the interceptor's identity and falls back to the peer, so
// handlers behave the same whether or not the interceptor ran (direct calls
// in tests, or a server wired without it).
func resolve(ctx context.Context) (Identity, error) {
	if id, ok := FromContext(ctx); ok {
		return id, nil
	}
	return FromPeer(ctx)
}

// Authorize resolves the peer once and checks it against the policy for
// method. It returns a context carrying the identity for the handler.
func (p Policy) Authorize(ctx context.Context, method string) (context.Context, error) {
	required, ok := p[method]
	if !ok || required == "" {
		return nil, status.Errorf(codes.PermissionDenied, "%s has no role declaration", method)
	}
	id, e := FromPeer(ctx)
	if e != nil {
		return nil, e
	}
	if id.Role != required {
		return nil, status.Errorf(codes.PermissionDenied, "%s certificate required", required)
	}
	return WithIdentity(ctx, id), nil
}

// agentRequest is the part of an agent RPC's request the interceptor can
// check generically: an agent speaks only for itself and only from its
// enrolled location. Requests without these fields are left to the handler.
func agentRequest(id Identity, req any) error {
	if r, ok := req.(interface{ GetAgentId() string }); ok && r.GetAgentId() != id.ID {
		return status.Error(codes.PermissionDenied, "agent identity mismatch")
	}
	if r, ok := req.(interface{ GetLocation() string }); ok && r.GetLocation() != "" && r.GetLocation() != id.Location {
		return status.Error(codes.PermissionDenied, "agent location differs from certificate")
	}
	return nil
}

// UnaryInterceptor enforces the policy for unary RPCs and hands the handler
// a context carrying the identity.
func (p Policy) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, e := p.Authorize(ctx, info.FullMethod)
		if e != nil {
			return nil, e
		}
		if id, _ := FromContext(ctx); id.Role == RoleAgent {
			if e = agentRequest(id, req); e != nil {
				return nil, e
			}
		}
		return handler(ctx, req)
	}
}

type identifiedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s identifiedStream) Context() context.Context { return s.ctx }

// StreamInterceptor enforces the policy for streaming RPCs. The first
// message is not visible here, so per-request agent checks stay in the
// handler; the identity itself is resolved once and carried on the stream.
func (p Policy) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, e := p.Authorize(ss.Context(), info.FullMethod)
		if e != nil {
			return e
		}
		return handler(srv, identifiedStream{ServerStream: ss, ctx: ctx})
	}
}
