package identity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// peerContext fakes a verified mTLS peer whose leaf carries the given SAN
// path; an empty path is a plaintext peer.
func peerContext(path string) context.Context {
	if path == "" {
		return context.Background()
	}
	u, _ := url.Parse("spiffe://xtcp/" + path)
	cert := &x509.Certificate{URIs: []*url.URL{u}}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
}

// agentRequestFixture mimics a request with the generic agent fields.
type agentRequestFixture struct{ agentID, location string }

func (r agentRequestFixture) GetAgentId() string  { return r.agentID }
func (r agentRequestFixture) GetLocation() string { return r.location }

const (
	checkIn = "/xtcp_treatment.v1.AgentControlService/CheckIn"
	propose = "/xtcp_treatment.v1.OperatorService/Propose"
	watch   = "/xtcp_treatment.v1.AgentControlService/WatchDesiredState"
)

var testPolicy = Policy{checkIn: RoleAgent, watch: RoleAgent, propose: RoleOperator}

func TestPolicyUnaryInterceptor(t *testing.T) {
	for _, tc := range []struct {
		name, description, peer, method string
		request                         any
		expected                        codes.Code
		wantIdentity                    Identity
	}{
		{"agent-calls-checkin", "positive: an agent certificate may call an agent RPC for itself", "agent/dc1/host-1", checkIn, agentRequestFixture{"host-1", "dc1"}, codes.OK, Identity{Role: RoleAgent, Location: "dc1", ID: "host-1"}},
		{"operator-calls-propose", "positive: an operator certificate may call an operator RPC", "operator/alice", propose, struct{}{}, codes.OK, Identity{Role: RoleOperator, ID: "alice"}},
		{"agent-calls-propose", "negative: an agent certificate cannot call an operator RPC", "agent/dc1/host-1", propose, struct{}{}, codes.PermissionDenied, Identity{}},
		{"operator-calls-checkin", "negative: an operator certificate cannot call an agent RPC", "operator/alice", checkIn, agentRequestFixture{"host-1", "dc1"}, codes.PermissionDenied, Identity{}},
		{"no-peer-cert", "negative: a plaintext peer is unauthenticated before any role check", "", propose, struct{}{}, codes.Unauthenticated, Identity{}},
		{"bad-spiffe-path", "negative: a certificate whose SAN is not an xtcp identity is refused", "agent/only-one-segment", checkIn, agentRequestFixture{"only-one-segment", ""}, codes.PermissionDenied, Identity{}},
		{"unclassified-method", "boundary: a method missing from the policy is refused for everyone (fail closed)", "operator/alice", "/xtcp_treatment.v1.OperatorService/Undeclared", struct{}{}, codes.PermissionDenied, Identity{}},
		{"agent-id-mismatch", "corner: an agent naming another host is refused before the handler", "agent/dc1/host-1", checkIn, agentRequestFixture{"host-2", "dc1"}, codes.PermissionDenied, Identity{}},
		{"location-mismatch", "corner: an agent reporting from another location is refused before the handler", "agent/dc1/host-1", checkIn, agentRequestFixture{"host-1", "dc2"}, codes.PermissionDenied, Identity{}},
		{"location-unset", "boundary: a request without a location leaves the location check to enrollment", "agent/dc1/host-1", checkIn, agentRequestFixture{"host-1", ""}, codes.OK, Identity{Role: RoleAgent, Location: "dc1", ID: "host-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen Identity
			called := false
			handler := func(ctx context.Context, _ any) (any, error) {
				called = true
				seen, _ = FromContext(ctx)
				return nil, nil
			}
			_, err := testPolicy.UnaryInterceptor()(peerContext(tc.peer), tc.request, &grpc.UnaryServerInfo{FullMethod: tc.method}, handler)
			if status.Code(err) != tc.expected {
				t.Fatalf("%s: got %v, expected %v", tc.description, err, tc.expected)
			}
			if called != (tc.expected == codes.OK) {
				t.Fatalf("%s: handler called=%t", tc.description, called)
			}
			if seen != tc.wantIdentity {
				t.Fatalf("%s: handler saw %+v, want %+v", tc.description, seen, tc.wantIdentity)
			}
		})
	}
}

type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s fakeStream) Context() context.Context { return s.ctx }

func TestPolicyStreamInterceptor(t *testing.T) {
	for _, tc := range []struct {
		name, description, peer, method string
		expected                        codes.Code
	}{
		{"agent-watch", "positive: an agent may open an agent stream and the handler sees its identity once", "agent/dc1/host-1", watch, codes.OK},
		{"operator-watch", "negative: an operator cannot open an agent stream", "operator/alice", watch, codes.PermissionDenied},
		{"plaintext", "negative: a plaintext stream is unauthenticated", "", watch, codes.Unauthenticated},
		{"unclassified", "boundary: an undeclared stream method is refused", "agent/dc1/host-1", "/xtcp_treatment.v1.AgentControlService/Undeclared", codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := func(_ any, ss grpc.ServerStream) error {
				called = true
				id, ok := FromContext(ss.Context())
				if !ok || id.ID != "host-1" {
					return errors.New("stream lost identity")
				}
				// The handler's own check must not need to re-read the peer.
				if _, err := Agent(ss.Context(), "host-1"); err != nil {
					return err
				}
				return nil
			}
			err := testPolicy.StreamInterceptor()(nil, fakeStream{ctx: peerContext(tc.peer)}, &grpc.StreamServerInfo{FullMethod: tc.method}, handler)
			if status.Code(err) != tc.expected || called != (tc.expected == codes.OK) {
				t.Fatalf("%s: err=%v called=%t expected %v", tc.description, err, called, tc.expected)
			}
		})
	}
}

func TestResolvePrefersInterceptorIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		ctx               context.Context
		want              string
		expected          codes.Code
	}{
		{"context", "positive: a resolved identity is reused without re-reading the peer", WithIdentity(context.Background(), Identity{Role: RoleOperator, ID: "alice"}), "alice", codes.OK},
		{"peer", "positive: without an interceptor the peer certificate is read", peerContext("operator/bob"), "bob", codes.OK},
		{"context-wins", "corner: the interceptor's identity takes precedence over a conflicting peer", WithIdentity(peerContext("agent/dc1/host-1"), Identity{Role: RoleOperator, ID: "carol"}), "carol", codes.OK},
		{"none", "negative: neither source yields an identity", context.Background(), "", codes.Unauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Operator(tc.ctx)
			if status.Code(err) != tc.expected || got != tc.want {
				t.Fatalf("%s: got %q %v", tc.description, got, err)
			}
		})
	}
}
