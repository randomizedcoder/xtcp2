package xtcp

import (
	"testing"

	linkmonitorpb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// TestGRPCMethodAccessComplete registers every service the daemon registers
// and requires an explicit access class for each method. A new RPC without
// an (xtcp_auth.v1.access) option, or a new service missing from
// grpcMethodAccess, fails here instead of silently becoming admin-only.
func TestGRPCMethodAccessComplete(t *testing.T) {
	s := grpc.NewServer()
	reflection.Register(s)
	xtcp_flat_record.RegisterXTCPFlatRecordServiceServer(s, xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer{})
	xtcp_config.RegisterConfigServiceServer(s, xtcp_config.UnimplementedConfigServiceServer{})
	linkmonitorpb.RegisterLinkMonitorServiceServer(s, linkmonitorpb.UnimplementedLinkMonitorServiceServer{})
	healthpb.RegisterHealthServer(s, grpchealth.NewServer())

	table := grpcMethodAccess()
	for service, info := range s.GetServiceInfo() {
		for _, m := range info.Methods {
			name := "/" + service + "/" + m.Name
			if table[name] == xtcp_auth.Access_ACCESS_UNSPECIFIED {
				t.Errorf("%s has no explicit access class", name)
			}
		}
	}
}

func TestGRPCMethodAccess(t *testing.T) {
	table := grpcMethodAccess()
	tests := []struct {
		category    string
		description string
		method      string
		expected    xtcp_auth.Access
	}{
		{"positive", "reading the redacted config is READ", xtcp_config.ConfigService_Get_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"positive", "observing the record stream is READ", xtcp_flat_record.XTCPFlatRecordService_FlatRecords_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"negative", "replacing the config is ADMIN", xtcp_config.ConfigService_Set_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"negative", "driving polls from a client stream is ADMIN", xtcp_flat_record.XTCPFlatRecordService_PollFlatRecords_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"boundary", "a single on-demand poll is still ADMIN", xtcp_config.ConfigService_TriggerPoll_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"positive", "link monitor status is READ", linkmonitorpb.LinkMonitorService_GetStatus_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"negative", "a link monitor rebaseline is ADMIN", linkmonitorpb.LinkMonitorService_Rebaseline_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"corner", "gRPC health probes are READ", "/grpc.health.v1.Health/Check", xtcp_auth.Access_ACCESS_READ},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := table[tc.method]; got != tc.expected {
				t.Fatalf("access = %s, want %s", got, tc.expected)
			}
		})
	}
}
