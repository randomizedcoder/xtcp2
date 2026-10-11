package xtcp

import (
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// grpcMethodAccess is the access table for every service the daemon
// registers. ConfigService declares its classes in the proto; it is read once
// here at startup.
func grpcMethodAccess() listenerauth.Methods {
	config := xtcp_config.File_xtcp_config_v1_xtcp_config_proto.Services().ByName("ConfigService")
	return listenerauth.StandardMethods().
		Merge(listenerauth.ServiceMethods(config)).
		Merge(listenerauth.ServiceMethods(linkmonitorgrpc.Descriptor())).
		Merge(listenerauth.Methods{
			// xtcp_flat_record.proto doubles as ClickHouse's format schema and
			// must not import xtcp_auth, so its classes are declared here.
			// FlatRecords only observes polls; PollFlatRecords drives them.
			xtcp_flat_record.XTCPFlatRecordService_FlatRecords_FullMethodName:     xtcp_auth.Access_ACCESS_READ,
			xtcp_flat_record.XTCPFlatRecordService_PollFlatRecords_FullMethodName: xtcp_auth.Access_ACCESS_ADMIN,
		})
}
