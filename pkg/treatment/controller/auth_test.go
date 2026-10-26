package controller

import (
	"context"
	"strings"
	"testing"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// servedMethods lists every RPC the controller registers, from the generated
// service descriptors, so a new RPC is covered the moment it exists.
func servedMethods() map[string]bool {
	out := map[string]bool{}
	for _, sd := range []grpc.ServiceDesc{pb.AgentControlService_ServiceDesc, pb.OperatorService_ServiceDesc} {
		for _, m := range sd.Methods {
			out["/"+sd.ServiceName+"/"+m.MethodName] = false
		}
		for _, s := range sd.Streams {
			out["/"+sd.ServiceName+"/"+s.StreamName] = true
		}
	}
	return out
}

// go test ./pkg/treatment/controller -run TestRolePolicy
func TestRolePolicy(t *testing.T) {
	policy := RolePolicy()
	served := servedMethods()
	if len(served) == 0 {
		t.Fatal("no served methods found")
	}
	for method := range served {
		role, ok := policy[method]
		if !ok {
			t.Errorf("%s has no (xtcp_treatment.v1.role) declaration; the interceptor refuses it for everyone", method)
			continue
		}
		want := identity.RoleOperator
		if strings.HasPrefix(method, "/xtcp_treatment.v1.AgentControlService/") {
			want = identity.RoleAgent
		}
		if role != want {
			t.Errorf("%s declares %s, expected %s: agents must never reach operator RPCs and vice versa", method, role, want)
		}
	}
	for method := range policy {
		if _, ok := served[method]; !ok {
			t.Errorf("policy names %s, which no served service has", method)
		}
	}
}

// TestEveryMethodAgainstEveryRole is the exhaustive matrix: each served
// method is attempted with an agent certificate, an operator certificate
// and no certificate, and only the declared role may reach the handler.
func TestEveryMethodAgainstEveryRole(t *testing.T) {
	policy := RolePolicy()
	callers := []struct {
		description string
		ctx         context.Context
		role        identity.Role
	}{
		{"agent certificate", testIdentity("agent/dc1/host"), identity.RoleAgent},
		{"operator certificate", testIdentity("operator/alice"), identity.RoleOperator},
		{"no certificate", context.Background(), ""},
	}
	for method, stream := range servedMethods() {
		for _, c := range callers {
			t.Run(method+"/"+c.description, func(t *testing.T) {
				expected := codes.PermissionDenied
				if c.role == "" {
					expected = codes.Unauthenticated
				} else if policy[method] == c.role {
					expected = codes.OK
				}
				reached := false
				var err error
				if stream {
					err = policy.StreamInterceptor()(nil, &desiredTestStream{ctx: c.ctx}, &grpc.StreamServerInfo{FullMethod: method}, func(any, grpc.ServerStream) error { reached = true; return nil })
				} else {
					// A request with no agent fields so only the role decides.
					_, err = policy.UnaryInterceptor()(c.ctx, struct{}{}, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) { reached = true; return nil, nil })
				}
				if status.Code(err) != expected || reached != (expected == codes.OK) {
					t.Fatalf("%s with %s: err=%v reached=%t expected %v", method, c.description, err, reached, expected)
				}
			})
		}
	}
}

func TestGRPCOptionsCarryInterceptors(t *testing.T) {
	// The options must at least construct a server; the interceptor behaviour
	// itself is covered above and by the identity package's mTLS test.
	s := grpc.NewServer(GRPCOptions()...)
	defer s.Stop()
	pb.RegisterAgentControlServiceServer(s, &Server{})
	pb.RegisterOperatorServiceServer(s, &Server{})
	if len(s.GetServiceInfo()) != 2 {
		t.Fatalf("services registered: %d", len(s.GetServiceInfo()))
	}
}
