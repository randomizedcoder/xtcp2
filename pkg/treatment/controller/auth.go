package controller

import (
	"fmt"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// serviceFiles are the proto files whose services the controller serves.
var serviceFiles = []protoreflect.FileDescriptor{pb.File_xtcp_treatment_v1_agent_proto, pb.File_xtcp_treatment_v1_operator_proto}

// RolePolicy derives the per-RPC authorization table from the
// (xtcp_treatment.v1.role) option on every method of the served services.
// A method declared without a role (or ROLE_UNSPECIFIED) is left out of the
// policy, and the interceptor then refuses it for everyone; TestRolePolicy
// fails the build's test run if that ever happens, so the declaration is
// the single place a new RPC's audience is decided.
func RolePolicy() identity.Policy {
	p := identity.Policy{}
	for _, f := range serviceFiles {
		services := f.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			methods := sd.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				name := fmt.Sprintf("/%s/%s", sd.FullName(), m.Name())
				role, ok := proto.GetExtension(m.Options(), pb.E_Role).(pb.Role)
				if !ok {
					continue
				}
				switch role {
				case pb.Role_ROLE_AGENT:
					p[name] = identity.RoleAgent
				case pb.Role_ROLE_OPERATOR:
					p[name] = identity.RoleOperator
				case pb.Role_ROLE_UNSPECIFIED:
					// Left out of the policy, so the interceptors deny the method
					// and the coverage test names it.
				}
			}
		}
	}
	return p
}

// GRPCOptions are the server options every controller listener uses: the
// authorization interceptors over RolePolicy and the message bounds. The
// caller adds transport credentials; without mTLS the interceptors refuse
// every call as unauthenticated.
func GRPCOptions() []grpc.ServerOption {
	p := RolePolicy()
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(p.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(p.StreamInterceptor()),
		grpc.MaxRecvMsgSize(treatment.MaxStateBytes),
		grpc.MaxSendMsgSize(treatment.MaxStateBytes),
		grpc.MaxConcurrentStreams(8),
	}
}
