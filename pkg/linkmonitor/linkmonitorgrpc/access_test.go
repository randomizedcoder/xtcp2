package linkmonitorgrpc

import (
	"testing"

	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

func TestServiceAccessClasses(t *testing.T) {
	methods := listenerauth.ServiceMethods(Descriptor())
	tests := []struct {
		category, description, method string
		expected                      xtcp_auth.Access
	}{
		{"positive", "GetStatus only reads", pb.LinkMonitorService_GetStatus_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"positive", "ListInterfaces only reads", pb.LinkMonitorService_ListInterfaces_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"positive", "ListCollectors only reads", pb.LinkMonitorService_ListCollectors_FullMethodName, xtcp_auth.Access_ACCESS_READ},
		{"positive", "Rebaseline changes the baseline", pb.LinkMonitorService_Rebaseline_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"positive", "Resync drives work", pb.LinkMonitorService_Resync_FullMethodName, xtcp_auth.Access_ACCESS_ADMIN},
		{"negative", "a reserved, unimplemented method is absent", "/linkmonitor.v1.LinkMonitorService/AcknowledgeErrors", xtcp_auth.Access_ACCESS_UNSPECIFIED},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := methods[tc.method]; got != tc.expected {
				t.Fatalf("access = %s", got)
			}
		})
	}
	sd := Descriptor()
	for i := range sd.Methods().Len() {
		name := "/" + string(sd.FullName()) + "/" + string(sd.Methods().Get(i).Name())
		if methods[name] == xtcp_auth.Access_ACCESS_UNSPECIFIED {
			t.Errorf("%s has no explicit (xtcp_auth.v1.access) option", name)
		}
	}
	if len(methods) != sd.Methods().Len() {
		t.Fatalf("methods = %d, descriptor methods = %d", len(methods), sd.Methods().Len())
	}
}
