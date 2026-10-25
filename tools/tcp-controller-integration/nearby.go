package main

import (
	"encoding/json"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/protobuf/proto"
)

func nearbySettings() *pb.TCPSettings {
	return &pb.TCPSettings{
		CongestionControl: proto.String("cubic"), RtoMinMs: proto.Uint32(5),
		QuickAck: proto.Bool(true), InitialCongestionWindow: proto.Uint32(100),
		InitialReceiveWindow: proto.Uint32(100),
	}
}

// Check the kernel's installed metrics, not just the controller's desired state.
func assertNearbyRoute(host, destination string) {
	raw := status(host).Routes
	var routes []struct {
		Destination string `json:"dst"`
		Metrics     []struct {
			Congestion string `json:"congestion"`
			RtoMin     uint32 `json:"rto_min"`
			QuickAck   uint32 `json:"quickack"`
			InitCwnd   uint32 `json:"initcwnd"`
			InitRwnd   uint32 `json:"initrwnd"`
		} `json:"metrics"`
	}
	check(json.Unmarshal([]byte(raw), &routes))
	for _, r := range routes {
		if r.Destination != destination && r.Destination != destination+"/32" {
			continue
		}
		require(len(r.Metrics) == 1, "%s missing route metrics: %s", host, raw)
		m := r.Metrics[0]
		require(m.Congestion == "cubic" && m.RtoMin == 5 && m.QuickAck == 1 && m.InitCwnd == 100 && m.InitRwnd == 100, "%s incorrect nearby route: %s", host, raw)
		write(evidenceDir+"/"+host+"-nearby-route.json", []byte(raw))
		return
	}
	panic("missing nearby route on " + host)
}
